#!/usr/bin/env bash
# Run as root after deploying both current application binaries.
set -euo pipefail
cd "$(dirname "$0")"
getent passwd uniminute >/dev/null || useradd --system --home-dir /nonexistent --shell /usr/sbin/nologin uniminute
install -d -m 0700 /etc/uniminute
install -d -m 0710 -o root -g uniminute /var/backups/uniminute
install -d -m 0755 /var/lib/uniminute-health
# The former root-only working directory must be traversable by the new group.
chgrp uniminute /var/www/Lamazon /var/www/Lamazon/bin
chmod 0750 /var/www/Lamazon /var/www/Lamazon/bin
python3 - <<'PY'
import base64,os,pathlib,secrets,shutil,grp
key=pathlib.Path('/etc/uniminute/backup.key')
if not key.exists():
 key.write_text(secrets.token_hex(32)+'\n');os.chmod(key,0o600)
env=pathlib.Path('/var/www/Lamazon/.env.systemd');text=env.read_text()
if not any(line.startswith('ADMIN_MFA_KEY=') for line in text.splitlines()):
 backup=pathlib.Path('/etc/uniminute/environment-before-security.env')
 if not backup.exists():shutil.copy2(env,backup);os.chmod(backup,0o600)
 temp=env.with_suffix('.security-new');temp.write_text(text.rstrip()+'\nADMIN_MFA_KEY='+base64.b64encode(secrets.token_bytes(32)).decode()+'\n');os.chmod(temp,0o600);temp.replace(env)
values={line.split('=',1)[0]:line.split('=',1)[1].strip().strip('"').strip("'") for line in env.read_text().splitlines() if '=' in line and not line.startswith('#')}
credentials=values.get('GOOGLE_APPLICATION_CREDENTIALS','')
if credentials:
 path=pathlib.Path(credentials)
 if path.is_file():os.chown(path,-1,grp.getgrnam('uniminute').gr_gid);os.chmod(path,0o640)
for path in env.parent.glob('.env*'):
 if path.is_file():os.chmod(path,0o600)
PY
install -m 0755 backup.py /usr/local/sbin/uniminute-backup
install -m 0755 health-check.py /usr/local/sbin/uniminute-health-check
for unit in uniminute-*.service uniminute-*.timer; do install -m 0644 "$unit" "/etc/systemd/system/$unit"; done
for service in lamazon-api lamazon-web; do
 install -d -m 0755 "/etc/systemd/system/$service.service.d"
 install -m 0644 service-hardening.conf "/etc/systemd/system/$service.service.d/security.conf"
done
systemctl daemon-reload
systemctl restart lamazon-api lamazon-web
ready=0
for attempt in $(seq 1 20); do
 if curl -fsS --max-time 2 http://127.0.0.1:8081/api/health >/dev/null 2>&1 && curl -fsS --max-time 2 http://127.0.0.1:8100/login >/dev/null 2>&1; then ready=1; break; fi
 sleep 1
done
[ "$ready" = 1 ] || { printf '%s\n' 'Application startup failed; inspect the systemd journals.'; exit 1; }
systemctl enable --now uniminute-backup.timer uniminute-restore-check.timer uniminute-health.timer
systemctl start uniminute-health.service
systemctl start uniminute-backup.service
systemctl start uniminute-restore-check.service
systemctl is-active lamazon-api lamazon-web
printf '%s\n' 'Security services installed; backup and restore verification completed.'
