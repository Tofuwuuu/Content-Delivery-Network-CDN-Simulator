import requests
import time

ROUTER_URL = "http://localhost:8082"
REGION = "US"


def main(asset_id: str, iterations: int = 5) -> None:
    for i in range(iterations):
        start = time.time()
        resp = requests.get(f"{ROUTER_URL}/assets/{asset_id}", params={"region": REGION})
        elapsed = (time.time() - start) * 1000
        edge = resp.headers.get("X-Routed-Edge", "unknown")
        cache = resp.headers.get("X-Cache", "unknown")
        print(
            f"[{i+1}] {REGION} -> edge={edge} status={resp.status_code} "
            f"cache={cache} time_ms={elapsed:.1f} size={len(resp.content)}"
        )


if __name__ == "__main__":
    import sys

    if len(sys.argv) < 2:
        print("usage: client_region_a.py <asset_id>")
        raise SystemExit(1)
    main(sys.argv[1])

