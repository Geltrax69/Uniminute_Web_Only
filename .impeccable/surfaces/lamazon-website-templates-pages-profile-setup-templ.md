---
version: 1
slug: "lamazon-website-templates-pages-profile-setup-templ"
primary_target: "Lamazon_website/templates/pages/profile-setup.templ"
related_targets: ["Lamazon_website/templates/pages/profile-avatars.go","Lamazon_website/static/js/app.js","Lamazon_website/static/css/app.css"]
---

# Profile card setup

Mode: Operate. Follow the user's phone sketches inside the existing forest,
ivory, lime and Inter Tight visual world. A single profile card accumulates
name, verified email, phone and delivery address. Five stages: interest/avatar,
name, Indian mobile, optional password, address; then a saved success screen.
All 22 supplied PNG avatars are grouped by their interest. Persist the choice
server-side and render it on the account screen. Never display the password on
the card or persist it in browser storage. Flip the card for its password step.
Use native phone keyboards, never illustrated keyboard placeholders.
Micro-interactions use transform/opacity, 120–240ms, small pressed scale .98,
and non-bouncing ease-out. Progress uses scaleX. Respect reduced motion with an
instant face change. Validate at each step; keep values on errors; suppress
double submissions. Save profile and delivery address in one transaction, reuse
the setup request ID for uncertain retries, and show 100% only after confirmed
persistence. An existing password is retained unless explicitly reauthenticated.
