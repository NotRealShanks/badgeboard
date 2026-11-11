# Deployment Guide

`badgeboard` supports two primary deployment topologies:
1. **Dynamic Web Application on Google Cloud Run** (full live scraping backend with SSE streaming).
2. **Static Interactive Demo on GitHub Pages** (standalone static ledger running in client-side DEMO mode).

---

## 1. Google Cloud Run (Live Backend Service)

The service runs as a stateless, non-root distroless container with in-memory run tracking and strict security controls.

### Quick Deploy (from source)
```bash
gcloud run deploy badgeboard \
  --source . \
  --region us-central1 \
  --platform managed \
  --allow-unauthenticated \
  --port 8080 \
  --memory 512Mi \
  --cpu 1 \
  --min-instances 0 \
  --max-instances 3
```

### Docker Build & Run (Local Container Testing)
```bash
# Build the minimal distroless image
docker build -t badgeboard:latest .

# Run with port forwarding
docker run --rm -p 8080:8080 badgeboard:latest
```

### Environment & Server Flags
In hosted mode (`--hosted`), the server applies:
- `$PORT` environment variable binding (required by Cloud Run).
- Zero disk caching (all student data remains ephemeral in memory only).
- Stricter per-IP rate limiting on `POST /api/runs` to prevent abuse.
- In-memory TTL janitor running every 2 minutes (deletes runs older than 30 minutes).

---

## 2. GitHub Pages (Static Interactive Demo)

The `docs/` directory is structured for zero-configuration GitHub Pages hosting:

1. In GitHub repository settings: **Settings > Pages > Build and deployment**.
2. Select **Source: Deploy from a branch**.
3. Select **Branch: `web-app` (or `main`) / folder: `/docs`**.
4. Save.

### How Standalone Demo Mode Works:
- On load, the frontend sends a probe to `/healthz`.
- When deployed on GitHub Pages (static CDN), the request returns 404.
- The frontend detects static hosting and automatically switches to **DEMO Mode**.
- An honest **"Demo Data"** badge is displayed in the header.
- The UI provides interactive simulation of live streaming, progress distribution, filtering, searching, CSV/JSON/HTML exports, and dev state inspection (`?state=...`).
- Real participant data is never faked or misrepresented.

---

## 3. Local Binary Service
To run locally without Docker:
```bash
# Compile single static binary
go build -o bin/badgeboard ./cmd/badgeboard

# Start web service
./bin/badgeboard serve --addr 127.0.0.1:8080
```
Open [http://127.0.0.1:8080](http://127.0.0.1:8080) in your browser.
