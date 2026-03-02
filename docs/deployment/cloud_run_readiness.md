# Cloud Run Readiness Guide

**Scope:** Test and demo deployment of `accounting-agent` to Google Cloud Run, with PostgreSQL hosted on a Google Cloud VM (Compute Engine).

**Status at the time of writing:** MVP complete, 70 tests passing, no Dockerfile, no graceful shutdown.

---

## What Is Already Cloud Run-Compatible

The following require **no code changes** before deploying:

| Aspect | Evidence |
|---|---|
| Port binding | `SERVER_PORT` env var, defaults to `8080` — matches Cloud Run convention |
| Configuration | All config via env vars; `godotenv.Load()` silently no-ops when `.env` is absent |
| Static files | Embedded in binary at build time (`//go:embed static`) — no filesystem dependency |
| Generated templates | `*_templ.go` files committed to repo — no `templ generate` needed in Docker |
| Compiled CSS | `web/static/css/app.css` committed — no `tailwindcss` needed in Docker |
| Health endpoint | `GET /api/health` — public, no auth, suitable for Cloud Run liveness probe |
| Database driver | `pgxpool` (pgx v5) — designed for cloud, manages connections correctly |

---

## Required Changes

### Change 1 — Graceful Shutdown in `cmd/server/main.go`

**Why:** Cloud Run sends `SIGTERM` when scaling down or redeploying. The current code blocks on `http.ListenAndServe()` and never handles `SIGTERM`, so in-flight requests are dropped immediately and the database pool is never cleanly closed.

**File:** `cmd/server/main.go`

Replace the current 63-line file with the following. The only structural changes are: wrap in `http.Server{}`, run `ListenAndServe` in a goroutine, listen for `SIGTERM`/`SIGINT`, and call `Shutdown` with a 10-second timeout.

```go
package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	webAdapter "accounting-agent/internal/adapters/web"
	"accounting-agent/internal/ai"
	"accounting-agent/internal/app"
	"accounting-agent/internal/core"
	"accounting-agent/internal/db"

	"github.com/joho/godotenv"
)

func main() {
	_ = godotenv.Load()

	ctx := context.Background()
	pool, err := db.NewPool(ctx)
	if err != nil {
		log.Fatalf("database: %v", err)
	}
	defer pool.Close()

	docService := core.NewDocumentService(pool)
	ledger := core.NewLedger(pool, docService)
	ruleEngine := core.NewRuleEngine(pool)
	orderService := core.NewOrderService(pool, ruleEngine)
	inventoryService := core.NewInventoryService(pool, ruleEngine)
	reportingService := core.NewReportingService(pool)
	userService := core.NewUserService(pool)
	vendorService := core.NewVendorService(pool)
	purchaseOrderService := core.NewPurchaseOrderService(pool)

	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		log.Println("Warning: OPENAI_API_KEY is not set")
	}
	agent := ai.NewAgent(apiKey)

	svc := app.NewAppService(pool, ledger, docService, orderService, inventoryService,
		reportingService, userService, vendorService, purchaseOrderService, agent)

	jwtSecret := os.Getenv("JWT_SECRET")
	if jwtSecret == "" {
		log.Fatalf("JWT_SECRET is not set — refusing to start. Set a strong random value in .env")
	}

	port := os.Getenv("SERVER_PORT")
	if port == "" {
		port = "8080"
	}

	allowedOrigins := os.Getenv("ALLOWED_ORIGINS")
	handler := webAdapter.NewHandler(svc, allowedOrigins, jwtSecret)

	srv := &http.Server{
		Addr:              ":" + port,
		Handler:           handler,
		ReadHeaderTimeout: 15 * time.Second,
	}

	// Start server in a background goroutine.
	go func() {
		log.Printf("server starting on :%s", port)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("server: %v", err)
		}
	}()

	// Block until Cloud Run (or operator) sends SIGTERM or SIGINT.
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGTERM, syscall.SIGINT)
	<-quit

	log.Println("server: shutdown signal received, draining connections...")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Printf("server: forced shutdown after timeout: %v", err)
	}
	log.Println("server: stopped cleanly")
}
```

---

### Change 2 — Connection Pool Tuning in `internal/db/db.go`

**Why:** The current pool uses pgx defaults (up to 16 connections per instance). Cloud Run can spawn multiple instances concurrently. At 10 instances × 16 connections = 160 connections, which will exhaust PostgreSQL's `max_connections` on a small VM. Capping at 5 connections per instance keeps total load predictable.

**File:** `internal/db/db.go`

Add import `"time"` and set explicit pool limits after `ParseConfig`:

```go
package db

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func NewPool(ctx context.Context) (*pgxpool.Pool, error) {
	connStr := os.Getenv("DATABASE_URL")
	if connStr == "" {
		return nil, fmt.Errorf("DATABASE_URL environment variable not set")
	}

	config, err := pgxpool.ParseConfig(connStr)
	if err != nil {
		return nil, fmt.Errorf("unable to parse DATABASE_URL: %w", err)
	}

	// Limit connections per instance to avoid exhausting PostgreSQL's
	// max_connections when Cloud Run scales out.
	config.MaxConns = 5
	config.MinConns = 1
	config.MaxConnLifetime = 30 * time.Minute
	config.MaxConnIdleTime = 5 * time.Minute

	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return nil, fmt.Errorf("unable to create connection pool: %w", err)
	}

	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("unable to ping database: %w", err)
	}

	return pool, nil
}
```

> **PostgreSQL VM side:** Set `max_connections = 100` in `postgresql.conf` (default is often 100 already). With 5 connections per Cloud Run instance, this supports up to 20 concurrent instances safely, which is more than enough for test/demo traffic.

---

### Change 3 — Add a `Dockerfile`

**Why:** No Dockerfile exists in the repository. Cloud Run can auto-build with buildpacks, but a custom Dockerfile gives you a reproducible, minimal image and avoids buildpack surprises.

**Key decisions baked in:**
- Multi-stage build: Go compiler only in the build stage, not in the final image.
- `CGO_ENABLED=0` — produces a fully static binary that runs in a scratch-like environment.
- `ca-certificates` — required for outbound TLS to OpenAI API and PostgreSQL with `sslmode=require`.
- `tzdata` — avoids timezone-related panics if any date formatting code is added later.
- No `templ generate` or `tailwindcss` steps — generated files are already committed to the repo.

**File:** `Dockerfile` (project root)

```dockerfile
# ── Build stage ───────────────────────────────────────────────────────────────
FROM golang:1.25-alpine AS builder

WORKDIR /app

# Download dependencies first so this layer is cached on subsequent builds
# when only source files change.
COPY go.mod go.sum ./
RUN go mod download

# Copy all source. The *_templ.go generated files and web/static/css/app.css
# are committed to the repo, so no templ or tailwindcss tooling is needed here.
COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o server ./cmd/server

# ── Runtime stage ─────────────────────────────────────────────────────────────
FROM alpine:3.21

# ca-certificates: required for TLS connections to OpenAI API and PostgreSQL.
# tzdata: avoids timezone panics in date formatting.
RUN apk add --no-cache ca-certificates tzdata

WORKDIR /app
COPY --from=builder /app/server ./server

# Cloud Run injects PORT env var. The app reads SERVER_PORT and defaults to
# 8080 if unset — matching Cloud Run's default expectation.
EXPOSE 8080

CMD ["./server"]
```

**Build and test locally before pushing:**

```bash
docker build -t accounting-agent:local .
docker run --rm -p 8080:8080 \
  -e DATABASE_URL="postgres://user:pass@host:5432/appdb" \
  -e OPENAI_API_KEY="sk-..." \
  -e JWT_SECRET="some-random-secret" \
  accounting-agent:local
```

---

## Infrastructure Setup on GCP

### Step 1 — GCP Project Prerequisites

```bash
# Set your project ID.
export PROJECT_ID="your-gcp-project-id"
export REGION="asia-south1"   # Mumbai — adjust to nearest region

gcloud config set project $PROJECT_ID

# Enable required APIs.
gcloud services enable \
  run.googleapis.com \
  artifactregistry.googleapis.com \
  secretmanager.googleapis.com \
  compute.googleapis.com \
  vpcaccess.googleapis.com
```

---

### Step 2 — PostgreSQL VM Setup (Compute Engine)

If the VM does not yet exist, create it:

```bash
gcloud compute instances create accounting-db \
  --zone="${REGION}-a" \
  --machine-type="e2-medium" \
  --image-family="debian-12" \
  --image-project="debian-cloud" \
  --boot-disk-size="20GB" \
  --no-address   # No external IP — internal VPC only
```

SSH into the VM and install PostgreSQL:

```bash
gcloud compute ssh accounting-db --zone="${REGION}-a"

# On the VM:
sudo apt update && sudo apt install -y postgresql postgresql-contrib

# Create database and user.
sudo -u postgres psql <<'SQL'
CREATE USER acct_app WITH PASSWORD 'choose-a-strong-password';
CREATE DATABASE accounting OWNER acct_app;
GRANT ALL PRIVILEGES ON DATABASE accounting TO acct_app;
SQL

# Allow connections from VPC (not just localhost).
# Edit /etc/postgresql/15/main/postgresql.conf:
#   listen_addresses = '*'
# Edit /etc/postgresql/15/main/pg_hba.conf — add:
#   host  accounting  acct_app  10.0.0.0/8  scram-sha-256

sudo systemctl restart postgresql
```

Note the VM's internal IP:

```bash
gcloud compute instances describe accounting-db \
  --zone="${REGION}-a" \
  --format="get(networkInterfaces[0].networkIP)"
# Example output: 10.128.0.5
```

Run migrations from your local machine (one-time only, while connected to VPN or via Cloud Shell):

```bash
DATABASE_URL="postgres://acct_app:password@10.128.0.5:5432/accounting" \
  go run ./cmd/verify-db
```

---

### Step 3 — VPC Connector (Cloud Run ↔ VM Private Networking)

Cloud Run runs outside your VPC by default. A Serverless VPC Access Connector bridges it to the VM's private IP.

```bash
# Create the connector in the same region as Cloud Run.
gcloud compute networks vpc-access connectors create accounting-connector \
  --region="${REGION}" \
  --network="default" \
  --range="10.8.0.0/28"   # A /28 CIDR not already in use on your VPC

# Verify.
gcloud compute networks vpc-access connectors describe accounting-connector \
  --region="${REGION}"
# State should be: READY
```

> **Firewall rule:** The VM's PostgreSQL port (5432) must be reachable from the VPC connector's CIDR range (`10.8.0.0/28`). Check with:
> ```bash
> gcloud compute firewall-rules list --filter="direction=INGRESS"
> ```
> If no rule allows port 5432 from `10.8.0.0/28`, add one:
> ```bash
> gcloud compute firewall-rules create allow-pg-from-connector \
>   --direction=INGRESS \
>   --action=ALLOW \
>   --rules=tcp:5432 \
>   --source-ranges="10.8.0.0/28" \
>   --target-tags="accounting-db"
>
> # Tag the VM.
> gcloud compute instances add-tags accounting-db \
>   --zone="${REGION}-a" \
>   --tags="accounting-db"
> ```

---

### Step 4 — Store Secrets in Secret Manager

Never pass secret values as plain environment variables in Cloud Run configuration. Use Secret Manager.

```bash
# DATABASE_URL — use the VM's internal IP, not localhost.
echo -n "postgres://acct_app:password@10.128.0.5:5432/accounting?sslmode=disable" \
  | gcloud secrets create DATABASE_URL --data-file=-

# OPENAI_API_KEY
echo -n "sk-proj-..." \
  | gcloud secrets create OPENAI_API_KEY --data-file=-

# JWT_SECRET — generate a strong random value.
openssl rand -base64 48 \
  | gcloud secrets create JWT_SECRET --data-file=-
```

> **sslmode=disable** is used here because the connection is within your private VPC — no need for TLS between Cloud Run and the VM on a private network. If you prefer TLS anyway, set `sslmode=require` and configure PostgreSQL's SSL certificate on the VM.

Grant the Cloud Run service account access to these secrets:

```bash
# Find the default Cloud Run service account (or create a dedicated one).
export SA="$(gcloud projects describe $PROJECT_ID --format='value(projectNumber)')-compute@developer.gserviceaccount.com"

for SECRET in DATABASE_URL OPENAI_API_KEY JWT_SECRET; do
  gcloud secrets add-iam-policy-binding $SECRET \
    --member="serviceAccount:${SA}" \
    --role="roles/secretmanager.secretAccessor"
done
```

---

### Step 5 — Artifact Registry (Container Images)

```bash
# Create a Docker repository in Artifact Registry.
gcloud artifacts repositories create accounting-agent \
  --repository-format=docker \
  --location="${REGION}" \
  --description="Accounting Agent container images"

# Authenticate Docker to push.
gcloud auth configure-docker "${REGION}-docker.pkg.dev"

# Build and push.
export IMAGE="${REGION}-docker.pkg.dev/${PROJECT_ID}/accounting-agent/server:latest"

docker build -t $IMAGE .
docker push $IMAGE
```

---

### Step 6 — Deploy to Cloud Run

```bash
gcloud run deploy accounting-agent \
  --image="${IMAGE}" \
  --region="${REGION}" \
  --platform="managed" \
  --port=8080 \
  --min-instances=0 \
  --max-instances=3 \
  --memory=512Mi \
  --cpu=1 \
  --timeout=60 \
  --vpc-connector="accounting-connector" \
  --vpc-egress="private-ranges-only" \
  --set-secrets="DATABASE_URL=DATABASE_URL:latest,OPENAI_API_KEY=OPENAI_API_KEY:latest,JWT_SECRET=JWT_SECRET:latest" \
  --set-env-vars="COMPANY_CODE=1000" \
  --allow-unauthenticated
```

> **`--allow-unauthenticated`** lets the browser reach the app's own login page. The application enforces its own JWT authentication — Cloud Run IAM auth is not needed for demo use. Remove this flag if you want to restrict access to specific Google accounts.

> **`COMPANY_CODE=1000`** is required because the database has only one company; without it, the app will error if it finds the company by code rather than auto-detecting. Set the actual code matching your seed data.

---

### Step 7 — Configure Cloud Run Health Check

```bash
gcloud run services update accounting-agent \
  --region="${REGION}" \
  --set-custom-audiences="" \
  --startup-probe-initial-delay-seconds=5 \
  --startup-probe-period-seconds=5 \
  --startup-probe-failure-threshold=5 \
  --startup-probe-type=http \
  --startup-probe-path=/api/health
```

---

## Post-Deployment Verification

After deploying, retrieve the service URL:

```bash
gcloud run services describe accounting-agent \
  --region="${REGION}" \
  --format="value(status.url)"
# Example: https://accounting-agent-abc123-el.a.run.app
```

Run the following checks:

```bash
export SERVICE_URL="https://accounting-agent-abc123-el.a.run.app"

# 1. Health check — should return {"status":"ok","company":"..."}
curl -s "${SERVICE_URL}/api/health" | python3 -m json.tool

# 2. Login page — should return HTTP 200 HTML
curl -s -o /dev/null -w "%{http_code}" "${SERVICE_URL}/login"

# 3. Protected route without auth — should redirect to /login (HTTP 302)
curl -s -o /dev/null -w "%{http_code}" "${SERVICE_URL}/dashboard"
```

Login via the browser at `$SERVICE_URL` with credentials `admin / Admin@1234` (the seeded admin user). Verify:
- Dashboard loads and shows company name
- Trial balance page loads data
- AI Chat (home screen) responds to a prompt
- P&L and Balance Sheet pages render

---

## Continuous Deployment (Optional, After Initial Setup)

Add a Cloud Build trigger to rebuild and redeploy on every push to `main`:

```bash
gcloud builds triggers create github \
  --repo-name="accounting-agent" \
  --repo-owner="<your-github-org>" \
  --branch-pattern="^main$" \
  --build-config="cloudbuild.yaml"
```

`cloudbuild.yaml` (project root):

```yaml
steps:
  - name: "gcr.io/cloud-builders/docker"
    args: ["build", "-t", "$_IMAGE", "."]
  - name: "gcr.io/cloud-builders/docker"
    args: ["push", "$_IMAGE"]
  - name: "gcr.io/google.com/cloudsdktool/cloud-sdk"
    args:
      - gcloud
      - run
      - deploy
      - accounting-agent
      - --image=$_IMAGE
      - --region=$_REGION
      - --platform=managed
      - --quiet

substitutions:
  _REGION: asia-south1
  _IMAGE: asia-south1-docker.pkg.dev/$PROJECT_ID/accounting-agent/server:$SHORT_SHA

options:
  logging: CLOUD_LOGGING_ONLY
```

---

## Summary of All Changes Required

| # | Type | File / Location | Priority |
|---|---|---|---|
| 1 | Code change | `cmd/server/main.go` — graceful shutdown with SIGTERM handling | **Critical** |
| 2 | Code change | `internal/db/db.go` — pool MaxConns=5, idle/lifetime limits | High |
| 3 | New file | `Dockerfile` — multi-stage Go build | **Critical** |
| 4 | GCP infra | Secret Manager — `DATABASE_URL`, `OPENAI_API_KEY`, `JWT_SECRET` | **Critical** |
| 5 | GCP infra | VPC Connector — Cloud Run → VM private network connectivity | **Critical** |
| 6 | GCP infra | Firewall rule — allow port 5432 from VPC connector CIDR | **Critical** |
| 7 | GCP infra | Artifact Registry repository + Docker push | Required |
| 8 | GCP infra | `gcloud run deploy` with secrets, VPC connector, env vars | Required |
| 9 | Optional | `cloudbuild.yaml` — CI/CD trigger for auto-redeploy on push to main | Optional |

Items 1–3 are code/file changes that must be committed to the repository before building the Docker image. Items 4–8 are one-time GCP infrastructure steps. None of the changes affect the REPL or CLI entrypoints (`cmd/app/main.go`).
