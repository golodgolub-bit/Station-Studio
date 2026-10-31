package local.station.studio;
import android.content.*;
import android.os.*;
import org.json.JSONObject;
import java.io.*;
import java.lang.reflect.*;
import android.net.LocalServerSocket;
import android.net.LocalSocket;

/** Root broker for the existing signature-protected conversation service. */
public final class NativeConversation {
 public static final String NAME="station.studio.conversation",TOKEN="local.station.studio.NativeConversation";
 private static final String STATUS="/data/local/tmp/station-panel/native-conversation.json";
 private static volatile IBinder remote,application;private static volatile Object connection;private static Binder serviceCallback;private static long bindingSince;private static int helperUid;private static long stops;private static String error="Waiting for the panel helper";
 private static final Binder client=new Binder();
 private static void mediaServer(){try{Class<?> at=Class.forName("android.app.ActivityThread");Object thread=at.getMethod("systemMain").invoke(null);final Context context=(Context)at.getMethod("getSystemContext").invoke(thread);new Thread(()->{try{
  try(LocalServerSocket listener=new LocalServerSocket("stationstudio.media.v1")){for(;;){try(LocalSocket socket=listener.accept()){
   if(socket.getPeerCredentials().getUid()!=0)continue;socket.setSoTimeout(12000);
   ByteArrayOutputStream buffer=new ByteArrayOutputStream();InputStream in=socket.getInputStream();int value;
   while((value=in.read())!=-1&&value!='\n'){if(buffer.size()>=4096)throw new IOException("Media request too large");buffer.write(value);}
   JSONObject input=new JSONObject(buffer.toString("UTF-8"));String op=input.getString("operation"),pkg=input.getString("package"),query=input.optString("query","");
   if(!java.util.Arrays.asList("status","metadata","providers","search","uri","seek","seek-by","shuffle","repeat","pause","play","next","previous").contains(op)||!pkg.matches("[a-zA-Z0-9_]+(?:\\.[a-zA-Z0-9_]+)+")||query.length()>500)throw new IOException("Invalid media request");
   byte[] response=(RootMedia.execute(context,new String[]{op,pkg,query}).toString()+"\n").getBytes("UTF-8");socket.getOutputStream().write(response);
  }catch(Exception ignored){}}}
 }catch(Throwable ignored){}},"station-media-controls").start();}catch(Throwable ignored){}}
 private static Object manager()throws Exception{return Class.forName("android.app.ActivityManager").getMethod("getService").invoke(null);}
 private static synchronized void status(){try{JSONObject out=new JSONObject();out.put("ready",remote!=null&&remote.isBinderAlive());out.put("attached",application!=null);out.put("stops",stops);out.put("error",error);out.put("time",System.currentTimeMillis());File temp=new File(STATUS+".tmp");try(FileOutputStream stream=new FileOutputStream(temp)){stream.write(out.toString().getBytes("UTF-8"));}android.system.Os.chmod(temp.getPath(),0600);temp.renameTo(new File(STATUS));}catch(Exception ignored){}}
 private static String detail(Throwable e){while(e instanceof InvocationTargetException&&e.getCause()!=null)e=e.getCause();return e.getClass().getSimpleName()+": "+String.valueOf(e.getMessage());}
 private static synchronized boolean stop(){IBinder target=remote;if(target==null||!target.isBinderAlive())return false;Parcel data=Parcel.obtain(),reply=Parcel.obtain();try{byte[] request={42,0};data.writeInt(request.length);data.writeByteArray(request);data.writeStrongBinder(client);if(!target.transact(3,data,reply,0))throw new IOException("Conversation method unavailable");reply.readException();int length=reply.readInt();byte[] response=reply.createByteArray();boolean ok=response!=null&&length==response.length&&response.length==2&&response[0]==8&&response[1]==1;if(ok){stops++;error="";}else error="Conversation service rejected stop";status();return ok;}catch(Throwable e){error=detail(e);status();return false;}finally{data.recycle();reply.recycle();}}
 private static synchronized void attach(IBinder caller)throws Exception{
  if(caller==null||!caller.isBinderAlive())throw new IOException("Panel application unavailable");
  if(caller.equals(application)&&remote!=null&&remote.isBinderAlive())return;
  if(caller.equals(application)&&connection!=null&&SystemClock.elapsedRealtime()-bindingSince<15000)return;
  Class<?> connectionType=Class.forName("android.app.IServiceConnection");Object am=manager();
  if(connection!=null)try{Class.forName("android.app.IActivityManager").getMethod("unbindService",connectionType).invoke(am,connection);}catch(Exception ignored){}
  remote=null;application=caller;bindingSince=SystemClock.elapsedRealtime();
  Binder callback=new Binder(){@Override protected boolean onTransact(int code,Parcel data,Parcel reply,int flags)throws RemoteException{
   if(code==INTERFACE_TRANSACTION){reply.writeString("android.app.IServiceConnection");return true;}if(code!=1)return super.onTransact(code,data,reply,flags);
   data.enforceInterface("android.app.IServiceConnection");ComponentName name=data.readInt()!=0?ComponentName.CREATOR.createFromParcel(data):null;IBinder service=data.readStrongBinder();boolean dead=data.dataAvail()>=4&&data.readInt()!=0;
   if(dead||service==null||name==null||!"com.yandex.io.sdk".equals(name.getPackageName())){remote=null;error="Conversation service disconnected";}else{remote=service;error="";}status();return true;
  }};
  serviceCallback=callback;connection=Class.forName("android.app.IServiceConnection$Stub").getMethod("asInterface",IBinder.class).invoke(null,callback);
  Intent intent=new Intent("ru.yandex.quasar.io.sdk.START_ALICE_SERVICE").setPackage("com.yandex.io.sdk");
  Method bind=Class.forName("android.app.IActivityManager").getMethod("bindService",Class.forName("android.app.IApplicationThread"),IBinder.class,Intent.class,String.class,connectionType,int.class,String.class,int.class);
  Object app=Class.forName("android.app.IApplicationThread$Stub").getMethod("asInterface",IBinder.class).invoke(null,caller);
  int result=((Number)bind.invoke(am,app,null,intent,null,connection,Context.BIND_AUTO_CREATE,"local.station.studio",0)).intValue();if(result<=0)throw new IOException("Conversation bind failed: "+result);error=remote!=null&&remote.isBinderAlive()?"":"Binding conversation service";status();
 }
 public static void main(String[] args)throws Exception{
  if(android.os.Process.myUid()!=0||args.length!=1)throw new SecurityException("Root agent required");helperUid=Integer.parseInt(args[0]);if(helperUid<10000)throw new SecurityException("Invalid helper UID");Looper.prepareMainLooper();
  Binder broker=new Binder(){@Override protected boolean onTransact(int code,Parcel data,Parcel reply,int flags)throws RemoteException{
   if(code==INTERFACE_TRANSACTION){reply.writeString(TOKEN);return true;}int uid=Binder.getCallingUid();if(uid!=helperUid&&uid!=0)throw new SecurityException("Panel helper required");data.enforceInterface(TOKEN);long identity=Binder.clearCallingIdentity();try{if(code==1){try{attach(data.readStrongBinder());reply.writeNoException();reply.writeInt(1);}catch(Throwable e){error=detail(e);status();reply.writeNoException();reply.writeInt(0);}return true;}if(code==2){boolean ok=stop();reply.writeNoException();reply.writeInt(ok?1:0);return true;}return super.onTransact(code,data,reply,flags);}finally{Binder.restoreCallingIdentity(identity);}
  }};
  Handler handler=new Handler(Looper.getMainLooper());Runnable publish=new Runnable(){public void run(){try{if(application==null||!application.isBinderAlive()||remote==null||!remote.isBinderAlive()){Intent notify=new Intent().setComponent(new ComponentName("local.station.studio","local.station.studio.VisualService"));Bundle extras=new Bundle();extras.putBinder("conversationBroker",broker);notify.putExtras(extras);notify.putExtra("operation","conversation-broker");Class.forName("android.app.IActivityManager").getMethod("startService",Class.forName("android.app.IApplicationThread"),Intent.class,String.class,boolean.class,String.class,int.class).invoke(manager(),null,notify,null,true,"com.android.shell",0);}}catch(Throwable e){error=detail(e);status();}handler.postDelayed(this,5000);}};
  status();mediaServer();handler.post(publish);Looper.loop();
 }
}
