#!/usr/bin/env python3
import http.server,json,pathlib,threading,time
state=pathlib.Path("/opt/scopesentry/scan/state")
page=b"<!doctype html><html><head><title>ScopeSentry JP deployment smoke 20261124</title></head><body><h1>node-jp-01 owned deployment fixture</h1></body></html>"
class H(http.server.BaseHTTPRequestHandler):
 def do_GET(self):
  with (state/"fixture-requests.jsonl").open("a") as f: f.write(json.dumps({"time":time.time(),"host":self.headers.get("Host",""),"path":self.path,"peer":self.client_address[0],"ua":self.headers.get("User-Agent","")})+"\n")
  self.send_response(200); self.send_header("Content-Type","text/html; charset=utf-8"); self.send_header("Content-Length",str(len(page))); self.end_headers(); self.wfile.write(page)
 def do_HEAD(self):
  self.send_response(200); self.send_header("Content-Type","text/html; charset=utf-8"); self.send_header("Content-Length",str(len(page))); self.end_headers()
 def log_message(self,*args): pass
def serve(addr):
 http.server.ThreadingHTTPServer(addr,H).serve_forever()
for a in [("127.0.0.1",18765),("100.65.237.20",18765)]:
 threading.Thread(target=serve,args=(a,),daemon=True).start()
while True: time.sleep(3600)
