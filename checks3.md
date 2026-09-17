# CHECK 3 — Production QA / UI / UX Release Gate

**Release decision: NO-GO — but for one new, narrow reason, not the four old ones.**

Every launch blocker from CHECK 2 has been genuinely fixed and verified behaviourally. A new P1 was introduced in the same window.

---

## 0. Test Control, Scope & Evidence

| Field | Value |
|---|---|
| Build / Version | `534f11b` (18 commits after the CHECK 2 baseline `519ebf0`; 100 files, +8626/−1324) |
| Build tested | **Release** web build, plus `tool/prune_fonts.py` so the bundle matches what Vercel ships |
| API | Rebuilt and restarted at `534f11b` (the running instance was 4 commits stale) |
| Test date | 2026-09-11 |
| Browser | Chromium via Playwright |
| Viewports | 1280×800 desktop, 375×812 mobile |
| Roles tested | Guest, Shopper, Seller, Admin, Delivery rider |
| Catalogue | 42 real products across 2 approved stores (previously empty) |
| Evidence | 22 screenshots in `qa-evidence/checks3/` |
| Test suites | `flutter test` — **140 passed**; `go test ./...` — **passed** (6.9s, uncached) |

### Environment limitations (declared)

1. `SKIP_LOGIN_CODE=1` locally; the deployed API enforces the real code flow (verified in CHECK 2). Not reported as a defect.
2. **A correction to CHECK 2:** that report quoted a 6.6 MB cold first load. That figure was an undercount — it excluded two 2 MB PNGs that happened to be cache hits during measurement. The true CHECK 2 payload was comparable to today's. **The payload did not regress; my earlier number was wrong.**
3. Rapid programmatic typing dropped 9 leading characters from the login field. Discrete keypresses work correctly, and a synthetic paste event cannot trigger the browser's default insert, so I **could not determine** whether real clipboard paste or password-manager autofill is affected. Logged as OBSERVED, not CONFIRMED.
4. **Not tested:** wishlist/Saved screen, compare screen, shop detail screen, notifications, settings, help, profile setup, admin photo manager, push delivery, CSV export, photo cropper, order cancellation, rider UI screens (rider **API** lifecycle was fully tested in CHECK 2 and is unchanged).
5. No real screen-reader pass (NVDA/VoiceOver/TalkBack). All accessibility findings come from the live accessibility tree and pixel measurement.

### Residue left in the test environment

User `qa3.buyer@lamazon.test` + address `addr-4`; order `order-7` (2 × cake, ₹1613, stage `received`). Test seasons `qa-live`/`qa-overlap` were created and **deleted**; stock levels for `item-42` (cake) and `item-9` (Cheese Corn Sandwich) were changed during testing and **restored** to 10 and 20.

---

## 1. Executive Summary

CHECK 2 closed with four launch blockers and a 4/10. All four are fixed, and the fixes are better than what I asked for.

The order-cascade P0 is closed twice over — a `RESTRICT` foreign key *and* an explicit guard on both the seller and the newly-added admin delete path, with a `delisted` flag so sellers can still retire a product, all covered by tests. The seller's phantom controls now hit the API and I watched stock move 20→21 on the server. The cart caps at available stock with the best piece of microcopy in the product ("That is all the shop has"). The focus ring, where it is applied, measures **10.02:1** against a requirement of 3:1. The palette debt fell from 229 hardcoded colours to 98 — and a ratchet test now makes it impossible to grow back.

Two entirely new features arrived in the same window and both are built carefully. The remote-banner fetcher is a textbook SSRF defence: I threw thirteen vectors at it — loopback, AWS metadata, CGNAT `100.64/10`, IPv4-mapped IPv6, DNS rebinding via `nip.io` — and every one was refused, because the check runs on the *dialled address* after DNS, not on the URL string. Seasons refuse any colour pair below 4.5:1 contrast, server-side.

Against that, one regression blocks release. The product-card, category-tile and admin-chip rework **stripped `tabindex` from interactive elements across the app**. On the home screen, 16 of 21 buttons are unreachable by keyboard: every product card, every category tile. The add-to-cart `+` has no semantic representation at all — it is not in the accessibility tree in any form. In the admin panel, only *Refresh* and *Sign out* are focusable; all eleven section chips lost their role and their tabindex.

CHECK 2's blocker C2-003 was "keyboard users cannot save an address, so they cannot order". That is fixed. But the funnel is now severed one step earlier: **keyboard users cannot reach a product to add it in the first place.**

### Ratings

| Area | CHECK 2 | CHECK 3 | Basis |
|---|---:|---:|---|
| Functionality | 5 | **7** | Stock cap, seller controls, delist, seasons, banner media all verified working |
| UX | 4 | **6** | ETA, order numbers, result counts, sort, clear, recovery snackbar, confirm dialog |
| UI | 4 | **6** | Real per-category art, uniform cards, labels wrap, palette halved; nav overlap and ragged rows remain |
| Reliability | 3 | **8** | P0 closed on both paths, FK + guard + delist + tests; 140 frontend tests green |
| Accessibility | 3 | **3** | Labels and roles much improved, focus ring excellent where applied — cancelled out by the tabindex regression |
| Performance | 4 | **5** | −2.44 MB fonts, −4.3 MB dead assets; still 8.64 MB cold, dominated by two 2 MB PNGs |
| Security posture | 7 | **8** | SSRF defence, season contrast guard, delete guard on both paths |
| Production readiness | 2 | **5** | One P1 blocker, down from four |

**Overall Score: 6/10** (was 4/10)

### Would you ship this to real users today?

**NO.**

One reason, and it is narrow: a keyboard user cannot add a product to the cart from any browsing surface, because the cards and tiles they would need to reach are no longer in the tab order, and the add-to-cart control does not exist in the accessibility tree. That is a smaller, more tractable problem than the four blockers it replaced — the elements are still `role="button"` with correct labels, they have simply lost `tabindex`. But until it is fixed, an entire class of user cannot buy anything, and the app was closer to keyboard-operable a fortnight ago than it is today.

---

## 2. Critical Problems (release blockers)

### [C3-001] Product cards, category tiles and admin chips are no longer keyboard-reachable

**Status:** CONFIRMED · **Severity:** P1 · **Category:** Accessibility, Regression
**Route:** `/` (home), search results, `/admin` · **Component:** product card, category tile, admin section chip
**Frequency:** Every time · **User impact:** Severe · **Business impact:** High · **Discoverability:** Hidden · **Recoverability:** Impossible without a pointer

**Preconditions:** None. Any browsing surface.

**Steps to reproduce:**
1. Open the home screen.
2. Enumerate the accessibility tree: `document.querySelectorAll('flt-semantics[role="button"]')` and read each node's `tabindex`.
3. Press Tab repeatedly and observe where focus lands.

**Expected:** Every `role="button"` is in the tab order and activatable with Enter or Space.

**Actual (measured on the live tree):**

| Surface | `role="button"` | Focusable | Not focusable |
|---|---:|---:|---|
| Home | 21 | **5** | 4 product cards + 12 category tiles |
| Search results ("burger") | 7 | **3** | all 4 result cards |
| Search results ("cake") | 4 | **3** | the 1 result card |
| Admin dashboard | 8 | **2** | 6 KPI tiles; all 11 section chips have no role *and* no tabindex |

On home, the only focusable controls are the header, the campaign hero, the two carousel arrows, two "See all" links and the four nav tabs.

Separately, and worse: on a search result page the **complete** interactive inventory is `Back`, `Clear search`, `Best match`, the unfocusable card, and the four nav tabs. **There is no add-to-cart node in the accessibility tree in any form** — no role, no label, no element. The `+` is painted but does not exist to assistive technology. The same is true of the wishlist heart.

**Regression evidence:** CHECK 2 measured these same elements as `role="button"` with `tabindex="0"` and full labels ("Add समोसा 🥟 日本語 to cart", "Save product"). The loss coincides with the card/tile rework in commits `0f86685`, `34462ac`, `3a104a5`, `8638256`.

**Impact:** A keyboard-only or screen-reader user can search, sort and read results, then cannot select or add anything. The purchase funnel is closed to them one step earlier than it was in CHECK 2. Admin is almost entirely keyboard-inoperable — section navigation is unreachable.

**Note on scope:** The **product detail page is fine** — Back, Save product, Open cart, Increase quantity, Add to Cart and Buy Now are all `tabindex="0"`. The defect is specific to card grids, tiles and chips.

**Suggested fix:** Restore `tabindex="0"` by giving these widgets real focusable semantics — `InkWell`/`Semantics(button: true, focusable: true)` rather than a bare tap handler. Expose the `+` and the heart as their own `Semantics` buttons inside the card, as they were in CHECK 2. Add a widget test asserting a minimum focusable count per surface, in the same spirit as `palette_ratchet_test.dart`.

**Regression scope:** Every card grid; the category strip; admin chips and KPI tiles; the shops list; any other control reworked in those four commits.

---

### [C3-002] The floating bottom nav still covers content at both breakpoints

**Status:** CONFIRMED · **Severity:** P2 (P1 on mobile) · **Category:** UI · **Was:** C2-008, unfixed
**Route:** `/` home, `/store` · **Frequency:** Every time

**Measured at 375×812:** the nav occupies y=728–800. Every product card in the "Around you" row spans y=621→812 or beyond.

| Card | Card bottom | Pixels hidden behind nav |
|---|---:|---:|
| Aloo Tikki Burger | 812 | **84** |
| Aloo Cheese Burger | 812 | **84** |
| DELL 15 (2025) | 812 | **84** |
| cake | 903 | **175** |

The hidden band contains the price row and the add-to-cart button. At 1280×800 the same defect clips the "Fresh picks with a real saving" subtitle and the "Food" section heading.

**Suggested fix:** Add bottom padding equal to nav height + safe-area inset to every scroll view behind the nav. Unchanged recommendation from CHECK 2.

**Regression scope:** home, cart, seller dashboard, account, admin.

---

## 3. CHECK 2 Blocker Verification

The point of this pass. All verified behaviourally, not by reading diffs.

| ID | CHECK 2 | CHECK 3 | Evidence |
|---|---|---|---|
| **C2-001** order cascade (P0) | OPEN | **FIXED** | Placed order #0007 (2 × cake, ₹1613). `DELETE /api/seller/items/item-42` → **409**; `DELETE /api/admin/items/item-42` → **409**. Message: *"1 order placed for this product. Hide it from the shop instead — deleting it would take the orders with it."* Order survived; `orders` count unchanged at 4. |
| **C2-002** fake seller controls (P1) | OPEN | **FIXED** | Clicked "Add one to stock" in the seller UI → server went 20 → 21 and propagated to `/api/products` in the same breath. Delete now opens a confirmation dialog, and a refused delete raises a snackbar with a **"Hide instead"** action button. |
| **C2-003** keyboard checkout (P1) | OPEN | **FIXED at the address form** | The `GestureDetector` is gone; the CTA is an `ActionButton` with `loading` state, and the two-step "Check availability → Save address" is collapsed to one. *Superseded upstream by C3-001.* |
| **C2-004** invisible focus (P1) | OPEN | **PARTIAL** | Where applied: `#1E4A3A` ring measured **10.02:1** against the white card — exceeds WCAG's 3:1. Where not applied (home search field): still a lime fill at **1.10:1** with no ring. |
| **C2-005** unpublished policies (P1) | OPEN | **DOWNGRADED to P3** | All five policies still return *"This policy is not published yet"* — but the login screen's consent claim is gone, replaced by a neutral **"Read our"**. The app no longer asserts agreement to a non-existent contract, which was the legal half of the finding. |
| **C2-007** cart accepts over-stock (P1) | OPEN | **FIXED** | With stock set to 2, the quantity `+` becomes `aria-disabled="true"` and its label changes to **"That is all the shop has"**. Enforced on both the detail page and the cart. Totals recalculated correctly (₹1598 + ₹15 = ₹1613). |
| **C2-008** nav overlap (P1) | OPEN | **STILL OPEN** | See C3-002. |
| C2-009 identical category glyphs | OPEN | **FIXED** | Every category tile now carries its own photograph — Earphones shows AirPods, Batteries shows batteries, Laptops shows laptops. |
| C2-011 duplicate label/hint | OPEN | **FIXED** | `hintText: hint` removed; no field prints its label twice. |
| C2-012 seller thumbnails blank | OPEN | **FIXED** | Every inventory row renders its real product photo; products genuinely lacking one show an explicit placeholder. |
| C2-013 seller/shopper stock mismatch | OPEN | **FIXED** | Seller now sees **"Sold out · 0 to sell · 2 in orders"** — reserved stock made explicit, matching what shoppers see. |
| C2-014 dead assets | OPEN | **FIXED** | `category-atlas.png` and `everyday-campaign.png` (4.3 MB) removed from the build. |
| C2-019 duplicated a11y labels | OPEN | **FIXED** | Cards announce once, as one sentence: *"Aloo Tikki Burger, ₹69, 13 percent off, from PURE BITES"* — and "13 percent off" is spelled out for speech. |
| C2-020 unlabelled admin KPIs | OPEN | **FIXED** | Now "People: 7", "Sellers: 3", "Orders: 4". |
| C2-021 admin chips `role="checkbox"` | OPEN | **FIXED (then regressed)** | No longer checkboxes, now in a labelled `group "Admin section"` — but they lost their role and tabindex entirely. See C3-001. |
| C2-024 palette debt | OPEN | **SUBSTANTIALLY FIXED** | 229 → **98** literals; 200 → **411** theme refs. All five worst offenders (`#F1F1EF`, `#6B6B6B`, `#1A1A1A`, `#D32F2F`, `#2E7D32`) now at **zero**. A ratchet test caps further growth. |
| C2-026 search affordances | OPEN | **FIXED** | Truthful result count ("4 results for \"burger\""), a clear (×) button, and a 4-option sort that I verified sorts correctly (₹129, ₹119, ₹89, ₹69 descending). |
| C2-027 "Skip login" dev-speak | OPEN | **FIXED** | Now **"Browse the shop"**. |
| C2-032 raw order id | OPEN | **FIXED** | Now **"Order #0007"**. |
| C2-036 admin empty-state mess | OPEN | **FIXED** | Three contradictory messages and the dead Previous/Next replaced by one clean empty state. |
| C2-039 missing ETA | OPEN | **FIXED** | "Arrives in about 12 mins" in the cart and "Arriving in about 12 mins" on the confirmation. |
| C2-040 zero-padded quantity | OPEN | **FIXED** | Now "2", not "02". |
| C2-045 truncated category labels | OPEN | **FIXED** | Labels wrap to two lines; all nine fully readable. |
| C2-047 inconsistent card sizing | OPEN | **PARTIAL** | Card boxes are now uniform (166×282). The **images inside** still vary in height and top offset — measured tops of 117/137/152/117 px within a single row. |
| C2-048 scarcity cries wolf | OPEN | **FIXED** | 20-unit items now read "In stock", not "Low stock". |
| Snackbar over the nav bar | OPEN | **FIXED** | Snackbar now sits at y=610, clear of the nav at 728. |

---

## 4. Complete Issue Register

| ID | Status | Sev | Category | Route / Feature | Problem | Impact | Evidence |
|---|---|---|---|---|---|---|---|
| C3-001 | CONFIRMED | P1 | Accessibility | Home, search, admin | Cards, tiles and chips lost `tabindex`; add-to-cart absent from the a11y tree entirely | Keyboard users cannot add anything to a cart | §2 |
| C3-002 | CONFIRMED | P2 | UI | Home, seller dashboard | Floating nav hides 84–175 px of every product card at 375 px | Price and add button obscured | measured |
| C3-003 | CONFIRMED | P2 | Performance | First load | **8.64 MB cold**, of which `category-atlas-v2.png` (2.03 MB) + `campaign-forest.png` (1.97 MB) = 4 MB of unoptimised PNG | Slow first load on mobile data | measured from disk |
| C3-004 | CONFIRMED | P2 | UI | Seasons | A season re-skins **only the header**; hero, tiles and nav stay forest green, so navy-on-forest reads as a rendering fault | Feature looks broken when used | `20-season-active.png` |
| C3-005 | CONFIRMED | P3 | Data/Logic | Seasons | Overlapping season windows are accepted silently and the later one wins, with no warning or stated priority | Admin cannot predict which season shows | API |
| C3-006 | CONFIRMED | P3 | Content | Policies | All five policies still unpublished (`terms`, `privacy`, `shipping`, `refunds`, `contact`) | Empty pages linked from login | API |
| C3-007 | CONFIRMED | P3 | UI | Search / home cards | Images within a row vary in height and top offset (117/137/152 px) despite uniform card boxes | Ragged grid | `06`, `08` |
| C3-008 | CONFIRMED | P3 | Accessibility | Home search field | Focus is a lime fill at **1.10:1**, no ring, where other controls get a 10:1 ring | Inconsistent focus visibility | measured |
| C3-009 | CONFIRMED | P3 | Content | Seasons API | JSON decode failure returns bare `"invalid season"`, well below this codebase's own standard | Unhelpful for API clients | `seasons.go:105` |
| C3-010 | CONFIRMED | P3 | Data | Catalogue | A 194-character product title exists despite a 160-char cap on create and update — legacy data was never migrated or flagged | Title dominates the a11y label; truncates in UI | `item-21` |
| C3-011 | CONFIRMED | P3 | Content | Product detail | Seller-uploaded photos are screenshots containing another app's UI chrome; nothing flags or rejects them | Looks unprofessional on the core buying page | `12-product-detail.png` |
| C3-012 | CONFIRMED | P4 | UI | Product detail | Portrait images letterbox against a muddy olive-brown fill that is not in the design palette | Off-brand | `12` |
| C3-013 | CONFIRMED | P4 | UI | Search sort menu | Popup sits flush to the viewport edge (x=1105→1280) with zero gutter, unlike every other element | Looks clipped | measured |
| C3-014 | CONFIRMED | P4 | UI | Product detail | "That is all the shop has" tooltip overlaps the "About this product" heading | Minor collision | `13` |
| C3-015 | CONFIRMED | P4 | Content | Prices | Large amounts render without separators — `₹59999`, `₹88690` rather than `₹59,999`, `₹88,690` | Harder to read at a glance | `02`, `16` |
| C3-016 | CONFIRMED | P4 | UI | Seller dashboard | "Orders (4)" active tab is pure black — off-palette | Inconsistent | `16` |
| C3-017 | CONFIRMED | P4 | UX | Cart | "Cash on delivery" still presented as a selectable card with a checkmark despite being the only option | Fake choice | `14` |
| C3-018 | CONFIRMED | P4 | UI | Admin | 11 section chips still mix navigation with filters; "Banners" still the only chip without a count | IA confusion | `22` |
| C3-019 | OBSERVED | P3 | Bug | Login field | Rapid programmatic typing dropped the first 9 characters; discrete keypresses fine; paste not conclusively testable | Possible autofill/paste failure | **needs developer verification** |
| C3-020 | OBSERVED | P2 | Security | `web/index.html` | Hardcoded fallback `firebaseConfig` for project `messages-34023` — carried over from CHECK 2 (C2-042), still unresolved | Unknown | **needs developer verification** |

---

## 5. New Feature Assessment

Two substantial features shipped since CHECK 2. Both were tested adversarially.

### 5.1 Remote banner media — `POST /api/admin/campaign-media`

An admin pastes a link; the server fetches it, resolves Open Graph tags if it is a web page, and re-hosts the result. **This is our server making a request to an address a stranger chose**, so it is the highest-risk surface added in this window.

**Every vector I tried was refused:**

| Vector | Result |
|---|---|
| `https://127.0.0.1:8080/api/health` | 400 — private address |
| `https://localhost:8080/...` | 400 — private address |
| `https://169.254.169.254/latest/meta-data/` (cloud metadata) | 400 — private address |
| `https://10.0.0.1/`, `https://192.168.1.1/` | 400 — private address |
| `https://100.64.0.1/` (CGNAT — `IsPrivate()` misses this) | 400 — private address |
| `https://[::ffff:127.0.0.1]/` (IPv4-mapped IPv6) | 400 — private address |
| `https://[::1]/`, `https://0.0.0.0/` | 400 — private address |
| **`https://127.0.0.1.nip.io/`** (DNS rebinding) | **400 — private address** |
| **`https://169.254.169.254.nip.io/`** | **400 — private address** |
| `http://`, `file://`, `gopher://` | 400 — https only |
| `https://user:pass@example.com/` | 400 — userinfo rejected |
| 2100-character URL | 400 — too long |
| A real public image | 200 — **re-hosted to Cloudinary**, not hotlinked |

The DNS-rebinding cases are the ones that matter, and they pass because `publicOnly` is a `Dialer.Control` hook that inspects the **address actually being dialled**, after resolution — not the hostname in the URL. Redirects are re-checked per hop, the body is capped at 2 MB, and timeouts are bounded. Re-hosting to Cloudinary also means shoppers' browsers never touch the third-party origin.

**Verdict: CONFIRMED STRENGTH.** This is better than most production implementations of the same feature.

### 5.2 Seasons — schedule a festival theme without a deploy

**Validation is strong.** End-before-start and equal dates are refused with *"The season has to end after it starts."*; names are capped at 40 characters; colours must be hex. Most importantly, **colour contrast is enforced server-side**:

> `#FFFFFF` ground with `#FFFFFF` ink → 400: *"The text colour is too close to the ground to read. Pick a lighter or darker one — it needs a contrast ratio of 4.5, for the same reason road signs do."*

That is a real WCAG 2.1 luminance calculation (`backend/seasons.go:192`) guarding a self-service theming control, mirrored client-side in `campaign_palette.dart`. Most products ship this feature with no guardrail at all. **CONFIRMED STRENGTH.**

**Two problems:**
- **C3-004:** only the header re-skins. I set a navy ground and the shop became a navy header bolted onto an otherwise forest-green page — the hero, category tiles and nav all stayed forest. The result reads as a bug, not a theme.
- **C3-005:** overlapping seasons are accepted silently and the later one wins, with no conflict warning and no stated priority.

---

## 6. UI Audit

**Hierarchy.** Home is materially better: the header lost three controls, category labels wrap and are fully readable, and "Around you — Fresh picks with a real saving" leads with real stock and real savings. Product cards now put price first with a struck-through MRP and a percentage — a clear, scannable hierarchy.

**Layout and spacing.** The remaining defects are rhythm, not structure. Card boxes are uniform (166×282 measured), but the images inside are not: tops staggered at 117/137/152 px in a single row, so `+` buttons and price rows do not line up across a grid. Four search results occupy 632 px of a 1280 px viewport, leaving 648 px empty. The sort menu sits flush to the viewport edge with no gutter.

**Typography.** Inter Tight applied consistently. Large amounts still lack thousands separators (`₹59999`, `₹88690`).

**Colour.** The headline improvement of this release. Hardcoded literals fell 229 → 98; theme references rose 200 → 411; the five worst off-system neutrals are eliminated outright. `palette_ratchet_test.dart` caps literals at 60 and distinct hex values at 35 outside the two legitimate palette files, with a comment that says to lower the numbers as screens migrate and never raise them. That is the correct engineering response to design debt.

**States.** Disabled is now excellent — the stock-capped `+` is greyed, `aria-disabled`, and carries a reason. Loading exists on the address CTA. Empty states remain strong. Error recovery is the standout: a refused delete produces a snackbar that both explains and offers the fix as a button.

**Trust signals.** Junk categories ("Test", "123456789") are gone from the tree. The login marquee now draws largely on real catalogue photography. Against that, C3-011 — product photos that are screenshots of another app, complete with its toolbar — undercuts the core buying page.

---

## 7. UX Audit

**Task efficiency — buy something.** Materially shorter than CHECK 2. Real stock appears above the fold under "Around you"; departments now show genuine per-category photography instead of ~40 identical glyphs leading to empty results. The detail page carries everything a decision needs: price, MRP, saving, availability, delivery cost, ETA, payment method, description.

**Feedback.** The order confirmation is strong — "Order #0007", amount, address, ETA, and two clear exits. The stock cap explains itself on the control. The delete guard explains itself and offers the alternative in one tap.

**Error recovery.** Best in the product:

> *"1 order placed for this product. Hide it from the shop instead — deleting it would take the orders with it."* · **[Hide instead]**

WHAT → WHY → WHAT NEXT, with the next step as a button. The confirmation dialog even pre-warns about the guard before the user commits.

**Remaining friction.** The login wall still precedes any product on `/`. Cash on delivery is still a fake single-option selector. Overlapping seasons give an admin no way to predict what shoppers will see.

---

## 8. Admin Findings

Assessed as a separate product, admin improved and regressed in the same pass.

**Better:** "Signed in as admin" resolves who is operating. KPI tiles collapsed to one row and now announce labelled values. The three stacked contradictory empty-state messages and the dead Previous/Next pagination are gone, replaced by a single clean empty state. A new "Products (42)" section gives admin the whole catalogue with delete, listing and stock controls — and the new delete path carries the same order guard as the seller path, verified with a 409.

**Worse:** the section chips went from `role="checkbox"` (wrong role, but focusable) to **no role and no tabindex** (unreachable). The six KPI tiles are `role="button"` with no tabindex. On the entire admin dashboard only **Refresh** and **Sign out** can be reached by keyboard. Admin is now effectively pointer-only.

---

## 9. User Journey Results

| Journey | Result | Failure point | Severity | Notes |
|---|---|---|---|---|
| A — First-time visitor browses and buys | **PASS** | — | — | Login wall remains, but "Browse the shop" is honest and real stock is immediately visible |
| B — Returning shopper places an order | **PASS** | — | — | Sign in → detail → quantity → Buy Now → cart → Place order → "Order #0007". Clean throughout |
| C — Stock cap under pressure | **PASS** | — | — | Capped at 2 with "That is all the shop has"; totals correct |
| D — Seller manages inventory | **PASS** | — | — | Stock `+` moved the server 20→21; delete confirmed, guarded and recoverable |
| E — Seller deletes a product with a live order | **PASS** | — | — | 409 on both seller and admin paths; order survived; "Hide instead" offered |
| F — Delist and relist | **PASS** | — | — | Hidden from shop (42→41) and from search (0 results); order untouched; relist restored it |
| G — Admin manages catalogue | **PARTIAL** | Keyboard-inoperable | P1 | Works by mouse; section chips unreachable by keyboard |
| H — Admin schedules a season | **PARTIAL** | Only the header re-skins | P2 | Validation and contrast guard excellent; visual result incoherent |
| I — Admin pastes a banner link | **PASS** | — | — | 13 SSRF vectors refused; valid image re-hosted |
| **J — Keyboard-only purchase** | **FAIL** | Cannot focus any product card or add-to-cart control | **P1** | C3-001 |
| K — Screen-reader purchase | **FAIL** | Add-to-cart absent from the a11y tree | P1 | Labels are otherwise much improved |

---

## 10. Accessibility Findings

| ID | Area | Finding | Evidence | Sev |
|---|---|---|---|---|
| A11Y-001 | Keyboard operable | Cards, tiles and chips have `role="button"` with no `tabindex`; add-to-cart and wishlist have no semantic node at all | live a11y tree, 4 surfaces | **P1** |
| A11Y-002 | Focus visible | Ring measures **10.02:1** where applied — but the home search field still gets only a **1.10:1** fill with no ring | pixel measurement | P3 |
| A11Y-003 | Name, role, value | **Fixed** — cards announce once as one sentence; percentages spelled out for speech | a11y tree | — |
| A11Y-004 | Name, role, value | **Fixed** — admin KPIs announce "People: 7" rather than "7" | a11y tree | — |
| A11Y-005 | Menus | Sort menu uses correct `role="menu"` / `role="menuitem"` with a Dismiss control | a11y tree | — |
| A11Y-006 | Disabled state | Stock-capped `+` is `aria-disabled="true"` with an explanatory label — not colour alone | a11y tree | — |
| A11Y-007 | Headings | Still **zero** heading semantics anywhere in the app | a11y tree | P2 |
| A11Y-008 | Label length | A 194-character product title becomes the entire accessible name of its card | `item-21` | P3 |

Reduced-motion handling remains implemented in code; still not behaviourally verified.

---

## 11. Performance Findings

| ID | Observation | Impact | Sev |
|---|---|---|---|
| PERF-001 | **Cold first load 8.64 MB** (production-equivalent, after `prune_fonts.py`): `main.dart.js` 3.44 MB, `category-atlas-v2.png` 2.03 MB, `campaign-forest.png` 1.97 MB, `lucide.ttf` 718 KB, `InterTight` 567 KB | Slow on mobile data | P2 |
| PERF-002 | **Fixed:** `prune_fonts.py` drops 6 unused icon-font weights, −2.44 MB, and is wired into the Vercel build with a documented skip-if-no-python3 fallback | — | — |
| PERF-003 | **Fixed:** 4.3 MB of unreferenced assets removed from the build | — | — |
| PERF-004 | The two remaining PNGs are now the dominant cost — 4 MB of decorative atlas imagery, unoptimised, fetched on first paint | Biggest remaining win | P2 |
| PERF-005 | `tool/audit_images.py` now exists to police asset weight | — | — |

**Correction to CHECK 2:** that report's "6.6 MB" was an undercount that excluded these two PNGs as cache hits. The payload has not regressed; the earlier figure was wrong.

---

## 12. What's Actually Good

Demonstrated, not inferred:

1. **The order-cascade P0 is closed properly** — `RESTRICT` FK *and* an explicit guard on both delete paths, plus a `delisted` alternative, plus tests. Verified against a live order on both routes.
2. **The delete-guard recovery loop** — confirmation dialog that pre-warns, a refusal that explains, and a "Hide instead" action button in the snackbar.
3. **"That is all the shop has"** — a disabled control that says why, on the control.
4. **SSRF defence** — 13 vectors refused including CGNAT, IPv4-mapped IPv6 and DNS rebinding, because the check runs post-resolution on the dialled address.
5. **Season contrast guard** — WCAG 4.5:1 enforced server-side on admin-supplied colours, with a memorable explanation.
6. **The palette ratchet** — debt cut 57%, worst offenders eliminated, and a test that makes regrowth fail CI.
7. **Search** — truthful counts, working sort (verified descending), clear button, correct menu semantics.
8. **Per-category photography** — 40 identical glyphs replaced by real, distinct images.
9. **Seller stock truth** — "Sold out · 0 to sell · 2 in orders" makes reserved stock legible.
10. **140 frontend tests and a green backend suite**, including regression tests aimed squarely at CHECK 2's findings (`stock_cap_test.dart`, `item_delete_test.go`, `palette_ratchet_test.dart`, `disabled_control_test.dart`).

---

## 13. Top 5 Fixes

**#1 — Restore keyboard reach to cards, tiles and chips (C3-001)** · P1
Why: keyboard and screen-reader users cannot add anything to a cart. Fix: give these widgets focusable semantics and re-expose the `+`/heart as their own `Semantics` buttons; add a per-surface focusable-count test. Regression scope: every card grid, the category strip, admin chips and KPI tiles.

**#2 — Stop the nav covering content (C3-002)** · P2
Why: 84–175 px of every product card is unreachable at 375 px, including the add button. Fix: bottom padding = nav height + safe area on every scroll view behind it.

**#3 — Optimise the two atlas PNGs (C3-003)** · P2
Why: 4 MB of the 8.64 MB cold load is two decorative images. Fix: WebP/AVIF with a PNG fallback, correctly sized per breakpoint; `audit_images.py` already exists to enforce it.

**#4 — Make a season dress the whole shop (C3-004)** · P2
Why: re-skinning only the header produces a clash that reads as a bug. Fix: thread `ground`/`accent`/`ink` through the hero, tiles and nav, or scope the feature honestly to a header banner.

**#5 — Publish the policies (C3-006)** · P3
Why: five linked pages that say they are not published. No longer a consent problem — now a completeness one. Fix: write them; the editor works.

---

## 14. Production Readiness

### MUST FIX BEFORE LAUNCH
1. **C3-001** — keyboard cannot reach products or add to cart (P1)

### SHOULD FIX SOON
C3-002 (nav overlap), C3-003 (payload), C3-004 (season coverage), C3-005 (overlapping seasons), C3-008 (search-field focus ring), C3-020 (verify the Firebase fallback), A11Y-007 (headings).

### NICE TO HAVE
C3-006 policies, C3-007 image raggedness, C3-009 decode message, C3-010 legacy title, C3-011 screenshot photos, C3-012–018 (polish), C3-019 (verify paste).

---

## 15. Final Release Decision

| Area | Score |
|---|---:|
| Functionality | 7/10 |
| UX | 6/10 |
| UI | 6/10 |
| Reliability | 8/10 |
| Accessibility | 3/10 |
| Performance | 5/10 |
| Security posture | 8/10 |
| Production readiness | 5/10 |
| **Overall** | **6/10** |

### Decision: **DO NOT SHIP — one blocker**

### Exact launch blocker
1. Keyboard and screen-reader users cannot focus a product card, a category tile, or any add-to-cart control, and the add-to-cart button has no representation in the accessibility tree at all.

### Conditions for approval
1. Every `role="button"` on a browsing surface is in the tab order; the `+` and the heart are their own labelled, focusable nodes. Verified by completing a purchase with Tab, Enter and Space alone.
2. A widget test asserts a minimum focusable count per surface, so this cannot silently regress again.
3. No product card has content hidden behind the nav at 375 px.

---

## FINAL QA STATEMENT

**If I were the QA lead signing this release, I would not approve it — but I would say so with considerably more optimism than a fortnight ago.**

CHECK 2 found a shop that destroyed completed orders when a seller tidied their catalogue, lied to that seller about their own stock, and could not be operated without a mouse. All of that is gone, and it is gone properly. The cascade is closed by a foreign key *and* a guard *and* a delist path *and* tests, on both the old seller route and a new admin one. The seller's phantom buttons now move real numbers, behind a confirmation that warns you what will happen and a refusal that hands you the alternative as a button. Someone read that report closely and fixed the cause rather than the symptom.

The two new features are the strongest evidence of that. A server-side URL fetcher is the kind of thing that ships with an SSRF hole nine times out of ten; this one refused every vector I had, including DNS rebinding, because the check was put in the one place that actually works. A self-service theming panel normally ships with no contrast guard at all; this one refuses white-on-white with a line about road signs. That is not luck.

Which is why the blocker is frustrating rather than damning. The card rework improved almost everything about those cards — one clean spoken sentence instead of a stuttered triple, real photography, uniform boxes, price-first hierarchy — and dropped `tabindex` on the way past. The add-to-cart button did not get a worse label; it stopped existing for assistive technology entirely. A keyboard user in CHECK 2 could browse products and fill a cart and then hit a wall at the address form. Today they hit the wall earlier, at the first product they try to select. The wall moved; it did not come down.

That is a smaller problem than the four it replaced, and the shape of the fix is known. But it is the same class of defect: work that improves what most users see while quietly removing what some users depend on. The palette ratchet test is the right instinct applied to colour — a number that can only go down, checked in CI. The same instinct applied to focusable elements would have caught this before I did, and is the second condition of my approval for exactly that reason.

Fix the tab order and I will sign this. Everything else on the list is work you can do after launch with a clear conscience.
