package main

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"
)

const (
	edgeDefaultTTL  = 60 * time.Second
	cacheDir        = "data/edge"
	originBaseURL   = "http://origin:8080"
	cacheHeaderName = "X-Cache"
)

type cacheEntry struct {
	ID          string        `json:"id"`
	Path        string        `json:"path"`
	ContentType string        `json:"content_type"`
	Size        int64         `json:"size"`
	StoredAt    time.Time     `json:"stored_at"`
	TTL         time.Duration `json:"ttl"`
}

type cacheStore struct {
	mu     sync.RWMutex
	items  map[string]cacheEntry
	hits   int64
	misses int64
}

func newCacheStore() *cacheStore {
	return &cacheStore{
		items: make(map[string]cacheEntry),
	}
}

func (c *cacheStore) get(id string) (cacheEntry, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	entry, ok := c.items[id]
	if !ok {
		return cacheEntry{}, false
	}
	if time.Since(entry.StoredAt) > entry.TTL {
		return cacheEntry{}, false
	}
	return entry, true
}

func (c *cacheStore) put(e cacheEntry) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.items[e.ID] = e
}

func (c *cacheStore) purge(id string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if entry, ok := c.items[id]; ok {
		_ = os.Remove(entry.Path)
		delete(c.items, id)
	}
}

func (c *cacheStore) stats() map[string]any {
	c.mu.RLock()
	defer c.mu.RUnlock()
	total := c.hits + c.misses
	ratio := 0.0
	if total > 0 {
		ratio = float64(c.hits) / float64(total)
	}
	return map[string]any{
		"hits":        c.hits,
		"misses":      c.misses,
		"hit_ratio":   ratio,
		"items":       len(c.items),
		"edge_name":   os.Getenv("EDGE_NAME"),
		"edge_region": os.Getenv("REGION"),
	}
}

func (c *cacheStore) recordHit() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.hits++
}

func (c *cacheStore) recordMiss() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.misses++
}

type edgeServer struct {
	cache *cacheStore
	dir   string
}

func newEdgeServer() *edgeServer {
	dir := cacheDir
	if err := os.MkdirAll(dir, 0o755); err != nil {
		log.Fatalf("create cache dir: %v", err)
	}
	return &edgeServer{
		cache: newCacheStore(),
		dir:   dir,
	}
}

func (e *edgeServer) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/assets/", e.handleGetAsset)
	mux.HandleFunc("/purge/", e.handlePurge)
	mux.HandleFunc("/stats", e.handleStats)
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	return loggingMiddleware(mux)
}

func (e *edgeServer) handleGetAsset(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	id := filepath.Base(r.URL.Path)

	if entry, ok := e.cache.get(id); ok {
		e.cache.recordHit()
		w.Header().Set(cacheHeaderName, "HIT")
		http.ServeFile(w, r, entry.Path)
		return
	}

	e.cache.recordMiss()
	originURL := originBaseURL + "/assets/" + id
	resp, err := http.Get(originURL)
	if err != nil {
		log.Printf("fetch from origin error: %v", err)
		http.Error(w, "origin unavailable", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		w.WriteHeader(resp.StatusCode)
		_, _ = io.Copy(w, resp.Body)
		return
	}

	contentType := resp.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "application/octet-stream"
	}

	cachePath := filepath.Join(e.dir, id)
	tmpPath := cachePath + ".tmp"
	out, err := os.Create(tmpPath)
	if err != nil {
		log.Printf("cache write error: %v", err)
		http.Error(w, "cache write error", http.StatusInternalServerError)
		return
	}
	n, err := io.Copy(out, resp.Body)
	_ = out.Close()
	if err != nil {
		log.Printf("cache copy error: %v", err)
		_ = os.Remove(tmpPath)
		http.Error(w, "cache error", http.StatusInternalServerError)
		return
	}
	if err := os.Rename(tmpPath, cachePath); err != nil {
		log.Printf("cache rename error: %v", err)
		http.Error(w, "cache error", http.StatusInternalServerError)
		return
	}

	entry := cacheEntry{
		ID:          id,
		Path:        cachePath,
		ContentType: contentType,
		Size:        n,
		StoredAt:    time.Now().UTC(),
		TTL:         edgeDefaultTTL,
	}
	e.cache.put(entry)

	w.Header().Set("Content-Type", contentType)
	w.Header().Set(cacheHeaderName, "MISS")
	w.Header().Set("Cache-Control", "public, max-age="+strconv.Itoa(int(edgeDefaultTTL.Seconds())))

	file, err := os.Open(cachePath)
	if err != nil {
		http.Error(w, "cache read error", http.StatusInternalServerError)
		return
	}
	defer file.Close()
	_, _ = io.Copy(w, file)
}

func (e *edgeServer) handlePurge(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	id := filepath.Base(r.URL.Path)
	e.cache.purge(id)
	w.WriteHeader(http.StatusNoContent)
}

func (e *edgeServer) handleStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(e.cache.stats())
}

func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		log.Printf("edge %s %s from %s took %s", r.Method, r.URL.Path, r.RemoteAddr, time.Since(start))
	})
}

func main() {
	srv := newEdgeServer()
	addr := ":8081"
	log.Printf("edge server listening on %s", addr)
	if err := http.ListenAndServe(addr, srv.routes()); err != nil {
		log.Fatalf("edge server error: %v", err)
	}
}
