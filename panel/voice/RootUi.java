package local.station.studio;

import android.app.UiAutomation;
import android.os.*;
import android.graphics.Rect;
import android.accessibilityservice.AccessibilityServiceInfo;
import android.view.accessibility.AccessibilityNodeInfo;
import org.xmlpull.v1.XmlSerializer;
import java.io.*;

/** Bounded, read-only hierarchy snapshot. Does not wait for animated UI to idle. */
public final class RootUi {
 private static int nodes;
 private static String clean(CharSequence value){if(value==null)return "";String s=value.toString();StringBuilder out=new StringBuilder();for(int i=0;i<s.length()&&out.length()<500;i++){char c=s.charAt(i);if(c>=32||c=='\n'||c=='\t'||c=='\r')out.append(c);}return out.toString();}
 private static void node(XmlSerializer xml,AccessibilityNodeInfo n,int depth)throws Exception{
  if(n==null||depth>35||nodes++>4000)return;
  xml.startTag(null,"node");xml.attribute(null,"text",clean(n.getText()));xml.attribute(null,"content-desc",clean(n.getContentDescription()));xml.attribute(null,"package",clean(n.getPackageName()));xml.attribute(null,"class",clean(n.getClassName()));xml.attribute(null,"resource-id",clean(n.getViewIdResourceName()));xml.attribute(null,"clickable",Boolean.toString(n.isClickable()));xml.attribute(null,"focusable",Boolean.toString(n.isFocusable()));xml.attribute(null,"enabled",Boolean.toString(n.isEnabled()));Rect r=new Rect();n.getBoundsInScreen(r);xml.attribute(null,"bounds","["+r.left+","+r.top+"]["+r.right+","+r.bottom+"]");
  for(int i=0;i<n.getChildCount();i++){AccessibilityNodeInfo child=n.getChild(i);if(child!=null){node(xml,child,depth+1);child.recycle();}}
  xml.endTag(null,"node");
 }
 public static void main(String[] args)throws Exception{
  if(android.os.Process.myUid()!=0||args.length!=2||!args[0].equals("com.spotify.tv.android"))throw new SecurityException("Root Spotify adapter required");
  File output=new File(args[1]);if(!output.getCanonicalPath().matches("/data/local/tmp/station-panel/search-view-[a-f0-9]+\\.xml"))throw new SecurityException("Invalid snapshot location");
  HandlerThread thread=new HandlerThread("station-ui-snapshot");thread.start();UiAutomation automation=null;
  try{
   Object connection=Class.forName("android.app.UiAutomationConnection").getConstructor().newInstance();
   automation=(UiAutomation)UiAutomation.class.getConstructor(Looper.class,Class.forName("android.app.IUiAutomationConnection")).newInstance(thread.getLooper(),connection);
   UiAutomation.class.getMethod("connect",int.class).invoke(automation,1);
   AccessibilityServiceInfo info=automation.getServiceInfo();info.flags|=AccessibilityServiceInfo.FLAG_REPORT_VIEW_IDS|AccessibilityServiceInfo.FLAG_INCLUDE_NOT_IMPORTANT_VIEWS;automation.setServiceInfo(info);
   AccessibilityNodeInfo root=null;for(int i=0;i<8&&root==null;i++){root=automation.getRootInActiveWindow();if(root==null)Thread.sleep(100);}
   if(root==null||!args[0].contentEquals(root.getPackageName()))throw new IOException("Spotify is not the active window");
   try(FileOutputStream stream=new FileOutputStream(output)){XmlSerializer xml=android.util.Xml.newSerializer();xml.setOutput(stream,"UTF-8");xml.startDocument("UTF-8",true);xml.startTag(null,"hierarchy");node(xml,root,0);xml.endTag(null,"hierarchy");xml.endDocument();}
   root.recycle();System.out.println("STUDIO_UI_SNAPSHOT_OK "+nodes);
  }finally{if(automation!=null)try{UiAutomation.class.getMethod("disconnect").invoke(automation);}catch(Exception ignored){}thread.quitSafely();}
  System.exit(0);
 }
}
