# CDN Simulator Demo

1. Run `docker compose -f deploy/docker-compose.yml up --build`.
2. Open the frontend at `http://localhost:3000`.
3. Upload an asset using the upload form.
4. Use the UI or the client scripts in `scripts/` to request the asset from different regions and observe cache hits/misses and per-edge metrics.

