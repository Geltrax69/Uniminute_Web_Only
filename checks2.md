# CHECK 2 — Production QA / UI / UX Release Gate

**Release decision: NO-GO.**

---

## 0. Test Control, Scope & Evidence

| Field | Value |
|---|---|
| Build / Version | `codex/functionality-checkout` @ `519ebf0`; API commit `349ad74`; Flutter 3.41.6 (stable) |
| Build tested | **Release** web build (`flutter build web --release`), served statically with SPA fallback. A debug build was also exercised and is noted separately. |
| Test date | 2026-09-10 |
| Browser | Chromium (Playwright), plus in-app Chromium pane |
| Viewports | 1280×800 desktop, 375×812 mobile |
| Roles tested | Guest (unauthenticated), Shopper, Seller, Admin, Delivery rider |
| Backend | Local Go API on `localhost:8080` + Postgres 16 (`lamazon-pg`) |
| Evidence | 37 screenshots in `qa-evidence/checks2/` |
| Tester | QA pass, agentic |

### Environment limitations (declared)

1. **`SKIP_LOGIN_CODE=1` is set on the local API**, so email sign-in issues a token without a code. I verified against the deployed API (`api.geltrax.engineer`) that the real code flow **is** enforced there. This is a local-dev flag only and is **not** reported as a defect.
2. `SKIP_SEED=1` — the catalogue started empty (0 products). I built the test dataset through the app's own seller API so every product, order and rider in this report was created by a documented flow, not injected into the database.
3. The in-app browser pane was hidden for part of the session, which blocks synthetic click/type. All interactive testing was therefore done via Playwright. No finding depends on the hidden-pane condition.
4. **Not tested** (no coverage; stated so the report is not read as complete): product detail screen, wishlist / Saved screen, compare screen, shop detail screen, notifications screen, settings, help, profile setup, campaign manager, admin photo manager, push-notification delivery, CSV export, the photo picker/cropper UI, order cancellation, and the delivery-rider UI screen (the rider **API** lifecycle was fully tested).
5. Reduced-motion handling is confirmed **in code** but was not behaviourally verified in a `prefers-reduced-motion` browser.

### Residue left in the test environment

`inventory_items` item-2 and item-3 (QA products), `orders` order-6, rider `9000000001`, user `qa.buyer@lamazon.test` + address. Category `Test` was deleted and re-created to verify the delete guard, so its sort position moved from 1 to 5 under *Mobile Accessories*.

---

## 1. Executive Summary

Lamazon has a **solid, well-reasoned backend** and a **handful of genuinely excellent UI moments**, wrapped around a frontend that does not yet tell the truth about what it is doing.

The server-side work is the strongest part of this codebase. Checkout is transactional and idempotent, stock is reserved under row locks, role separation is enforced on every protected route, password attempts are rate-limited with a well-designed DB-backed limiter, input validation is thorough with genuinely good error copy, and SQL injection is properly parameterised. Category deletion is carefully guarded against orphaning data.

Against that, three findings block release:

1. **A seller deleting a product permanently destroys every order ever placed for it**, including delivered orders and their financial record — silently, with an HTTP 204 and no warning. The buyer's order history and the admin's records vanish; the rider's delivery counter keeps counting an order that no longer exists.
2. **The seller's "remove product" and "restock ±" controls are fakes.** They mutate local memory only and never call the API. The product disappears from the seller's screen while remaining live and purchasable in the shop; the stock counter drifts from reality with every press.
3. **Keyboard-only users cannot place an order.** The delivery-address form's submit control is not a button, has no role and no tabindex, and cannot be focused or activated. A delivery address is mandatory for checkout.

Beneath those, the app has a design system it does not follow: **229 hardcoded colour literals against 200 theme references, 62 distinct hex values in a system that defines 8**, and a search screen built as exactly the "rainbow-department dashboard" that `DESIGN.md` explicitly refuses. First load is **6.6 MB**, of which **3.2 MB is icon fonts the app does not use**, and another **4.3 MB of dead image assets** ship in every build.

### Ratings

| Area | Score | Basis |
|---|---:|---|
| Functionality | 5/10 | Core shopping and the full order lifecycle work end-to-end; two seller controls are non-functional illusions |
| UX | 4/10 | Good empty states and confirmation; login wall before any product, no stock cap in cart, two-step address save, no undo |
| UI | 4/10 | Strong hero/home/empty states; four primary-button styles, four back-button positions, palette contradicted on its own terms |
| Reliability | 3/10 | Silent, unrecoverable order destruction; client/server state divergence in the seller dashboard |
| Accessibility | 3/10 | Measured focus contrast 1.10:1 (needs 3:1); keyboard cannot complete checkout; zero headings; duplicated labels throughout |
| Performance | 4/10 | 6.6 MB first load, ~50% of it unused fonts; 7-day cache on an unhashed bundle |
| Security posture | 7/10 | Genuinely good: role separation, rate limiting, parameterised SQL, guarded category deletes. Loses points for the unguarded item delete and boilerplate Firebase config in `index.html` |
| Production readiness | 2/10 | Data-loss and trust defects, not polish items |

**Overall Score: 4/10**

### Would you ship this to real users today?

**NO.**

Not because the app is unfinished — most of it works — but because three specific defects each cause harm that the user cannot see, cannot undo, and cannot be compensated for. A shop that silently deletes completed orders when a seller tidies their catalogue, tells that seller a product is off sale when it is still selling, and cannot be operated at all by a keyboard user, is not a shop that should take money. Every one of these is a small, well-scoped fix.

---

## 2. Critical Problems (release blockers)

### [C2-001] Deleting a product permanently destroys every order for it, including delivered ones

**Status:** CONFIRMED · **Severity:** P0 · **Category:** Data/Logic, Reliability
**Route:** `DELETE /api/seller/items/{id}` · **Component:** `backend/seller.go:423` + `backend/schema.sql:54`
**Frequency:** Every time · **User impact:** Severe · **Business impact:** Severe · **Discoverability:** Hidden · **Recoverability:** Impossible

**Preconditions:** An approved store with at least one product that has orders against it.

**Steps to reproduce:**
1. Create a product as a seller.
2. Place an order for it as a buyer (`POST /api/orders/checkout`).
3. Accept it as the seller, assign a rider, pick it up and deliver it with the correct code — the order reaches stage `delivered`.
4. Confirm the buyer sees the delivered order (`GET /api/orders` → 1 order, ₹65) and the admin sees it (`GET /api/admin/orders` → 1 order).
5. As the seller: `DELETE /api/seller/items/{id}`.

**Expected:** The request is refused while orders reference the item — exactly as `handleDeleteCategory` refuses with *"2 items still listed under Food. Move them first."* Or the item is soft-deleted / delisted, preserving order history.

**Actual:** `HTTP 204 No Content`. The buyer's orders go from 1 to 0. The admin's orders go from 1 to 0. The `orders` table drops from 1 row to 0. The ₹65 sale record no longer exists anywhere.

Measured across the full test run: an unguarded delete of a single product destroyed **3 live `received` orders** in one call; a second delete destroyed a **`delivered`** order.

**Impact:** Irreversible loss of financial and fulfilment records. A buyer loses proof of purchase. A seller loses sales history. An admin loses the audit trail. The rider's `delivered` counter still reads `1` while the order it counted no longer exists — a permanently unreconcilable statistic.

**Root cause:**
```sql
-- backend/schema.sql:54
item_id TEXT NOT NULL REFERENCES inventory_items (id) ON DELETE CASCADE
```
```go
// backend/seller.go:425 — no guard, no soft-delete
DELETE FROM inventory_items WHERE id = $1 AND owner = $2
```
`inventory_items.owner` also cascades from `seller_stores`, so removing a store would take every item and therefore every order with it.

**Suggested fix:** Change the FK to `ON DELETE RESTRICT`, and in `handleDeleteItem` count referencing orders first and return `409` with the same shape of message `handleDeleteCategory` already uses. For catalogue hygiene, add a `delisted` flag so a seller can hide a product without destroying its history.

**Regression scope:** Every FK with `ON DELETE CASCADE` in `schema.sql` (4 of them); store deletion; `addresses → users`; the admin overview counters; rider `delivered` counts.

---

### [C2-002] The seller's "remove product" and "restock ±" controls change nothing on the server

**Status:** CONFIRMED · **Severity:** P1 · **Category:** Bug, Deceptive UI, Data/Logic
**Route:** `/store` (Seller dashboard → Inventory) · **Component:** `frontend/lib/data/seller.dart:396` and `:402`
**Frequency:** Every time · **User impact:** Severe · **Business impact:** High · **Discoverability:** Misleading · **Recoverability:** Easy (reload) but the seller has no reason to suspect it

**Steps to reproduce (delete):**
1. Sign in as a seller with an approved store and ≥2 products. Open **My store → Inventory**.
2. Press the red trash icon on a row.
3. Observe: the row disappears; *Products* drops 2 → 1; *Inventory (2)* → *Inventory (1)*; *Inventory value* falls ₹70 → ₹10. No confirmation dialog, no undo, no toast.
4. Query the server.

**Expected:** The product is removed (or delisted) server-side and no longer purchasable.

**Actual:** `GET /api/products` still returns the product with `availableStock: 5`. The database still holds it. **It is still on the shop's home page and still in the buyer's search results, fully purchasable.**

**Steps to reproduce (restock):**
1. On the same screen press `+` on a row showing "1 left".
2. Observe: badge → "2 left", *Units* 1 → 2, *Inventory value* ₹10 → ₹20.
3. Query the server: `stock` is still `1`, and the shopper-facing `availableStock` is still `0` (out of stock).

**Impact:** A seller pulling a spoiled, mispriced or discontinued product watches it disappear and reasonably believes it is off sale. Customers keep ordering it. A seller restocking after a delivery believes stock is back; shoppers keep seeing "Out of stock". Every dashboard number on that screen — Products, Units, Needs restock, Inventory value — becomes fiction after a single press.

**Root cause:** Both methods are synchronous and never touch `Api.instance`, unlike every other mutation in the class (`_pushItem`, `_patchItem`, `acceptOrder`, `rejectOrder` are all `async` and call the API):
```dart
// frontend/lib/data/seller.dart:396
void removeItem(String id) {
  _items.removeWhere((i) => i.id == id);
  notifyListeners();
}
```
`PATCH /api/seller/items/{id}/stock` exists on the backend and **has no caller anywhere in the frontend**. `DELETE /api/seller/items/{id}` likewise has no frontend caller — `api.dart` only ever issues `DELETE` for addresses.

**Suggested fix:** Wire both to the API through the existing `_move`-style optimistic-with-rollback pattern. Add a confirmation dialog on delete (naming the product and any live orders) and an undo affordance. Do not ship delete at all until C2-001 is fixed — right now a working delete button would be a data-loss button.

**Regression scope:** Every counter on the seller dashboard; the shop home page; search results; the cart's stock validation; admin store views.

---

### [C2-003] Keyboard-only users cannot save a delivery address, and therefore cannot order

**Status:** CONFIRMED · **Severity:** P1 · **Category:** Accessibility, Bug
**Route:** `/account` → Address book → Add new address ("Enter Location") · **Component:** `frontend/lib/screens/location_screen.dart:285`
**Frequency:** Every time · **User impact:** Severe · **Recoverability:** Impossible without a pointer

**Steps to reproduce:**
1. Open the address form.
2. Inspect the accessibility tree / attempt to reach the controls with Tab only.

**Expected:** Every control is focusable, has a role, and is activatable with Enter or Space.

**Actual (measured from the live accessibility tree):**

| Control | role | tabindex |
|---|---|---|
| **"Check availability" (submit)** | **(none)** | **(none)** |
| "Home" / "Office" / "Other" label chips | button | **(none)** |
| "Lovely Professional University" city chip | button | **(none)** |
| "Enter the recipient name." (error) | (none) | (none) |

The three text fields are reachable. Nothing else is. The submit control is a bare `GestureDetector`, so it is not a button to assistive technology, is not in the tab order, and does not respond to Enter or Space.

The same pattern appears one screen earlier: on **Saved Addresses**, `"Add new address"` has `role="button"` but **no tabindex** — the primary action of that screen is unreachable by keyboard.

**Impact:** A delivery address is mandatory to place any order. A keyboard-only or screen-reader user can browse, search, and fill a cart, then hits an unreachable wall at the one step they must complete. The whole purchase funnel is closed to them.

**Suggested fix:** Replace the `GestureDetector` at `location_screen.dart:285` with the app's own `ActionButton` (or any `InkWell`/`FilledButton`), which already carries semantics and focus. Make the label chips a `RadioListTile`-style group (they are single-select, not checkboxes). Give the error text `liveRegion: true` and associate it with the offending field.

**Regression scope:** Audit every `GestureDetector` used as a control across the app; every chip group; the seller onboarding form, which uses the same disabled-CTA-with-helper-text pattern.

---

## 3. Complete Issue Register

| ID | Status | Sev | Category | Route / Feature | Problem | Impact | Evidence |
|---|---|---|---|---|---|---|---|
| C2-001 | CONFIRMED | P0 | Data/Logic | `DELETE /api/seller/items/{id}` | Deleting a product cascades and destroys all its orders, including delivered | Irreversible loss of financial + fulfilment records | §2 |
| C2-002 | CONFIRMED | P1 | Bug / Deceptive UI | `/store` Inventory | Trash and stock ± change local memory only; product stays live and purchasable | Seller acts on false inventory; customers order pulled stock | `seller-after-delete.png`, `seller-stock-bump.png` |
| C2-003 | CONFIRMED | P1 | Accessibility | Address form | Submit is not a button; no role, no tabindex; chips unreachable | Keyboard users cannot complete checkout | live a11y tree |
| C2-004 | CONFIRMED | P1 | Accessibility | App-wide | Focus indicator measured at **1.10:1** contrast (WCAG needs 3:1); some controls paint none at all | Keyboard users cannot see where they are | `focus-1.png`, `focus-4.png` |
| C2-005 | CONFIRMED | P1 | Content / Legal | `/login` → Terms, Privacy | Sign-up requires agreeing to policies that all read "This policy is not published yet" | Consent obtained against a non-existent contract | `terms.png` |
| C2-006 | CONFIRMED | P1 | Performance | First load | 6.6 MB initial payload; **3.2 MB is 7 Lucide icon-font files** (6 unused weights) | ~2 min first load on slow mobile data | `performance.getEntriesByType` |
| C2-007 | CONFIRMED | P1 | UX | Home / cart | Cart accepts **5 units of an item with stock 1**, quotes ₹65, warns only at "Place order" | Failure at the last step of the funnel | `cart-guest.png` |
| C2-008 | CONFIRMED | P1 | UI | Home, all breakpoints | Floating bottom nav overlaps the first row of every department grid, desktop and mobile | Content permanently obscured | `home-guest-full.png`, `home-mobile.png` |
| C2-009 | CONFIRMED | P2 | UI / Product | Home | Every category tile in a department shows the **same** glyph — "Toys & Games" and "Glue & Tape" both get a book icon | ~40 tiles conveying zero information, misleading | `home-scroll1.png`, `oos-card.png` |
| C2-010 | CONFIRMED | P2 | Data / Trust | Category tree | Live shopper-facing categories named **"Test", "Work", "123456789", "567890"** under Electronics → Mobile Accessories | Visible test junk in production taxonomy | `GET /api/categories` |
| C2-011 | CONFIRMED | P2 | UI | Address form, search | `labelText` and `hintText` set to the same string, so the label prints twice, stacked, in the focused field | Unreadable field | `location_screen.dart:358-359` |
| C2-012 | CONFIRMED | P2 | Bug | Seller inventory | Product thumbnails always show a grey placeholder; `cover` reads local upload bytes, never `imageUrls` | Seller never sees their own photos after reload | `seller-inventory.png` |
| C2-013 | CONFIRMED | P2 | Data | Seller vs shopper | Seller sees "1 left"; shopper sees "Out of stock" for the same item (raw stock vs available stock) | Seller believes they can still sell | cross-checked via API |
| C2-014 | CONFIRMED | P2 | Performance | Build | 4.3 MB of **unreferenced** image assets ship (`category-atlas.png` 2.1 MB, `everyday-campaign.png` 2.2 MB, `banner.png`) | Wasted bandwidth on every install | `pubspec.yaml` ships `assets/categories/` wholesale |
| C2-015 | CONFIRMED | P2 | Reliability | `vercel.json` | `/(.*)\.js` cached `max-age=604800` but `main.dart.js` is **not content-hashed** | Returning users can run a week-old bundle against a live API | `vercel.json`, built filename |
| C2-016 | CONFIRMED | P2 | Content | PWA manifest + meta | Untouched Flutter boilerplate: description "A new Flutter project.", `theme_color` `#0175C2` (Flutter blue), name "lamazon" | Off-brand install/splash, wrong link previews | `manifest.json`, `index.html` |
| C2-017 | CONFIRMED | P2 | UI | Maskable icons | `Icon-maskable-*.png` are **byte-identical** to the standard icons (same MD5) — no safe-zone padding | Android crops into the logo | `md5` |
| C2-018 | CONFIRMED | P2 | Accessibility | App-wide | **Zero heading semantics** anywhere; every title is a `generic` | Screen-reader users cannot navigate by heading | a11y tree, all screens |
| C2-019 | CONFIRMED | P2 | Accessibility | App-wide | Every accessible label is emitted 2–3× ("Back Back", "Open account Open account Open account") | Screen readers announce everything twice | a11y tree |
| C2-020 | CONFIRMED | P2 | Accessibility | Admin dashboard | The six KPI tiles expose bare numbers ("4, 2, 0, 1, 0, 0") with no labels in the a11y tree | Metrics meaningless to screen-reader users | a11y tree |
| C2-021 | CONFIRMED | P2 | Accessibility | Admin | Section navigation chips have `role="checkbox"` but are single-select navigation | Wrong mental model announced | a11y tree |
| C2-022 | CONFIRMED | P2 | Accessibility | Cart | Quantity ± buttons are **38×38 px** (WCAG 2.5.8 / Material minimum is 44–48) | Hard to hit on mobile | measured |
| C2-023 | CONFIRMED | P2 | UX | Cart | Removing the last item has **no confirmation and no undo** | Cart lost in one click | `cart-empty.png` |
| C2-024 | CONFIRMED | P2 | UI | App-wide | **229 hardcoded colour literals vs 200 theme refs; 62 distinct hex values** for a system that defines 8 | Systemic visual inconsistency | `grep` counts |
| C2-025 | CONFIRMED | P2 | UI | `/search` | Department tiles are pastel blue / peach / lilac / pink — the "rainbow-department dashboard" `DESIGN.md` explicitly refuses | Contradicts the product's own design thesis | `search-empty.png` |
| C2-026 | CONFIRMED | P2 | UX | Search | No result count, no clear (×), no filters, no sort | Cannot trust or refine results | `search-robert.png` |
| C2-027 | CONFIRMED | P2 | UX | Login | A shopping app shows a **login wall before any product**; the only enabled control is "Skip login" | Conversion killer; dev-speak label | `login-desktop.png` |
| C2-028 | CONFIRMED | P2 | Content | Login marquee | The entry screen advertises ~12 stock products (bread, pizza, teddy bear) that do not exist in the catalogue | First impression is fabricated stock | `login-desktop.png` |
| C2-029 | CONFIRMED | P2 | UX | Address form | Two-step submit: "Check availability" → "Save address" for a 3-field form | Extra step, unclear CTA | `location_screen.dart:305` |
| C2-030 | CONFIRMED | P2 | Content | Address / seller forms | Validation error shown on a pristine, untouched form, one field at a time, with no error styling | Reads as an accusation before any input | `deeplink-account-nosession.png` |
| C2-031 | CONFIRMED | P3 | Bug | Cart empty state | "Start shopping" renders home but leaves the URL at `/cart`, and restores the previous scroll position mid-page | URL lies; user dumped in the Stationery section | observed |
| C2-032 | CONFIRMED | P3 | Content | Order confirmation | Shows raw id as **"Order order-6"** | Leaks database identifiers | `order-confirm.png` |
| C2-033 | CONFIRMED | P3 | Content | API | `/api/orders` and `/api/addresses` return **"sign in to manage your store"** when unauthenticated | Wrong copy for a shopper | curl |
| C2-034 | CONFIRMED | P3 | Content | API | A wrong-role or entirely bogus token returns **"session expired — sign in again"** | Misleading; nothing expired | curl |
| C2-035 | CONFIRMED | P3 | Content | Account | "Your information" and "Other Information" — inconsistent capitalisation 220 px apart | Sloppy | `account-scroll2.png` |
| C2-036 | CONFIRMED | P3 | UX | Admin, empty list | **Three** stacked empty-state messages, two contradictory in tone; "Previous/Next" pagination shown for 0 records | Confusing; dead controls | `admin-dash2.png` |
| C2-037 | CONFIRMED | P3 | UI | Admin | Eight red delete icons are more visually prominent than the single "+ Department" primary action | Destructive louder than constructive | `admin-categories.png` |
| C2-038 | CONFIRMED | P3 | UX | Cart | "Cash on delivery" is a selectable card with a checkmark, but is the only option | Fake choice | `cart-guest.png` |
| C2-039 | CONFIRMED | P3 | UX | Cart / confirmation | Delivery ETA is never shown, though `/api/locations` returns `eta: "12 mins"` | Missing the most-wanted information | API vs UI |
| C2-040 | CONFIRMED | P3 | UI | Cart | Quantity renders zero-padded as "05" / "01" | Reads as a code, not a count | `cart-guest.png` |
| C2-041 | CONFIRMED | P3 | UX | Home | Push-permission banner appears above the hero on first paint, before any user action | Tanks opt-in rates | `home-mobile.png` |
| C2-042 | OBSERVED | P2 | Security | `web/index.html` | A hardcoded fallback `firebaseConfig` with a live-looking API key and project `messages-34023` ships to every client | Needs developer verification | `index.html` |
| C2-043 | CONFIRMED | P3 | Data | `POST /api/orders` (legacy) | No idempotency — three identical requests created three orders, each charging a full ₹15 delivery fee | Duplicate orders for any non-web client | curl ×3 |
| C2-044 | CONFIRMED | P4 | UI | App-wide | **Four** primary-button styles (solid forest, solid black, outlined green, solid charcoal) and **four** back-button positions (x = 20 / 42 / 331 / 346) | No visual system | screenshots |
| C2-045 | CONFIRMED | P4 | UI | Home | Four of nine category labels truncate ("Household…", "Grocery & …", "Snacks & …", "Stationery …") | Unreadable navigation | `home-guest-full.png` |
| C2-046 | CONFIRMED | P4 | Performance | Debug build | The debug (DDC) web build **crashes on first paint** with a Stack Overflow in `initializeAndLinkLibrary` while loading the Lucide icon library | Blocks local development | console log |
| C2-047 | CONFIRMED | P4 | UI | Product cards | Card width differs between home (232 px) and search (193 px); images letterbox with grey bands | Inconsistent | screenshots |
| C2-048 | CONFIRMED | P4 | UX | "Only N left" | Scarcity badge and seller "Low stock" both fire at 5 units | Cries wolf | observed |
| C2-049 | OBSERVED | P3 | Security | `POST /api/admin/riders` | Response returns the rider's PIN in plaintext and the admin UI displays it | By design for handover; confirm it is shown once and never logged | curl |
| C2-050 | CONFIRMED | P4 | Accessibility | App-wide | `user-scalable=no, maximum-scale=1.0` blocks pinch-zoom (WCAG 1.4.4) | Low-vision users cannot zoom | viewport meta |

---

## 4. UI Audit

### 4.1 The core problem: a design system that is documented but not used

`DESIGN.md` is a genuinely good document. It names eight colours, one divider colour, a type scale, and a clear thesis: *"It refuses the rainbow-department dashboard and the endless stack of unrelated carousels."*

The code does not follow it. Measured across `frontend/lib`:

| Metric | Count |
|---|---:|
| Hardcoded `Color(0xFF…)` literals | **229** |
| `LamazonTheme.*` references | 200 |
| Distinct hardcoded hex values | **62** |
| Colours defined in `DESIGN.md` | 8 |

The three most-used hardcoded colours are **none of the system's**:

| Hex | Uses | System says |
|---|---:|---|
| `#F1F1EF` | 35 | canvas is warm ivory `#F7F6F0` |
| `#6B6B6B` | 31 | muted is warm green-grey `#66716A` |
| `#1A1A1A` | 31 | text is `#17221D` |

That is 87 uses of three cool neutrals in an app whose identity is *warm*. It is why the address, account and search screens read visibly greyer than home — not a perception, a substitution. `#D32F2F` (11×) and `#2E7D32` (9×) are **stock Material palette** red and green: the error and success colours of a bespoke brand are Google's defaults.

Nineteen of twenty-nine screens hardcode four or more colours; `compare_screen.dart` and `admin_screen.dart` hardcode sixteen each.

### 4.2 Hierarchy

- **Home** is the best screen in the app. Forest service header, prominent search, one campaign with its copy in a protected left field. It does what the thesis says.
- **Home also buries its own content**: the floating bottom nav sits over the first row of every department grid at both 1280×800 and 375×812.
- **Search** inverts the thesis — pastel blue / peach / lilac / pink department blocks, each 613×236 px to carry one 24 px icon and one word. Two departments fit per viewport; reaching the ninth takes four screens of scrolling.
- **Admin** spends the top 340 px (42% of an 800 px viewport) on six KPI tiles and eleven chips before any working area, on every section.
- **Login** makes the only enabled control on the first screen the escape hatch ("Skip login"), while the primary CTA is disabled.

### 4.3 Spacing & alignment

- Back buttons sit at x = 20 (cart), 42 (addresses), 331 (policy), 346 (account). Four screens, four positions.
- The address form leaves ~500 px of dead vertical space between the last field and a bottom-pinned CTA at 375 px width.
- Admin category cards are ragged: "Gifts" is 64 px tall beside "Beauty" at 96 px in the same row.
- On desktop, nine category tiles occupy 682 px of a 1216 px container, hugging the left edge; the search screen's one result renders as a single 193 px card with 1,050 px of empty space.

### 4.4 Typography

- Inter Tight is applied consistently — a genuine strength.
- "Your information" (sentence case) sits 220 px above "Other Information" (Title Case) on the same screen.
- Four of nine category labels truncate at 66 px tile width.
- The address and search fields print their label twice, stacked, because `labelText` and `hintText` are the same string.

### 4.5 Component states

| State | Verdict |
|---|---|
| Default | Pass |
| Hover | Not systematically implemented |
| **Focus** | **Fail — measured 1.10:1 contrast; some controls paint nothing** |
| Active/pressed | Partial (`GestureDetector` CTAs give none) |
| Loading | Not observed on any async action |
| **Disabled** | Pass — out-of-stock `+` is grey with a text label, not colour alone |
| Error | Partial — present but unstyled, unassociated, not announced |
| Success | Pass — order confirmation is strong |
| **Empty** | **Pass — the best component work in the app** |

The pattern is clean: the four files that use the shared `status_views.dart` produce excellent results. The bespoke screens produce the debt.

### 4.6 Trust signals

Three things actively undermine trust:
1. The login screen advertises a dozen stock products the shop does not sell.
2. The category tree contains "Test", "Work", "123456789", "567890".
3. Sign-up requires agreeing to four policies that all say they are not published.

---

## 5. UX Audit

### 5.1 Learnability

A first-time visitor sees a login wall, not a shop. `main.dart` routes `/` to `LoginScreen` unless `onboarded`. The escape hatch is labelled **"Skip login"** — developer language — in small secondary styling top-right, while the primary CTA is disabled.

Note the gate is also inconsistent: `/account`, `/cart`, `/saved` are all reachable with no session at all, so the wall only guards `/`.

### 5.2 Task efficiency — add a delivery address

**Current:** Home → header "Choose delivery location" → **"Saved Addresses"** → "Add new address" → **"Enter Location"** → fill 3 fields → "Check availability" → "Save address" → back.

Three screens, three different names for one concept ("delivery location" → "Saved Addresses" → "Enter Location"), and a form titled *Location* whose first field is *Full name*. Two taps to submit a three-field form.

**Better:** Home → "Add delivery address" → fill 3 fields → **"Save address"** → back. Run the serviceability check on the city selection (there is exactly one option) rather than gating submit behind a separate press. **Saves one screen and one tap, and removes two of the three names.**

### 5.3 Task efficiency — buy something

**Current:** Login wall → Skip/sign in → home → scroll past ~40 empty category tiles → product → `+` → Cart tab → scroll → Place order.

The ~40 category tiles are the problem: every one leads to an empty result set, and each department costs ~600 px of vertical scroll to show six identical glyphs.

**Better:** Show real stock first. Reduce the fallback tiles to a compact text list, or hide departments with no products. **Removes ~4 screens of scrolling between the shopper and the only two things they can actually buy.**

### 5.4 Feedback

- Add-to-cart gives no toast — only a badge in the bottom bar, 220 px away from the click.
- No loading state was observed on any async action.
- Removing the last cart item gives no undo.
- The order confirmation is genuinely good: amount, address, order id, item, store, and two clear exits.

### 5.5 Error recovery

Backend error copy is excellent and consistently actionable — *"not enough stock for QA Masala Chai"*, *"MRP cannot be below the selling price"*, *"2 items still listed under Food. Move them first."*, *"wrong code — ask the customer to read it out again"*. This is better than most production apps.

Two exceptions: *"prices have changed — remove and re-add the affected items"* fires on any `expectedTotal` mismatch and asserts a cause that may be false; and *"session expired — sign in again"* is returned for wrong-role and bogus tokens alike.

### 5.6 Dead ends

- A guest can browse, add five items and open the cart before learning at the final button that they must sign in.
- "Start shopping" from the empty cart leaves the URL at `/cart` and restores the previous scroll position mid-page.
- Logged-out users are offered Address book / Saved items / Notifications rows that cannot hold anything.

---

## 6. Missing Features / Controls / States

### Missing functionality
- **Order cancellation from the UI.** `POST /api/orders/{id}/cancel` exists and `api.dart:569` calls it, but no tested surface exposes it.
- **Stock cap in the cart.** Nothing prevents adding 5 of an item with stock 1.
- **Search filters, sorting, result count, and a clear (×) control.** None exist.
- **Delisting a product** without destroying it.
- **Online payment.** Cash-on-delivery only, surfaced as a negative at the moment of purchase.

### Missing controls
- Undo on cart removal and product deletion.
- Confirmation dialog on any destructive action (product delete, category delete).
- A pause control for the continuously animating login marquee (WCAG 2.2.2).

### Missing states
- Loading/skeleton state on every async action, including the ~8 MB category artwork, which pops in from blank tiles.
- Live-region announcement for validation errors.
- A distinct focus state on controls outside the five that define one.

### Missing information
- Delivery ETA (`/api/locations` returns `"12 mins"`; nothing shows it).
- App version in the account footer (`AppInfo.load()` exists).
- Which admin is signed in (only "Sign out" is shown).
- Policy "last updated" date (the API returns `updatedAt`).

---

## 7. Admin Findings

Assessed as a separate product, the admin panel is **the second-strongest part of the build**.

**What works:**
- The sign-in screen is on-brand and correct: password reveal, clear "Staff only" framing, ivory canvas, forest CTA, lime accent.
- **Responsive behaviour is genuinely well done.** At 375 px the eleven section chips collapse into a properly labelled "Admin section" dropdown, the KPI grid reflows to 3-up, and cards go single-column. This is real adaptation, not a squeeze.
- **Category deletion is properly guarded** — verified live: `"2 items still listed under Food. Move them first."` and `"6 categories still inside Electronics. Remove them first."`
- Downstream effects are correct. An order placed in the shopper UI appeared immediately in the admin overview (`orders: 1`), the seller queue, and reduced `availableStock` — verified across all three surfaces.

**What fails:**
- Six KPI tiles, four reading zero, consuming the first 200 px of every section.
- Eleven navigation destinations styled as filter chips, mixing IA levels — "Orders", "Insights" and "Policies" are pages, not filters on the current list.
- "Banners" is the only chip without a count.
- Three stacked empty-state messages, two contradictory in tone.
- "Previous"/"Next" rendered for 0 records.
- Eight red delete icons outweigh the single "+ Department" action.
- Accessibility: KPI numbers announce as bare digits; the section chips claim `role="checkbox"`; the search field is absent from the accessibility tree entirely; the refresh button generates seven semantic nodes.

---

## 8. User Journey Results

| Journey | Result | Failure point | Severity | Notes |
|---|---|---|---|---|
| **A — First-time visitor discovers and buys** | **PARTIAL** | Login wall before any product; ~40 empty category tiles before real stock | P2 | Completes, but the funnel fights the user |
| **A′ — Guest adds to cart and checks out** | **FAIL** | Guest can add 5 of a 1-stock item, reach the cart, then hits "Sign in to place order" | P1 | Sign-in requirement disclosed only at the final step |
| **B — Returning shopper places an order** | **PASS** | — | — | Sign in → add → cart → address auto-filled → "Place order · ₹25" → confirmation with order id. Clean. |
| **C — Full order lifecycle** | **PASS** | — | — | received → accepted → assigned → picked → delivered (wrong code correctly rejected, right code accepted). Stock reserved on order, rider counter incremented. Genuinely solid. |
| **D — Seller manages inventory** | **FAIL** | Trash and stock ± change nothing server-side | P1 | C2-002 |
| **E — Admin reviews and manages** | **PASS** | — | — | Login, sections, category guards, downstream propagation all correct |
| **F — Keyboard-only purchase** | **FAIL** | Address form submit not focusable | P1 | C2-003 |
| **G — Screen-reader purchase** | **FAIL** | No headings; duplicated labels; unlabelled KPIs; wrong roles | P1 | C2-018/019/020/021 |
| **H — Seller deletes a product after a delivery** | **FAIL** | Completed order and its ₹65 record destroyed silently | P0 | C2-001 |

---

## 9. Accessibility Findings

| ID | Area | Finding | Evidence | Sev |
|---|---|---|---|---|
| A11Y-001 | Focus visible | Focus is a lime background wash only, never an outline. Measured **#FFFDF8 → #EAF8C3 = 1.10:1**; WCAG 1.4.11 requires 3:1. Styled in only 5 places app-wide, so many controls paint nothing. | pixel measurement from `focus-1.png` / `focus-4.png` | P1 |
| A11Y-002 | Keyboard operable | Address-form submit has no role and no tabindex; label and city chips are not focusable; "Add new address" has `role=button` with no tabindex | live a11y tree | P1 |
| A11Y-003 | Headings | **Zero** heading semantics in the entire app; every title is a `generic` | a11y tree, all screens | P2 |
| A11Y-004 | Name, role, value | Labels emitted 2–3× ("Back Back", "Open account Open account Open account", "Add … to cart" ×3) | a11y tree | P2 |
| A11Y-005 | Name, role, value | Admin KPI tiles announce bare numbers with no labels | a11y tree | P2 |
| A11Y-006 | Name, role, value | Admin section chips use `role="checkbox"` for single-select navigation | a11y tree | P2 |
| A11Y-007 | Target size | Cart quantity ± are 38×38 px (minimum 44×44) | measured | P2 |
| A11Y-008 | Error identification | Validation errors are unstyled `Text`, not associated with fields, no live region, shown before any input | `location_screen.dart:268-283` | P2 |
| A11Y-009 | Resize text | `maximum-scale=1.0, user-scalable=no` blocks pinch-zoom | viewport meta | P3 |
| A11Y-010 | Pause, stop, hide | The login marquee animates continuously with no pause control | `image_marquee.dart:30` | P3 |

**Strength:** reduced motion is handled deliberately in seven places via `MediaQuery.disableAnimationsOf`, including the marquee. Confirmed in code; not behaviourally verified.

**Not tested:** actual screen-reader output (NVDA/VoiceOver/TalkBack). All findings above come from the live accessibility tree and pixel measurement, not from assumption.

---

## 10. Security Findings

| ID | Area | Finding | Verification needed | Sev |
|---|---|---|---|---|
| SEC-001 | Data integrity | Unguarded item delete cascades into `orders` (C2-001) | No — confirmed | P0 |
| SEC-002 | Secrets in client | `web/index.html` ships a hardcoded fallback `firebaseConfig` with a live-looking API key and project id `messages-34023`, which does not match this product's naming | **Yes — developer verification.** Firebase web keys are not strictly secret, but a fallback pointing at an unrelated project suggests a leftover | P2 |
| SEC-003 | Credential handling | `POST /api/admin/riders` returns the PIN in plaintext and the admin UI displays it | **Yes** — confirm it is shown once, not persisted in UI state or logs | P3 |
| SEC-004 | Info disclosure | A buyer's order response includes the rider's raw phone number | Product decision | P4 |

### Confirmed security strengths

These were tested, not assumed:

- **Role separation is enforced server-side.** A buyer token on `/api/admin/*` and `/api/delivery/*` → 401. An admin token on `/api/me` and `/api/orders` → 401. Every protected route rejects an absent token.
- **Rate limiting is well designed and works.** Verified live: 9 failed admin sign-ins pass, the 10th returns `429` with `Retry-After: 879` and *"too many sign-in attempts — try again in a few minutes"*. It is DB-backed (survives restarts, coordinates replicas), counted before password hashing so parallel requests cannot outrun it, keys are hashed so attempted usernames are never stored, denied requests do not extend the lockout, and it covers all three realms — admin, rider and shopper.
- **Passwords use PBKDF2-SHA256, 100k iterations, per-row salt, constant-time compare** — stdlib `crypto/pbkdf2`, no third-party dependency.
- **SQL injection is properly parameterised.** A product titled `Robert'); DROP TABLE orders;--` was stored and rendered as literal text; tables intact.
- **HTML/script payloads are rejected at input.** `<script>alert(1)</script>` as a title → `400 "title contains unsupported characters"`.
- **Order state transitions carry both stage and actor in the WHERE clause**, keeping one seller's panel away from another's orders.
- **The delivery code is returned only to the buyer**, making it real proof of delivery. A wrong code is rejected.
- **The deployed API enforces the email code**; the passwordless local path is a dev flag only.

---

## 11. Performance Findings

| ID | Observation | Trigger | Impact | Sev |
|---|---|---|---|---|
| PERF-001 | **6.6 MB first load**, of which **3.2 MB is seven Lucide icon-font files** (`lucide.ttf` 719 KB + six `LucideVariable-w100…w600` at ~400 KB each). The app uses one weight. Tree-shaking reduced w400 by only 5.1% and kept all six weights. | Every cold load | ~2 min on slow mobile data, for a mobile-first Indian campus product | P1 |
| PERF-002 | **4.3 MB of dead assets ship**: `category-atlas.png` (2.1 MB) and `everyday-campaign.png` (2.2 MB) are referenced nowhere in `lib/`, but `pubspec.yaml` ships `assets/categories/` wholesale. `banner.png` likewise unused. | Every build | Wasted bandwidth and install size | P2 |
| PERF-003 | `vercel.json` caches `/(.*)\.js` for 7 days, but the bundle is `main.dart.js` with **no content hash**. The hand-versioned `category-atlas-v2.png` is the symptom of the same problem on `/assets/(.*)`. | Repeat visits after a deploy | Users run a week-old client against a live API | P2 |
| PERF-004 | Category artwork (~8 MB total) loads with **no placeholder or skeleton** — tiles render as blank white rounded rectangles for several seconds on first paint. | First load | Home looks broken while loading | P3 |
| PERF-005 | `main.dart.js` is 3.35 MB uncompressed. | Every cold load | Parse/compile cost on low-end Android | P3 |
| PERF-006 | The **debug** (DDC) web build crashes on first paint — `Stack Overflow` in `initializeAndLinkLibrary` while loading `login_screen.dart:291` (the Lucide icon library, 571 scripts). | `flutter run -d web-server` | Blocks local development; the release build is unaffected | P4 |
| PERF-007 | An external Roboto woff2 is fetched from `fonts.gstatic.com` despite Inter Tight being the app face; Firebase JS is fetched from `gstatic.com`. | Every load | Third-party dependency + privacy on first paint | P4 |

---

## 12. Repeated Pattern Detection

| Pattern | First finding | Scope confirmed | Action |
|---|---|---|---|
| PAT-001 | Duplicate `labelText`/`hintText` (address form) | Also on the search field | Audit every `InputDecoration` |
| PAT-002 | Off-system hardcoded colour (address screen) | 229 literals, 62 distinct values, 19 of 29 screens | Move to `LamazonTheme`; lint against raw `Color(0xFF` |
| PAT-003 | Duplicated accessibility label (home) | Every screen tested | Fix the shared semantics wrapper |
| PAT-004 | Bottom nav overlapping content (desktop home) | Mobile home, admin, seller dashboard, cart | Add bottom padding equal to nav height + safe area |
| PAT-005 | Non-focusable `GestureDetector` CTA (address form) | "Add new address", label chips, city chip | Audit every `GestureDetector` used as a control |
| PAT-006 | Local-only mutation (`removeItem`) | `adjustStock` identical | Audit every non-`async` mutator in `data/` |
| PAT-007 | Unlabelled/mislabelled semantic role (admin chips) | KPI tiles, admin search field | Audit admin semantics wholesale |
| PAT-008 | Identical fallback glyph per department (Electronics) | Food, Beauty, Stationery, all departments | `category_visual.dart` keys on department, not category |

---

## 13. What's Actually Good

Concrete and demonstrated, not encouragement:

1. **Checkout is transactional and idempotent.** Replaying the same `requestId` returned the original order and created no duplicate — verified against the database. Lines are sorted before locking to prevent deadlocks, stock is checked under `FOR UPDATE`, and the whole basket commits or none of it does.
2. **Rate limiting is textbook.** Fires at exactly 10, returns `429` with `Retry-After`, DB-backed, counted before hashing, keys hashed, denied requests don't extend the lockout, covers all three realms.
3. **Role separation holds.** Every cross-role and unauthenticated probe returned 401.
4. **Input validation is thorough with excellent copy.** Empty and whitespace titles, price 0 / negative / non-numeric / absurdly large, negative stock, MRP below price, 1000-character titles, unknown categories, HTML payloads — all rejected with specific, actionable messages naming the actual bounds.
5. **Category deletion is properly guarded**, with a message that tells the admin exactly what to do first.
6. **The full order lifecycle works**, including proof-of-delivery: the code goes only to the buyer, a wrong code is rejected with *"wrong code — ask the customer to read it out again"*, and delivery increments the rider's count and releases stock.
7. **Cross-surface data consistency is correct.** An order placed in the shopper UI appeared instantly in the admin overview, the seller queue, and the shopper's stock display.
8. **The empty states are the best UI work in the app.** "Your cart is empty" and "No products found" are on-brand, clearly written, and each offers a real next action.
9. **The out-of-stock product state is done right** — red text label *plus* a greyed button, so it never relies on colour alone.
10. **The cart stepper turns into a labelled trash icon at quantity 1** instead of leaving a dead disabled minus. A thoughtful detail.
11. **Admin responsive behaviour genuinely adapts** — eleven chips become a labelled dropdown at 375 px, not a horizontal scroll.
12. **Reduced motion is handled deliberately** in seven places, including the marquee.
13. **The home hero and campaign region are excellent** — art-directed, text in a protected field over a real scrim, working at both breakpoints.
14. **Unicode is handled correctly end to end** — `समोसा 🥟 日本語` stored, listed, searched, ordered and rendered without corruption.

---

## 14. Top 10 Fixes

**#1 — Deleting a product destroys its orders (C2-001)**
Why: irreversible loss of financial and fulfilment records; the single worst outcome in the app.
Fix: `ON DELETE RESTRICT` on `orders.item_id`; count referencing orders in `handleDeleteItem` and return 409 like `handleDeleteCategory` does; add a `delisted` flag for catalogue hygiene.
Priority: **P0.** Regression: all four cascading FKs, store deletion, admin counters, rider counts.

**#2 — Seller delete and restock buttons are fakes (C2-002)**
Why: the seller acts on inventory that isn't real; customers order pulled stock.
Fix: wire `removeItem`/`adjustStock` to `DELETE /api/seller/items/{id}` and `PATCH /api/seller/items/{id}/stock` using the existing optimistic-with-rollback pattern; add confirm + undo. Do not enable delete until #1 lands.
Priority: **P1.** Regression: all seller dashboard counters, shop home, search, cart validation.

**#3 — Keyboard users cannot check out (C2-003)**
Why: closes the entire purchase funnel to keyboard and screen-reader users.
Fix: replace the `GestureDetector` at `location_screen.dart:285` with `ActionButton`; make the label chips a radio group; add tabindex to "Add new address".
Priority: **P1.** Regression: audit every `GestureDetector` used as a control.

**#4 — Focus indicator is invisible (C2-004)**
Why: measured 1.10:1 against a 3:1 requirement; keyboard users cannot see where they are.
Fix: add a 2 px `strong` (`#1D4939`) outline with 2 px offset to the global focus theme; define it once so all controls inherit it.
Priority: **P1.** Regression: every interactive component.

**#5 — Publish the policies (C2-005)**
Why: sign-up currently collects consent against four documents that say they are not published.
Fix: publish terms, privacy, shipping and refunds via the admin policy editor, and show `updatedAt`. Until then, remove the "By continuing, you agree to" line.
Priority: **P1.**

**#6 — Cut 7.5 MB from first load (C2-006, C2-014)**
Why: ~50% of a 6.6 MB payload is unused icon-font weights, plus 4.3 MB of unreferenced images.
Fix: pin Lucide to the one weight in use (or switch to per-icon SVG); replace `assets/categories/` in `pubspec.yaml` with explicit file entries; delete `category-atlas.png`, `everyday-campaign.png`, `banner.png`.
Priority: **P1.** Regression: category artwork, campaign hero, login logo.

**#7 — Cap cart quantity at available stock (C2-007)**
Why: the cart lets a user build a ₹65 basket of an item with stock 1 and fail at the final button.
Fix: disable `+` at `availableStock` with an inline reason; re-validate on cart open.
Priority: **P1.** Regression: checkout `expectedTotal` reconciliation.

**#8 — Stop the bottom nav covering content (C2-008)**
Why: the first row of every department grid is obscured at both breakpoints.
Fix: add bottom padding equal to nav height + safe-area inset on every scroll view behind the nav.
Priority: **P1.** Regression: home, cart, admin, seller dashboard, account.

**#9 — Fix or remove the fallback category tiles (C2-009)**
Why: ~40 tiles show one glyph per *department*, so "Toys & Games" and "Glue & Tape" both get a book icon, each costing ~600 px of scroll and leading to empty results.
Fix: key the glyph on category, or replace the tile grid with a compact text list, or hide departments with no stock.
Priority: **P2.** Regression: home, search, category navigation.

**#10 — Consolidate the palette (C2-024, C2-025)**
Why: 229 hardcoded colours against 200 theme references, 62 distinct values for an 8-colour system, and a search screen that is the exact rainbow dashboard `DESIGN.md` refuses.
Fix: replace `#F1F1EF`/`#6B6B6B`/`#1A1A1A` (87 uses) with `canvas`/`muted`/`text`; move `#D32F2F`/`#2E7D32` into named `danger`/`success` tokens; re-skin the search departments in the forest/ivory system; add a lint against raw `Color(0xFF`.
Priority: **P2.** Regression: all 19 affected screens.

---

## 15. Production Readiness

### MUST FIX BEFORE LAUNCH
1. **C2-001** — item delete cascading into orders (P0)
2. **C2-002** — fake seller delete and restock (P1)
3. **C2-003** — keyboard cannot complete checkout (P1)
4. **C2-004** — invisible focus indicator (P1)
5. **C2-005** — unpublished policies behind a consent gate (P1)
6. **C2-007** — cart accepts more than available stock (P1)
7. **C2-010** — "Test"/"123456789" junk categories live to shoppers (P2, trivial)
8. **C2-042** — verify the hardcoded Firebase fallback config (P2, needs a developer answer)

### SHOULD FIX SOON
C2-006 and C2-014 (payload), C2-008 (nav overlap), C2-009 (fallback tiles), C2-011 (double labels), C2-012 (seller thumbnails), C2-013 (stock mismatch), C2-015 (cache on unhashed bundle), C2-016/017 (manifest and icons), C2-018–022 (accessibility set), C2-023 (undo), C2-024/025 (palette), C2-026 (search affordances), C2-027 (login wall), C2-028 (fake marquee stock), C2-029/030 (address form), C2-043 (legacy order idempotency), C2-046 (debug build crash).

### NICE TO HAVE
Delivery ETA in cart and confirmation; order cancellation in the UI; app version in the account footer; search filters and sorting; loading skeletons; per-category scarcity thresholds; a pause control on the marquee; consolidating the four button styles and four back-button positions.

---

## 16. Final Release Decision

| Area | Score |
|---|---:|
| Functionality | 5/10 |
| UX | 4/10 |
| UI | 4/10 |
| Reliability | 3/10 |
| Accessibility | 3/10 |
| Performance | 4/10 |
| Security posture | 7/10 |
| Production readiness | 2/10 |
| **Overall** | **4/10** |

### Decision: **DO NOT SHIP**

### Exact launch blockers
1. A seller deleting a product permanently destroys every order for it, including delivered orders and their financial record, with no warning and no recovery.
2. The seller's delete and restock controls are client-side illusions — the product stays live and purchasable, and the stock counter drifts from reality on every press.
3. Keyboard-only and screen-reader users cannot save a delivery address, and therefore cannot place an order at all.
4. Sign-up collects agreement to four policies that all state they are not published.

### Conditions for approval
1. `orders.item_id` no longer cascades; `handleDeleteItem` refuses while orders reference the item; a `delisted` path exists for legitimate catalogue hygiene. Verified by repeating the C2-001 reproduction and confirming the delivered order survives.
2. Seller delete and restock hit the API, with confirmation and undo. Verified by pressing each control and querying `/api/products` and the database.
3. The address form is completable with the keyboard alone, and the focus indicator meets 3:1. Verified by a Tab-only run of the full purchase journey and a pixel measurement of the focused state.
4. Policies published, or the consent line removed.
5. The junk categories are deleted, and first-load payload is under ~2.5 MB.

---

## FINAL QA STATEMENT

**If I were the QA lead signing this production release, I would not approve it.**

I want to be precise about why, because this is not a weak build. The backend is the work of someone who has thought carefully about correctness: idempotent checkout under row locks, a rate limiter that counts before hashing and hashes its own keys, order transitions that carry the actor in the WHERE clause, proof-of-delivery codes that only ever reach the buyer, and validation messages better than most shipped products. Category deletion refuses to orphan data and tells the admin exactly how to proceed. That is real engineering.

Which is exactly what makes the blockers indefensible. The same codebase that carefully refuses to delete a category with two items in it will, one endpoint away, delete a product and take a delivered order and its ₹65 with it — silently, returning 204. That is not a philosophy about data; it is an oversight, and the fix is the guard that already exists eight files away.

The second blocker is worse in kind if not in scale. A delete button that does nothing is a bug. A delete button that removes the row from your screen, updates four counters to agree with itself, and leaves the product on sale is a lie the interface tells its own operator. A seller pulls a spoiled item, watches it vanish, and goes back to work while orders keep arriving. Nothing in the UI will ever tell them otherwise.

The third is a matter of who gets to be a customer. Everything up to the address form works with a keyboard. The submit control is a `GestureDetector` — no role, no tabindex, unreachable. One widget swap stands between this app and being operable by people who don't use a mouse, and until it is made, that group cannot buy anything here.

Underneath the blockers is a quieter problem the team should take seriously: the app has a written design system it does not follow. 229 hardcoded colours against 200 theme references, 62 distinct hex values for a system that names 8, and a search screen built as precisely the "rainbow-department dashboard" that `DESIGN.md` opens by refusing. The good screens — the home hero, the empty states, the order confirmation, the admin's responsive collapse — prove the system works when it is used. The debt is not a taste problem; it is 19 screens that stopped reaching for the tokens.

Fix the four blockers and this is a conditional pass. They are small, well-scoped changes: one foreign-key constraint, two API calls, one widget swap, one focus outline. None of them require rethinking anything. The distance between this build and a shippable one is a few days of careful work, not a rewrite — which is the most useful thing I can tell you, and the reason the answer today is still no.

---

# 17. THE GAP TO SHIP — What 4/10 Is Missing, and Exactly What to Fix

This section exists to answer one question directly: **what stands between this build and one I would sign off?**

The short version: **not much code, but the right code.** Nothing here is a rewrite. The blockers are one foreign-key constraint, two missing API calls, one widget swap, one focus outline, and four content jobs. Everything else is debt you can ship with, provided you know you are carrying it.

## 17.1 Why the score is 4/10

The score is not an average of impressions. It is where the points are actually lost:

| What the app does well | What costs it the score |
|---|---|
| The **server** is correct: transactional idempotent checkout, row-locked stock, enforced role separation, real rate limiting, parameterised SQL, guarded category deletes | The **client** does not tell the truth: it reports deletions that did not happen, stock that does not exist, and destroys order history without saying so |
| The **happy path works end to end** — I placed a real order and ran it through accept → assign → pick → deliver | The **unhappy paths are unguarded** — over-ordering stock, deleting a product with orders, and any journey without a mouse |
| The **design system is well written** and the screens that use it are genuinely good | **19 of 29 screens do not use it** — 229 hardcoded colours against 200 theme references |

So: a 7/10 backend, a 4/10 frontend, and a 2/10 production readiness, because readiness is gated by the worst thing that can happen to a user, not the average thing.

## 17.2 The gap, score by score

What each number needs in order to move. This is the actual shopping list.

| Area | Now | Ships at | What closes the gap |
|---|---:|---:|---|
| Functionality | 5 | 7 | Wire seller delete + restock to the API (C2-002). Cap cart quantity at available stock (C2-007). |
| Reliability | 3 | 7 | Stop the order cascade (C2-001). That single constraint is most of this score. |
| Accessibility | 3 | 6 | Make the address form keyboard-completable (C2-003) and give focus a visible 3:1 outline (C2-004). |
| UX | 4 | 6 | Disclose sign-in before the cart, not at the final button. Collapse the two-step address save. Add undo on cart removal. |
| UI | 4 | 6 | Stop the bottom nav covering content (C2-008). Fix or remove the identical fallback tiles (C2-009). Replace the three off-system neutrals (87 uses). |
| Performance | 4 | 7 | Drop the six unused icon-font weights and the 4.3 MB of dead images. 6.6 MB → ~2 MB. |
| Security posture | 7 | 8 | Guard the item delete; get a developer answer on the hardcoded Firebase fallback. |
| **Production readiness** | **2** | **7** | **All four blockers plus the policies.** |

## 17.3 The work, in the order I would do it

Each item lists the exact file, what to change, and what it buys.

---

### FIX 1 — Stop the order cascade · P0 · ~1 hour

**Files:** `backend/schema.sql:54`, `backend/seller.go:423`

This is the highest-value hour of work in the project. Two changes.

**(a)** The constraint. Your schema is embedded and re-run on every boot, and it already uses an idempotent `DROP CONSTRAINT IF EXISTS` / `ADD CONSTRAINT` idiom in six places (`schema.sql:163`, `:226`, `:260`). Follow it exactly:

```sql
ALTER TABLE orders DROP CONSTRAINT IF EXISTS orders_item_id_fkey;
ALTER TABLE orders ADD CONSTRAINT orders_item_id_fkey
  FOREIGN KEY (item_id) REFERENCES inventory_items (id) ON DELETE RESTRICT;
```

**(b)** The guard. `handleDeleteItem` currently fires a bare `DELETE`. Give it the same shape as `handleDeleteCategory`, which already does this correctly one file away:

```go
var live int
a.db.sql.QueryRowContext(r.Context(),
    `SELECT count(*) FROM orders WHERE item_id = $1`, id).Scan(&live)
if live > 0 {
    writeError(w, http.StatusConflict, plural(live, "order")+
        " placed for this product. Hide it instead of deleting it.")
    return
}
```

**Buys you:** the P0, and Reliability 3 → 6. `idx_orders_item` already exists, so the count is cheap.

**Verify:** re-run the C2-001 reproduction. The delivered order must survive and the delete must return 409.

---

### FIX 2 — Add a delist path · P1 · ~3 hours

**Files:** `backend/schema.sql`, `backend/seller.go`, `backend/catalog.go`

Fix 1 makes delete refuse, which is correct but leaves the seller with no way to retire a product. Without this, Fix 1 turns a data-loss bug into a dead end — so the two ship together.

```sql
ALTER TABLE inventory_items ADD COLUMN IF NOT EXISTS delisted BOOLEAN NOT NULL DEFAULT false;
```

Filter `delisted` out of `productFilter` in `db.products()` so it leaves the shop, and keep it visible in the seller's own list with a "Hidden" badge and an "Unhide" action. History survives; the catalogue stays clean.

**Buys you:** the legitimate need behind the delete button, without the destruction.

---

### FIX 3 — Make the seller's controls real · P1 · ~3 hours

**Files:** `frontend/lib/data/seller.dart:396,402`, `frontend/lib/data/api.dart`, `frontend/lib/screens/seller_dashboard_screen.dart:873,878,885`

The backend endpoint you need already exists and already does the right thing. `handlePatchStock` (`backend/seller.go:254`) accepts `{"delta": n}` and floors at zero in SQL with `GREATEST(stock + $2, 0)`, so a racing update cannot go negative. It has **no caller anywhere in the frontend**.

Add the two API methods, then make both mutators `async` and route them through the existing `_move` pattern (`seller.dart:348`), which already does optimistic-update-then-reload-on-failure for `acceptOrder`/`rejectOrder`:

```dart
Future<void> adjustStock(String id, int delta) => _move(id, () async {
  final saved = await Api.instance.patchStock(id, delta: delta);
  _items.firstWhere((i) => i.id == id).stock = saved.stock;
});
```

Then, on the dashboard: put a confirmation dialog behind the trash icon that names the product and any live orders, and show an undo snackbar after.

**Buys you:** the P1, Functionality 5 → 7, and the seller dashboard's six counters stop being fiction.

**Verify:** press each control, then query `/api/products` and the database. The numbers must agree.

---

### FIX 4 — Make checkout keyboard-completable · P1 · ~2 hours

**Files:** `frontend/lib/screens/location_screen.dart:287`, `frontend/lib/screens/addresses_screen.dart:218`

`location_screen.dart:287` wraps the submit in a `GestureDetector`, which gives it no role, no focus, and no Enter/Space handling. Swap it for the app's own `ActionButton` — the one the login screen already uses — which carries semantics and focus for free. Do the same at `addresses_screen.dart:218` for "Add new address".

While you are in the file:
- Make the Home/Office/Other chips a radio group (`location_screen.dart:240`) — they are single-select, not checkboxes.
- Give the error `Text` at `:274` `liveRegion: true` so it is announced.
- Delete `hintText: hint` at `:359`. It duplicates `labelText`, which is why the label prints twice in a focused field (C2-011).

There are four `GestureDetector`s in this file and four in `addresses_screen.dart`. Audit all eight — that is the whole pattern (PAT-005).

**Buys you:** the P1, and the purchase funnel opens to keyboard and screen-reader users.

**Verify:** complete the whole purchase journey with Tab, Enter and Space only. No mouse.

---

### FIX 5 — Give focus a visible outline · P1 · ~1 hour

**File:** `frontend/lib/widgets/design_system.dart:151`

Focus is currently a lime background wash at 36–42% alpha and nothing else. I measured the focused search field at **#FFFDF8 → #EAF8C3, a contrast ratio of 1.10:1**, against WCAG 1.4.11's requirement of 3:1. Controls outside the five code paths that define `focusColor` paint no focus state at all.

Define one focus decoration in the theme — a 2 px `strong` (`#1D4939`) outline at 2 px offset — so every control inherits it instead of each screen deciding. Keep the lime wash if you like it; add the outline underneath it.

**Buys you:** the P1, and Accessibility 3 → 5. It is a theme-level change, so it fixes every screen at once.

---

### FIX 6 — Publish the policies · P1 · content, not code

All four policies — terms, privacy, shipping, refunds — currently return *"This policy is not published yet. Please check back before placing an order."* The login screen sits above the line **"By continuing, you agree to our Terms and Conditions · Privacy Policy"**.

You are collecting consent against documents that do not exist. The admin policy editor already works; this is a writing job, not an engineering one. Show `updatedAt` on the policy screen while you are there — the API already returns it.

If the policies are not ready by launch, remove the consent line rather than pointing it at empty pages.

---

### FIX 7 — Cap the cart at available stock · P1 · ~2 hours

**Files:** `frontend/lib/data/cart.dart`, `frontend/lib/widgets/product_card.dart`

Today the cart will hold **5 units of an item whose card says "Only 1 left"**, quote ₹65 with confidence, and only fail at "Place order". The server does the right thing and refuses — but at the last step of the funnel, which is the worst place to learn.

Disable `+` at `availableStock` with an inline reason, and re-validate the basket when the cart screen opens so a cart that went stale in a background tab corrects itself before the user commits.

---

### FIX 8 — Two content jobs · ~30 minutes total

- **Delete the junk categories.** `Test`, `Work`, `123456789`, `567890` are live, shopper-facing taxonomy under Electronics → Mobile Accessories. The admin delete guard already works; four clicks.
- **Get an answer on the Firebase fallback.** `web/index.html` ships a hardcoded `firebaseConfig` with a live-looking API key for a project called `messages-34023`, which does not match this product's naming. Web keys are not strictly secret, but a fallback pointing at an unrelated project needs a developer to confirm it is intentional and correctly scoped.

---

### FIX 9 — Cut the payload · P1 for launch quality · ~2 hours

**Files:** `frontend/pubspec.yaml`, `vercel.json`

Three changes, ~4.5 MB saved:

1. **3.2 MB of icon fonts.** Seven Lucide files ship (`lucide.ttf` 719 KB plus six `LucideVariable-w100…w600` at ~400 KB each). The app uses one weight; tree-shaking removed 5.1% and kept all six. Pin the single weight, or move to per-icon SVG.
2. **4.3 MB of dead images.** `pubspec.yaml` ships `assets/categories/` wholesale, which includes `category-atlas.png` (2.1 MB) and `everyday-campaign.png` (2.2 MB) — neither is referenced anywhere in `lib/`. `banner.png` is likewise unused. List the files explicitly instead of the directory.
3. **The cache trap.** `vercel.json` caches `/(.*)\.js` for 7 days, but the bundle is `main.dart.js` with no content hash. Returning users can run a week-old client against a live API. Either hash the filename or drop that rule to `no-cache` for the entry bundle. (The hand-versioned `category-atlas-v2.png` is the same problem already biting you on `/assets/`.)

For a mobile-first product on Indian campus data, **6.6 MB → ~2 MB** is the difference between a first load that completes and one that gets abandoned.

---

### FIX 10 — Stop the nav covering content · P2 · ~1 hour

The floating bottom nav overlaps the first row of every department grid at **both** 1280×800 and 375×812, and clips the "Beauty" heading on desktop. Add bottom padding equal to nav height plus safe-area inset to every scroll view that sits behind it — home, cart, admin, seller dashboard, account (PAT-004).

---

## 17.4 Definition of done

I would sign off when all of these are demonstrable, not asserted:

- [ ] Delete a product that has a **delivered** order. The order still appears for the buyer, for the seller, and in the admin overview. The delete returns 409.
- [ ] A seller can hide a product from the shop and unhide it, with its history intact.
- [ ] Press the seller's trash and `±`. Query `/api/products` and the database. The UI and the server agree.
- [ ] Complete a purchase — browse, add, address, place order — using **only** Tab, Enter and Space.
- [ ] Screenshot a focused control. Measure it. ≥ 3:1 against its unfocused state.
- [ ] All four policies return real text with a visible last-updated date.
- [ ] Add-to-cart stops at available stock, with a visible reason.
- [ ] `/api/categories` contains no test data.
- [ ] First load under ~2.5 MB, measured cold.
- [ ] No screen has content permanently hidden behind the bottom nav at 375 px or 1280 px.

**Estimated effort for the whole list: roughly 3–4 focused days,** most of it in Fixes 2, 3 and 9. Fixes 1, 5 and 8 together are under three hours and move Reliability, Accessibility and trust more than anything else on the list.

## 17.5 What you do NOT need to fix to ship

Scope discipline matters as much as the fix list. **None of the following should hold the release**, and I would push back on anyone who tried to bundle them in:

- The 229 hardcoded colours and 62 hex values. Real debt, worth a dedicated pass, but it makes the app inconsistent rather than wrong. Add a lint against raw `Color(0xFF` so it stops growing, then pay it down per-screen.
- The rainbow search departments, the four button styles, the four back-button positions. Same category — book them, do not gate on them.
- Search filters, sorting and result counts. Missing features, not broken ones, and with two products in the catalogue nobody will notice yet.
- The duplicated accessibility labels ("Back Back"). Annoying to screen-reader users; not blocking, because the labels are at least present and correct.
- The missing headings, the `role="checkbox"` admin chips, the 38 px touch targets. Fix in an accessibility sprint after launch, alongside a real screen-reader pass — which I did **not** do and which you should not skip.
- The debug-build stack overflow. It blocks local development, not users. Worth an afternoon, not a release slot.
- The order-confirmation "Order order-6", the zero-padded "05" quantity, the missing ETA. Polish. Do them when you are next in those files.

## 17.6 The one-line answer

**What is missing is not features — it is the frontend matching the backend's honesty.**

The server already refuses to delete a category that would orphan data, already rejects an order it cannot fulfil, already tells a rider exactly why a code was wrong. The client, one layer up, reports deletions that never happened, stock that does not exist, and quietly destroys order history on the way past. Close that gap — four blockers, roughly three days — and this ships.


---

# 18. RETEST LOG — 2026-09-10, 23:15 IST

The working tree moved after this report was written. HEAD advanced `519ebf0` → `3a0009a` (3 commits), with further uncommitted work in progress (a `seasons` feature touching `main.go`, `schema.sql`, `home_screen.dart`, `storefront.dart`).

Per §17's living-register rule, the original findings above are **left intact**. Their current status is recorded here.

## 18.1 Blocker status

| ID | Was | Now | Evidence |
|---|---|---|---|
| **C2-001** — order cascade | P0 OPEN | **FIXED** (code-verified) | `schema.sql:270` now re-declares the FK as `ON DELETE RESTRICT` via the idempotent ALTER idiom, over the original `CREATE TABLE` cascade at `:54`. `handleDeleteItem` counts orders first and returns 409: *"N orders placed for this product. Hide it from the shop instead — deleting it would take the orders with it."* |
| **C2-002** — fake seller controls | P1 OPEN | **FIXED** (code-verified) | `removeItem` (`seller.dart:442`) and `adjustStock` (`:486`) are now `async` and call `Api.instance.deleteItem` / `patchStock`. Both API methods now exist (`api.dart:585`, `:558`). The dashboard consumes a `failure` return at `seller_dashboard_screen.dart:868`. |
| **C2-003** — keyboard checkout | P1 OPEN | **FIXED** (code-verified) | The `GestureDetector` is gone. `location_screen.dart:312` is now `ActionButton(label:…, expand: true, loading: _saving, onPressed:…)`. `GestureDetector` count in the two address files dropped 8 → 3 (remaining uses are not CTAs). |
| **C2-004** — invisible focus | P1 OPEN | **FIXED** (code-verified) | `design_system.dart:51-59` adds `focusOutline = 2.0`, `focusColour = strong`, `focusBorder`, and a `focusSide()` helper applied through `WidgetState.focused`. The code comment cites the 1.10:1 measurement and states `strong` on `surface` measures ~8:1. |
| **C2-005** — unpublished policies | P1 OPEN | **STILL OPEN** | `GET /api/policies` returns *"This policy is not published yet"* for all of `terms`, `privacy`, `shipping`, `refunds` — and now a fifth, `contact`. Sign-up still collects consent against documents that do not exist. |

## 18.2 Also closed

| ID | Now | Note |
|---|---|---|
| C2-011 — duplicate `labelText`/`hintText` | **FIXED** | `hintText: hint` removed; only `labelText: hint` remains at `location_screen.dart:364`. |
| C2-029 — two-step address submit | **FIXED** | "Check availability" → "Save address" collapsed to one button. The comment gives the reasoning: the serviceability check now runs off the city selection. |
| Missing loading state (§6) | **PARTIALLY FIXED** | The address CTA now has a real `loading: _saving` state and a "Saving…" label. Other async actions not re-checked. |

## 18.3 Beyond the report

A **delist path** was added — the thing §17 Fix 2 argued had to ship alongside Fix 1, so that refusing the delete would not leave sellers with a dead end:

- `schema.sql:281` — `delisted BOOLEAN NOT NULL DEFAULT false`
- `db.go:195` — the shop query filters `AND NOT i.delisted`
- `PATCH /api/seller/items/{id}/listing` and `PATCH /api/admin/items/{id}/listing`
- Covered by `item_delete_test.go` and `admin_items_test.go`, including an assertion that *"a delisted item is still in the shop"* fails

`go test ./...` passes (2.5s, uncached, across the delete/listing/stock/order suites).

## 18.4 What this changes about the verdict

**Four of five launch blockers are closed, with tests.** That is the bulk of §17's Fixes 1–5, delivered in about twelve hours.

**The gate does not lift yet**, for two reasons — neither of them a criticism of the work:

1. **C2-005 is still open.** It is the one blocker that is a writing job rather than an engineering one, and it is the easiest to overlook precisely because no code change will close it.
2. **Every fix above is code-verified, not behaviourally verified.** §17.4 asks for these to be *demonstrable, not asserted* — delete a product with a delivered order and watch the order survive; press the seller's `±` and confirm the database agrees; complete a purchase with Tab and Enter only; measure a focused control at ≥3:1. I read the diffs and ran the Go suite; I did not re-run the browser journeys against this tree.

Also note the tree currently mixes these fixes with **unfinished `seasons` work**, so what I inspected is not a clean release candidate.

**Revised status: NO-GO → CONDITIONAL,** pending the policies and a behavioural retest pass against a clean tree.

