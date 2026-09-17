# Prompt: Convert Lamazon Flutter frontend to Go + Templ + HTMX + Alpine.js + Tailwind

Create a new folder `Lamazon_website/` implementing the storefront as a **Go + Templ + HTMX + Alpine.js + Tailwind CSS** frontend, reusing the existing Go backend in `backend/` for all data/APIs — do not reimplement business logic, only call it.

## Before writing any code

1. Use `graphify-out/GRAPH_REPORT.md` and `graphify-out/graph.json` to enumerate every screen/widget in `frontend/lib/` (the Flutter app) and its role.
2. Produce a mapping table: **Flutter screen/widget → target `.templ` file → backend endpoint(s) it calls.** Identify the real existing Go backend routes/handlers in `backend/` — do not invent parallel endpoints. If the existing API is missing something a screen needs, flag it explicitly instead of guessing.
3. Extract the Flutter theme (colors, typography, spacing scale) from wherever it's defined (e.g. `theme.dart` or equivalent) and translate it into `tailwind.config.js` so colors/fonts match exactly — no approximated values.
4. Copy the real assets from Flutter's `assets/` folder into `static/images/`, `static/icons/`, `static/fonts/` — do not regenerate or placeholder them.

## Behavior requirements

- **Templ** renders all pages server-side.
- **HTMX** handles every dynamic update that needs the server — add-to-cart, cart drawer, filters, sort, pagination, search results, wishlist toggle — via partial HTML fragments under `templates/fragments/`, using `hx-get`/`hx-post`/`hx-swap` conventions.
- **Alpine.js** handles pure client-side interactivity that doesn't need the server — mobile menu open/close, modals, dropdowns, image gallery, toasts. No server round-trip for these.
- Every backend call goes through the **existing** Go backend's handlers/routes.
- **Responsive**: mobile-first Tailwind breakpoints. Explicitly state what changes at each breakpoint (e.g. nav collapses to mobile-menu below `md`, product grid goes 4→2→1 columns) — match how the Flutter app already adapts across device sizes, if it does.
- **Visual fidelity**: match the Flutter app's exact colors, spacing, typography, and component states (hover, active, disabled, loading, empty, error). Pull real values from the Flutter code; don't approximate.
- Must work well on both **phone and desktop** viewports.

## Deliverable

A working `Lamazon_website/` with `go.mod`, `templ generate`-able templates, `tailwind.config.js`, `package.json` for the Tailwind build pipeline, and a `README.md` explaining how to run it locally against the existing backend.

## Folder structure

May add files/folders as needed, but keep this structure:

```
Lamazon_website/
├── go.mod
├── go.sum
├── main.go
├── README.md
├── backend -> (calls existing ../backend, not duplicated)
│
├── templates/
│   ├── layouts/
│   │   ├── base.templ
│   │   ├── shop.templ
│   │   ├── account.templ
│   │   └── checkout.templ
│   │
│   ├── pages/
│   │   ├── home.templ
│   │   ├── shop.templ
│   │   ├── product.templ
│   │   ├── collection.templ
│   │   ├── cart.templ
│   │   ├── checkout.templ
│   │   ├── login.templ
│   │   ├── register.templ
│   │   ├── account.templ
│   │   ├── orders.templ
│   │   └── 404.templ
│   │
│   ├── components/
│   │   ├── navigation/
│   │   │   ├── header.templ
│   │   │   ├── announcement.templ
│   │   │   ├── mobile-menu.templ
│   │   │   └── breadcrumbs.templ
│   │   │
│   │   ├── product/
│   │   │   ├── card.templ
│   │   │   ├── grid.templ
│   │   │   ├── gallery.templ
│   │   │   ├── info.templ
│   │   │   ├── price.templ
│   │   │   └── add-to-cart.templ
│   │   │
│   │   ├── cart/
│   │   │   ├── drawer.templ
│   │   │   ├── item.templ
│   │   │   ├── summary.templ
│   │   │   └── count.templ
│   │   │
│   │   ├── collection/
│   │   │   ├── card.templ
│   │   │   ├── filters.templ
│   │   │   ├── sort.templ
│   │   │   └── pagination.templ
│   │   │
│   │   ├── account/
│   │   ├── checkout/
│   │   ├── search/
│   │   ├── footer/
│   │   └── ui/
│   │       ├── button.templ
│   │       ├── modal.templ
│   │       ├── drawer.templ
│   │       ├── dropdown.templ
│   │       ├── toast.templ
│   │       ├── spinner.templ
│   │       ├── skeleton.templ
│   │       └── empty-state.templ
│   │
│   └── fragments/
│       ├── cart/
│       │   ├── added.templ
│       │   ├── updated.templ
│       │   └── removed.templ
│       │
│       ├── product/
│       │   ├── grid.templ
│       │   └── filters.templ
│       │
│       ├── search/
│       │   └── results.templ
│       │
│       └── wishlist/
│           └── toggle.templ
│
├── static/
│   ├── css/
│   │   ├── app.css
│   │   └── tailwind.css
│   │
│   ├── js/
│   │   ├── app.js
│   │   └── alpine/
│   │       ├── cart.js
│   │       ├── gallery.js
│   │       ├── menu.js
│   │       ├── modal.js
│   │       └── search.js
│   │
│   ├── images/
│   │   ├── products/
│   │   ├── collections/
│   │   ├── banners/
│   │   └── icons/
│   │
│   └── fonts/
│
├── tailwind.config.js
├── package.json
└── (README.md already listed above)
```
