package local.station.controller;

import android.content.Context;
import org.json.*;
import java.io.*;
import java.text.SimpleDateFormat;
import java.util.*;

/** Bounded app-private setup history. Never accepts command output or credentials. */
final class SetupJournal {
 private static final Object LOCK=new Object();
 private final File file;private final String version;private String session=UUID.randomUUID().toString().substring(0,8);private long started=android.os.SystemClock.elapsedRealtime();
 SetupJournal(Context c){file=new File(c.getFilesDir(),"setup-journal.json");String v="unknown";try{v=c.getPackageManager().getPackageInfo(c.getPackageName(),0).versionName;}catch(Exception ignored){}version=v;}
 static String safe(String s){if(s==null)return "";s=s.replaceAll("(?i)(Bearer\\s+)[^\\s]+","$1[скрыто]").replaceAll("https?://[^\\s]+","[ссылка]").replaceAll("[A-Za-z0-9_+/=-]{40,}","[скрыто]").replaceAll("[\\p{Cntrl}&&[^\\n\\t]]","");return s.substring(0,Math.min(600,s.length()));}
 JSONArray entries(){synchronized(LOCK){try(FileInputStream in=new FileInputStream(file)){if(file.length()>1048576)return new JSONArray();ByteArrayOutputStream out=new ByteArrayOutputStream();byte[] b=new byte[4096];int n;while((n=in.read(b))!=-1)out.write(b,0,n);return new JSONArray(new String(out.toByteArray(),"UTF-8"));}catch(Exception e){return new JSONArray();}}}
 void add(String code,String title,String detail){synchronized(LOCK){try{if("CHECK_START".equals(code)){session=UUID.randomUUID().toString().substring(0,8);started=android.os.SystemClock.elapsedRealtime();}JSONArray old=entries(),next=new JSONArray();JSONObject last=old.optJSONObject(old.length()-1);long now=System.currentTimeMillis();if(last!=null&&now-last.optLong("time")<1200&&code.equals(last.optString("code"))&&safe(detail).equals(last.optString("detail"))&&safe(title).equals(last.optString("title")))return;for(int i=Math.max(0,old.length()-199);i<old.length();i++)next.put(old.get(i));next.put(new JSONObject().put("time",now).put("code",safe(code)).put("title",safe(title)).put("detail",safe(detail)).put("app",version).put("session",session).put("elapsedMs",android.os.SystemClock.elapsedRealtime()-started));File tmp=new File(file.getPath()+".tmp");try(FileOutputStream out=new FileOutputStream(tmp)){out.write(next.toString().getBytes("UTF-8"));out.getFD().sync();}if(!tmp.renameTo(file))tmp.delete();}catch(Exception ignored){}}}
 void clear(){synchronized(LOCK){file.delete();}}
 String report(){StringBuilder b=new StringBuilder("AliceKit · журнал подключения · формат 2\nПриложение "+version+" · Android API "+android.os.Build.VERSION.SDK_INT+"\nБез аудио, паролей, токенов и ключей. Локальные адреса могут присутствовать.\n\n");JSONArray all=entries();JSONObject failure=null;for(int i=0;i<all.length();i++){JSONObject row=all.optJSONObject(i);if(row!=null&&(row.optString("code").endsWith("FAILED")||Arrays.asList("SETUP_ERROR","ROOT_RESULT","ROOT_INSTALL_OK","PANEL_READY","ROOT_QUEUE_READY").contains(row.optString("code"))))failure=row;}if(failure!=null)b.append("Последнее состояние: ").append(failure.optString("title")).append("\n").append(failure.optString("detail")).append("\n\n");SimpleDateFormat f=new SimpleDateFormat("dd.MM HH:mm:ss XXX",Locale.ROOT);for(int i=0;i<all.length();i++){JSONObject e=all.optJSONObject(i);if(e!=null)b.append(f.format(new Date(e.optLong("time")))).append(" · ").append(e.optString("code")).append(" · ").append(e.optString("title")).append("\nСессия ").append(e.optString("session","legacy")).append(" · +").append(e.optLong("elapsedMs")/1000.0).append(" с · версия ").append(e.optString("app","не записана")).append("\n").append(e.optString("detail")).append("\n\n");}return b.toString();}
}
