package local.station.controller;

import android.content.Context;
import org.json.JSONObject;
import javax.net.ssl.*;
import java.io.*;
import java.net.URL;
import java.security.*;
import java.security.cert.*;
import java.security.interfaces.RSAPublicKey;

/** Reuses only an already paired, pinned TLS connection. No discovery-based trust. */
final class SetupAccess {
 static JSONObject pairedStatus(Context c,String host)throws Exception{
  String saved=new SecureStore(c).load();if(saved==null)return null;JSONObject record=new JSONObject(saved);
  final String pin=record.getString("pin");SSLContext ssl=SSLContext.getInstance("TLS");ssl.init(null,new TrustManager[]{new X509TrustManager(){public void checkClientTrusted(X509Certificate[] chain,String auth)throws CertificateException{throw new CertificateException();}public void checkServerTrusted(X509Certificate[] chain,String auth)throws CertificateException{try{if(chain.length==0)throw new Exception();chain[0].checkValidity();String actual=PublicUpdates.hex(MessageDigest.getInstance("SHA-256").digest(chain[0].getEncoded()));if(!MessageDigest.isEqual(pin.getBytes("US-ASCII"),actual.getBytes("US-ASCII")))throw new Exception();}catch(Exception e){throw new CertificateException("Сертификат станции изменился");}}public X509Certificate[]getAcceptedIssuers(){return new X509Certificate[0];}}},new SecureRandom());
  HttpsURLConnection connection=(HttpsURLConnection)LanNetwork.open(c,new URL("https://"+host+":8789/api/setup/status"));connection.setSSLSocketFactory(ssl.getSocketFactory());connection.setHostnameVerifier((h,s)->h.equals(host));connection.setRequestProperty("Authorization","Bearer "+record.getString("token"));connection.setConnectTimeout(2200);connection.setReadTimeout(3500);connection.setInstanceFollowRedirects(false);
  try{if(connection.getResponseCode()!=200)return null;ByteArrayOutputStream out=new ByteArrayOutputStream();try(InputStream in=connection.getInputStream()){byte[] b=new byte[4096];int n;while((n=in.read(b))!=-1){out.write(b,0,n);if(out.size()>32768)throw new IOException("Слишком большой ответ");}}return new JSONObject(new String(out.toByteArray(),"UTF-8"));}finally{connection.disconnect();}
 }
 /** Explicit public-key handoff through a root terminal the owner already controls. */
 static String authorizeCommand(KeyPair key){String line=NativeAdb.base64(NativeAdb.adbPublic((RSAPublicKey)key.getPublic()))+" alicekit@android";return "set -e\n[ \"$(id -u)\" = 0 ] || { echo ALICEKIT_NEEDS_ROOT_TERMINAL; exit 1; }\n[ \"$(getprop ro.product.device)\" = yandexstation_2 ] || { echo ALICEKIT_UNSUPPORTED_MODEL; exit 1; }\n[ -d /data/misc/adb ] && [ ! -L /data/misc/adb ] || { echo ALICEKIT_ADB_DIRECTORY_UNAVAILABLE; exit 1; }\nk=/data/misc/adb/adb_keys\n[ ! -L \"$k\" ] || { echo ALICEKIT_UNSAFE_KEY_FILE; exit 1; }\n[ ! -e \"$k\" ] || [ -f \"$k\" ] || exit 1\numask 077\ntouch \"$k\"\ngrep -qF '"+line+"' \"$k\" || printf '\\n%s\\n' '"+line+"' >> \"$k\"\nchmod 640 \"$k\"\nchown 1000:2000 \"$k\"\nrestorecon \"$k\" 2>/dev/null || true\necho ALICEKIT_PUBLIC_KEY_ADDED\n";}
}
