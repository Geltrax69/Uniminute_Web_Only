#!/usr/bin/env bash
# Run as root after deploying both current application binaries.
set -euo pipefail
cd "$(dirname "$0")"
getent passwd uniminute >/dev/null || useradd --system --home-dir /nonexistent --shell /usr/sbin/nologin uniminute
install -d -m 0700 /etc/uniminute
install -d -m 0710 -o root -g uniminute /var/backups/uniminute
install -d -m 0755 /var/lib/uniminute-health
python3 - <<'PY'
import base64,os,pathlib,secrets,shutil
key=pathlib.Path('/etc/uniminute/backup.key')
if not key.exists():
 key.write_text(secrets.token_hex(32)+'\n');os.chmod(key,0o600)
env=pathlib.Path('/var/www/Lamazon/.env.systemd');text=env.read_text()
if not any(line.startswith('ADMIN_MFA_KEY=') for line in text.splitlines()):
 backup=pathlib.Path('/etc/uniminute/environment-before-security.env')
 if not backup.exists():shutil.copy2(env,backup);os.chmod(backup,0o600)
 temp=env.with_suffix('.security-new');temp.write_text(text.rstrip()+'\nADMIN_MFA_KEY='+base64.b64encode(secrets.token_bytes(32)).decode()+'\n');os.chmod(temp,0o600);temp.replace(env)
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
systemctl enable --now uniminute-backup.timer uniminute-restore-check.timer uniminute-health.timer
systemctl start uniminute-health.service
systemctl start uniminute-backup.service
systemctl start uniminute-restore-check.service
systemctl is-active lamazon-api lamazon-web
printf '%s\n' 'Security services installed; backup and restore verification completed.'
