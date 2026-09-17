#!/usr/bin/env bash
# Runs the full stack: Postgres, the Go API, and the Go+HTMX website.
# Both services run locally and talk to each other.
#
# Usage: ./dev-full-stack.sh
set -euo pipefail

API_PORT="${API_PORT:-8080}"
WEB_PORT="${WEB_PORT:-8100}"
LOCAL_API="http://localhost:$API_PORT"
DB_URL="postgres://lamazon:lamazon@localhost:5433/lamazon?sslmode=disable"
cd "$(dirname "$0")"

# Load environment variables from .env if it exists
if [ -f .env ]; then
  set -a; . ./.env; set +a
fi

# Postgres: reuse the container across runs
if [ -z "$(docker ps -q -f name=^lamazon-pg$)" ]; then
  if [ -n "$(docker ps -aq -f name=^lamazon-pg$)" ]; then
    echo "==> starting existing lamazon-pg"
    docker start lamazon-pg >/dev/null
  else
    echo "==> creating lamazon-pg"
    docker run -d --name lamazon-pg \
      -e POSTGRES_USER=lamazon -e POSTGRES_PASSWORD=lamazon -e POSTGRES_DB=lamazon \
      -v lamazon-pgdata:/var/lib/postgresql/data \
      -p 5433:5432 postgres:16-alpine >/dev/null
  fi
fi

echo "==> waiting for Postgres"
for _ in $(seq 30); do
  docker exec lamazon-pg pg_isready -U lamazon -q && break
  sleep 1
done
docker exec lamazon-pg pg_isready -U lamazon -q || { echo "Postgres never came up"; exit 1; }

# Kill existing processes on the ports if they're from this script
for PORT in $API_PORT $WEB_PORT; do
  held=$(lsof -ti "tcp:$PORT" -sTCP:LISTEN 2>/dev/null | tr '\n' ' ' || true)
  if [ -n "$held" ]; then
    for pid in $held; do
      case "$(ps -o command= -p "$pid" 2>/dev/null)" in
      *lamazon-api*|*lamazon-website*)
        echo "==> replacing existing process on $PORT (pid $pid)"
        kill "$pid" 2>/dev/null
        ;;
      *)
        echo "Port $PORT is held by pid $pid: $(ps -o command= -p "$pid" 2>/dev/null)"
        echo "Stop it, or run API_PORT=8081 WEB_PORT=8101 $0"
        exit 1
        ;;
      esac
    done
    # Wait for port to be free
    for _ in $(seq 20); do
      lsof -ti "tcp:$PORT" -sTCP:LISTEN >/dev/null 2>&1 || break
      sleep 0.25
    done
  fi
done

# Build and run the API
echo "==> building API"
API_BIN=$(mktemp -t lamazon-api)
go build -C backend -o "$API_BIN" .

SKIP_LOGIN_CODE="${SKIP_LOGIN_CODE:-1}"
SKIP_SEED="${SKIP_SEED:-1}"
SEED_USER="${SEED_USER:-}"
SEED_USER_PASSWORD="${SEED_USER_PASSWORD:-}"
SEED_USER_NAME="${SEED_USER_NAME:-}"
SEED_USER_PHONE="${SEED_USER_PHONE:-}"
SEED_USER_ADDRESS="${SEED_USER_ADDRESS:-}"

echo "==> starting API on $API_PORT"
[ "$SKIP_LOGIN_CODE" = "1" ] && echo "==> sign-in code is OFF (local only)"
DATABASE_URL="$DB_URL" PORT="$API_PORT" SKIP_LOGIN_CODE="$SKIP_LOGIN_CODE" \
  SKIP_SEED="$SKIP_SEED" \
  SEED_USER="$SEED_USER" SEED_USER_PASSWORD="$SEED_USER_PASSWORD" \
  SEED_USER_NAME="$SEED_USER_NAME" SEED_USER_PHONE="$SEED_USER_PHONE" \
  SEED_USER_ADDRESS="$SEED_USER_ADDRESS" "$API_BIN" &
API_PID=$!

# Cleanup on exit
trap 'kill "$API_PID" 2>/dev/null; kill "$WEB_PID" 2>/dev/null; rm -f "$API_BIN" "$WEB_BIN"; exit' EXIT INT TERM

# Wait for API health check
echo "==> waiting for $LOCAL_API/api/health"
for _ in $(seq 60); do
  curl -sf "$LOCAL_API/api/health" >/dev/null && break
  kill -0 "$API_PID" 2>/dev/null || { echo "API exited, see the log above"; exit 1; }
  sleep 1
done
curl -sf "$LOCAL_API/api/health" >/dev/null || { echo "API never answered /api/health"; exit 1; }

# Check catalog
products=$(curl -sf "$LOCAL_API/api/products" | grep -o '"id"' | wc -l | tr -d ' ' || true)
echo "==> API healthy, catalog has ${products:-0} products"

# Build website assets
echo "==> building website assets (Tailwind)"
cd Lamazon_website
npm run build --silent 2>/dev/null || echo "   (npm build skipped or failed, proceeding)"
cd ..

# Generate templ code if needed
echo "==> checking templ templates"
if [ -n "$(find Lamazon_website/templates -name '*.templ' 2>/dev/null)" ]; then
  if command -v templ &> /dev/null; then
    echo "==> generating templ code"
    templ generate -path Lamazon_website/templates
  else
    echo "   (templ not found, assuming code is already generated)"
  fi
fi

# Build and run the website
echo "==> building Lamazon website"
WEB_BIN=$(mktemp -t lamazon-website)
go build -C Lamazon_website -o "$WEB_BIN" ./cmd/server

echo "==> starting website on $WEB_PORT (connecting to API at $LOCAL_API)"
API_BASE="$LOCAL_API" PORT="$WEB_PORT" "$WEB_BIN" &
WEB_PID=$!

# Wait for website to be ready
echo "==> waiting for http://localhost:$WEB_PORT"
for _ in $(seq 30); do
  curl -sf "http://localhost:$WEB_PORT" >/dev/null && break
  kill -0 "$WEB_PID" 2>/dev/null || { echo "Website exited"; exit 1; }
  sleep 1
done
curl -sf "http://localhost:$WEB_PORT" >/dev/null || { echo "Website never answered"; exit 1; }

echo ""
echo "=================================="
echo "✓ Full stack is running!"
echo "=================================="
echo "Website:   http://localhost:$WEB_PORT"
echo "API:       $LOCAL_API"
echo "Database:  localhost:5433"
echo ""
echo "Press Ctrl+C to stop all services"
echo "=================================="
echo ""

# Keep running until interrupted
wait
