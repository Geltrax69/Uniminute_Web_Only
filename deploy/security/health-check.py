#!/usr/bin/env python3
import datetime,json,os,pathlib,urllib.request
os.umask(0o022)
status={'checkedAt':datetime.datetime.now(datetime.timezone.utc).isoformat()}
for name,url in [('api','http://127.0.0.1:8081/api/health'),('website','http://127.0.0.1:8100/login')]:
    try:
        with urllib.request.urlopen(url,timeout=10) as response:status[name]=response.status==200
    except Exception:status[name]=False
path=pathlib.Path('/var/lib/uniminute-health');path.mkdir(mode=0o755,exist_ok=True)
temp=path/'status.new';temp.write_text(json.dumps(status));temp.replace(path/'status.json')
if not status['api'] or not status['website']:print('Uniminute service health failed');raise SystemExit(1)
