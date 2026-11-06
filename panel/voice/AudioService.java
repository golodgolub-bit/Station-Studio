package local.station.studio;
import android.app.Service;
import android.content.*;
import android.media.*;
import android.os.*;
import org.json.*;
import java.io.*;

/** Plays only media imported into the app's private library. No TTS engine. */
public class AudioService extends CommandService {
 private MediaPlayer player; private AudioManager audio;private boolean focus;
 private final AudioManager.OnAudioFocusChangeListener listener=change->{if(change==AudioManager.AUDIOFOCUS_LOSS)stopSelf();};
 private void release(){if(player!=null){try{player.stop();}catch(Exception ignored){}player.release();player=null;}if(focus&&audio!=null)audio.abandonAudioFocus(listener);focus=false;}
 private void result(String state,String detail){try{JSONObject o=new JSONObject();o.put("state",state);o.put("detail",detail);o.put("time",System.currentTimeMillis());try(FileOutputStream f=openFileOutput("audio-state.json",0)){f.write(o.toString().getBytes("UTF-8"));}}catch(Exception ignored){}}
 @Override public int onStartCommand(Intent i,int flags,int id){release();if(i==null||"stop".equals(i.getStringExtra("operation"))){result("stopped","");stopSelf();return START_NOT_STICKY;}
  String media=i.getStringExtra("media");if(media==null||!media.matches("[a-f0-9]{24}")){result("error","Invalid media");stopSelf();return START_NOT_STICKY;}
  try{File file=new File(getFilesDir(),"audio-"+media+".media");if(!file.isFile()||file.length()>20*1024*1024)throw new IOException("Media unavailable");audio=(AudioManager)getSystemService(AUDIO_SERVICE);focus=audio.requestAudioFocus(listener,AudioManager.STREAM_MUSIC,AudioManager.AUDIOFOCUS_GAIN_TRANSIENT_MAY_DUCK)==AudioManager.AUDIOFOCUS_REQUEST_GRANTED;if(!focus)throw new IOException("Audio focus denied");
   player=new MediaPlayer();player.setAudioAttributes(new AudioAttributes.Builder().setUsage(AudioAttributes.USAGE_MEDIA).setContentType(AudioAttributes.CONTENT_TYPE_MUSIC).build());player.setDataSource(file.getAbsolutePath());player.setLooping(false);float v=Math.max(0,Math.min(100,i.getIntExtra("volume",80)))/100f;player.setVolume(v,v);
   player.setOnPreparedListener(p->{if(p.getDuration()>300000){result("error","Maximum duration is five minutes");stopSelf();}else{p.start();result("playing",media);}});player.setOnCompletionListener(p->{result("finished",media);stopSelf();});player.setOnErrorListener((p,w,e)->{result("error","Unsupported or damaged audio");stopSelf();return true;});player.prepareAsync();
  }catch(Exception e){result("error",e.getMessage());stopSelf();}return START_NOT_STICKY;
 }
 @Override public IBinder onBind(Intent i){return null;}@Override public void onDestroy(){release();super.onDestroy();}
}
