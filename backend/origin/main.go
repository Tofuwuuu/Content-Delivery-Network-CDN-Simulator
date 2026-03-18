package main

import (
	"encoding/json"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"github.com/google/uuid"
)

const (
	defaultTTL       = 60 * time.Second
	storageDir       = "data/origin"
	metadataFilename = "metadata.json"
)

type assetMeta struct {
	ID           string        `json:"id"`
	Filename     string        `json:"filename"`
	ContentType  string        `json:"content_type"`
	Path         string        `json:"path"`
	Size         int64         `json:"size"`
	UploadedAt   time.Time     `json:"uploaded_at"`
	TTL          time.Duration `json:"ttl"`
	LastModified time.Time     `json:"last_modified"`
}

type metadataStore struct {
	mu     sync.RWMutex
	assets map[string]assetMeta
}

func newMetadataStore() *metadataStore {
	return &metadataStore{
		assets: make(map[string]assetMeta),
	}
}

func (s *metadataStore) load(path string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	file, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	defer file.Close()

	dec := json.NewDecoder(file)
	return dec.Decode(&s.assets)
}

func (s *metadataStore) save(path string) error {
	s.mu.RLock()
	defer s.mu.RUnlock()

	tmp := path + ".tmp"
	file, err := os.Create(tmp)
	if err != nil {
		return err
	}
	defer file.Close()

	enc := json.NewEncoder(file)
	enc.SetIndent("", "  ")
	if err := enc.Encode(s.assets); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func (s *metadataStore) put(a assetMeta) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.assets[a.ID] = a
}

func (s *metadataStore) get(id string) (assetMeta, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	a, ok := s.assets[id]
	return a, ok
}

func (s *metadataStore) delete(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.assets, id)
}

func (s *metadataStore) all() []assetMeta {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]assetMeta, 0, len(s.assets))
	for _, a := range s.assets {
		out = append(out, a)
	}
	return out
}

type originServer struct {
	store       *metadataStore
	storagePath string
	metaPath    string
}

func newOriginServer() *originServer {
	base := storageDir
	if err := os.MkdirAll(base, 0o755); err != nil {
		log.Fatalf("create storage dir: %v", err)
	}
	metaPath := filepath.Join(base, metadataFilename)

	store := newMetadataStore()
	if err := store.load(metaPath); err != nil {
		log.Printf("failed to load metadata: %v", err)
	}

	return &originServer{
		store:       store,
		storagePath: base,
		metaPath:    metaPath,
	}
}

func (s *originServer) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/upload", s.handleUpload)
	mux.HandleFunc("/assets/", s.handleGetAsset)
	mux.HandleFunc("/assets", s.handleListAssets)
	mux.HandleFunc("/purge/", s.handlePurge)
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	return loggingMiddleware(mux)
}

func (s *originServer) handleUpload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if err := r.ParseMultipartForm(32 << 20); err != nil {
		http.Error(w, "invalid multipart form", http.StatusBadRequest)
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		http.Error(w, "file is required", http.StatusBadRequest)
		return
	}
	defer file.Close()

	id := uuid.NewString()
	filename := header.Filename
	contentType := header.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "application/octet-stream"
	}

	dstPath := filepath.Join(s.storagePath, id)
	size, err := saveMultipartFile(file, dstPath)
	if err != nil {
		log.Printf("save file error: %v", err)
		http.Error(w, "failed to save file", http.StatusInternalServerError)
		return
	}

	now := time.Now().UTC()
	meta := assetMeta{
		ID:           id,
		Filename:     filename,
		ContentType:  contentType,
		Path:         dstPath,
		Size:         size,
		UploadedAt:   now,
		TTL:          defaultTTL,
		LastModified: now,
	}

	s.store.put(meta)
	if err := s.store.save(s.metaPath); err != nil {
		log.Printf("save metadata error: %v", err)
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-cache")
	resp := map[string]any{
		"id":       id,
		"filename": filename,
		"url":      "/assets/" + id,
		"ttl_sec":  int(defaultTTL.Seconds()),
	}
	_ = json.NewEncoder(w).Encode(resp)
}

func saveMultipartFile(src multipart.File, dstPath string) (int64, error) {
	tmpPath := dstPath + ".tmp"
	out, err := os.Create(tmpPath)
	if err != nil {
		return 0, err
	}
	defer out.Close()

	n, err := io.Copy(out, src)
	if err != nil {
		return 0, err
	}

	if err := os.Rename(tmpPath, dstPath); err != nil {
		return 0, err
	}
	return n, nil
}

func (s *originServer) handleGetAsset(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	id := filepath.Base(r.URL.Path)
	meta, ok := s.store.get(id)
	if !ok {
		http.NotFound(w, r)
		return
	}

	info, err := os.Stat(meta.Path)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	lastModified := info.ModTime().UTC()

	w.Header().Set("Content-Type", meta.ContentType)
	w.Header().Set("Cache-Control", "public, max-age="+strconv.Itoa(int(meta.TTL.Seconds())))
	w.Header().Set("Last-Modified", lastModified.Format(http.TimeFormat))
	w.Header().Set("ETag", `"`+meta.ID+`"`)

	http.ServeFile(w, r, meta.Path)
}

func (s *originServer) handleListAssets(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	assets := s.store.all()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(assets)
}

func (s *originServer) handlePurge(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	id := filepath.Base(r.URL.Path)
	meta, ok := s.store.get(id)
	if !ok {
		http.NotFound(w, r)
		return
	}
	_ = os.Remove(meta.Path)
	s.store.delete(id)
	if err := s.store.save(s.metaPath); err != nil {
		log.Printf("save metadata error: %v", err)
	}
	w.WriteHeader(http.StatusNoContent)
}

func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		log.Printf("%s %s from %s took %s", r.Method, r.URL.Path, r.RemoteAddr, time.Since(start))
	})
}

func main() {
	srv := newOriginServer()
	addr := ":8080"
	log.Printf("origin server listening on %s", addr)
	if err := http.ListenAndServe(addr, srv.routes()); err != nil {
		log.Fatalf("server error: %v", err)
	}
}
