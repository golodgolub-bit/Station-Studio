package local.station.controller;

import org.json.*;
import java.io.*;
import java.security.MessageDigest;
import java.util.*;
import java.util.zip.*;

/** Phone-side host for the community's exact 1.2.4 / 0921 installer.
 * No kernel offsets are inferred and no executable is accepted outside pinned hashes.
 */
final class RootInstaller {
 interface Transport {String shell(String command,int timeout)throws Exception;void push(String path,byte[] bytes)throws Exception;void progress(String message);default void operation(String id,String boot) {}}
 static final String FINGERPRINT="Yandex/yandexstation_2/yandexstation_2:9/PPR1.180610.011/5000000921:user/release-keys";
 private final Transport remote;private final JSONObject trusted;private Map<String,byte[]> files;private String lastId,lastBoot;private boolean attempted;
 RootInstaller(Transport transport,JSONObject trusted){remote=transport;this.trusted=trusted;}
 static String sha(byte[] bytes)throws Exception{return PublicUpdates.hex(MessageDigest.getInstance("SHA-256").digest(bytes));}
 static boolean compatible(String device,String build,String fingerprint,String kernel){return "yandexstation_2".equals(device)&&"5000000921".equals(build)&&FINGERPRINT.equals(fingerprint)&&"4.9.113".equals(kernel);}
 void load(InputStream input)throws Exception{
  files=null;
  Map<String,byte[]> found=new HashMap<>();Set<String> names=new HashSet<>();int count=0,total=0;
  try(ZipInputStream zip=new ZipInputStream(input)){ZipEntry entry;while((entry=zip.getNextEntry())!=null){String name=entry.getName();if(++count>100||name.startsWith("/")||name.contains("\\")||name.contains("..")||!names.add(name))throw new IOException("Некорректный архив Hybrid Root");if(entry.isDirectory())continue;
   String relative=null;for(Iterator<String> it=trusted.keys();it.hasNext();){String candidate=it.next();if(name.equals(candidate)||name.endsWith("/"+candidate)){relative=candidate;break;}}
   ByteArrayOutputStream out=new ByteArrayOutputStream();byte[] buffer=new byte[8192];int n;while((n=zip.read(buffer))!=-1){total+=n;if(total>16*1024*1024||out.size()+n>4*1024*1024)throw new IOException("Архив превышает размер профиля");out.write(buffer,0,n);}
   if(relative!=null){if(found.containsKey(relative)||!sha(out.toByteArray()).equals(trusted.getString(relative)))throw new IOException("Не совпал проверенный файл: "+relative);found.put(relative,out.toByteArray());}
  }}
  if(found.size()!=trusted.length())throw new IOException("Нужен полный оригинальный station-max-hybrid-1.2.4.zip для 0921");files=found;
 }
 boolean ready(){return files!=null;}
 boolean attempted(){return attempted;}
 void restore(String id,String boot){if(id!=null&&id.matches("[a-f0-9]{32}")&&boot!=null&&boot.matches("[a-f0-9-]{36}")){lastId=id;lastBoot=boot;}}
 JSONObject recover()throws Exception {
  if(lastId==null)return new JSONObject().put("state","none");
  String stage="/data/local/tmp/station-studio-install-"+lastId;
  String raw=remote.shell("cat /proc/sys/kernel/random/boot_id; echo ALICEKIT_RECEIPT; cat "+stage+"/receipt 2>/dev/null || true; echo ALICEKIT_STATE; cat /data/local/tmp/rootd-op.state 2>/dev/null || true; echo ALICEKIT_LOG; tail -n 12 "+stage+"/install.log 2>/dev/null || true",30000);
  String[] a=raw.split("\nALICEKIT_RECEIPT\n",2);if(a.length!=2)throw new IOException("Станция не вернула результат проверки");
  String[] b=a[1].split("ALICEKIT_STATE\n",2),c=b.length==2?b[1].split("ALICEKIT_LOG\n",2):new String[0];
  Map<String,String> receipt=fields(b[0]),state=fields(c.length>0?c[0]:"");
  boolean current=lastBoot.equals(a[0].trim());String operation="alicekit-"+lastId;
  boolean matched=operation.equals(receipt.get("operation_id"))&&lastBoot.equals(receipt.get("boot_id"));
  boolean completed=current&&((matched&&"0".equals(receipt.get("exit"))&&c.length==2&&c[1].contains("INSTALL_OK backup="))||(operation.equals(state.get("operation_id"))&&lastBoot.equals(state.get("boot_id"))&&"complete".equals(state.get("phase"))));
  String result=completed?"installed":!current?"rebooted":matched?"failed":"unknown";
  return new JSONObject().put("state",result).put("boot",lastBoot).put("phase",operation.equals(state.get("operation_id"))?state.get("phase"):"");
 }
 private static Map<String,String> fields(String raw){Map<String,String> out=new HashMap<>();for(String line:raw.split("\n")){int i=line.indexOf('=');if(i>0)out.put(line.substring(0,i),line.substring(i+1).trim());}return out;}
 String preflight()throws Exception {
  String[] f=remote.shell("getprop ro.product.device; getprop ro.build.version.incremental; getprop ro.build.fingerprint; uname -r; cat /proc/sys/kernel/random/boot_id; cat /proc/uptime; id -u",30000).split("\n");
  if(f.length!=7||!compatible(f[0],f[1],f[2],f[3]))throw new IOException("Этот root-профиль подходит только Max 5000000921 / ядро 4.9.113. Изменений нет.");
  if("0".equals(f[6]))throw new IOException("Root уже доступен: перейдите к установке панели");
  if(Double.parseDouble(f[5].split(" ")[0])<180)throw new IOException("После запуска станции должно пройти минимум три минуты");
  String status=remote.shell("cat /data/local/tmp/rootstatus 2>/dev/null || true",30000);if(status.contains("state=PROTECTED"))throw new IOException("Hybrid Root уже работает. Повторное получение не требуется");
  remote.shell("set -eu; [ ! -e /data/local/tmp/rootd-op.inflight ]; [ ! -L /data/local/tmp/rootd-op.inflight ]; [ ! -e /data/local/tmp/rootd-op.lock ]; [ ! -e /odm/bin/rootd ]; names=$(ps -A -o NAME); ! printf '%s\\n' \"$names\" | grep -E '^(rootd|root-bridge|esc-bridge|esc36449|cve36449)$'",30000);
  return f[4];
 }
 String install(String router,String dns)throws Exception {
  attempted=false;
  if(!ready())throw new IOException("Сначала выберите проверенный архив Hybrid Root");router=StationDiscovery.local(router);dns=StationDiscovery.local(dns);
  remote.progress("Проверяем точную прошивку и отсутствие другого root-процесса…");String boot=preflight();
  String id=UUID.randomUUID().toString().replace("-",""),operation="alicekit-"+id,stage="/data/local/tmp/station-studio-install-"+id;
  remote.shell("set -eu; umask 077; [ ! -L /data/local/tmp ]; mkdir "+stage+"; mkdir "+stage+"/hybrid",30000);
  for(String name:files.keySet()){remote.progress("Проверенный комплект: "+name);String dest=name.equals("bootstrap/root-bridge")?"root-bridge":name;remote.push(stage+"/"+dest,files.get(name));}
  remote.push(stage+"/install.conf",("ROUTER_IP="+router+"\nDNS_IP="+dns+"\nIPV6=drop\n").getBytes("US-ASCII"));
  StringBuilder sums=new StringBuilder();for(String name:files.keySet())sums.append(trusted.getString(name)).append("  ").append(name.equals("bootstrap/root-bridge")?"root-bridge":name).append('\n');remote.push(stage+"/SHA256SUMS",sums.toString().getBytes("US-ASCII"));
  remote.shell("set -eu; cd "+stage+"; sha256sum -c SHA256SUMS; chmod 755 root-bridge hybrid/permissive hybrid/install-run.sh hybrid/install-device.sh hybrid/rollback-run.sh hybrid/rollback-device.sh; printf 'format=1\\noperation_id="+operation+"\\nnonce="+id+"\\n' > .station-max-stage",30000);
  // Recheck after delivery. Atomically publish a persistent marker before the sole kernel attempt.
  if(!boot.equals(preflight()))throw new IOException("Станция перезагрузилась во время проверки; root не запускался");
  String marker="set -eu; [ \"$(cat /proc/sys/kernel/random/boot_id)\" = "+boot+" ]; set -C; umask 077; printf 'version=1\\noperation_id="+operation+"\\nboot_id="+boot+"\\nstaging_nonce="+id+"\\nstage=install\\ncreated=%s\\n' \"$(date '+%Y-%m-%dT%H:%M:%S')\" > /data/local/tmp/rootd-op.inflight";
  lastId=id;lastBoot=boot;remote.operation(id,boot);
  remote.shell(marker,30000);attempted=true;remote.progress("Получаем root и устанавливаем автозапуск. Не отключайте питание. Повторных попыток не будет.");
  String result;
  try{result=remote.shell("(cd "+stage+" && BASE="+stage+" OPERATION_ID="+operation+" STAGING_NONCE="+id+" INFLIGHT_OWNED=1 ./root-bridge esc-shell < hybrid/install-run.sh) > "+stage+"/install.log 2>&1; rc=$?; umask 077; printf 'operation_id="+operation+"\\nboot_id="+boot+"\\nexit=%s\\n' \"$rc\" > "+stage+"/receipt.new && mv "+stage+"/receipt.new "+stage+"/receipt; cat "+stage+"/install.log; exit \"$rc\"",300000);}
  catch(Exception interrupted){remote.progress("Проверяем сохранённый результат на станции. Root повторно не запускается…");try{if("installed".equals(recover().getString("state")))return boot;}catch(Exception ignored){}throw new IOException("Завершение установки пока не подтверждено. Нажмите «Проверить результат»: приложение прочитает состояние без повторного получения root.",interrupted);}
  if(!result.contains("INSTALL_OK backup="))throw new IOException("Установщик не подтвердил завершение. Повтор не выполняется. Диагностика: "+stage);
  remote.progress("Установщик подтвердил root. Резервная копия сохранена на станции. Для проверки автозапуска нужна перезагрузка.");return boot;
 }
}
