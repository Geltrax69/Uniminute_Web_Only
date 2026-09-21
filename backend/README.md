# Lamazon API

Go 1.22+ stdlib routing (`net/http`, no framework) over **PostgreSQL**, via
pgx. The schema applies itself on boot and the catalog seeds once, so a fresh
database is ready without any manual step.

## Running everything

```bash
./dev.sh              # from the repo root: Postgres + API + the app in Chrome
./dev.sh macos        # any flutter device id
PUBLIC=1 ./dev.sh     # use the Cloudflare tunnel / deployed API hostname
PORT=8081 ./dev.sh    # if 8080 is taken
```

It starts (or reuses) the Postgres container, waits for it, builds and starts
the API, blocks until `/api/health` answers, and only then launches Flutter —
so the app never opens onto a backend that is still compiling. Quitting Flutter
stops the API; Postgres stays up for next time.

The app calls the local API by default. Use `PUBLIC=1 ./dev.sh` when you
explicitly want to exercise the Cloudflare tunnel / deployed hostname.

## Running the pieces by hand

```bash
docker run -d --name lamazon-pg \
  -e POSTGRES_USER=lamazon -e POSTGRES_PASSWORD=lamazon -e POSTGRES_DB=lamazon \
  -p 5433:5432 postgres:16-alpine

set -a && source ../.env && set +a
go run .              # http://localhost:8080
go test ./...         # runs against DATABASE_URL, skips if no database
```

`DATABASE_URL` defaults to
`postgres://lamazon:lamazon@localhost:5433/lamazon?sslmode=disable`.
Razorpay checkout also requires `RAZORPAY_KEY_ID` and
`RAZORPAY_KEY_SECRET`; `dev-full-stack.sh` loads both from the ignored
repository-root `.env` automatically.

## Calling it from Flutter

The app reads `API_BASE_URL`, defaulting to `http://localhost:8080`:

```bash
cd ../frontend
flutter run -d chrome                                   # localhost
flutter run --dart-define=API_BASE_URL=http://192.168.1.5:8080   # phone on wifi
```

`loadCatalog()` and `loadShops()` call the API and fall back to the bundled
sample data when it is unreachable, so the app still runs with the backend
down — and so widget tests do not need a server.

## Photos

Uploads go to **Cloudinary**; Postgres keeps the URLs (`seller_stores.photo_url`,
`inventory_items.image_urls`). Credentials come from the environment —
`CLOUDINARY_CLOUD_NAME`, `CLOUDINARY_API_KEY`, `CLOUDINARY_API_SECRET`, kept in
the gitignored `.env` at the repo root and loaded by `dev.sh`. Unset, the API
still runs and the two upload routes answer 503.

Everything lands under one folder per store:

```
Lamazon/
  Campus_Snacks/
    store_image
    Campus_Snacks_Cold_Coffee_300ml_1
    Campus_Snacks_Cold_Coffee_300ml_2
```

Non-alphanumeric characters in a store or item name become `_`, so a name
cannot invent a folder. Re-uploading a store photo overwrites `store_image`;
item photos append and keep counting from what the item already has.

## Notifications

Two channels, one call. `notify()` sends the email **always** — it needs
nothing installed and works on every phone — and adds a browser notification
wherever the person allowed one. Neither can fail an order: problems are
logged and the request carries on.

Web push uses Firebase Cloud Messaging. The website reads its public browser
configuration from the API and registers `/firebase-messaging-sw.js`; the
server uses service-account credentials to send through FCM.

```
FIREBASE_CREDENTIALS_JSON                              # service account JSON
GOOGLE_APPLICATION_CREDENTIALS                         # or path to that JSON
FIREBASE_WEB_PUSH_PUBLIC_KEY                           # web push certificate public key
FIREBASE_WEB_CONFIG                                    # Firebase web config JSON
# Or provide the web config as individual values:
FIREBASE_API_KEY
FIREBASE_AUTH_DOMAIN
FIREBASE_PROJECT_ID
FIREBASE_STORAGE_BUCKET
FIREBASE_MESSAGING_SENDER_ID
FIREBASE_APP_ID
```

Browser registration needs the public key plus the four required web values
(`apiKey`, `projectId`, `messagingSenderId`, and `appId`). Test pushes and real
order pushes also need `FIREBASE_CREDENTIALS_JSON` or
`GOOGLE_APPLICATION_CREDENTIALS`; without server credentials, notifications
quietly go by email only.

Worth knowing before relying on it:

- **iOS Safari delivers push only after the site is added to the Home Screen.**
  Android Chrome and desktop work straight after the permission prompt.
- Nothing sends while this API is asleep. An order at 3am notifies no one
  until it is back up.
- FCM tokens that answer 404 or 410 are deleted, so dead browsers are not
  retried on every order.

## Endpoints

| Method | Path | Purpose |
|---|---|---|
| GET | `/api/health` | liveness |
| GET | `/api/products` | catalog; `?tab=`, `?category=`, `?q=` stack |
| GET | `/api/products/{id}` | one product |
| GET | `/api/shops` | shops; `?tab=` |
| GET | `/api/shops/{name}/products` | a shop's own listings **plus** items it stocks, at its price |
| GET | `/api/locations` | where delivery runs |
| GET | `/api/locations/check?city=` | serviceability; accepts `LPU`, `lpu, phagwara`, any casing |
| POST | `/api/login` | `{"email"}` — mails a six-digit code |
| POST | `/api/login/verify` | `{"email","code"}` — returns an access + refresh token |
| POST | `/api/login/refresh` | `{"refreshToken"}` — rotates the pair |
| GET | `/api/push/key` | web-push public key for Firebase Messaging |
| GET | `/api/push/config` | public Firebase browser config + web-push key |
| POST/DELETE | `/api/push/subscribe` | register / drop this browser's FCM token (needs a session) |
| POST | `/api/delivery/push/subscribe` | register a signed-in rider for assignment alerts |
| GET | `/api/seller/categories` | categories a seller can list under |
| POST/GET | `/api/seller/store` | open / read the seller's store |
| POST | `/api/seller/store/photo` | multipart `file` — store cover, to Cloudinary |
| GET/POST | `/api/seller/items` | inventory + summary / add a line |
| PATCH | `/api/seller/items/{id}/stock` | `{"delta"}` or `{"stock"}`, floors at 0 |
| POST | `/api/seller/items/{id}/photos` | multipart `file` (repeatable) — product photos |
| DELETE | `/api/seller/items/{id}` | remove a line |
| GET/POST | `/api/seller/orders` | orders + stage counts / place one |
| POST | `/api/seller/orders/{id}/accept` | accept, issue delivery code, and assign a rider |
| POST | `/api/seller/orders/{id}/reject` | reject with a customer-visible reason |
| POST | `/api/delivery/orders/{id}/pick` | rider confirms collection |
| POST | `/api/delivery/orders/{id}/deliver` | verify the customer's code and complete delivery |

## Rules worth knowing

- The enforced lifecycle is **received → accepted → picked → delivered**;
  rejection is terminal from received.
- Accepting generates the customer's four-digit delivery code and assigns the
  least-loaded active rider. Delivering with that code removes units from stock.
- Stock floors at zero; a delivery can never drive it negative.
- Delivering twice returns 409 rather than double-decrementing.
- A store outside the serviceable area is refused at creation.

## Where the rules live

Some invariants sit in the schema rather than in Go, so no code path can get
around them:

- `inventory_items.stock` has `CHECK (stock >= 0)`; updates use `GREATEST(..., 0)`
- `inventory_items.owner` is a foreign key to `seller_stores`, which is what
  makes "no stock without a store" a 409 instead of an orphan row
- `orders.stage` is constrained to received, accepted, rejected, picked, or delivered
- Delivering runs the order update and the stock decrement in one transaction

## Sign-in

A six-digit code by email through Resend. Codes come from `crypto/rand`, are
stored only as a sha256, are single use, allow five attempts (counted in the
statement that reads them, so scripting cannot race the limit), and one send
per address per minute.

Verifying returns an access token (1 hour) and a refresh token (30 days).
Both are stored hashed. **The refresh token rotates on every use**, so a
stolen one is good for a single call. Everything under `/api/seller/` requires
a live token.

## Not built yet

No payments. Nothing prunes expired rows from `auth_sessions` or
`login_codes`, and deleting an item leaves its Cloudinary photos behind.
