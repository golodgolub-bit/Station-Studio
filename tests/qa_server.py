from http.server import ThreadingHTTPServer,SimpleHTTPRequestHandler
from pathlib import Path
from urllib.parse import urlparse,parse_qs
import json,base64,time
ROOT=Path(__file__).resolve().parents[1]/'panel/web'
OUT=ROOT.parents[1]/'artifacts'
OUT.mkdir(exist_ok=True)
state={'active':False,'state':'off','config':{},'error':'','level':.12,'coverAvailable':True}
def device(i,name):return {'id':i,'name':name,'type':'devices.types.light','state':'online','room':'room1','groups':['group1'],'capabilities':[{'type':'devices.capabilities.on_off','state':{'instance':'on','value':True}},{'type':'devices.capabilities.color_setting','parameters':{'color_model':'hsv'},'state':{'instance':'hsv','value':{'h':270,'s':50,'v':70}}}]}
home={'connected':True,'rooms':[{'id':'room1','name':'Гостиная'}],'devices':[device('l1','Свет у дивана'),device('l2','Торшер')],'groups':[{'id':'group1','name':'Вечерний свет','devices':['l1','l2']}]}
class Handler(SimpleHTTPRequestHandler):
 def __init__(self,*a,**kw):super().__init__(*a,directory=str(ROOT),**kw)
 def log_message(self,*a):pass
 def send(self,value,status=200):
  raw=json.dumps(value,ensure_ascii=False).encode();self.send_response(status);self.send_header('Content-Type','application/json');self.send_header('Content-Length',str(len(raw)));self.end_headers();self.wfile.write(raw)
 def do_POST(self):
  raw=self.rfile.read(int(self.headers.get('Content-Length','0')));q=json.loads(raw or b'{}')
  if self.path.startswith('/qa/report'):
   name=parse_qs(urlparse(self.path).query).get('screen',['result'])[0];assert name.replace('-','').isalnum();(OUT/('qa27-'+name+'.json')).write_text(json.dumps(q,ensure_ascii=False,indent=2),encoding='utf-8');self.send({'ok':True});return
  if self.path=='/api/music/visual':state.update(active=q.get('action')=='start',state='running' if q.get('action')=='start' else 'off',config=q,error='')
  self.send({'ok':True})
 def do_GET(self):
  p=urlparse(self.path)
  if p.path=='/qa.js':
   raw=(Path(__file__).resolve().parent/'qa_browser.js').read_bytes();self.send_response(200);self.send_header('Content-Type','text/javascript');self.end_headers();self.wfile.write(raw);return
  if p.path.startswith('/api/'):
   route=p.path[5:]
   if route=='status':self.send({'root':True,'selinux':'0','track':'Amour plastique','artist':'Videoclub','playing':True,'volume':6,'volumeMax':15,'config':{'ring':'breathe','color':'#9370db','brightness':40,'mode':'tv','sleepAt':0},'events':[],'metrics':{'cpu':12,'temp':47,'memUsed':1100000000,'memAvailable':700000000,'memTotal':2000000000,'diskFree':200000000,'diskTotal':8000000000,'uptime':50000}})
   elif route=='music/visual':self.send(state)
   elif route=='station/live':self.send({'timestamp':int(time.time()*1000),'frame':base64.b64encode(bytes([int((i%25)/24*180) if i//25 in range(5,11) else 0 for i in range(400)])).decode(),'ring':[[130,65,230]]*24,'displayAvailable':True,'ringAvailable':True,'width':25,'height':16,'brightness':70,'assistant':'idle'})
   elif route=='home':self.send(home)
   elif route=='home/account':self.send({'connected':True,'mode':'web_session'})
   elif route=='music/providers':self.send({'providers':[{'package':'com.spotify.tv.android','name':'Spotify'}],'selected':'com.spotify.tv.android'})
   elif route=='music/cover':self.send({'available':False,'title':'Amour plastique','palette':[],'error':'QA: без настоящей обложки'})
   elif route=='music/album':self.send({'error':'QA fixture'},404)
   elif route=='lights':self.send({'lamps':[],'active':False,'config':{},'error':''})
   elif route=='lights/home':self.send({'lamps':[{'id':'home:l1','name':'Торшер','kind':'yandex','host':'Дом с Алисой'}]})
   elif route in ['apps','matrix/library','ambient/designs']:self.send([])
   elif route=='scenarios':self.send({'settings':{'rules':[],'provider':'com.spotify.tv.android','musicRouting':True},'observer':'idle','lastResult':''})
   elif route=='scenarios/catalog':self.send({'commands':[],'customRules':[]})
   elif route=='plugins':self.send({'installed':[],'packages':[],'services':{}})
   elif route=='maintenance':self.send({'caches':[{'package':'com.spotify.tv.android','bytes':1000000}]})
   else:self.send({'ok':True})
   return
  if p.path in ['/','/index.html','/tv.html']:
   raw=(ROOT/('tv.html' if p.path=='/tv.html' else 'index.html')).read_text(encoding='utf-8');raw=raw.replace('<head>','<head><script>localStorage.setItem("stationAccess","'+('a'*48)+'");</script>',1);raw=raw.replace('</body>','<script src="/qa.js"></script></body>');data=raw.encode();self.send_response(200);self.send_header('Content-Type','text/html; charset=utf-8');self.send_header('Content-Length',str(len(data)));self.end_headers();self.wfile.write(data);return
  super().do_GET()
if __name__=='__main__':ThreadingHTTPServer(('127.0.0.1',18890),Handler).serve_forever()
