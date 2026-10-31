package local.station.studio;
import android.content.*;
import android.content.pm.*;
import android.graphics.*;
import android.graphics.drawable.Drawable;
import android.os.IBinder;
import org.json.*;
import java.io.*;
public class AssetsService extends CommandService {
 @Override public int onStartCommand(Intent intent,int flags,int startId){new Thread(()->{try{JSONArray apps=new JSONArray();PackageManager pm=getPackageManager();for(ApplicationInfo app:pm.getInstalledApplications(0)){String pkg=app.packageName;if(!app.enabled||pkg.equals("com.google.android.gms")||pkg.equals("com.google.android.gsf"))continue;if((app.flags&ApplicationInfo.FLAG_SYSTEM)!=0&&!pkg.equals("com.android.vending")&&!pkg.equals("com.android.settings"))continue;if(pm.getLeanbackLaunchIntentForPackage(pkg)==null&&pm.getLaunchIntentForPackage(pkg)==null)continue;Drawable icon=pm.getApplicationIcon(app);Bitmap bitmap=Bitmap.createBitmap(144,144,Bitmap.Config.ARGB_8888);Canvas canvas=new Canvas(bitmap);icon.setBounds(0,0,144,144);icon.draw(canvas);File file=new File(getFilesDir(),"icon-"+pkg+".png");try(FileOutputStream out=new FileOutputStream(file)){bitmap.compress(Bitmap.CompressFormat.PNG,100,out);}bitmap.recycle();JSONObject row=new JSONObject();row.put("package",pkg);row.put("name",pm.getApplicationLabel(app).toString());apps.put(row);}File temp=new File(getFilesDir(),"apps-index.tmp");try(FileOutputStream out=new FileOutputStream(temp)){out.write(apps.toString().getBytes("UTF-8"));}temp.renameTo(new File(getFilesDir(),"apps-index.json"));}catch(Exception ignored){}stopSelf(startId);},"Station-app-images").start();return START_NOT_STICKY;}
 @Override public IBinder onBind(Intent i){return null;}
}
