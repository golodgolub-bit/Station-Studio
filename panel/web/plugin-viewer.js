(()=>{'use strict';
const el=(tag,text,cls)=>{const e=document.createElement(tag);if(text!==undefined)e.textContent=text;if(cls)e.className=cls;return e};
window.StationPluginViewer={async open(name,settings,api,dialog,toast){
 const j=await api('plugins/ui?name='+encodeURIComponent(name)),tv=document.body.classList.contains('station-tv');
 dialog(settings.panel.title,d=>{
  let modal=d.closest('dialog');modal?.classList.add('plugin-viewer');if(settings.panel.uiMode==='scene')modal?.classList.add('expanded');
  const frame=el('iframe',undefined,'plugin-frame');frame.title=settings.panel.title;frame.setAttribute('sandbox','allow-scripts');frame.setAttribute('allow',"camera 'none'; microphone 'none'; geolocation 'none'; payment 'none'");frame.referrerPolicy='no-referrer';
  const policy="default-src 'none'; script-src 'unsafe-inline' blob:; style-src 'unsafe-inline'; img-src data: blob:; media-src data: blob:; font-src data: blob:; connect-src blob:; frame-src 'none'; worker-src blob:; base-uri 'none'; form-action 'none'";
  frame.srcdoc='<meta name="viewport" content="width=device-width,initial-scale=1"><meta http-equiv="Content-Security-Policy" content="'+policy+'"><style>:root{--alicekit-accent:#7739ef;--alicekit-bg:#f7f4fb;--alicekit-text:#2b2433;--kontur-accent:var(--alicekit-accent);--kontur-soft:#ece2fc;--kontur-bg:var(--alicekit-bg);--kontur-text:var(--alicekit-text);--kontur-muted:#8c7e99}*{box-sizing:border-box}body{margin:0;padding:20px;background:var(--alicekit-bg);color:var(--alicekit-text);font:15px/1.5 -apple-system,BlinkMacSystemFont,Roboto,sans-serif}button,input,select,textarea{font:inherit;max-width:100%}button{border:0;border-radius:14px;padding:12px 16px;background:var(--kontur-soft);color:var(--alicekit-accent)}</style>'+j.html;
  const toolbar=el('div',undefined,'plugin-viewer-toolbar'),expand=el('button',settings.panel.uiMode==='scene'?'Обычный размер':'На весь экран'),input=el('button','Управлять сценой','primary'),caption=el('p',tv?'Стрелки — камера. OK — действие. Назад — вернуться к панели.':'Интерактивная сцена автора. Управление и качество зависят от плагина.','plugin-caption');
  expand.onclick=()=>{modal?.classList.toggle('expanded');expand.textContent=modal?.classList.contains('expanded')?'Обычный размер':'На весь экран';notifyViewport()};toolbar.append(expand);if(tv)toolbar.append(input);d.append(toolbar,caption,frame);
  let inflight=false,disposed=false,remote=false;
  function send(data,transfer=[]){if(!disposed&&frame.isConnected)frame.contentWindow.postMessage(data,'*',transfer)}
  function notifyViewport(){send({channel:'alicekit-input-v1',type:'viewport',width:frame.clientWidth,height:frame.clientHeight})}
  const inputController={get active(){return remote},key(key){send({channel:'alicekit-input-v1',type:'keydown',key})},leave(){remote=false;modal?.classList.remove('interacting');input.textContent='Управлять сценой';input.focus()}};
  input.onclick=()=>{remote=true;modal?.classList.add('interacting');input.textContent='Назад — выйти из сцены';window.StationPluginInput=inputController;inputController.key('focus')};
  const onKey=e=>{if(!remote||disposed)return;const key={ArrowUp:'up',ArrowDown:'down',ArrowLeft:'left',ArrowRight:'right',Enter:'enter',Escape:'back'}[e.key];if(!key)return;e.preventDefault();e.stopPropagation();if(key==='back')inputController.leave();else inputController.key(key)};
  document.addEventListener('keydown',onKey,true);
  const receive=async event=>{
   if(disposed||event.source!==frame.contentWindow||event.data?.channel!=='station-studio-v1'||typeof event.data.id!=='string'||event.data.id.length>80)return;
   const q=event.data;if(inflight){send({channel:'station-studio-v1',id:q.id,error:'Дождитесь завершения предыдущего запроса'});return}inflight=true;
   try{let result,transfer=[];
    if(q.action==='settings.read')result=await api('plugins/settings?name='+encodeURIComponent(name));
    else if(q.action==='settings.write')result=await api('plugins/settings?name='+encodeURIComponent(name),{values:q.values});
    else if(q.action==='home.read')result=await api('plugins/home?name='+encodeURIComponent(name));
    else if(q.action==='home.control')result=await api('plugins/home?name='+encodeURIComponent(name),q.data);
    else if(q.action==='runtime.info')result={version:'2.6',edition:tv?'tv':'touch',input:tv?'remote':'pointer',assetLimit:16*1024*1024,htmlLimit:256*1024,externalNetwork:false,assets:true};
    else if(q.action==='assets.read'){
     if(typeof q.path!=='string'||q.path.length>512)throw Error('Укажите путь ресурса внутри своего плагина');
     const r=await fetch('/api/plugins/asset?name='+encodeURIComponent(name)+'&path='+encodeURIComponent(q.path),{headers:{Authorization:'Bearer '+(localStorage.getItem('stationAccess')||'')}});
     if(!r.ok){const j=await r.json();throw Error(j.error||'Ресурс недоступен')}const bytes=await r.arrayBuffer();result={path:q.path,mime:r.headers.get('Content-Type')||'application/octet-stream',bytes};transfer=[bytes];
    }else throw Error('Действие недоступно интерфейсу');
    send({channel:'station-studio-v1',id:q.id,result},transfer)
   }catch(e){send({channel:'station-studio-v1',id:q.id,error:e.message})}finally{inflight=false}
  };
  window.addEventListener('message',receive);frame.onload=notifyViewport;
  const observer=new MutationObserver(()=>{if(!frame.isConnected)dispose()});observer.observe(document.body,{subtree:true,childList:true});
  const resize=window.ResizeObserver?new ResizeObserver(notifyViewport):null;resize?.observe(frame);
  function dispose(){if(disposed)return;disposed=true;remote=false;window.removeEventListener('message',receive);document.removeEventListener('keydown',onKey,true);observer.disconnect();resize?.disconnect();if(window.StationPluginInput===inputController)delete window.StationPluginInput;modal?.classList.remove('plugin-viewer','expanded','interacting');frame.srcdoc='';modal?.removeEventListener('close',onClose)}
  function onClose(){if(!modal?.open)dispose()}
  Promise.resolve().then(()=>{modal=d.closest('dialog');modal?.classList.add('plugin-viewer');if(settings.panel.uiMode==='scene')modal?.classList.add('expanded');modal?.addEventListener('close',onClose);notifyViewport()});
 })
}};
})();
