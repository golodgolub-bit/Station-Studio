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
 interface Transport {String shell(String command,int timeout)throws Exception;void push(String path,byte[] bytes)throws Exception;void progress(String message);}
 static final String FINGERPRINT="Yandex/yandexstation_2/yandexstation_2:9/PPR1.180610.011/5000000921:user/release-keys";
 private final Transport remote;private final JSONObject trusted;private Map<String,byte[]> files;
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
  remote.shell(marker,30000);remote.progress("Получаем root и устанавливаем автозапуск. Не отключайте питание. Повторных попыток не будет.");
  String result=remote.shell("cd "+stage+" && BASE="+stage+" OPERATION_ID="+operation+" STAGING_NONCE="+id+" INFLIGHT_OWNED=1 ./root-bridge esc-shell < hybrid/install-run.sh",300000);
  if(!result.contains("INSTALL_OK backup="))throw new IOException("Установщик не подтвердил завершение. Повтор не выполняется. Диагностика: "+stage);
  remote.progress("Установщик подтвердил root. Резервная копия сохранена на станции. Для проверки автозапуска нужна перезагрузка.");return boot;
 }
}
