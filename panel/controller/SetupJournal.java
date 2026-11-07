package local.station.controller;

import android.content.Context;
import org.json.*;
import java.io.*;
import java.text.SimpleDateFormat;
import java.util.*;

/** Bounded app-private setup history. Never accepts command output or credentials. */
final class SetupJournal {
 private static final Object LOCK=new Object();
 private final File file;
 SetupJournal(Context c){file=new File(c.getFilesDir(),"setup-journal.json");}
 static String safe(String s){if(s==null)return "";s=s.replaceAll("(?i)(Bearer\\s+)[^\\s]+","$1[скрыто]").replaceAll("https?://[^\\s]+","[ссылка]").replaceAll("[A-Za-z0-9_+/=-]{40,}","[скрыто]").replaceAll("[\\p{Cntrl}&&[^\\n\\t]]","");return s.substring(0,Math.min(600,s.length()));}
 JSONArray entries(){synchronized(LOCK){try(FileInputStream in=new FileInputStream(file)){if(file.length()>160000)return new JSONArray();ByteArrayOutputStream out=new ByteArrayOutputStream();byte[] b=new byte[4096];int n;while((n=in.read(b))!=-1)out.write(b,0,n);return new JSONArray(new String(out.toByteArray(),"UTF-8"));}catch(Exception e){return new JSONArray();}}}
 void add(String code,String title,String detail){synchronized(LOCK){try{JSONArray old=entries(),next=new JSONArray();for(int i=Math.max(0,old.length()-199);i<old.length();i++)next.put(old.get(i));next.put(new JSONObject().put("time",System.currentTimeMillis()).put("code",safe(code)).put("title",safe(title)).put("detail",safe(detail)));File tmp=new File(file.getPath()+".tmp");try(FileOutputStream out=new FileOutputStream(tmp)){out.write(next.toString().getBytes("UTF-8"));out.getFD().sync();}if(!tmp.renameTo(file))tmp.delete();}catch(Exception ignored){}}}
 void clear(){synchronized(LOCK){file.delete();}}
 String report(){StringBuilder b=new StringBuilder("AliceKit · журнал подключения\nБез аудио, паролей, токенов и ключей. Локальные адреса могут присутствовать.\n\n");JSONArray all=entries();SimpleDateFormat f=new SimpleDateFormat("dd.MM HH:mm:ss",Locale.ROOT);for(int i=0;i<all.length();i++){JSONObject e=all.optJSONObject(i);if(e!=null)b.append(f.format(new Date(e.optLong("time")))).append(" · ").append(e.optString("code")).append(" · ").append(e.optString("title")).append("\n").append(e.optString("detail")).append("\n\n");}return b.toString();}
}
