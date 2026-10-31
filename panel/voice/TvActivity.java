package local.station.studio;
import android.app.*;
import android.os.*;
import android.content.*;
import android.net.Uri;
import android.webkit.*;
import android.view.*;
import org.json.JSONObject;
import java.io.*;

/** The station edition reads its own locally provisioned key, never an APK secret. */
public class TvActivity extends Activity {
 private WebView web;private ValueCallback<Uri[]> files;private String key;
 @Override public void onCreate(Bundle b){super.onCreate(b);getWindow().setFlags(1024,1024);
  try{key=new String(java.nio.file.Files.readAllBytes(new File(getFilesDir(),"panel.key").toPath()),"UTF-8").trim();if(!key.matches("[a-f0-9]{48}"))throw new IOException();}catch(Exception e){android.widget.TextView t=new android.widget.TextView(this);t.setText("Station Studio\nЗавершите установку агента панели для этой станции.");t.setPadding(48,48,48,48);t.setTextSize(24);setContentView(t);return;}
  web=new WebView(this);web.setBackgroundColor(0xfff2f2f7);web.getSettings().setJavaScriptEnabled(true);web.getSettings().setDomStorageEnabled(true);web.getSettings().setCacheMode(WebSettings.LOAD_NO_CACHE);web.clearCache(true);web.getSettings().setAllowFileAccess(false);web.getSettings().setAllowContentAccess(false);
  web.setWebViewClient(new WebViewClient(){@Override public void onPageFinished(WebView v,String u){if(u.startsWith("http://127.0.0.1:8787/"))v.evaluateJavascript("if(window.stationNativeConnect)window.stationNativeConnect("+JSONObject.quote(key)+");document.body.classList.add('tv-mode');document.querySelector('nav button').focus();",null);}@Override public boolean shouldOverrideUrlLoading(WebView v,String u){return !u.startsWith("http://127.0.0.1:8787/");}});
  web.setWebChromeClient(new WebChromeClient(){@Override public boolean onJsConfirm(WebView v,String u,String m,JsResult r){new AlertDialog.Builder(TvActivity.this).setMessage(m).setPositiveButton("Продолжить",(d,w)->r.confirm()).setNegativeButton("Отмена",(d,w)->r.cancel()).setOnCancelListener(d->r.cancel()).show();return true;}@Override public boolean onJsAlert(WebView v,String u,String m,JsResult r){new AlertDialog.Builder(TvActivity.this).setMessage(m).setPositiveButton("Хорошо",(d,w)->r.confirm()).setOnCancelListener(d->r.cancel()).show();return true;}@Override public boolean onShowFileChooser(WebView v,ValueCallback<Uri[]> c,FileChooserParams p){if(files!=null)files.onReceiveValue(null);files=c;try{Intent choose=new Intent(Intent.ACTION_OPEN_DOCUMENT);choose.setType("*/*");choose.addCategory(Intent.CATEGORY_OPENABLE);startActivityForResult(choose,901);}catch(Exception e){files.onReceiveValue(null);files=null;}return true;}});
  setContentView(web);web.loadUrl("http://127.0.0.1:8787/?tv=1");web.requestFocus();
 }
 @Override protected void onActivityResult(int code,int result,Intent data){super.onActivityResult(code,result,data);if(code==901&&files!=null){files.onReceiveValue(result==RESULT_OK&&data!=null&&data.getData()!=null?new Uri[]{data.getData()}:null);files=null;}}
 private boolean keyboardShowing(){android.graphics.Rect r=new android.graphics.Rect();getWindow().getDecorView().getWindowVisibleDisplayFrame(r);return getWindow().getDecorView().getRootView().getHeight()-r.bottom>getWindow().getDecorView().getRootView().getHeight()/5;}
 @Override public boolean dispatchKeyEvent(KeyEvent event){if(web!=null&&!keyboardShowing()&&event.getAction()==KeyEvent.ACTION_DOWN){String direction=event.getKeyCode()==KeyEvent.KEYCODE_DPAD_LEFT?"left":event.getKeyCode()==KeyEvent.KEYCODE_DPAD_RIGHT?"right":event.getKeyCode()==KeyEvent.KEYCODE_DPAD_UP?"up":event.getKeyCode()==KeyEvent.KEYCODE_DPAD_DOWN?"down":null;if(direction!=null){web.evaluateJavascript("if(window.stationTvKey)window.stationTvKey("+JSONObject.quote(direction)+")",null);return true;}if(event.getKeyCode()==KeyEvent.KEYCODE_DPAD_CENTER){web.evaluateJavascript("window.stationTvActivate?window.stationTvActivate():null",value->{if("\"input\"".equals(value))((android.view.inputmethod.InputMethodManager)getSystemService(INPUT_METHOD_SERVICE)).showSoftInput(web,android.view.inputmethod.InputMethodManager.SHOW_IMPLICIT);});return true;}}return super.dispatchKeyEvent(event);}
 @Override public void onBackPressed(){if(web!=null)web.evaluateJavascript("window.stationTvBack?window.stationTvBack():false",value->{if(!"true".equals(value))finish();});else super.onBackPressed();}
 @Override public void onDestroy(){if(files!=null)files.onReceiveValue(null);if(web!=null)web.destroy();super.onDestroy();}
}
