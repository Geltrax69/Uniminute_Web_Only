# Lamazon Website

Go + Templ + HTMX + Alpine.js + Tailwind CSS storefront. Calls the existing
`../backend` for all data — no business logic is re-implemented here.

---

## Prerequisites

| Tool | Version | Install |
|------|---------|---------|
| Go | ≥ 1.22 | https://go.dev/dl |
| templ | v0.3.x | `go install github.com/a-h/templ/cmd/templ@latest` |
| Node.js | ≥ 18 | https://nodejs.org |

---

## First-time setup

```bash
cd Lamazon_website

# 1. Resolve Go dependencies (writes go.sum)
go mod tidy

# 2. Install Tailwind
npm install

# 3. Generate templ Go files from .templ sources
templ generate

# 4. Build the Tailwind CSS bundle
npm run build
```

---

## Running locally

Start the existing backend first (from the repo root):

```bash
# Terminal 1 — backend on :8080
cd backend && go run .
```

Then start the website:

```bash
# Terminal 2 — website on :8100
cd Lamazon_website
go run .
```

Open http://localhost:8100.

### Environment variables

| Variable | Default | Description |
|----------|---------|-------------|
| `API_BASE` | `http://localhost:8080` | URL of the existing backend |
| `PORT` | `8100` | Port this server listens on |

---

## Development workflow

In development you want Tailwind to rebuild on every template change:

```bash
# Terminal A — Tailwind watcher
npm run watch

# Terminal B — templ watcher (re-generates _templ.go on save)
templ generate --watch

# Terminal C — Go hot-reload (or just re-run manually)
go run .
```

---

## Architecture

```
Request → handlers.go (Site.handleXxx)
            │
            ├── shop.*          cookie helpers (cart, wishlist, session)
            ├── backend.*       typed HTTP client → ../backend API
            └── templ.*         server-side HTML rendering
                    │
                    ├── layouts/     base, shop, account, checkout
                    ├── pages/       one file per route
                    ├── components/  navigation, product, cart, collection, ui
                    └── fragments/   HTMX partial swaps (cart, search, wishlist)
```

### Key conventions

- **HTMX** handles every server round-trip: add-to-cart, cart mutations,
  wishlist toggle, product grid refresh, search results.
- **Alpine.js** handles pure client state: mobile menu, cart drawer open/close,
  image gallery, toasts, option pickers.
- **OOB swaps** (`hx-swap-oob="true"`) keep the bottom nav cart badge in sync
  after any cart mutation without a page reload.
- **Session** is in `HttpOnly` cookies (`lw_at` access, `lw_rt` refresh).
  The server reads them on every request and injects the bearer token when
  proxying `/api/*` to the backend.
- **Cart & wishlist** live in cookies (`lw_cart`, `lw_wish`), mirroring what
  the Flutter app stores on the device. Prices and stock are always validated
  by the backend at checkout.

### Responsive breakpoints (from `tailwind.config.js`)

| Breakpoint | Width | What changes |
|-----------|-------|-------------|
| — (default) | 0–480px | 3-column product grid, mobile bottom nav |
| `sm` | 480px | — |
| `md` | 700px | 4-column grid, filter sidebar visible, mobile menu hidden |
| `lg` | 1024px | 5-column grid |
| `xl` | 1200px | 6-column grid, wider gutter (32px) |

---

## Folder structure

```
Lamazon_website/
├── main.go              # entry point, route table
├── handlers.go          # all http.HandlerFunc implementations
├── go.mod / go.sum
│
├── backend/             # typed API client (no business logic)
│   └── client.go
│
├── shop/                # cookie helpers
│   ├── cart.go
│   ├── images.go
│   ├── money.go
│   └── session.go
│
├── viewdata/            # Page struct shared by every layout
│   └── viewdata.go
│
├── tpl/                 # tiny template helpers (When, Attr)
│   └── tpl.go
│
├── templates/
│   ├── layouts/         base, shop, account, checkout
│   ├── pages/           home, shop, product, collection, cart,
│   │                    checkout, login, register, account, orders,
│   │                    policies, 404
│   ├── components/
│   │   ├── navigation/  header, announcement, breadcrumbs,
│   │   │                departments (+ mobile menu), bottomnav
│   │   ├── product/     card, grid, gallery, info, price, add-to-cart
│   │   ├── cart/        drawer, item, summary, count (OOB badge)
│   │   ├── collection/  card, filters, sort, pagination
│   │   ├── footer/
│   │   └── ui/          icons, button, spinner, skeleton, empty-state,
│   │                    toast, drawer, dropdown
│   └── fragments/       HTMX partial responses
│       ├── cart/        updated, removed
│       ├── product/     grid, filters
│       ├── search/      results
│       └── wishlist/    toggle
│
├── static/
│   ├── css/
│   │   ├── app.css      Tailwind source (edit this)
│   │   └── tailwind.css built output (gitignored)
│   ├── js/
│   │   ├── htmx.min.js  vendored
│   │   ├── app.js       Alpine stores + HTMX hooks
│   │   └── alpine/
│   │       └── cdn.min.js vendored
│   ├── images/
│   └── fonts/
│
├── tailwind.config.js   design tokens from Flutter theme
└── package.json
```

---

## Known gaps / follow-ups

1. **`/saved` route** — The bottom nav "Saved" tab links to `/saved` but no
   handler exists. Simplest fix: redirect to `/account` or implement a
   wishlist page that reads the `lw_wish` cookie and fetches those products.

2. **Stores page** (`/stores`) — Currently redirects to `/shop`. A proper
   implementation would call `GET /api/shops` and display a store directory.

3. **Token refresh** — The server silently refreshes an expired access token
   once per request using the refresh cookie. If the refresh token is also
   expired the user is treated as a guest (no error shown). A production
   build should redirect to `/login` in that case.

4. **Images / fonts** — Copy Flutter's `assets/` into `static/images/`,
   `static/icons/`, `static/fonts/` so the logo, fallback images, and
   InterTight font resolve. The font face is declared in `static/css/app.css`
   and falls back to `system-ui` if the file is missing.

5. **go.sum** — Generated by `go mod tidy` (requires internet access to
   fetch the templ module). Run once after cloning.
