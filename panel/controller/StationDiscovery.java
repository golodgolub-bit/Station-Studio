package local.station.controller;

import android.content.Context;
import android.net.nsd.*;
import android.net.wifi.WifiManager;
import android.os.*;
import org.json.*;
import java.net.*;
import java.util.*;
import java.util.concurrent.atomic.AtomicBoolean;

/** Stock Glagol/mDNS and installed AliceKit are distinct discovery evidence. */
final class StationDiscovery implements AutoCloseable {
 interface Listener {void found(JSONObject station);void finished();}
 private final Context context;private final Listener listener;private final Handler main=new Handler(Looper.getMainLooper());
 private final AtomicBoolean closed=new AtomicBoolean();private final Map<String,JSONObject> devices=new HashMap<>();
 private NsdManager manager;private NsdManager.DiscoveryListener discovery;private WifiManager.MulticastLock multicast;
 private DatagramSocket udp;private final ArrayDeque<NsdServiceInfo> resolveQueue=new ArrayDeque<>();private boolean resolving;
 StationDiscovery(Context context,Listener listener){this.context=context;this.listener=listener;}
 static String local(String host)throws Exception{if(host==null||!host.matches("[0-9.]+"))throw new Exception("Нужен локальный IPv4-адрес");InetAddress ip=InetAddress.getByName(host);if(!(ip instanceof Inet4Address)||!ip.isSiteLocalAddress())throw new Exception("Нужен адрес в домашней сети");return ip.getHostAddress();}
 void start(){
  try{WifiManager wifi=(WifiManager)context.getApplicationContext().getSystemService(Context.WIFI_SERVICE);multicast=wifi.createMulticastLock("alicekit-stock-discovery");multicast.setReferenceCounted(false);multicast.acquire();}catch(Exception ignored){}
  manager=(NsdManager)context.getSystemService(Context.NSD_SERVICE);
  discovery=new NsdManager.DiscoveryListener(){public void onDiscoveryStarted(String type){}public void onDiscoveryStopped(String type){}public void onStopDiscoveryFailed(String t,int e){}public void onStartDiscoveryFailed(String t,int e){}public void onServiceLost(NsdServiceInfo s){}
   public void onServiceFound(NsdServiceInfo s){main.post(()->{if(closed.get()||resolveQueue.size()>32)return;resolveQueue.add(s);resolveNext();});}};
  try{manager.discoverServices("_yandexio._tcp.",NsdManager.PROTOCOL_DNS_SD,discovery);}catch(Exception ignored){}
  new Thread(this::panelBroadcast,"alicekit-discovery").start();
  main.postDelayed(()->{if(!closed.get()){close();listener.finished();}},12000);
 }
 private void resolveNext(){if(resolving||closed.get()||resolveQueue.isEmpty())return;resolving=true;NsdServiceInfo service=resolveQueue.remove();
  try{manager.resolveService(service,new NsdManager.ResolveListener(){public void onResolveFailed(NsdServiceInfo s,int error){main.post(()->{resolving=false;resolveNext();});}
   public void onServiceResolved(NsdServiceInfo s){main.post(()->{try{JSONObject q=new JSONObject();q.put("host",local(s.getHost().getHostAddress()));q.put("name",s.getServiceName());q.put("source","mdns");q.put("glagolPort",s.getPort());Map<String,byte[]> attrs=s.getAttributes();for(String key:new String[]{"platform","deviceId"}){byte[] value=attrs.get(key);if(value!=null&&value.length<=128)q.put(key,new String(value,"UTF-8"));}emit(q);}catch(Exception ignored){}resolving=false;resolveNext();});}});
  }catch(Exception e){resolving=false;resolveNext();}}
 private void emit(JSONObject q){if(closed.get())return;try{String host=local(q.getString("host"));JSONObject old=devices.get(host);if(old==null)old=new JSONObject();for(Iterator<String> i=q.keys();i.hasNext();){String key=i.next();old.put(key,q.get(key));}devices.put(host,old);listener.found(new JSONObject(old.toString()));}catch(Exception ignored){}}
 private void panelBroadcast(){try(DatagramSocket socket=new DatagramSocket()){udp=socket;socket.setBroadcast(true);socket.setSoTimeout(450);byte[] query=("STATION_STUDIO_DISCOVER_V1:"+UUID.randomUUID().toString().replace("-","")).getBytes("US-ASCII");Set<String> destinations=new HashSet<>();destinations.add("255.255.255.255");Enumeration<NetworkInterface> interfaces=NetworkInterface.getNetworkInterfaces();while(interfaces.hasMoreElements())for(InterfaceAddress a:interfaces.nextElement().getInterfaceAddresses())if(a.getBroadcast()!=null&&a.getAddress().isSiteLocalAddress())destinations.add(a.getBroadcast().getHostAddress());long last=0,until=System.currentTimeMillis()+11000;
  while(!closed.get()&&System.currentTimeMillis()<until){if(System.currentTimeMillis()-last>1800){for(String host:destinations)socket.send(new DatagramPacket(query,query.length,InetAddress.getByName(host),8788));last=System.currentTimeMillis();}try{byte[] b=new byte[4096];DatagramPacket p=new DatagramPacket(b,b.length);socket.receive(p);JSONObject q=new JSONObject(new String(b,0,p.getLength(),"UTF-8"));if(q.optInt("version")<2||q.optInt("tlsPort")!=8789||!q.optBoolean("pairing"))continue;q.put("host",local(p.getAddress().getHostAddress()));q.put("panel",true);q.put("source","alicekit");main.post(()->emit(q));}catch(SocketTimeoutException ignored){}}
 }catch(Exception ignored){}finally{udp=null;}}
 public void close(){if(!closed.compareAndSet(false,true))return;if(udp!=null)udp.close();try{if(manager!=null&&discovery!=null)manager.stopServiceDiscovery(discovery);}catch(Exception ignored){}try{if(multicast!=null&&multicast.isHeld())multicast.release();}catch(Exception ignored){}resolveQueue.clear();}
}
