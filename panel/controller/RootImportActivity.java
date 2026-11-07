package local.station.controller;

import android.app.*;import android.content.*;import android.net.Uri;import android.os.Bundle;import android.widget.TextView;
import org.json.JSONObject;import java.io.*;

/** Receive the author's original file through Android Share. Never starts root or installs files. */
public final class RootImportActivity extends Activity {
 @Override public void onCreate(Bundle saved){super.onCreate(saved);TextView text=new TextView(this);text.setText("Проверяем установщик…\n\nФайл останется в закрытом хранилище AliceKit.");text.setTextSize(18);text.setPadding(32,64,32,32);setContentView(text);
  Uri uri=Intent.ACTION_SEND.equals(getIntent().getAction())?getIntent().getParcelableExtra(Intent.EXTRA_STREAM):getIntent().getData();
  new Thread(()->{try{if(uri==null||!"content".equals(uri.getScheme()))throw new IOException("Выберите файл в Telegram → Поделиться → AliceKit");try(InputStream in=getContentResolver().openInputStream(uri)){cache(this,in);}runOnUiThread(()->{Intent next=new Intent(this,BootstrapActivity.class).addFlags(Intent.FLAG_ACTIVITY_CLEAR_TOP|Intent.FLAG_ACTIVITY_SINGLE_TOP);String host=getSharedPreferences("setup-last-target",0).getString("host",null);if(host!=null)next.putExtra("host",host);startActivity(next);finish();});}catch(Exception e){runOnUiThread(()->new AlertDialog.Builder(this).setTitle("Установщик не принят").setMessage(e.getMessage()).setPositiveButton("Закрыть",(d,w)->finish()).setOnCancelListener(d->finish()).show());}},"alicekit-import").start();
 }
 static File file(Context c){return new File(c.getFilesDir(),"verified-hybrid-124.zip");}
 static void cache(Context context,InputStream input)throws Exception{
  ByteArrayOutputStream bytes=new ByteArrayOutputStream();try(InputStream in=input){if(in==null)throw new IOException("Не удалось открыть файл");byte[] b=new byte[8192];int n;while((n=in.read(b))!=-1){if(bytes.size()+n>16*1024*1024)throw new IOException("Файл больше установщика Max 0921");bytes.write(b,0,n);}}
  byte[] raw=bytes.toByteArray();ByteArrayOutputStream profile=new ByteArrayOutputStream();try(InputStream in=context.getAssets().open("root-profile.json")){byte[] b=new byte[4096];int n;while((n=in.read(b))!=-1)profile.write(b,0,n);}
  new RootInstaller(null,new JSONObject(profile.toString("UTF-8"))).load(new ByteArrayInputStream(raw));
  File temp=new File(context.getFilesDir(),"verified-hybrid-124.pending");try(FileOutputStream out=new FileOutputStream(temp)){out.write(raw);out.getFD().sync();}if(!temp.renameTo(file(context))){temp.delete();throw new IOException("Не удалось сохранить проверенный установщик");}
 }
}
