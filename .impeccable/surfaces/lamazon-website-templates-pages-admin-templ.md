---
version: 1
slug: "lamazon-website-templates-pages-admin-templ"
primary_target: "Lamazon_website/templates/pages/admin.templ"
related_targets: ["Lamazon_website/templates/pages/admin-reports.templ","Lamazon_website/templates/pages/admin-security.templ"]
---

# Admin operations

Mode: Operate. Scope: /admin reports, existing admin navigation, security settings.
Audience: the Uniminute operator supervising local stores and deliveries daily.
Tasks: filter by IST month/store/day; compare delivered product sales with current
order stages; inspect rejection reasons/customer and delivery records; export a
six-sheet Excel workbook; enroll MFA and review operational security health.
Preserve the existing visual identity and every operational destination. Use
Inter Tight, clear sidebar/mobile selection and readable tables. Phone tables
scroll inside their own regions with an explicit cue. Never invent metrics.
Revenue is delivered item value before refunds, fees separately; date and
order-line counting definitions must remain visible. Private exports require
admin authentication. Preview data is explicitly labelled synthetic/example.
MFA requires human authenticator enrollment; never claim it is enabled before
confirmation. Off-site automation/notification destinations remain external setup.

People actions: buyer-only deletion requires exact email confirmation and the
current admin password, with inline recoverable errors and a native modal.
Revoke sessions and erase live profile data atomically; keep completed sale
amounts with buyer details removed. Block deletion during active orders or
recent unsettled payments. Store owners can only be suspended or restored:
hide their store/products across public catalogues and block new products and
uploads; retain accounts, inventory and operational order access. Record the
suspension reason and show suspended stores in their own admin destination.
Keep destructive actions explicitly labelled and separate from normal rows.
