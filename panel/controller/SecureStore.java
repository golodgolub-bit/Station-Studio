package local.station.controller;
import android.content.Context;
import android.security.keystore.*;
import android.util.Base64;
import javax.crypto.*;
import javax.crypto.spec.GCMParameterSpec;
import java.security.*;
public class SecureStore {
 private final Context context;private final SecretKey key;
 public SecureStore(Context c)throws Exception{context=c;KeyStore store=KeyStore.getInstance("AndroidKeyStore");store.load(null);String alias="station-studio-connection-v2";if(!store.containsAlias(alias)){KeyGenerator generator=KeyGenerator.getInstance(KeyProperties.KEY_ALGORITHM_AES,"AndroidKeyStore");generator.init(new KeyGenParameterSpec.Builder(alias,KeyProperties.PURPOSE_ENCRYPT|KeyProperties.PURPOSE_DECRYPT).setBlockModes(KeyProperties.BLOCK_MODE_GCM).setEncryptionPaddings(KeyProperties.ENCRYPTION_PADDING_NONE).build());generator.generateKey();}key=(SecretKey)store.getKey(alias,null);}
 public void save(String value)throws Exception{savePrivate("connection",value);}
 public String load()throws Exception{return loadPrivate("connection");}
 public void savePrivate(String name,String value)throws Exception{Cipher cipher=Cipher.getInstance("AES/GCM/NoPadding");cipher.init(Cipher.ENCRYPT_MODE,key);String out=Base64.encodeToString(cipher.getIV(),Base64.NO_WRAP)+":"+Base64.encodeToString(cipher.doFinal(value.getBytes("UTF-8")),Base64.NO_WRAP);if(!context.getSharedPreferences("station-private",0).edit().putString(name,out).commit())throw new java.io.IOException("Не удалось сохранить защищённые данные");}
 public String loadPrivate(String name)throws Exception{String value=context.getSharedPreferences("station-private",0).getString(name,null);if(value==null)return null;String[] parts=value.split(":",2);Cipher cipher=Cipher.getInstance("AES/GCM/NoPadding");cipher.init(Cipher.DECRYPT_MODE,key,new GCMParameterSpec(128,Base64.decode(parts[0],Base64.NO_WRAP)));return new String(cipher.doFinal(Base64.decode(parts[1],Base64.NO_WRAP)),"UTF-8");}
 public void clearPrivate(String name){context.getSharedPreferences("station-private",0).edit().remove(name).commit();}
 public void clear(){context.getSharedPreferences("station-private",0).edit().remove("connection").apply();}
}
