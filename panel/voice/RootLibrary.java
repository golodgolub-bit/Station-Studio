package local.station.studio;

import android.content.*;
import android.os.*;
import android.support.v4.media.MediaBrowserCompat;
import android.support.v4.media.session.MediaControllerCompat;
import org.json.*;
import java.util.*;
import java.util.concurrent.*;

/** Uses the installed app's Android media library. Does not scrape its account. */
public final class RootLibrary {
 public static JSONObject execute(Context context,String operation,String pkg,String query){
  Intent modern=new Intent("androidx.media3.session.MediaLibraryService").setPackage(pkg);
  java.util.List<android.content.pm.ResolveInfo> modernServices=context.getPackageManager().queryIntentServices(modern,0);
  if(!modernServices.isEmpty()){android.content.pm.ServiceInfo s=modernServices.get(0).serviceInfo;return ModernLibrary.execute(context,operation,new ComponentName(s.packageName,s.name),query);}
  JSONObject output=new JSONObject();HandlerThread thread=new HandlerThread("station-library");thread.start();Handler handler=new Handler(thread.getLooper());
  CountDownLatch ready=new CountDownLatch(1);java.util.concurrent.atomic.AtomicReference<MediaBrowserCompat> holder=new java.util.concurrent.atomic.AtomicReference<>();
  try{
   Intent find=new Intent("android.media.browse.MediaBrowserService").setPackage(pkg);
   java.util.List<android.content.pm.ResolveInfo> services=context.getPackageManager().queryIntentServices(find,0);
   if(services.isEmpty())throw new Exception("Плеер не предоставляет музыкальный каталог Android");
   android.content.pm.ServiceInfo service=services.get(0).serviceInfo;
   handler.post(()->{
    try{
     MediaBrowserCompat browser=new MediaBrowserCompat(context,new ComponentName(service.packageName,service.name),new MediaBrowserCompat.ConnectionCallback(){
      @Override public void onConnected(){
       try{
        MediaBrowserCompat client=holder.get();
        if("play-id".equals(operation)){
         MediaControllerCompat controller=new MediaControllerCompat(context,client.getSessionToken());
         controller.getTransportControls().playFromMediaId(query,new Bundle());
         output.put("ok",true);output.put("dispatched",true);ready.countDown();return;
        }
        client.search(query,new Bundle(),new MediaBrowserCompat.SearchCallback(){
         @Override public void onSearchResult(String text,Bundle extras,List<MediaBrowserCompat.MediaItem> items){
          try{JSONArray rows=new JSONArray();for(MediaBrowserCompat.MediaItem item:items){if(rows.length()>=30)break;JSONObject row=new JSONObject();row.put("id",item.getMediaId());row.put("title",String.valueOf(item.getDescription().getTitle()));row.put("artist",String.valueOf(item.getDescription().getSubtitle()));row.put("playable",item.isPlayable());rows.put(row);}output.put("rows",rows);output.put("ok",true);}catch(Exception e){error(output,e.getMessage());}ready.countDown();
         }
         @Override public void onError(String text,Bundle extras){error(output,"Плеер не разрешил поиск в музыкальном каталоге");ready.countDown();}
        });
       }catch(Exception e){error(output,e.getMessage());ready.countDown();}
      }
      @Override public void onConnectionFailed(){error(output,"Плеер отклонил подключение к каталогу");ready.countDown();}
      @Override public void onConnectionSuspended(){error(output,"Связь с каталогом плеера прервалась");ready.countDown();}
     },null);
     holder.set(browser);browser.connect();
    }catch(Exception e){error(output,e.getMessage());ready.countDown();}
   });
   if(!ready.await(8,TimeUnit.SECONDS))throw new Exception("Плеер не ответил на поиск в каталоге");
  }catch(Exception e){error(output,e.getMessage());}
  finally{
   // Media3 may resolve an item asynchronously after the Binder call returns.
   // Keep this controller alive until the app has had time to accept playback.
   handler.postDelayed(()->{MediaBrowserCompat client=holder.get();if(client!=null)client.disconnect();thread.quitSafely();},"play-id".equals(operation)?10000:500);
  }
  return output;
 }
 private static void error(JSONObject output,String message){try{output.put("ok",false);output.put("error",message==null?"Каталог плеера недоступен":message);}catch(Exception ignored){}}
}
