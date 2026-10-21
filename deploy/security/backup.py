#!/usr/bin/env python3
"""Root-only encrypted PostgreSQL backups with authenticated restore checks.
Install under /usr/local/sbin/uniminute-backup. No secrets enter argv/logs.
"""
import argparse, fcntl, grp, datetime, hashlib, hmac, json, os, pathlib, re, subprocess, tempfile, urllib.parse
ROOT=pathlib.Path('/var/backups/uniminute')
KEY=pathlib.Path('/etc/uniminute/backup.key')
ENV=pathlib.Path('/var/www/Lamazon/.env.systemd')
STATUS=ROOT/'status.json'

def run(args,**kwargs):
    return subprocess.run(args,check=True,stdout=subprocess.PIPE,stderr=subprocess.PIPE,**kwargs)

def database():
    values={}
    for line in ENV.read_text().splitlines():
        line=line.strip()
        if line and not line.startswith('#') and '=' in line:
            k,v=line.split('=',1);values[k]=v.strip().strip('"').strip("'")
    url=urllib.parse.urlparse(values['DATABASE_URL'])
    return url.username,url.path.lstrip('/')

def status(**changes):
    old={}
    if STATUS.exists():old=json.loads(STATUS.read_text())
    old.update(changes)
    temp=STATUS.with_suffix('.new');temp.write_text(json.dumps(old));os.chmod(temp,0o640);os.chown(temp,-1,grp.getgrnam('uniminute').gr_gid);temp.replace(STATUS)

def mac(path):
    digest=hmac.new(hashlib.sha256(KEY.read_bytes()+b'uniminute-backup-auth-v1').digest(),digestmod=hashlib.sha256)
    with path.open('rb') as f:
        for block in iter(lambda:f.read(1024*1024),b''):digest.update(block)
    return digest.hexdigest()

def verify(path):
    if not path.name.endswith('.dump.enc') or path.parent.resolve()!=ROOT.resolve():raise ValueError('invalid backup path')
    supplied=path.with_suffix(path.suffix+'.mac').read_text().strip()
    if not hmac.compare_digest(mac(path),supplied):raise ValueError('backup authentication failed')

def decrypt(path,out):
    verify(path)
    run(['openssl','enc','-d','-aes-256-cbc','-pbkdf2','-iter','200000','-pass','file:'+str(KEY),'-in',str(path),'-out',str(out)])

def restore_check(path):
    user,_=database();name='uniminute_restorecheck_'+datetime.datetime.now().strftime('%Y%m%d%H%M%S')
    if not re.fullmatch(r'uniminute_restorecheck_\d{14}',name):raise ValueError('invalid restore database')
    created=False
    with tempfile.TemporaryDirectory(prefix='uniminute-restore-',dir=ROOT) as work:
        dump=pathlib.Path(work)/'check.dump';decrypt(path,dump)
        try:
            run(['docker','exec','lamazon-postgres','createdb','-U',user,name]);created=True
            with dump.open('rb') as source:
                run(['docker','exec','-i','lamazon-postgres','pg_restore','-U',user,'-d',name,'--no-owner','--no-privileges','--exit-on-error'],stdin=source)
            run(['docker','exec','lamazon-postgres','psql','-U',user,'-d',name,'-v','ON_ERROR_STOP=1','-c','SELECT count(*) FROM users; SELECT count(*) FROM orders;'])
        finally:
            if created:run(['docker','exec','lamazon-postgres','dropdb','-U',user,'--force',name])
    status(lastRestoreCheck=datetime.datetime.now(datetime.timezone.utc).isoformat(),restoreHealthy=True)

def backup():
    user,name=database();stamp=datetime.datetime.now(datetime.timezone.utc).strftime('%Y%m%dT%H%M%SZ')
    dest=ROOT/('uniminute-'+stamp+'.dump.enc')
    with tempfile.TemporaryDirectory(prefix='uniminute-backup-',dir=ROOT) as work:
        dump=pathlib.Path(work)/'database.dump'
        with dump.open('wb') as out:
            subprocess.run(['docker','exec','lamazon-postgres','pg_dump','-U',user,'-d',name,'-Fc','--no-owner','--no-privileges'],check=True,stdout=out,stderr=subprocess.PIPE)
        run(['openssl','enc','-aes-256-cbc','-pbkdf2','-iter','200000','-salt','-pass','file:'+str(KEY),'-in',str(dump),'-out',str(dest)])
    dest.with_suffix(dest.suffix+'.mac').write_text(mac(dest)+'\n')
    os.chmod(dest,0o600);os.chmod(dest.with_suffix(dest.suffix+'.mac'),0o600)
    # Never delete the most recent backup, even if the system clock is wrong.
    cutoff=datetime.datetime.now().timestamp()-14*86400
    for old in ROOT.glob('uniminute-*.dump.enc'):
        if old!=dest and old.stat().st_mtime<cutoff:
            old.unlink();old.with_suffix(old.suffix+'.mac').unlink(missing_ok=True)
    status(lastBackup=datetime.datetime.now(datetime.timezone.utc).isoformat(),backupHealthy=True,lastBackupFile=dest.name,retentionDays=14)
    return dest

def main():
    os.umask(0o077);ROOT.mkdir(mode=0o700,parents=True,exist_ok=True)
    lock=(ROOT/'.backup.lock').open('w')
    try:fcntl.flock(lock,fcntl.LOCK_EX|fcntl.LOCK_NB)
    except BlockingIOError:print('Uniminute backup task already running');return
    parser=argparse.ArgumentParser();parser.add_argument('--verify-latest',action='store_true');args=parser.parse_args()
    try:
        if args.verify_latest:
            backups=sorted(ROOT.glob('uniminute-*.dump.enc'))
            if not backups:raise ValueError('no backup exists')
            restore_check(backups[-1])
        else:backup()
        print('Uniminute backup task completed')
    except Exception as exc:
        status(**({'restoreHealthy':False} if args.verify_latest else {'backupHealthy':False}),lastFailure=datetime.datetime.now(datetime.timezone.utc).isoformat())
        # Do not print command output; a database error can contain sensitive data.
        print('Uniminute backup task failed:',type(exc).__name__)
        raise SystemExit(1)
if __name__=='__main__':main()
