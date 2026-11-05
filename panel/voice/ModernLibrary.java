package local.station.studio;

import android.content.*;
import android.os.*;
import androidx.media3.common.MediaItem;
import androidx.media3.session.*;
import com.google.common.util.concurrent.ListenableFuture;
import org.json.*;
import java.util.concurrent.*;
import java.util.concurrent.atomic.*;

/** Current Media3 protocol, with no dependency on the player's screen layout. */
public final class ModernLibrary {
 public static JSONObject execute(Context context,String operation,ComponentName service,String query){
  JSONObject output=new JSONObject();try{output.put("protocol","media3");}catch(Exception ignored){}HandlerThread thread=new HandlerThread("station-media3");thread.start();Handler handler=new Handler(thread.getLooper());
  CountDownLatch done=new CountDownLatch(1);AtomicReference<MediaBrowser> browser=new AtomicReference<>();AtomicBoolean reading=new AtomicBoolean();
  java.util.concurrent.Executor executor=command->handler.post(command);
  Runnable read=()->{
   MediaBrowser client=browser.get();if(client==null||done.getCount()==0||!reading.compareAndSet(false,true))return;
   try{ListenableFuture<LibraryResult<com.google.common.collect.ImmutableList<MediaItem>>> results=client.getSearchResult(query,0,30,null);
    results.addListener(()->{try{LibraryResult<com.google.common.collect.ImmutableList<MediaItem>> result=results.get();if(result.resultCode!=0)throw new Exception("Каталог не вернул результаты ("+result.resultCode+")");JSONArray rows=new JSONArray();if(result.value!=null)for(MediaItem item:result.value){JSONObject row=new JSONObject();row.put("id",item.mediaId);row.put("title",String.valueOf(item.mediaMetadata.title));row.put("artist",String.valueOf(item.mediaMetadata.artist));row.put("playable",Boolean.TRUE.equals(item.mediaMetadata.isPlayable));rows.put(row);}output.put("ok",true);output.put("protocol","media3");output.put("rows",rows);done.countDown();}catch(Throwable e){failure(output,e);done.countDown();}},executor);
   }catch(Throwable e){failure(output,e);done.countDown();}
  };
  handler.post(()->{try{
   SessionToken token=new SessionToken(context,service);
   ListenableFuture<MediaBrowser> connection=new MediaBrowser.Builder(context,token).setApplicationLooper(thread.getLooper()).setListener(new MediaBrowser.Listener(){
    @Override public void onSearchResultChanged(MediaBrowser client,String text,int count,MediaLibraryService.LibraryParams params){if(query.equals(text))read.run();}
   }).buildAsync();
   connection.addListener(()->{try{
    MediaBrowser client=connection.get();browser.set(client);JSONArray playerCommands=new JSONArray();for(int i=0;i<client.getAvailableCommands().size();i++)playerCommands.put(client.getAvailableCommands().get(i));JSONArray sessionCommands=new JSONArray();for(SessionCommand command:client.getAvailableSessionCommands().commands){JSONObject entry=new JSONObject();entry.put("code",command.commandCode);if(command.customAction!=null)entry.put("action",command.customAction);sessionCommands.put(entry);}output.put("playerCommands",playerCommands);output.put("sessionCommands",sessionCommands);
    if("play-id".equals(operation)||"search-play".equals(operation)){if(!client.isCommandAvailable(androidx.media3.common.Player.COMMAND_SET_MEDIA_ITEM))throw new Exception("Плеер не разрешает выбирать песню через Media3");MediaItem item="play-id".equals(operation)?new MediaItem.Builder().setMediaId(query).build():new MediaItem.Builder().setRequestMetadata(new MediaItem.RequestMetadata.Builder().setSearchQuery(query).build()).build();client.setMediaItem(item);client.prepare();client.play();output.put("ok",true);output.put("dispatched",true);output.put("protocol","media3");done.countDown();return;}
    ListenableFuture<LibraryResult<Void>> search=client.search(query,null);
    search.addListener(()->{try{LibraryResult<Void> result=search.get();if(result.resultCode!=0)throw new Exception("Плеер отклонил поиск в каталоге (Media3 "+result.resultCode+")");handler.postDelayed(read,600);}catch(Throwable e){failure(output,e);done.countDown();}},executor);
   }catch(Throwable e){failure(output,e);done.countDown();}},executor);
  }catch(Throwable e){failure(output,e);done.countDown();}});
  try{if(!done.await(8,TimeUnit.SECONDS))throw new Exception("Плеер не ответил на запрос Media3");}catch(Throwable e){failure(output,e);}
  handler.postDelayed(()->{MediaBrowser client=browser.get();if(client!=null)client.release();thread.quitSafely();},("play-id".equals(operation)||"search-play".equals(operation))?10000:500);
  return output;
 }
 private static void failure(JSONObject output,Throwable error){while(error.getCause()!=null)error=error.getCause();try{output.put("ok",false);output.put("error",error.getMessage()==null?error.getClass().getSimpleName():error.getMessage());}catch(Exception ignored){}}
}
