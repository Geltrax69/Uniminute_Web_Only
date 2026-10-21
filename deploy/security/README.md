# Uniminute security operations

Deploy both current API/website binaries, copy this folder to the host, then run
`sudo ./install.sh`. It installs an unprivileged `uniminute` account, systemd
restrictions, and root-only backup tasks. Both Go servers bind to loopback;
Nginx is the public entry point. Other applications on the host are unchanged.

## Secrets and MFA

The installer initializes `ADMIN_MFA_KEY` in the systemd environment if missing.
Never replace it without migrating encrypted MFA secrets. Admins enroll under
`/admin?tab=security`, verify a code, and save ten one-use recovery codes.
Enrollment revokes their other admin sessions. MFA starts only after enrollment.
There is no public bypass if both authenticator and recovery codes are lost.
Keep the systemd environment and backup key securely off-server as well; a dump
alone cannot recover encrypted MFA secrets without `ADMIN_MFA_KEY`.

## Backups and monitoring

Daily PostgreSQL custom archives use AES-256-CBC with PBKDF2 (200,000 iterations)
and a separate encrypt-then-HMAC-SHA256 check. The random backup password is
`/etc/uniminute/backup.key` (root only). Encrypted backups have 14-day retention
under `/var/backups/uniminute`. Only the non-secret status file is readable by
the application group. Weekly verification authenticates/decrypts the newest
backup, restores it into a disposable database, checks core tables, and drops
that temporary database. It never restores over production.

Every-minute service checks, backup failures and restore failures leave systemd
journal records and admin-visible status. Stale status is labelled overdue.
External email/pager delivery and automated off-server storage require a
configured destination and credentials. This installer does not invent one or
send messages. One off-server copy is not an ongoing off-site backup policy.

```sh
systemctl list-timers 'uniminute-*'
journalctl -u uniminute-backup -u uniminute-restore-check -u uniminute-health
systemctl show lamazon-api lamazon-web -p User -p NoNewPrivileges -p ProtectSystem
nginx -t
```

Hardening lives in each service's `service.d/security.conf`. For a startup
restriction failure, inspect the journal, remove only that drop-in, reload
systemd and restart the service. Preserve backup/MFA keys. Binaries and Nginx
configuration are backed up separately before deployment.

## Application controls

- HTTPS-only production cookies, origin checks before cookie-to-token proxying,
  frame denial, nosniff, HSTS, referrer/permissions policies and a script/frame
  allow-listed CSP. Existing Alpine still needs unsafe-eval and inline scripts;
  a strict nonce-based, CSP-compatible Alpine migration remains future work.
- Backend CORS allows owned production origins. Local development must add
  `ALLOWED_ORIGINS` explicitly. Authenticated reads bypass the website cache.
- Logout revokes server tokens. Verified resets revoke old sessions. Profile
  password changes require reauthentication and revoke other sessions. Shopper
  bootstrap settings never overwrite a nonempty password on restart.
- PBKDF2-HMAC-SHA256 uses 600,000 iterations; successful password logins migrate
  older hashes. Account budgets persist in PostgreSQL. The connection-IP budget
  is larger for loopback proxies; forwarded headers never override identity.
- Public auth: 600 requests/minute per connection; code send/reset start:
  10/address/endpoint per 10 minutes plus the existing 60-second send floor.
  Authenticated writes: 120/minute/account. Uploads: 20/minute and 100/day/account.
  Catalog search: 600/minute/connection. Payments keep separate tighter budgets.
  Database/counter failures stop the operation.
- Images: max 8 files/upload, max 12 gallery images; dimensions at most 16 MP
  and 8000 pixels/side. Product images are decoded, resized to a 2000-pixel
  longest side, and re-encoded. Two decodes run concurrently per process.
  Animated admin banner frames remain intact after dimension checks; videos
  retain admin-only access and existing byte/type limits.
- Seller ownership is established before revealing foreign order counts.
  Deletion locks the inventory row and checks orders in one transaction.
- Admin changes and report exports are audited without passwords, codes, bodies
  or customer addresses. The UI shows the latest 100 events. Audit and expired
  limiter records currently need an operational retention/archiving policy.

Cloudflare fronts the API. CDN/Vercel WAF and bot rules still require access to
those accounts; origin limits do not prove that edge rules are enabled.

## Reports

`/admin` defaults to Reports. Dates are IST. Delivered item value uses delivery
date; legacy records without that timestamp explicitly use placement date.
Fees stay separate. Sales are before refunds, not profit or confirmed cash
settlement. Order counts are current-stage order-line records placed in the
period; a basket can create several records. Product/rider review counts use
the latest review update date.

Excel has Summary, Store totals, Daily stores, Orders, Rejected orders, Reviews.
User text is escaped inline-string data, never formulas or external links.
UI detail previews stop at 500 rows; totals still cover the selected month.
Exports support 50,000 relevant records; choose one store for larger months.
Customer details require admin access and exports should be stored securely.
