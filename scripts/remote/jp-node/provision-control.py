#!/usr/bin/env python3
"""创建日本节点专属 Mongo 账号；凭据只写入 0600 文件，绝不输出。"""
import json,os,pathlib,secrets,subprocess
os.umask(0o077)
root=pathlib.Path("/opt/scopesentry/jp-onboarding")
root.mkdir(mode=0o700,parents=True,exist_ok=True)
path=root/"node-jp-01.json"
if path.exists():
 raise SystemExit("credential file already exists: inspect before rerun")
r=json.loads(subprocess.check_output(["docker","inspect","scopesentry-redis"],text=True))[0]
env=dict(x.split("=",1) for x in r["Config"]["Env"] if "=" in x)
rp=env.get("REDIS_PASSWORD")
if not rp: raise SystemExit("Redis password missing")
u="scopesentry_scan_jp01"
pw=secrets.token_urlsafe(36)
mongo_js = "const a=db.getSiblingDB(\"admin\"); if(!a.auth(process.env.MONGO_INITDB_ROOT_USERNAME,process.env.MONGO_INITDB_ROOT_PASSWORD))throw Error(\"admin auth failed\"); if(a.getUser(" + json.dumps(u) + "))throw Error(\"dedicated user already exists\"); a.createUser({user:" + json.dumps(u) + ",pwd:" + json.dumps(pw) + ",roles:[{role:\"readWrite\",db:\"ScopeSentry\"}]}); print(\"dedicated_mongo_user_created\");"
p=subprocess.run(["docker","exec","-i","scopesentry-mongodb","mongosh","--quiet"],input=mongo_js,text=True,capture_output=True)
if p.returncode: raise SystemExit("Mongo provisioning failed; "+p.stderr[-300:])
cfg={"NodeName":"node-jp-01","state":2,"TimeZoneName":"Asia/Shanghai","debug":False,"mongodb":{"ip":"100.115.186.92","port":"27017","user":u,"password":pw,"database":"ScopeSentry"},"redis":{"ip":"100.115.186.92","port":"6379","password":rp}}
with path.open("x") as f: json.dump(cfg,f,indent=2); f.write("\n")
os.chmod(path,0o600)
mods=(root/"modules.yaml").read_text()
rcli=["docker","exec","-e","REDISCLI_AUTH="+rp,"scopesentry-redis","redis-cli","--no-auth-warning"]
exists=subprocess.check_output(rcli+["EXISTS","node:node-jp-01"],text=True).strip()
if exists!="0": raise SystemExit("node key exists: inspect before seeding")
subprocess.run(rcli+["HSET","node:node-jp-01","modulesConfig",mods,"state","2"],check=True,stdout=subprocess.DEVNULL)
print(json.dumps({"mongoUser":u,"mongoRoles":["readWrite:ScopeSentry"],"node":"node-jp-01","initialState":2,"maxTaskNum":1,"credentials":"private-file-only"}))
