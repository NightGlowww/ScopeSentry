#!/usr/bin/env python3
import datetime,json,socket,sys,time
from zoneinfo import ZoneInfo
def command(s,f,*parts):
 data=[str(p).encode() for p in parts]
 s.sendall(b"*"+str(len(data)).encode()+b"\r\n"+b"".join(b"$"+str(len(p)).encode()+b"\r\n"+p+b"\r\n" for p in data))
 t=f.read(1); line=f.readline().rstrip(b"\r\n")
 if t==b"+": return line.decode()
 if t==b"$":
  n=int(line)
  if n<0: return None
  data=f.read(n); assert f.read(2)==b"\r\n"; return data.decode()
 raise RuntimeError("redis command failed")
try:
 c=json.load(open("/run/secrets/redis.json"))
 with socket.create_connection((c["ip"],int(c["port"])),timeout=8) as s:
  s.settimeout(8); f=s.makefile("rb")
  assert command(s,f,"AUTH",c["password"])=="OK"
  stamp=command(s,f,"HGET","node:node-jp-01","updateTime")
  if not stamp: raise RuntimeError("heartbeat not yet registered")
  tick=datetime.datetime.strptime(stamp,"%Y-%m-%d %H:%M:%S").replace(tzinfo=ZoneInfo("Asia/Shanghai")).timestamp()
  age=time.time()-tick
  if age>100 or age < -30: raise RuntimeError("heartbeat age "+str(round(age))+"s")
except Exception as e:
 print(type(e).__name__+": "+str(e)); sys.exit(1)
