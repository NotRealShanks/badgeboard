# Deployment Guide

`badgeboard` supports two primary deployment topologies:
1. **Dynamic Web Application on Google Cloud Run** (full live scraping backend with SSE streaming, access control, and in-memory run tracking).
2. **Static Interactive Demo on GitHub Pages** (standalone static ledger running in client-side DEMO mode).

---

## 1. Google Cloud Run (Live Backend Service)

The service runs as a stateless, non-root distroless container with in-memory run tracking, strict security controls, and zero disk writes.

### Cloud Run Architecture & Settings
- **`max-instances=1`**: Ensures in-memory active runs and SSE connection channels remain consolidated on a single instance without requiring distributed synchronization.
- **`session-affinity`**: Pins organizer client requests to the active container.
- **`timeout=3600s`**: Accommodates long streaming evaluation sessions with respectful ratelimiting.
- **`min-instances=0`**: Scales to zero when idle to eliminate hosting costs.
- **Memory**: 256Mi - 512Mi.
- **Non-root Distroless Image**: Runs under UID `65532:65532` (`gcr.io/distroless/static-debian12:nonroot`).
- **No Persistent Volumes**: Strictly in-memory data store with automatic TTL janitor purging.

### Production Deploy Command

```bash
# 1. Build and push container to Google Artifact Registry / GCR
gcloud builds submit --tag gcr.io/$PROJECT_ID/badgeboard:latest

# 2. Deploy to Cloud Run with hardened hosted flags and access code
gcloud run deploy badgeboard \
  --image gcr.io/$PROJECT_ID/badgeboard:latest \
  --region us-central1 \
  --platform managed \
  --allow-unauthenticated \
  --port 8080 \
  --memory 512Mi \
  --cpu 1 \
  --min-instances 0 \
  --max-instances 1 \
  --timeout 3600 \
  --session-affinity \
  --set-env-vars BADGEBOARD_ACCESS_CODE="your-organizer-secret-code"
```

> **Note on Demo-Only Hosted Mode:**
> If you wish to host a live container for demonstration without allowing public live scraping or requiring an access code, launch with `--demo-only`:
> ```bash
> /badgeboard serve --hosted --demo-only
> ```
> In `--demo-only` mode, the server serves the interactive demo and disables all live `/api/runs` scraping endpoints.

---

## 2. Copy-Pasteable Verification Procedure

After deploying to Cloud Run, execute this step-by-step verification checklist using `curl`:

### Step 1: Verify Health Probe
```bash
export SERVICE_URL="https://your-service-url.run.app"
export ACCESS_CODE="your-organizer-secret-code"

curl -s -i "$SERVICE_URL/healthz"
```
**Expected Output:**
`HTTP/2 200 OK`
```json
{"status":"ok","time":"..."}
```

### Step 2: Verify Built-in Demo Endpoint
```bash
curl -s -i "$SERVICE_URL/api/demo"
```
**Expected Output:**
`HTTP/2 200 OK`
Contains synthetic demo snapshot with 11 learners, distribution buckets, and unreachable profiles.

### Step 3: Verify Access Code Protection
```bash
# Test unauthorized POST (should fail with 401)
curl -s -o /dev/null -w "%{http_code}\n" -X POST "$SERVICE_URL/api/runs" -H "Content-Type: application/json" -d '{"urls":[]}'
# => 401
```

### Step 4: Run Real Verification using Synthetic Roster
Upload `testdata/sample_roster.csv` using the access code:
```bash
RUN_RES=$(curl -s -X POST \
  -H "Authorization: Bearer $ACCESS_CODE" \
  -F "file=@testdata/sample_roster.csv" \
  -F "goal=10" \
  "$SERVICE_URL/api/runs")

echo "$RUN_RES"
# Extract run ID
RUN_ID=$(echo "$RUN_RES" | grep -o '"run_id":"[^"]*' | cut -d'"' -f4)
echo "Active Run ID: $RUN_ID"
```

### Step 5: Stream Live SSE Progress & Egress IP Response
```bash
curl -N -H "Last-Event-ID: 0" "$SERVICE_URL/api/runs/$RUN_ID/events"
```

#### Egress IP Assessment Matrix:
| Egress Behavior | Upstream HTTP Status | UI & API Headline Result |
| :--- | :--- | :--- |
| **Clean Egress** | `200 OK` from `skills.google` | Learner names and badges appear live in roster order with normal distribution. |
| **Throttled Egress** | `429 Too Many Requests` | Engine pauses automatically, respects `Retry-After`, and resumes. |
| **Cloud IP Blocked / Anti-Bot** | `403 Forbidden` / Cloudflare Challenge | Profiles are categorized under **"Couldn't be checked"** with status `network_error`. The summary headline explicitly states: <br>**"The server could not reach Google Skills from here."** <br>*Learners are never falsely displayed as having 0 badges.* |

---

## 3. GitHub Pages (Static Interactive Demo)

The published site on GitHub Pages runs as a pure client-side static application from the `docs/` folder, generated deterministically from `internal/web/assets`.

### One-Time Repository Configuration:
1. Navigate to your GitHub repository: **Settings > Pages**.
2. Under **Build and deployment > Source**, select **GitHub Actions**.
3. Pushes to `main` touching `internal/web/assets/**` or `docs/**` will automatically trigger `.github/workflows/deploy-pages.yml`, run `make pages`, and publish the verified demo site.

---

## 4. Local Execution

```bash
# Build binary
make build

# Start local server with custom access code
./bin/badgeboard serve --addr 127.0.0.1:8080 --access-code "mysecret"
```
Visit `http://127.0.0.1:8080` in your web browser.
