---
version: 1
slug: "lamazon-website-templates-pages-login-templ"
primary_target: "Lamazon_website/templates/pages/login.templ"
related_targets: ["Lamazon_website/site/handlers.go","Lamazon_website/static/js/app.js"]
---

# Shopper sign-in recovery

Mode: Operate. Keep the existing forest/ivory single-card sign-in design,
floating field labels, touch targets, typography and email/password alternatives.
An unfinished signup resumes through email ownership verification and profile
setup; a completed password account retains password login. Verification and
reset steps expose a resend button with an elapsed-time 30-second countdown.
The backend enforces cooldowns and send budgets. Preserve the deadline through
wrong-code errors, release timers on card replacement, and never represent a
general rate limit or failed email send as successful delivery. No password
is created by merely opening or abandoning profile setup.
