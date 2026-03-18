const http = require("http");
const url = require("url");

const PORT = process.env.PORT || 8082;

const edges = [
  { name: "edge_a", region: "US", baseUrl: process.env.EDGE_A_URL || "http://edge_a:8081" },
  { name: "edge_b", region: "EU", baseUrl: process.env.EDGE_B_URL || "http://edge_b:8081" },
];

function chooseEdge(clientRegion) {
  if (!clientRegion) {
    return edges[0];
  }
  const exact = edges.find((e) => e.region.toUpperCase() === clientRegion.toUpperCase());
  if (exact) return exact;
  return edges[0];
}

const server = http.createServer((req, res) => {
  const parsed = url.parse(req.url, true);

  if (parsed.pathname === "/health") {
    res.writeHead(200, { "Content-Type": "text/plain" });
    res.end("ok");
    return;
  }

  if (!parsed.pathname.startsWith("/assets/")) {
    res.writeHead(404, { "Content-Type": "application/json" });
    res.end(JSON.stringify({ error: "not found" }));
    return;
  }

  const region = parsed.query.region || "US";
  const edge = chooseEdge(region);

  const targetPath = parsed.pathname;
  const edgeUrl = new url.URL(edge.baseUrl + targetPath);

  const options = {
    method: req.method,
    headers: req.headers,
    hostname: edgeUrl.hostname,
    port: edgeUrl.port,
    path: edgeUrl.pathname + (edgeUrl.search || ""),
  };

  const proxyReq = http.request(options, (proxyRes) => {
    res.writeHead(proxyRes.statusCode || 500, {
      ...proxyRes.headers,
      "X-Routed-Edge": edge.name,
      "X-Routed-Region": region,
    });
    proxyRes.pipe(res);
  });

  proxyReq.on("error", (err) => {
    console.error("router proxy error:", err);
    res.writeHead(502, { "Content-Type": "application/json" });
    res.end(JSON.stringify({ error: "edge unavailable" }));
  });

  req.pipe(proxyReq);
});

server.listen(PORT, () => {
  console.log(`Router listening on ${PORT}`);
});

