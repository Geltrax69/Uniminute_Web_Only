# Catalogue classification pipeline

This is the single source for customer-facing product groups. It runs on every
catalogue read, so current products, imports, new inventory and later title edits
use the same rules. No inventory/category migration or external AI service is
required. Merchant titles, category labels, stock, prices and IDs stay intact.

## Stages

1. Normalize Unicode case, punctuation and word boundaries.
2. Restrict rules to the category's department, derived by the catalogue query.
3. Match explicit product types. Specific accessory/drink rules outrank related
   broad terms: an iPhone case is an accessory; cold coffee is not a hot drink.
4. Group related types for browsing, with a deliberate display order. Tea, coffee,
   herbal tea and hot chocolate share Hot Beverages. Pav Bhaji is Street Food.
5. Fall back to a specific merchant category when the name is branded or unclear
   (for example Biscoff under Frappes). Broad, conflicting or equally plausible
   classifications enter the review queue. Unknown products remain visible.
6. Audit same-store, normalized-name duplicates. Report them for review; never
   merge or delete stock automatically.

The API returns `classification` with the inferred product category, customer group, display order, rule, version,
reason and review flag. Public category filters use this result while preserving
meaningful merchant subcategories. Store menus consume the same result; they
fall back to old category labels during independent frontend/backend rollouts.

## Operator and seller workflows

- Admin: `/admin?tab=classification` shows uncertain classifications and possible
  duplicate published listings. Confirm the product type with the seller, then
  correct its title/category through the normal inventory editor.
- Seller: the inventory editor previews “Shown in …” after title/category changes.
  If a matching catalogue category exists, the seller can explicitly choose
  “Use … category”; the pipeline never silently rewrites their selected category.
  Preview failure does not block saving.
- Protected API: `GET /api/admin/catalog-audit` and `POST /api/seller/classify`.

Run a read-only batch audit from `backend/`:

```sh
curl -fsS https://api.geltrax.engineer/api/products | go run ./cmd/catalog-audit > catalogue-audit.json
```

For one shop, use `/api/shops/PURE%20BITES/products` instead. The report includes
customer groups/counts, uncertain products and duplicate candidates. No database
credentials are needed and no data is written.

## Extending rules safely

Edit the department-scoped rules in `pipeline.go`, add positive and near-miss
examples in `pipeline_test.go`, and bump `Version`. Do not add broad substring
rules: `tea` must not match `steak`; `phone` must not match `headphone`. If a
product could belong to multiple types, flag it instead of inventing certainty.
Keep merchant categories as evidence; do not automatically move products between
departments or merge similar-looking listings.

`testdata/pure_bites.json` is a public catalogue regression fixture: 40 products
must remain visible, with 7 Hot Beverages, 1 Street Food item and separate Cold
Coffee/Frappes groups. The two Red Sauce Pasta IDs must be reported, not removed.
