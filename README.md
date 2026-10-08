# Uniminute

> ## Status: 🟡 In Progress
>
> <progress value="75" max="100"></progress>
> **Progress: 75%** — the core commerce loop (browse → cart → checkout → rider delivery) works end to end: 111 API routes, both Go modules compile, `go vet` is clean, and CI is green. Remaining work is the functionality hardening listed in the QA report (real notification history, cross-device cart sync, publishable policy documents) plus the storefront redesign.

<p align="center">
  <img src="banner.webp" alt="Uniminute banner" width="100%" />
</p>

[![Go](https://img.shields.io/badge/Go-1.25-00ADD8?style=flat-square&logo=go)](https://go.dev)
[![PostgreSQL](https://img.shields.io/badge/PostgreSQL-pgx-336791?style=flat-square&logo=postgresql)](https://www.postgresql.org)
[![templ](https://img.shields.io/badge/templ-UI-7d7d7d?style=flat-square)](https://templ.guide)

## What it is

Uniminute is a campus food & grocery delivery marketplace (previously called "Lamazon" — the old Flutter README this file replaces was stale). It has two parts: a Go + PostgreSQL **backend API** with auth, catalog, shops, cart/checkout, Razorpay payments, orders, reviews, campaigns, notifications, and seller/admin/rider roles; and a Go + templ + HTMX + Alpine.js **storefront** (`Lamazon_website/`) that calls the backend for all data and deploys to Vercel. The checked-out branch `codex/functionality-checkout` carries the functionality-fix work tracked in `FIX_TRACKER.md`.

## What works (verified)

- ✅ `backend/` compiles — `go build ./...` passes (Go 1.25.6, verified 2026-10-08)
- ✅ `backend/` is vet-clean — `go vet ./...` passes with no findings
- ✅ `Lamazon_website/` compiles — `go build ./...` passes (templ-generated `_templ.go` files are committed)
- ✅ 111 API routes registered in `backend/main.go`: login (OTP + password + reset), products, categories, shops, cart/checkout, Razorpay payments, orders, reviews, campaigns, compare, notifications, push, policies, charges, preferences, addresses — plus seller, admin, and rider endpoints
- ✅ CI green — the "Google API key guard" workflow succeeds on `codex/functionality-checkout` (latest runs, 2026-09-30)
- ✅ Core commerce loop verified end to end in `QA_REPORT.md` (2026-09-09): browse → cart → order → shop accepts → rider delivers

## Tech stack

| Layer | Technology |
|---|---|
| Backend API | Go 1.25, `net/http` mux |
| Database | PostgreSQL via `pgx/v5` |
| Payments | Razorpay (`razorpay-go`) |
| Storefront | Go + templ + HTMX + Alpine.js + Tailwind CSS |
| Storefront deploy | Vercel (`api/index.go` handler + `vercel.json`) |
| Media | Cloudinary |

## How to run

Only the build steps below were tested (Go 1.25.6). Running the server needs a live PostgreSQL database and Razorpay credentials, which were not available in this audit.

```bash
# 1. Backend API
cd backend
go mod download
go build ./...

# run it (needs Postgres; falls back to a local default DSN)
DATABASE_URL="postgres://lamazon:lamazon@localhost:5433/lamazon?sslmode=disable" \
ADMIN_USER=admin ADMIN_PASSWORD=secret \
go run .

# 2. Storefront (needs the backend running; APIBase points at it)
cd Lamazon_website
npm install        # tailwind build only
go build ./...
```

Regenerating templ components after editing `.templ` files (per the storefront README):

```bash
cd Lamazon_website
templ generate
```

Helper scripts at the repo root (`dev.sh`, `dev-full-stack.sh`, `deploy.sh`, `tunnel.sh`) wire the pieces together; they were read but not executed here.

## Screenshots

QA screenshots from the 2026-09-09 audit live in `qa-evidence/` (dozens of captures: admin login, dashboards, cart, checkout, rider flows):

![Admin login](qa-evidence/checks2/admin-dash.png)
![Admin categories](qa-evidence/checks2/admin-categories.png)
![Guest cart](qa-evidence/checks2/cart-guest.png)

## What you can add more

- [ ] Real notification history — the fabricated notifications were removed (B5); persistent user notifications are still unimplemented
- [ ] Cross-device cart/wishlist sync — cart and wishlist survive restart on one device only (B2)
- [ ] Publishable policy documents — templates still contain placeholders and publishing is blocked until they are filled (B4)
- [ ] Rider staffing — checkout refuses new orders without active riders; production had zero riders at QA time (B3)
- [ ] Finish the storefront redesign — per `FIX_TRACKER.md`, only batches 1–2 are deployed; later batches need coordinated API/web rollout

## Project structure

```
backend/               Go API (module github.com/Geltrax69/Lamazon/backend): auth, catalog,
                       shops, orders, checkout, Razorpay, reviews, campaigns, notify, push,
                       seller/admin/rider, policies — 33 source files + 24 test files
Lamazon_website/       Go + templ + HTMX + Alpine storefront calling backend/ for all data;
                       deploys to Vercel via api/index.go (module lamazon/website)
qa-evidence/           QA screenshots (checks2/, checks3/, fix/)
DESIGN.md              design notes
TRACKER.md             Lamazon_website build tracker
FIX_TRACKER.md         functionality fix tracker (batches B1–B8)
QA_REPORT.md           pre-launch QA report, 2026-09-09
chat.py                tiny stdlib-only CLI chatbot for a rewind.ai-style endpoint (unrelated util)
dev.sh / dev-full-stack.sh / deploy.sh / tunnel.sh   dev & deploy helpers
```

---
*README written after code audit on 2026-10-08.*
