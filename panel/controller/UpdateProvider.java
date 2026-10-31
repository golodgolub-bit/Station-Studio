package local.station.controller;
import android.content.*;
import android.database.Cursor;
import android.net.Uri;
import android.os.*;
import java.io.*;
public class UpdateProvider extends ContentProvider {
 public boolean onCreate(){return true;}
 @Override public ParcelFileDescriptor openFile(Uri uri,String mode)throws FileNotFoundException{if(!"r".equals(mode)||!"/update.apk".equals(uri.getPath()))throw new FileNotFoundException();return ParcelFileDescriptor.open(new File(getContext().getCacheDir(),"studio-update.apk"),ParcelFileDescriptor.MODE_READ_ONLY);}
 public String getType(Uri uri){return "application/vnd.android.package-archive";}public Cursor query(Uri u,String[]p,String s,String[]a,String o){return null;}public Uri insert(Uri u,ContentValues v){throw new UnsupportedOperationException();}public int update(Uri u,ContentValues v,String s,String[]a){throw new UnsupportedOperationException();}public int delete(Uri u,String s,String[]a){throw new UnsupportedOperationException();}
}
