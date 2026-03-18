import React, { useEffect, useState } from 'react';
import './App.css';

type Asset = {
  id: string;
  filename: string;
};

type EdgeStats = {
  edge_name: string;
  edge_region: string;
  hits: number;
  misses: number;
  hit_ratio: number;
  items: number;
};

const ORIGIN_URL = process.env.REACT_APP_ORIGIN_URL || 'http://localhost:8080';
const ROUTER_URL = process.env.REACT_APP_ROUTER_URL || 'http://localhost:8082';
const EDGE_A_URL = process.env.REACT_APP_EDGE_A_URL || 'http://localhost:8081';
const EDGE_B_URL = process.env.REACT_APP_EDGE_B_URL || 'http://localhost:8083';

function App() {
  const [assets, setAssets] = useState<Asset[]>([]);
  const [uploading, setUploading] = useState(false);
  const [file, setFile] = useState<File | null>(null);
  const [region, setRegion] = useState<'US' | 'EU'>('US');
  const [lastRequestInfo, setLastRequestInfo] = useState<string>('');
  const [edgeStats, setEdgeStats] = useState<EdgeStats[]>([]);

  const loadAssets = async () => {
    try {
      const res = await fetch(`${ORIGIN_URL}/assets`);
      if (!res.ok) return;
      const data = await res.json();
      setAssets(
        data.map((a: any) => ({
          id: a.id,
          filename: a.filename,
        }))
      );
    } catch {
      // ignore in UI
    }
  };

  const loadStats = async () => {
    try {
      const [a, b] = await Promise.all([
        fetch(`${EDGE_A_URL}/stats`).then((r) => (r.ok ? r.json() : null)),
        fetch(`${EDGE_B_URL}/stats`).then((r) => (r.ok ? r.json() : null)),
      ]);
      const list: EdgeStats[] = [];
      if (a) list.push(a);
      if (b) list.push(b);
      setEdgeStats(list);
    } catch {
      // ignore
    }
  };

  useEffect(() => {
    loadAssets();
    loadStats();
    const id = window.setInterval(loadStats, 3000);
    return () => window.clearInterval(id);
  }, []);

  const handleUpload = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!file) return;
    const form = new FormData();
    form.append('file', file);
    setUploading(true);
    try {
      const res = await fetch(`${ORIGIN_URL}/upload`, {
        method: 'POST',
        body: form,
      });
      if (res.ok) {
        await loadAssets();
      }
    } finally {
      setUploading(false);
      setFile(null);
    }
  };

  const requestViaRouter = async (id: string) => {
    try {
      const res = await fetch(`${ROUTER_URL}/assets/${id}?region=${region}`);
      const body = await res.blob();
      const fromEdge = res.headers.get('X-Routed-Edge') || 'unknown';
      const cacheHeader = res.headers.get('X-Cache') || 'unknown';
      setLastRequestInfo(
        `Region ${region} -> edge ${fromEdge}, cache=${cacheHeader}, status=${res.status}, size=${body.size} bytes`
      );
      await loadStats();
    } catch (err) {
      setLastRequestInfo(`Error requesting asset: ${String(err)}`);
    }
  };

  return (
    <div className="App">
      <h1>CDN Simulator</h1>

      <section>
        <h2>Upload asset to origin</h2>
        <form onSubmit={handleUpload}>
          <input
            type="file"
            onChange={(e) => setFile(e.target.files?.[0] ?? null)}
          />
          <button type="submit" disabled={uploading || !file}>
            {uploading ? 'Uploading...' : 'Upload'}
          </button>
        </form>
      </section>

      <section>
        <h2>Assets</h2>
        {assets.length === 0 && <p>No assets uploaded yet.</p>}
        {assets.length > 0 && (
          <table>
            <thead>
              <tr>
                <th>ID</th>
                <th>Filename</th>
                <th>Request via router</th>
              </tr>
            </thead>
            <tbody>
              {assets.map((a) => (
                <tr key={a.id}>
                  <td>{a.id}</td>
                  <td>{a.filename}</td>
                  <td>
                    <button onClick={() => requestViaRouter(a.id)}>
                      Request as {region}
                    </button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </section>

      <section>
        <h2>Client region</h2>
        <select
          value={region}
          onChange={(e) => setRegion(e.target.value as 'US' | 'EU')}
        >
          <option value="US">US</option>
          <option value="EU">EU</option>
        </select>
        {lastRequestInfo && <p>{lastRequestInfo}</p>}
      </section>

      <section>
        <h2>Edge metrics</h2>
        {edgeStats.length === 0 && <p>No stats available yet.</p>}
        {edgeStats.length > 0 && (
          <table>
            <thead>
              <tr>
                <th>Edge</th>
                <th>Region</th>
                <th>Hits</th>
                <th>Misses</th>
                <th>Hit ratio</th>
                <th>Items</th>
              </tr>
            </thead>
            <tbody>
              {edgeStats.map((s) => (
                <tr key={s.edge_name}>
                  <td>{s.edge_name}</td>
                  <td>{s.edge_region}</td>
                  <td>{s.hits}</td>
                  <td>{s.misses}</td>
                  <td>{s.hit_ratio.toFixed(2)}</td>
                  <td>{s.items}</td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </section>
    </div>
  );
}

export default App;
