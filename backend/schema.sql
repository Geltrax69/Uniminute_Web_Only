-- Idempotent: safe to run on every boot. ponytail: one file instead of a
-- migration tool, which earns its keep only once schemas start changing
-- under live data.

CREATE TABLE IF NOT EXISTS products (
    id          TEXT PRIMARY KEY,
    name        TEXT           NOT NULL,
    category    TEXT           NOT NULL,
    tab         TEXT           NOT NULL DEFAULT 'All',
    price       NUMERIC(10, 2) NOT NULL CHECK (price >= 0),
    image_url   TEXT           NOT NULL,
    store       TEXT           NOT NULL,
    description TEXT           NOT NULL DEFAULT ''
);

-- A product sold by another vendor at their own price.
CREATE TABLE IF NOT EXISTS offers (
    product_id TEXT           NOT NULL REFERENCES products (id) ON DELETE CASCADE,
    store      TEXT           NOT NULL,
    price      NUMERIC(10, 2) NOT NULL CHECK (price >= 0),
    PRIMARY KEY (product_id, store)
);

CREATE TABLE IF NOT EXISTS shops (
    name      TEXT PRIMARY KEY,
    tagline   TEXT NOT NULL DEFAULT '',
    image_url TEXT NOT NULL DEFAULT '',
    tab       TEXT NOT NULL DEFAULT 'All'
);

-- One store per seller, keyed by the email they signed in with.
CREATE TABLE IF NOT EXISTS seller_stores (
    owner      TEXT PRIMARY KEY,
    name       TEXT   NOT NULL,
    location   TEXT   NOT NULL,
    city       TEXT   NOT NULL,
    categories TEXT[] NOT NULL DEFAULT '{}'
);

CREATE TABLE IF NOT EXISTS inventory_items (
    id          TEXT PRIMARY KEY,
    owner       TEXT           NOT NULL REFERENCES seller_stores (owner) ON DELETE CASCADE,
    title       TEXT           NOT NULL,
    description TEXT           NOT NULL DEFAULT '',
    category    TEXT           NOT NULL DEFAULT '',
    price       NUMERIC(10, 2) NOT NULL CHECK (price > 0),
    -- The floor lives in the schema too, so no code path can drive stock
    -- negative even by accident.
    stock       INTEGER        NOT NULL DEFAULT 0 CHECK (stock >= 0)
);

CREATE TABLE IF NOT EXISTS orders (
    id         TEXT PRIMARY KEY,
    item_id    TEXT           NOT NULL REFERENCES inventory_items (id) ON DELETE CASCADE,
    item_title TEXT           NOT NULL,
    units      INTEGER        NOT NULL CHECK (units > 0),
    amount     NUMERIC(10, 2) NOT NULL,
    stage      TEXT           NOT NULL CHECK (stage IN ('received', 'accepted', 'delivered')),
    placed_at  TIMESTAMPTZ    NOT NULL DEFAULT now()
);

-- Everyone who has ever signed in. The row is created on first verified code
-- and never disappears, so a returning person is recognised rather than
-- re-registered.
--
-- The public id is what a person sees and quotes at support; the email stays
-- the key everything else joins on.
CREATE SEQUENCE IF NOT EXISTS user_ids START 1001;
CREATE TABLE IF NOT EXISTS users (
    email      TEXT PRIMARY KEY,
    public_id  TEXT        NOT NULL UNIQUE DEFAULT 'LMZ-' || nextval('user_ids'),
    name       TEXT        NOT NULL DEFAULT '',
    phone      TEXT        NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- The address book, one row per saved address. Kept per person rather than
-- per device so it follows them to a new browser.
CREATE TABLE IF NOT EXISTS addresses (
    id         TEXT PRIMARY KEY,
    email      TEXT        NOT NULL REFERENCES users (email) ON DELETE CASCADE,
    label      TEXT        NOT NULL DEFAULT 'Home',
    line       TEXT        NOT NULL,
    city       TEXT        NOT NULL,
    pincode    TEXT        NOT NULL DEFAULT '',
    name       TEXT        NOT NULL DEFAULT '',  -- who receives it
    phone      TEXT        NOT NULL DEFAULT '',  -- and on what number
    is_default BOOLEAN     NOT NULL DEFAULT false,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE SEQUENCE IF NOT EXISTS address_ids;
ALTER TABLE addresses ALTER COLUMN id SET DEFAULT 'addr-' || nextval('address_ids');
CREATE INDEX IF NOT EXISTS idx_addresses_email ON addresses (email);

-- One live sign-in code per address; a resend replaces the row.
CREATE TABLE IF NOT EXISTS login_codes (
    email      TEXT PRIMARY KEY,
    code_hash  TEXT        NOT NULL, -- sha256, so the table never holds a usable code
    expires_at TIMESTAMPTZ NOT NULL,
    sent_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    attempts   INTEGER     NOT NULL DEFAULT 0
);

-- Sessions hold hashes, never the tokens themselves: a leaked database dump
-- cannot be used to sign in. The access token is short-lived and the refresh
-- token is rotated on every use, so a stolen one is good for one call at most.
DROP TABLE IF EXISTS sessions; -- superseded by the shape below
CREATE TABLE IF NOT EXISTS auth_sessions (
    access_hash        TEXT PRIMARY KEY,
    refresh_hash       TEXT        NOT NULL UNIQUE,
    email              TEXT        NOT NULL,
    expires_at         TIMESTAMPTZ NOT NULL,
    refresh_expires_at TIMESTAMPTZ NOT NULL,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_sessions_email ON auth_sessions (email);

-- One row per browser that agreed to notifications. The endpoint stores the
-- Firebase Messaging token with an internal fcm: prefix.
CREATE TABLE IF NOT EXISTS push_subscriptions (
    endpoint   TEXT PRIMARY KEY,
    email      TEXT        NOT NULL,
    p256dh     TEXT        NOT NULL DEFAULT '',
    auth       TEXT        NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_push_email ON push_subscriptions (email);
ALTER TABLE push_subscriptions
    ALTER COLUMN p256dh SET DEFAULT '',
    ALTER COLUMN auth SET DEFAULT '';

-- Photos live in Cloudinary; Postgres keeps the URLs. Added with ALTER so an
-- existing database picks them up on the next boot.
ALTER TABLE seller_stores
    ADD COLUMN IF NOT EXISTS photo_url TEXT NOT NULL DEFAULT '';
ALTER TABLE inventory_items
    ADD COLUMN IF NOT EXISTS image_urls TEXT[] NOT NULL DEFAULT '{}';

-- 'login' or 'reset': a password-reset code cannot sign anyone in, and a
-- sign-in code cannot change a password.
ALTER TABLE login_codes
    ADD COLUMN IF NOT EXISTS purpose TEXT NOT NULL DEFAULT 'login';

-- Postgres hands out ids, not the clock: two requests in the same microsecond
-- used to generate the same primary key and one of them lost.
CREATE SEQUENCE IF NOT EXISTS inventory_item_ids;
CREATE SEQUENCE IF NOT EXISTS order_ids;
ALTER TABLE inventory_items
    ALTER COLUMN id SET DEFAULT 'item-' || nextval('inventory_item_ids');
ALTER TABLE orders
    ALTER COLUMN id SET DEFAULT 'order-' || nextval('order_ids');

-- A store is not a storefront until a person has looked at it. New stores
-- arrive pending and stay invisible to shoppers, and their owner cannot add
-- stock, until an admin approves them.
--
-- The two ALTERs are deliberate: adding the column with 'approved' backfills
-- every store that existed before review was a thing (they are already live,
-- and pending them would empty the catalogue), then the default flips so
-- everything opened from now on waits.
ALTER TABLE seller_stores
    ADD COLUMN IF NOT EXISTS status TEXT NOT NULL DEFAULT 'approved';
ALTER TABLE seller_stores
    ALTER COLUMN status SET DEFAULT 'pending';
ALTER TABLE seller_stores
    ADD COLUMN IF NOT EXISTS reject_reason TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS reviewed_at TIMESTAMPTZ;
ALTER TABLE seller_stores DROP CONSTRAINT IF EXISTS seller_stores_status_check;
ALTER TABLE seller_stores ADD CONSTRAINT seller_stores_status_check
    CHECK (status IN ('pending', 'approved', 'rejected'));

-- Whoever runs Lamazon. Passwords are PBKDF2-SHA256 with a per-row salt, so
-- the table never holds anything usable. Seeded from ADMIN_USER /
-- ADMIN_PASSWORD at boot, never from source.
CREATE TABLE IF NOT EXISTS admins (
    username   TEXT PRIMARY KEY,
    pass_hash  TEXT        NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- A delivery rider, created by an admin who types in their number. The PIN is
-- generated here and shown to the admin once; the rider signs in with number
-- and PIN at /delivery.
CREATE TABLE IF NOT EXISTS riders (
    phone      TEXT PRIMARY KEY,
    name       TEXT        NOT NULL DEFAULT '',
    pin_hash   TEXT        NOT NULL,
    active     BOOLEAN     NOT NULL DEFAULT true,
    delivered  INTEGER     NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Staff sign-ins (admin and rider) share one table: same shape, same
-- expiry, and one place to look when a token has to be revoked.
CREATE TABLE IF NOT EXISTS staff_sessions (
    token_hash TEXT PRIMARY KEY,
    role       TEXT        NOT NULL CHECK (role IN ('admin', 'rider')),
    subject    TEXT        NOT NULL, -- admin username, or rider phone
    expires_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- An order carries who it is for, frozen at the moment it was placed: a later
-- edit to the address book must not redirect a bag already on its way.
-- store_owner is copied for the same reason, and is what every seller-side
-- query filters on, so one seller can never touch another's order.
ALTER TABLE orders
    ADD COLUMN IF NOT EXISTS buyer_email      TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS store_owner      TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS store_name       TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS receiver_name    TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS receiver_phone   TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS receiver_address TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS reject_reason    TEXT NOT NULL DEFAULT '',
    -- The four digits the buyer reads out at the door. Kept in the clear
    -- because the buyer has to be able to read it back in the app; it is only
    -- ever sent to them, never to the rider, which is what makes typing it
    -- proof that the two of them met.
    ADD COLUMN IF NOT EXISTS delivery_code    TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS rider_phone      TEXT NOT NULL DEFAULT '',
    -- Who the admin wants on this one. Separate from rider_phone, which is
    -- only set when a rider actually has the bag: an order can be spoken for
    -- before anyone has picked it up. Empty means anyone may take it.
    ADD COLUMN IF NOT EXISTS assigned_to      TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS accepted_at      TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS picked_at        TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS delivered_at     TIMESTAMPTZ;

-- Rejected, picked: the stages the workflow gained. Dropped first so the
-- file stays runnable on every boot.
ALTER TABLE orders DROP CONSTRAINT IF EXISTS orders_stage_check;
ALTER TABLE orders ADD CONSTRAINT orders_stage_check
    CHECK (stage IN ('received', 'accepted', 'rejected', 'picked', 'delivered'));

-- Orders placed before the column existed still belong to someone; the item
-- says who, and without this they would vanish from their seller's list.
UPDATE orders o SET store_owner = i.owner
FROM inventory_items i WHERE i.id = o.item_id AND o.store_owner = '';
UPDATE orders o SET store_name = s.name
FROM seller_stores s WHERE s.owner = o.store_owner AND o.store_name = '';

CREATE INDEX IF NOT EXISTS idx_orders_buyer ON orders (buyer_email);
CREATE INDEX IF NOT EXISTS idx_orders_store ON orders (store_owner);
CREATE INDEX IF NOT EXISTS idx_orders_rider ON orders (rider_phone);

CREATE INDEX IF NOT EXISTS idx_offers_product ON offers (product_id);
CREATE INDEX IF NOT EXISTS idx_items_owner ON inventory_items (owner);
CREATE INDEX IF NOT EXISTS idx_orders_item ON orders (item_id);
-- ponytail: plain indexes for the tab/category filters. The ?q= search still
-- scans; add pg_trgm when the catalog outgrows a few thousand rows.
CREATE INDEX IF NOT EXISTS idx_products_tab ON products (tab);
CREATE INDEX IF NOT EXISTS idx_products_category ON products (category);
-- Placing an order sums this seller's outstanding units for one item.
CREATE INDEX IF NOT EXISTS idx_orders_item_stage ON orders (item_id, stage);

-- What the item costs before the discount. Zero means the seller did not set
-- one, which is the honest default: every existing row predates the field, and
-- backfilling it from price would invent a 0% discount on all of them.
--
-- The check is what stops a "discount" that raises the price. Equal is allowed
-- so a seller can clear a sale by matching the two rather than by knowing to
-- type a zero.
ALTER TABLE inventory_items
    ADD COLUMN IF NOT EXISTS mrp NUMERIC(10, 2) NOT NULL DEFAULT 0;
ALTER TABLE inventory_items DROP CONSTRAINT IF EXISTS inventory_items_mrp_check;
ALTER TABLE inventory_items ADD CONSTRAINT inventory_items_mrp_check
    CHECK (mrp = 0 OR mrp >= price);

-- An order is a financial record, not a detail of the product it names. An
-- item delete used to take every order for it with it — including delivered
-- ones — so the constraint now refuses instead. handleDeleteItem counts the
-- referencing orders first and says so, the way handleDeleteCategory does.
ALTER TABLE orders DROP CONSTRAINT IF EXISTS orders_item_id_fkey;
ALTER TABLE orders ADD CONSTRAINT orders_item_id_fkey
    FOREIGN KEY (item_id) REFERENCES inventory_items (id) ON DELETE RESTRICT;

-- The store cascade reached orders the same way, one table further out.
ALTER TABLE inventory_items DROP CONSTRAINT IF EXISTS inventory_items_owner_fkey;
ALTER TABLE inventory_items ADD CONSTRAINT inventory_items_owner_fkey
    FOREIGN KEY (owner) REFERENCES seller_stores (owner) ON DELETE RESTRICT;

-- Refusing the delete leaves a seller with no way to retire a product, so
-- this is the way out: the row stays, its history stays, and the shop stops
-- listing it. Delisting is what the trash icon does once orders exist.
ALTER TABLE inventory_items
    ADD COLUMN IF NOT EXISTS delisted BOOLEAN NOT NULL DEFAULT false;

-- The departments across the top of the shop, and the categories under each.
-- One table: a department is a row with no parent, a category is a row whose
-- parent names one. Two tables would duplicate the name, the ordering and
-- every query that walks them.
--
-- The name is the key because that is what products already store — orders,
-- inventory and seller stores all reference a category by its text. Which is
-- also why there is no rename: it would orphan every row pointing at the old
-- one, and an admin cannot be expected to know that.
CREATE TABLE IF NOT EXISTS catalog_categories (
    name      TEXT PRIMARY KEY,
    parent    TEXT    NOT NULL DEFAULT '',
    icon      TEXT    NOT NULL DEFAULT '',
    colour    TEXT    NOT NULL DEFAULT '',
    image_url TEXT    NOT NULL DEFAULT '',
    position  INTEGER NOT NULL DEFAULT 0
);

-- A picture beats a glyph on the shop's category grid, and the icon set is a
-- fixed list an admin cannot add to. Empty means fall back to the icon.
ALTER TABLE catalog_categories
    ADD COLUMN IF NOT EXISTS image_url TEXT NOT NULL DEFAULT '';

-- The five the app shipped with, so a fresh install has a shop rather than an
-- empty navigation bar. Only when the table is empty: this runs at every
-- boot, and re-adding a department the admin deleted would make deletion look
-- like it silently failed.
INSERT INTO catalog_categories (name, parent, icon, colour, position)
SELECT * FROM (VALUES
    ('Electronics', '', 'headphones', '#2F6FED', 1),
    ('Grocery',     '', 'carrot',     '#43A047', 2),
    ('Food',        '', 'utensils',   '#FF8A3D', 3),
    ('Gifts',       '', 'gift',       '#9C6ADE', 4),
    ('Beauty',      '', 'brush',      '#F06292', 5)
) AS seed
WHERE NOT EXISTS (SELECT 1 FROM catalog_categories);

CREATE INDEX IF NOT EXISTS idx_categories_parent
    ON catalog_categories (parent, position);

-- What a shopper picks before buying: size, colour, flavour, whatever the shop
-- sells by. JSONB rather than option and value tables, because nothing queries
-- across them — an item's options are read with the item and written with it,
-- and two more tables would buy joins we would never use.
--
-- Shape: [{"name":"Size","kind":"text","values":["S","M","L"]}]
-- kind is "text" or "colour"; a colour's values are hex, so the app can draw
-- swatches instead of spelling "Maroon".
ALTER TABLE inventory_items
    ADD COLUMN IF NOT EXISTS options JSONB NOT NULL DEFAULT '[]'::jsonb;
ALTER TABLE inventory_items
    ADD COLUMN IF NOT EXISTS variant_prices JSONB NOT NULL DEFAULT '[]'::jsonb;

-- The Food menu, three deep: the department, its sections, and what sits in
-- each. Seeded only when Food has nothing under it yet, so an admin who
-- reshapes the menu keeps their version across restarts.
--
-- A name appears once. It is the primary key, and products reference a
-- category by that text — two rows called "Fried Rice" would leave an item
-- filed under it unable to say which one it meant. Where the menu listed a
-- name twice it is filed under the section it belongs to most: Kulcha with
-- the breads, Fried Rice with Indo-Chinese, and South Indian as a section of
-- its own rather than also a line in Breakfast.
INSERT INTO catalog_categories (name, parent, position)
SELECT * FROM (VALUES
    ('Breakfast', 'Food', 1),
    ('Chole Bhature', 'Breakfast', 1),
    ('Poori Bhaji', 'Breakfast', 2),
    ('Paratha', 'Breakfast', 3),
    ('Street Food', 'Food', 2),
    ('Chaat', 'Street Food', 1),
    ('Gol Gappe', 'Street Food', 2),
    ('Samosa', 'Street Food', 3),
    ('Kachori', 'Street Food', 4),
    ('Pav Bhaji', 'Street Food', 5),
    ('Vada Pav', 'Street Food', 6),
    ('Main Course', 'Food', 3),
    ('Paneer', 'Main Course', 1),
    ('Dal', 'Main Course', 2),
    ('Mixed Vegetables', 'Main Course', 3),
    ('Kofta', 'Main Course', 4),
    ('Curry', 'Main Course', 5),
    ('Indian Breads', 'Food', 4),
    ('Naan', 'Indian Breads', 1),
    ('Kulcha', 'Indian Breads', 2),
    ('Roti', 'Indian Breads', 3),
    ('Lachha Paratha', 'Indian Breads', 4),
    ('Bhature', 'Indian Breads', 5),
    ('Rice', 'Food', 5),
    ('Jeera Rice', 'Rice', 1),
    ('Pulao', 'Rice', 2),
    ('Biryani', 'Rice', 3),
    ('Thali', 'Food', 6),
    ('Veg Thali', 'Thali', 1),
    ('Deluxe Veg Thali', 'Thali', 2),
    ('Punjabi Thali', 'Thali', 3),
    ('South Indian Thali', 'Thali', 4),
    ('Mini Thali', 'Thali', 5),
    ('Special Thali', 'Thali', 6),
    ('South Indian', 'Food', 7),
    ('Dosa', 'South Indian', 1),
    ('Uttapam', 'South Indian', 2),
    ('Idli', 'South Indian', 3),
    ('Vada', 'South Indian', 4),
    ('Indo-Chinese', 'Food', 8),
    ('Noodles', 'Indo-Chinese', 1),
    ('Fried Rice', 'Indo-Chinese', 2),
    ('Manchurian', 'Indo-Chinese', 3),
    ('Spring Rolls', 'Indo-Chinese', 4),
    ('Chilli Paneer', 'Indo-Chinese', 5),
    ('Fast Food', 'Food', 9),
    ('Pizza', 'Fast Food', 1),
    ('Burger', 'Fast Food', 2),
    ('Sandwich', 'Fast Food', 3),
    ('Wrap', 'Fast Food', 4),
    ('Fries', 'Fast Food', 5),
    ('Snacks', 'Food', 10),
    ('Pakoda', 'Snacks', 1),
    ('Momos', 'Snacks', 2),
    ('Spring Roll', 'Snacks', 3),
    ('Crispy Corn', 'Snacks', 4),
    ('Desserts', 'Food', 11),
    ('Gulab Jamun', 'Desserts', 1),
    ('Rasmalai', 'Desserts', 2),
    ('Ice Cream', 'Desserts', 3),
    ('Brownie', 'Desserts', 4),
    ('Halwa', 'Desserts', 5),
    ('Beverages', 'Food', 12),
    ('Tea', 'Beverages', 1),
    ('Coffee', 'Beverages', 2),
    ('Lassi', 'Beverages', 3),
    ('Shakes', 'Beverages', 4),
    ('Mocktails', 'Beverages', 5),
    ('Soft Drinks', 'Beverages', 6)
) AS seed
WHERE EXISTS (SELECT 1 FROM catalog_categories WHERE name = 'Food')
  AND NOT EXISTS (
      SELECT 1 FROM catalog_categories WHERE parent = 'Food'
  );

-- Two more departments, and the sections under the five that carry them.
-- Each block seeds only when that department has nothing under it yet, so an
-- admin who reshapes one keeps their version while the others still fill in.
--
-- Soft Drinks is not repeated under Snacks & Drinks: it already sits under
-- Food > Beverages, and a category exists once because products reference it
-- by name.
INSERT INTO catalog_categories (name, parent, icon, colour, position)
SELECT * FROM (VALUES
    ('Snacks & Drinks', '', 'cookie', '#C9A227', 6),
    ('Household Essentials', '', 'wrench', '#546E7A', 7)
) AS seed
WHERE NOT EXISTS (
    SELECT 1 FROM catalog_categories WHERE name IN ('Snacks & Drinks',
                                                    'Household Essentials')
);

INSERT INTO catalog_categories (name, parent, position)
SELECT * FROM (VALUES
    ('Atta, Rice & Dal', 'Grocery', 1),
    ('Oil & Ghee', 'Grocery', 2),
    ('Spices & Masala', 'Grocery', 3),
    ('Sugar & Salt', 'Grocery', 4),
    ('Kitchen Tools', 'Grocery', 5),
    ('Storage Containers', 'Grocery', 6)
) AS seed
WHERE EXISTS (SELECT 1 FROM catalog_categories WHERE name = 'Grocery')
  AND NOT EXISTS (SELECT 1 FROM catalog_categories WHERE parent = 'Grocery');

INSERT INTO catalog_categories (name, parent, position)
SELECT * FROM (VALUES
    ('Mobile Accessories', 'Electronics', 1),
    ('Chargers & Cables', 'Electronics', 2),
    ('Earphones', 'Electronics', 3),
    ('Smart Gadgets', 'Electronics', 4),
    ('Batteries', 'Electronics', 5)
) AS seed
WHERE EXISTS (SELECT 1 FROM catalog_categories WHERE name = 'Electronics')
  AND NOT EXISTS (SELECT 1 FROM catalog_categories WHERE parent = 'Electronics');

INSERT INTO catalog_categories (name, parent, position)
SELECT * FROM (VALUES
    ('Chips', 'Snacks & Drinks', 1),
    ('Biscuits', 'Snacks & Drinks', 2),
    ('Chocolates', 'Snacks & Drinks', 3),
    ('Juices', 'Snacks & Drinks', 4),
    ('Energy Drinks', 'Snacks & Drinks', 5)
) AS seed
WHERE EXISTS (SELECT 1 FROM catalog_categories WHERE name = 'Snacks & Drinks')
  AND NOT EXISTS (SELECT 1 FROM catalog_categories WHERE parent = 'Snacks & Drinks');

INSERT INTO catalog_categories (name, parent, position)
SELECT * FROM (VALUES
    ('Skincare', 'Beauty', 1),
    ('Hair Care', 'Beauty', 2),
    ('Oral Care', 'Beauty', 3),
    ('Makeup', 'Beauty', 4),
    ('Grooming', 'Beauty', 5)
) AS seed
WHERE EXISTS (SELECT 1 FROM catalog_categories WHERE name = 'Beauty')
  AND NOT EXISTS (SELECT 1 FROM catalog_categories WHERE parent = 'Beauty');

INSERT INTO catalog_categories (name, parent, position)
SELECT * FROM (VALUES
    ('Cleaning Supplies', 'Household Essentials', 1),
    ('Laundry', 'Household Essentials', 2),
    ('Dishwashing', 'Household Essentials', 3),
    ('Air Fresheners', 'Household Essentials', 4),
    ('Paper Products', 'Household Essentials', 5),
    ('Garbage Bags', 'Household Essentials', 6)
) AS seed
WHERE EXISTS (SELECT 1 FROM catalog_categories WHERE name = 'Household Essentials')
  AND NOT EXISTS (SELECT 1 FROM catalog_categories WHERE parent = 'Household Essentials');

-- Comparison groups. Categories answer "where do I browse to find this";
-- these answer "which products are fundamentally comparable", which is not
-- the same question: a 20W charger and a 25W charger sit in one group whether
-- or not the shop filed them under the same shelf.
--
-- The attribute template lives on the group as JSONB rather than in attribute
-- and product_attribute tables. Nothing queries across attribute values — a
-- comparison reads them with the products it is already loading — so the two
-- extra tables would buy joins we would never run. Shape:
--   [{"name":"Power","unit":"W"}]
CREATE TABLE IF NOT EXISTS comparison_groups (
    name       TEXT PRIMARY KEY,
    attributes JSONB NOT NULL DEFAULT '[]'::jsonb
);

-- Which group an item is in, and what it says for that group's fields.
-- Empty group means "not comparable to anything", which is most food.
--   {"Power": "20", "Warranty": "1 year"}
ALTER TABLE inventory_items
    ADD COLUMN IF NOT EXISTS compare_group TEXT  NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS attributes    JSONB NOT NULL DEFAULT '{}'::jsonb;

CREATE INDEX IF NOT EXISTS idx_items_compare_group
    ON inventory_items (compare_group) WHERE compare_group <> '';

-- A password on the shopper's account. Optional: most people sign in with an
-- emailed code and never set one, and an empty hash means "this address has
-- no password", not "any password will do" — the check is on the hash being
-- non-empty before it is ever compared.
ALTER TABLE users
    ADD COLUMN IF NOT EXISTS pass_hash TEXT NOT NULL DEFAULT '';

-- The written policies. In the database rather than the binary because an
-- admin has to be able to change them without a deploy — a refund policy that
-- needs an engineer is a refund policy that stays wrong.
--
-- One text blob per document, not a table of sections: the admin edits the
-- whole thing in one box, and "## " at the start of a line is a heading. That
-- is the entire format, and it survives being pasted in from anywhere.
CREATE TABLE IF NOT EXISTS policies (
    slug       TEXT PRIMARY KEY,
    title      TEXT        NOT NULL,
    body       TEXT        NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Password/PIN brute-force limits, shared by every API process.
CREATE TABLE IF NOT EXISTS password_attempts (
    key_hash TEXT PRIMARY KEY,
    attempts INTEGER NOT NULL CHECK (attempts > 0),
    expires_at TIMESTAMPTZ NOT NULL
);
CREATE INDEX IF NOT EXISTS password_attempts_expiry ON password_attempts(expires_at);

-- Historical orders retain their recorded total; never invent old charges.
ALTER TABLE orders ADD COLUMN IF NOT EXISTS delivery_fee NUMERIC(12,2) NOT NULL DEFAULT 0 CHECK (delivery_fee >= 0);

ALTER TABLE users ADD COLUMN IF NOT EXISTS preferences JSONB NOT NULL DEFAULT '{}';

-- Save each completed basket with its idempotency key in the same transaction.
CREATE TABLE IF NOT EXISTS checkout_attempts (
 buyer_email TEXT NOT NULL,
 request_id TEXT NOT NULL,
 fingerprint TEXT NOT NULL,
 response JSONB NOT NULL,
 created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 PRIMARY KEY (buyer_email, request_id)
);

-- Correct only the specific catalogue spelling errors documented in QA B33.
-- Product IDs, prices, inventory and historical order snapshots are preserved.
UPDATE inventory_items SET title=replace(replace(title,'Chesse','Cheese'),'TIkki','Tikki')
WHERE title LIKE '%Chesse%' OR title LIKE '%TIkki%';
UPDATE products SET name=replace(replace(name,'Chesse','Cheese'),'TIkki','Tikki')
WHERE name LIKE '%Chesse%' OR name LIKE '%TIkki%';


-- Staff-managed storefront artwork. Empty category means the whole catalogue;
-- empty department shows the campaign on Home and every department.
-- A festival, sale or season: a date-bounded skin for the shop.
--
-- Blinkit turns saffron for Ganesh Chaturthi and yellow again afterwards, and
-- nobody deploys anything to make that happen. This is that: the dates decide
-- when it is live, so an admin sets Diwali up in October and forgets about it.
--
-- Three colours, because one is not a theme. `ground` is the chrome the
-- service header and department strip sit on, `accent` is what actions on
-- that ground use, and `ink` is the text over it — a palette that cannot pick
-- its own foreground is a palette that produces unreadable headers.
--
-- `hints` is newline-separated search placeholders. Blinkit rotates
-- "decorative lights" and "ganesh idol" through the search field during the
-- festival, which is the cheapest merchandising in the app.
CREATE TABLE IF NOT EXISTS storefront_seasons (
    id        TEXT PRIMARY KEY,
    name      TEXT        NOT NULL,
    starts_at TIMESTAMPTZ NOT NULL,
    ends_at   TIMESTAMPTZ NOT NULL,
    ground    TEXT        NOT NULL DEFAULT '#143E32',
    accent    TEXT        NOT NULL DEFAULT '#C6EE63',
    ink       TEXT        NOT NULL DEFAULT '#FFFDF8',
    hints     TEXT        NOT NULL DEFAULT '',
    enabled   BOOLEAN     NOT NULL DEFAULT true,
    CONSTRAINT storefront_seasons_dates CHECK (ends_at > starts_at)
);

-- The lookup is "what is live right now", every time home loads.
CREATE INDEX IF NOT EXISTS idx_seasons_window
    ON storefront_seasons (starts_at, ends_at) WHERE enabled;

CREATE TABLE IF NOT EXISTS storefront_campaigns (
 id TEXT PRIMARY KEY,
 title TEXT NOT NULL,
 subtitle TEXT NOT NULL DEFAULT '',
 cta TEXT NOT NULL,
 category TEXT NOT NULL DEFAULT '',
 department TEXT NOT NULL DEFAULT '',
 image_url TEXT NOT NULL DEFAULT '',
 colour TEXT NOT NULL DEFAULT '#F2E8CE',
 enabled BOOLEAN NOT NULL DEFAULT true,
 position INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE IF NOT EXISTS storefront_migrations (id TEXT PRIMARY KEY);
-- Seed once, so deleting or hiding the initial campaign survives a restart.
WITH first_run AS (
 INSERT INTO storefront_migrations(id) VALUES ('campaigns-v1')
 ON CONFLICT DO NOTHING RETURNING id
)
INSERT INTO storefront_campaigns(id,title,subtitle,cta,colour)
SELECT 'everyday', 'Little joys. Everyday.',
 'Your local favourites, all in one place.', 'Explore the collection', '#F2E8CE'
FROM first_run ON CONFLICT DO NOTHING;

-- Checkout charges: Delivery is fixed (its amount is editable), anything else
-- an admin adds is charged on top, once per basket.
CREATE TABLE IF NOT EXISTS checkout_charges (
 id TEXT PRIMARY KEY,
 name TEXT NOT NULL,
 amount NUMERIC(12,2) NOT NULL CHECK (amount >= 0),
 position INTEGER NOT NULL DEFAULT 0
);
INSERT INTO checkout_charges(id,name,amount,position) VALUES ('delivery','Delivery',15,0)
ON CONFLICT DO NOTHING;

-- Refresh rotation with a grace minute: a page firing several requests at once
-- may present the same refresh token more than once before the new pair lands.
ALTER TABLE auth_sessions ADD COLUMN IF NOT EXISTS rotated_at TIMESTAMPTZ;

-- What the buyer picked (Colour: Black, Storage: 256GB), frozen on the order.
ALTER TABLE orders ADD COLUMN IF NOT EXISTS options JSONB NOT NULL DEFAULT '[]'::jsonb;

-- One review per delivered order: stars (1–5) and words for the product and
-- for the rider who brought it. Either half may be left out.
CREATE TABLE IF NOT EXISTS order_reviews (
 order_id     TEXT PRIMARY KEY REFERENCES orders(id) ON DELETE CASCADE,
 buyer_email  TEXT NOT NULL,
 item_id      TEXT NOT NULL,
 item_rating  SMALLINT CHECK (item_rating BETWEEN 1 AND 5),
 item_text    TEXT NOT NULL DEFAULT '',
 rider_phone  TEXT NOT NULL DEFAULT '',
 rider_rating SMALLINT CHECK (rider_rating BETWEEN 1 AND 5),
 rider_text   TEXT NOT NULL DEFAULT '',
 created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
 updated_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_reviews_item ON order_reviews (item_id);
-- When the "please review" nudge went out, so it goes out once.
ALTER TABLE orders ADD COLUMN IF NOT EXISTS review_requested_at TIMESTAMPTZ;

-- The in-app inbox: every notification a shopper or seller was sent, kept
-- whether or not email or push delivered it.
CREATE TABLE IF NOT EXISTS notifications (
 id         BIGSERIAL PRIMARY KEY,
 email      TEXT NOT NULL,
 title      TEXT NOT NULL,
 body       TEXT NOT NULL DEFAULT '',
 target     TEXT NOT NULL DEFAULT '',
 created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 read_at    TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS idx_notifications_email ON notifications (email, created_at DESC);

-- Generated studio category photography. Fill only missing artwork; an
-- administrator's existing or later choice always takes precedence.
UPDATE catalog_categories AS c
SET image_url = art.url
FROM (VALUES
    ('Arts & Crafts', 'https://res.cloudinary.com/dq3da5bkb/image/upload/v1789849410/Lamazon/Categories/Stationery_Games/Arts_Crafts.webp'),
    ('Atta, Rice & Dal', 'https://res.cloudinary.com/dq3da5bkb/image/upload/v1789849401/Lamazon/Categories/Grocery_Kitchen/Atta_Rice_Dal.webp'),
    ('Bags & School Needs', 'https://res.cloudinary.com/dq3da5bkb/image/upload/v1789849410/Lamazon/Categories/Stationery_Games/Bags_School_Needs.webp'),
    ('Bakery & Biscuits', 'https://res.cloudinary.com/dq3da5bkb/image/upload/v1789849404/Lamazon/Categories/Grocery_Kitchen/Bakery_Biscuits.webp'),
    ('Bath & Body', 'https://res.cloudinary.com/dq3da5bkb/image/upload/v1789849399/Lamazon/Categories/Beauty/Bath_Body.webp'),
    ('Beauty', 'https://res.cloudinary.com/dq3da5bkb/image/upload/v1789849117/Lamazon/Categories/Beauty/Beauty.webp'),
    ('Beauty & Cosmetics', 'https://res.cloudinary.com/dq3da5bkb/image/upload/v1789849399/Lamazon/Categories/Beauty/Beauty_Cosmetics.webp'),
    ('Books & Magazines', 'https://res.cloudinary.com/dq3da5bkb/image/upload/v1789849408/Lamazon/Categories/Stationery_Games/Books_Magazines.webp'),
    ('Chips & Namkeen', 'https://res.cloudinary.com/dq3da5bkb/image/upload/v1789849404/Lamazon/Categories/Snacks_Drinks/Chips_Namkeen.webp'),
    ('Dairy, Bread & Eggs', 'https://res.cloudinary.com/dq3da5bkb/image/upload/v1789849401/Lamazon/Categories/Grocery_Kitchen/Dairy_Bread_Eggs.webp'),
    ('Drinks & Juices', 'https://res.cloudinary.com/dq3da5bkb/image/upload/v1789849406/Lamazon/Categories/Snacks_Drinks/Drinks_Juices.webp'),
    ('Dry Fruits & Cereals', 'https://res.cloudinary.com/dq3da5bkb/image/upload/v1789849404/Lamazon/Categories/Grocery_Kitchen/Dry_Fruits_Cereals.webp'),
    ('Electronics', 'https://res.cloudinary.com/dq3da5bkb/image/upload/v1789849099/Lamazon/Categories/Electronics/Electronics.webp'),
    ('Feminine Hygiene', 'https://res.cloudinary.com/dq3da5bkb/image/upload/v1789849398/Lamazon/Categories/Beauty/Feminine_Hygiene.webp'),
    ('Files & Office Needs', 'https://res.cloudinary.com/dq3da5bkb/image/upload/v1789849410/Lamazon/Categories/Stationery_Games/Files_Office_Needs.webp'),
    ('Food', 'https://res.cloudinary.com/dq3da5bkb/image/upload/v1789849117/Lamazon/Categories/Food/Food.webp'),
    ('Gift Wraps & Bags', 'https://res.cloudinary.com/dq3da5bkb/image/upload/v1789849410/Lamazon/Categories/Stationery_Games/Gift_Wraps_Bags.webp'),
    ('Gifts', 'https://res.cloudinary.com/dq3da5bkb/image/upload/v1789849118/Lamazon/Categories/Gifts/Gifts.webp'),
    ('Glue & Tape', 'https://res.cloudinary.com/dq3da5bkb/image/upload/v1789849408/Lamazon/Categories/Stationery_Games/Glue_Tape.webp'),
    ('Grocery & Kitchen', 'https://res.cloudinary.com/dq3da5bkb/image/upload/v1789849118/Lamazon/Categories/Grocery_Kitchen/Grocery_Kitchen.webp'),
    ('Hair', 'https://res.cloudinary.com/dq3da5bkb/image/upload/v1789849399/Lamazon/Categories/Beauty/Hair.webp'),
    ('Home & Lifestyle', 'https://res.cloudinary.com/dq3da5bkb/image/upload/v1789849402/Lamazon/Categories/Household_Essentials/Home_Lifestyle.webp'),
    ('Household Essentials', 'https://res.cloudinary.com/dq3da5bkb/image/upload/v1789849118/Lamazon/Categories/Household_Essentials/Household_Essentials.webp'),
    ('Ice Creams & More', 'https://res.cloudinary.com/dq3da5bkb/image/upload/v1789849406/Lamazon/Categories/Snacks_Drinks/Ice_Creams_More.webp'),
    ('Instant Food', 'https://res.cloudinary.com/dq3da5bkb/image/upload/v1789849406/Lamazon/Categories/Snacks_Drinks/Instant_Food.webp'),
    ('Kitchenware & Appliances', 'https://res.cloudinary.com/dq3da5bkb/image/upload/v1789849404/Lamazon/Categories/Grocery_Kitchen/Kitchenware_Appliances.webp'),
    ('Notebooks & Diaries', 'https://res.cloudinary.com/dq3da5bkb/image/upload/v1789849408/Lamazon/Categories/Stationery_Games/Notebooks_Diaries.webp'),
    ('Oil, Ghee & Masala', 'https://res.cloudinary.com/dq3da5bkb/image/upload/v1789849401/Lamazon/Categories/Grocery_Kitchen/Oil_Ghee_Masala.webp'),
    ('Pens & Pencils', 'https://res.cloudinary.com/dq3da5bkb/image/upload/v1789849408/Lamazon/Categories/Stationery_Games/Pens_Pencils.webp'),
    ('Sauces & Spreads', 'https://res.cloudinary.com/dq3da5bkb/image/upload/v1789849406/Lamazon/Categories/Snacks_Drinks/Sauces_Spreads.webp'),
    ('Shoe Polish & Brush', 'https://res.cloudinary.com/dq3da5bkb/image/upload/v1789849413/Lamazon/Categories/Stationery_Games/Shoe_Polish_Brush.webp'),
    ('Skin & Face', 'https://res.cloudinary.com/dq3da5bkb/image/upload/v1789849399/Lamazon/Categories/Beauty/Skin_Face.webp'),
    ('Snacks & Drinks', 'https://res.cloudinary.com/dq3da5bkb/image/upload/v1789849118/Lamazon/Categories/Snacks_Drinks/Snacks_Drinks.webp'),
    ('Sports & Gym', 'https://res.cloudinary.com/dq3da5bkb/image/upload/v1789849411/Lamazon/Categories/Stationery_Games/Sports_Gym.webp'),
    ('Stationery & Games', 'https://res.cloudinary.com/dq3da5bkb/image/upload/v1789849118/Lamazon/Categories/Stationery_Games/Stationery_Games.webp'),
    ('Sweets & Chocolates', 'https://res.cloudinary.com/dq3da5bkb/image/upload/v1789849404/Lamazon/Categories/Snacks_Drinks/Sweets_Chocolates.webp'),
    ('Tea, Coffee & Milk Drinks', 'https://res.cloudinary.com/dq3da5bkb/image/upload/v1789849406/Lamazon/Categories/Snacks_Drinks/Tea_Coffee_Milk_Drinks.webp'),
    ('Toys & Games', 'https://res.cloudinary.com/dq3da5bkb/image/upload/v1789849408/Lamazon/Categories/Stationery_Games/Toys_Games.webp'),
    ('Vegetables & Fruits', 'https://res.cloudinary.com/dq3da5bkb/image/upload/v1789849401/Lamazon/Categories/Grocery_Kitchen/Vegetables_Fruits.webp')
) AS art(name, url)
WHERE c.name = art.name AND c.image_url = '';
