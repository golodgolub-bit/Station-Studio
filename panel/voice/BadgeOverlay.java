package local.station.studio;
import android.content.*;import android.graphics.*;import android.os.*;import android.view.*;import org.json.*;import java.io.*;

/** A small optional HDMI badge. Never creates a full-screen window or captures input. */
final class BadgeOverlay {
 private final Context context;private final Handler main=new Handler(Looper.getMainLooper());private final WindowManager wm;private View view;private String state="idle";private long previewUntil;private int generation;
 BadgeOverlay(Context c){context=c;wm=(WindowManager)c.getSystemService(Context.WINDOW_SERVICE);}
 void update(String s,boolean preview){if(s.equals("unknown"))return;if(!preview&&s.equals(state))return;state=s;if(preview)previewUntil=System.currentTimeMillis()+5000;main.post(()->render());}
 void close(){main.post(()->{generation++;if(view!=null){try{wm.removeView(view);}catch(Exception ignored){}view=null;}});}
 private void render(){generation++;final int ticket=generation;if(view!=null){try{wm.removeView(view);}catch(Exception ignored){}view=null;}
  try{File config=new File(context.getFilesDir(),"badge.json");if(!config.isFile())return;JSONObject q=new JSONObject(new String(java.nio.file.Files.readAllBytes(config.toPath()),"UTF-8"));boolean preview=System.currentTimeMillis()<previewUntil;if(q.optInt("consentVersion")!=1||!q.optBoolean("enabled")||(!preview&&state.equals("idle")))return;
   String file=q.optString("media");if(!file.equals("badge.media"))return;File source=new File(context.getFilesDir(),file);if(!source.isFile()||source.length()>8*1024*1024)return;
   byte[] data=java.nio.file.Files.readAllBytes(source.toPath());Movie movie=Movie.decodeByteArray(data,0,data.length);Bitmap bitmap=null;if(movie==null)bitmap=BitmapFactory.decodeByteArray(data,0,data.length);if(movie==null&&bitmap==null)return;final Movie animation=movie;final Bitmap image=bitmap;
   float density=context.getResources().getDisplayMetrics().density;int size=Math.round(q.optInt("size",64)*density);String shape=q.optString("shape","circle");boolean backing=q.optBoolean("backing",true);long start=SystemClock.uptimeMillis();
   view=new View(context){protected void onDraw(Canvas canvas){super.onDraw(canvas);if(ticket!=generation)return;Path clip=new Path();if(shape.equals("circle"))clip.addCircle(getWidth()/2f,getHeight()/2f,getWidth()/2f,Path.Direction.CW);else clip.addRoundRect(new RectF(0,0,getWidth(),getHeight()),shape.equals("square")?0:12*density,shape.equals("square")?0:12*density,Path.Direction.CW);canvas.save();canvas.clipPath(clip);if(backing)canvas.drawColor(Color.BLACK);int w=animation!=null?animation.width():image.getWidth(),h=animation!=null?animation.height():image.getHeight();float scale=Math.min(getWidth()/(float)w,getHeight()/(float)h);canvas.translate((getWidth()-w*scale)/2,(getHeight()-h*scale)/2);canvas.scale(scale,scale);if(animation!=null){animation.setTime((int)((SystemClock.uptimeMillis()-start)%Math.max(100,animation.duration())));animation.draw(canvas,0,0);postInvalidateDelayed(50);}else canvas.drawBitmap(image,0,0,new Paint(3));canvas.restore();}};
   WindowManager.LayoutParams p=new WindowManager.LayoutParams(size,size,WindowManager.LayoutParams.TYPE_APPLICATION_OVERLAY,WindowManager.LayoutParams.FLAG_NOT_FOCUSABLE|WindowManager.LayoutParams.FLAG_NOT_TOUCHABLE|WindowManager.LayoutParams.FLAG_LAYOUT_IN_SCREEN,PixelFormat.TRANSLUCENT);p.gravity=Gravity.LEFT|Gravity.BOTTOM;p.x=Math.round(q.optInt("x",24)*density);p.y=Math.round(q.optInt("y",24)*density);p.setTitle("Station Studio HDMI badge");wm.addView(view,p);
   if(preview)main.postDelayed(()->{if(ticket==generation)render();},5100);
  }catch(Exception ignored){}
 }
}
