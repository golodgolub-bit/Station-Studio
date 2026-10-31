package local.station.studio;
import android.app.*;
import android.content.Intent;
import android.os.Build;
public abstract class CommandService extends Service {
 @Override public void onCreate(){super.onCreate();if(Build.VERSION.SDK_INT>=26){NotificationManager m=(NotificationManager)getSystemService(NOTIFICATION_SERVICE);m.createNotificationChannel(new NotificationChannel("station-control","Управление станцией",NotificationManager.IMPORTANCE_MIN));Notification.Builder b=new Notification.Builder(this,"station-control").setSmallIcon(android.R.drawable.ic_media_play).setContentTitle("Station Studio").setContentText("Команды и оформление станции").setContentIntent(PendingIntent.getActivity(this,0,new Intent(this,TvActivity.class),PendingIntent.FLAG_UPDATE_CURRENT));startForeground(getClass().getName().hashCode()&0x7fffffff,b.build());}}
}
