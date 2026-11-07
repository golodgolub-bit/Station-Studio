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
  String raw=remote.shell("cat /proc/sys/kernel/random/boot_id; echo ALICEKIT_RECEIPT; if [ ! -L /data/local/tmp ] && [ ! -L "+stage+" ] && [ -d "+stage+" ]; then if [ -f "+stage+"/receipt ] && [ ! -L "+stage+"/receipt ]; then head -c 4096 "+stage+"/receipt; fi; fi; echo; echo ALICEKIT_STATE; if [ -f /data/local/tmp/rootd-op.state ] && [ ! -L /data/local/tmp/rootd-op.state ]; then head -c 4096 /data/local/tmp/rootd-op.state; fi; echo; echo ALICEKIT_LOG; if [ ! -L /data/local/tmp ] && [ ! -L "+stage+" ] && [ -d "+stage+" ] && [ -f "+stage+"/install.log ] && [ ! -L "+stage+"/install.log ]; then tail -c 16384 "+stage+"/install.log; fi",30000);
  return recoveryResult(raw,lastId,lastBoot);
 }
 static JSONObject recoveryResult(String raw,String id,String boot)throws Exception {
  raw=raw.replace("\r\n","\n");
  String[] a=raw.split("\nALICEKIT_RECEIPT\n",2);if(a.length!=2)throw new IOException("Станция не вернула результат проверки");
  String[] b=a[1].split("ALICEKIT_STATE\n",2),c=b.length==2?b[1].split("ALICEKIT_LOG\n",2):new String[0];
  Map<String,String> receipt=fields(b[0]),state=fields(c.length>0?c[0]:"");
  boolean current=boot.equals(a[0].trim());String operation="alicekit-"+id;
  boolean matched=operation.equals(receipt.get("operation_id"))&&boot.equals(receipt.get("boot_id"));
  String log=c.length==2?c[1]:"",exit=receipt.get("exit");boolean exited=matched&&exit!=null&&exit.matches("[0-9]{1,3}")&&Integer.parseInt(exit)<=255;
  boolean completed=current&&((exited&&"0".equals(exit)&&log.matches("(?s).*?(?m)^INSTALL_OK backup=[^\\n]+.*"))||(operation.equals(state.get("operation_id"))&&boot.equals(state.get("boot_id"))&&"complete".equals(state.get("phase"))));
  String result=completed?"installed":!current?"rebooted":exited&&!"0".equals(exit)?"failed":"unknown";
  JSONObject out=new JSONObject().put("state",result).put("kind",completed?"installed":result.equals("failed")?"attempt-failed":"unconfirmed").put("boot",boot).put("phase",operation.equals(state.get("operation_id"))?state.get("phase"):"");
  if(exited&&current)out.put("exitCode",Integer.parseInt(exit));
  if(result.equals("failed")){
   String reason="ROOT_ATTEMPT_FAILED",action="Попытка получения root завершилась с кодом "+exit+". Это ошибка установщика, а не сети или кнопки. Откройте техническую причину ниже; новая попытка автоматически не запускается.";
   String diagnostic=diagnostic(log);boolean entered=log.contains("shell: exec /system/bin/sh")||log.contains("ESCALATION OK")||log.contains("INSTALL_FAIL:")||log.contains("PERMISSIVE");
   if(!entered&&transientFailure(log)&&!Arrays.asList("124","126","127","137","143").contains(exit)){reason="ROOT_BOOTSTRAP_TRANSIENT";action="Метод получения root не сработал на этой попытке: установщик сообщил о сбое чтения памяти до запуска root-оболочки. Мастер проверит, есть ли следы записи системы. Повтор автоматически не запускается.";}
   else if(log.contains("required device commands missing:")||log.contains("required command check failed")){reason="ROOT_REQUIRED_TOOL_MISSING";action="В прошивке не хватает утилиты, необходимой оригинальному установщику. Переустановка AliceBridge и смена DNS это не исправят. Название отсутствующей утилиты показано ниже; нужен совместимый комплект автора Hybrid Root.";}
   else if(log.contains("permissive helper failed")||log.contains("after permissive")){reason="ROOT_PERMISSIVE_FAILED";action="Установщик не смог подготовить права SELinux. Установка автозапуска не подтверждена. Ниже сохранена конкретная ошибка; повтор root автоматически не запускается.";}
   else if(log.contains("INSTALL_FAIL:")){reason="ROOT_INSTALLER_FAILED";action="Оригинальный установщик остановил установку. Ниже показана его причина и последний этап. Не удаляйте файлы предыдущей попытки; при начавшейся записи требуется восстановление по гайду Hybrid Root.";}
   else if(exit.equals("126")||exit.equals("127")){reason="ROOT_EXEC_FAILED";action="Станция не смогла запустить команду установщика (код "+exit+"). Ниже показана техническая причина. Повторное получение root автоматически не запускается.";}
   out.put("reason",reason).put("guidance",action);if(!diagnostic.isEmpty())out.put("diagnostic",diagnostic);
  }else if(result.equals("unknown"))out.put("reason","ROOT_RESULT_UNCONFIRMED").put("guidance","Нет подтверждённого завершения этой попытки. Она может ещё выполняться. Нажмите «Обновить результат»; повторно root не запускайте.");
  else if(result.equals("rebooted"))out.put("reason","ROOT_BOOT_CHANGED").put("guidance","Станция перезапустилась после попытки. Нажмите «Проверить доступ заново»: мастер проверит фактические права в новом запуске.");
  return out;
 }
 private static boolean transientFailure(String log){for(String line:log.split("\n"))if(line.startsWith("RESULT:")&&!line.equals("RESULT: reader bring-up failed")&&!line.equals("RESULT: scan phase failed; NO writes done"))return false;String last=log.trim();return last.endsWith("RESULT: reader bring-up failed")||last.endsWith("RESULT: scan phase failed; NO writes done")||last.endsWith("bridge: reader bring-up failed")||last.endsWith("bridge: scan phase failed; NO writes done");}
 private static String diagnostic(String log){List<String> lines=new ArrayList<>();for(String line:log.split("\n")){line=line.trim();if(line.matches("^(RESULT:|bridge:|INSTALL_FAIL:|INSTALL: required device commands missing:|ERROR:|required device commands missing:|/system/bin/sh:).*")){String clean=SetupJournal.safe(line);if(clean.length()>160)clean=clean.substring(0,160);lines.add(clean);}}StringBuilder out=new StringBuilder();for(int i=Math.max(0,lines.size()-3);i<lines.size();i++){if(out.length()>0)out.append('\n');out.append(lines.get(i));}return out.toString();}
 JSONObject inspectResult()throws Exception{
  JSONObject own=recover();String state=own.optString("state");
  if(state.equals("installed"))return own;
  if(state.equals("none"))return preparePrevious();
  JSONObject history=preparePrevious();
  if(history.optString("state").equals("repaired"))return history;
  JSONObject evidence=history.optJSONObject("evidence");if(evidence!=null)own.put("evidence",evidence);
  if(history.optString("kind").equals("needs-review")||history.optString("kind").equals("active"))return own.put("state","blocked").put("kind",history.optString("kind")).put("guidance",history.optString("guidance"));
  if(state.equals("failed")&&own.optString("reason").equals("ROOT_BOOTSTRAP_TRANSIENT")&&evidence!=null&&evidence.optString("state_file").equals("absent")&&evidence.optString("status_file").equals("absent")&&evidence.optString("lock").equals("absent")&&evidence.optString("service").equals("absent")&&evidence.optString("process").equals("not-seen"))own.put("guidance","Попытка завершилась с известным сбоем чтения памяти до root-оболочки. Файлы этапов установки, автозапуска и активный root-процесс не обнаружены. Перезапустите станцию обычным способом, подождите три минуты и нажмите «Проверить доступ заново». Мастер заново проверит безопасность; следующую попытку вы подтвердите отдельно.");
  return own;
 }
 private static Map<String,String> fields(String raw){Map<String,String> out=new HashMap<>();for(String line:raw.split("\n")){int i=line.indexOf('=');if(i>0)out.put(line.substring(0,i),line.substring(i+1).trim());}return out;}

 static boolean conflict(Exception e){String m=e.getMessage();return m!=null&&(m.startsWith("ROOT_OPERATION_PENDING")||m.startsWith("ROOT_OPERATION_LOCKED")||m.startsWith("ROOT_SERVICE_EXISTS")||m.startsWith("ROOT_PROCESS_ACTIVE"));}
 static final String PREVIOUS_COMMAND="echo ALICEKIT_PREVIOUS_ROOT\nprintf 'boot='; cat /proc/sys/kernel/random/boot_id\nfor item in marker state status; do\n case \"$item\" in marker) file=/data/local/tmp/rootd-op.inflight;; state) file=/data/local/tmp/rootd-op.state;; status) file=/data/local/tmp/rootstatus;; esac\n if [ -L \"$file\" ]; then echo \"${item}_file=symlink\"; elif [ -f \"$file\" ]; then\n  if [ -r \"$file\" ]; then\n   echo \"${item}_file=readable\"\n   head -c 4096 \"$file\" | while IFS='=' read -r key value || [ -n \"$key$value\" ]; do\n    case \"$key\" in version|operation_id|boot_id|operation|phase|stage|state|created) [ \"${#value}\" -lt 180 ] && printf '%s_%s=%s\\n' \"$item\" \"$key\" \"$value\";; esac\n   done\n  else echo \"${item}_file=unreadable\"; fi\n elif [ -e \"$file\" ]; then echo \"${item}_file=other\"; else echo \"${item}_file=absent\"; fi\ndone\nif [ -e /data/local/tmp/rootd-op.lock ] || [ -L /data/local/tmp/rootd-op.lock ]; then echo lock=present; else echo lock=absent; fi\nservice=absent\nfor file in /odm/bin/rootd /odm/bin/permissive /odm/etc/init/rootd.rc /odm/etc/rootd.conf; do if [ -e \"$file\" ] || [ -L \"$file\" ]; then service=present; fi; done\necho service=$service\nnames=$(ps -A -o NAME 2>/dev/null) || names=UNKNOWN\nif [ \"$names\" = UNKNOWN ] || [ -z \"$names\" ]; then echo process=unknown; elif printf '%s\\n' \"$names\" | grep -qE '^(rootd|root-bridge|esc-bridge|esc36449|cve36449)$'; then echo process=present; else echo process=not-seen; fi\n";
 static final String CLEANUP_COMMAND="# AliceKit: clear only an old-boot pre-install marker. Never run an exploit here.\nROOT=/data/local/tmp\nODM=/odm\nBOOT_FILE=/proc/sys/kernel/random/boot_id\nUPTIME_FILE=/proc/uptime\nSHELL_UID=2000\nblocked() { printf 'ALICEKIT_REPAIR_BLOCKED:%s\\n' \"$1\"; exit 0; }\n[ \"$(id -u)\" = \"$SHELL_UID\" ] || blocked SHELL_ID\n[ \"$(getprop ro.product.device)\" = yandexstation_2 ] || blocked PROFILE\n[ \"$(getprop ro.build.version.incremental)\" = 5000000921 ] || blocked PROFILE\n[ \"$(getprop ro.build.fingerprint)\" = 'Yandex/yandexstation_2/yandexstation_2:9/PPR1.180610.011/5000000921:user/release-keys' ] || blocked PROFILE\n[ \"$(uname -r)\" = 4.9.113 ] || blocked PROFILE\n[ \"$(getprop sys.boot_completed)\" = 1 ] || blocked WAIT_BOOT\nread -r uptime rest < \"$UPTIME_FILE\" || blocked WAIT_BOOT\ncase \"$uptime\" in ''|*[!0-9.]*) blocked WAIT_BOOT;; esac\nuptime=${uptime%%.*}\n[ \"$uptime\" -ge 180 ] || blocked WAIT_BOOT\nboot=$(cat \"$BOOT_FILE\") || blocked BOOT_UNKNOWN\ncase \"$boot\" in ????????-????-????-????-????????????) ;; *) blocked BOOT_UNKNOWN;; esac\ncase \"$boot\" in *[!a-f0-9-]*) blocked BOOT_UNKNOWN;; esac\nmarker=$ROOT/rootd-op.inflight\nif [ ! -e \"$marker\" ] && [ ! -L \"$marker\" ]; then echo ALICEKIT_STALE_ABSENT; exit 0; fi\n[ ! -L \"$ROOT\" ] && [ -d \"$ROOT\" ] || blocked PATH_TYPE\n[ ! -L \"$marker\" ] && [ -f \"$marker\" ] && [ -r \"$marker\" ] || blocked MARKER_TYPE\n[ \"$(stat -c %u \"$marker\")\" = \"$SHELL_UID\" ] || blocked MARKER_OWNER\n[ \"$(stat -c %a \"$marker\")\" = 600 ] || blocked MARKER_MODE\nbytes=$(stat -c %s \"$marker\") || blocked MARKER_TYPE\ncase \"$bytes\" in ''|*[!0-9]*) blocked MARKER_TYPE;; esac\n[ \"$bytes\" -gt 0 ] && [ \"$bytes\" -le 1024 ] || blocked MARKER_FORMAT\ndigest=$(sha256sum \"$marker\") || blocked MARKER_READ\ndigest=${digest%% *}\nversion= operation= marker_boot= stage= created= nonce=\nseen='|'\nwhile IFS='=' read -r key value || [ -n \"$key$value\" ]; do\n case \"$seen\" in *\"|$key|\"*) blocked MARKER_FORMAT;; esac\n seen=$seen$key'|'\n case \"$value\" in ''|*[!A-Za-z0-9._:-]*) blocked MARKER_FORMAT;; esac\n [ \"${#value}\" -le 64 ] || blocked MARKER_FORMAT\n case \"$key\" in\n  version) version=$value;; operation_id) operation=$value;; boot_id) marker_boot=$value;;\n  stage) stage=$value;; created) created=$value;; staging_nonce) nonce=$value;;\n  *) blocked MARKER_FORMAT;;\n esac\ndone < \"$marker\"\n[ \"$version\" = 1 ] && [ \"$stage\" = install ] && [ -n \"$operation\" ] && [ -n \"$created\" ] || blocked MARKER_FORMAT\ncase \"$operation\" in *[!A-Za-z0-9._-]*) blocked MARKER_FORMAT;; esac\ncase \"$marker_boot\" in ????????-????-????-????-????????????) ;; *) blocked MARKER_FORMAT;; esac\ncase \"$marker_boot\" in *[!a-f0-9-]*) blocked MARKER_FORMAT;; esac\n[ \"$marker_boot\" != \"$boot\" ] || blocked CURRENT_BOOT\nguard() {\n [ \"$(cat \"$BOOT_FILE\")\" = \"$boot\" ] || blocked BOOT_CHANGED\n for file in \"$ROOT/rootd-op.state\" \"$ROOT/rootd-op.lock\" \"$ROOT/rootstatus\" \"$ODM/bin/rootd\" \"$ODM/bin/permissive\" \"$ODM/etc/init/rootd.rc\" \"$ODM/etc/rootd.conf\"; do\n  if [ -e \"$file\" ] || [ -L \"$file\" ]; then blocked INSTALL_EVIDENCE; fi\n done\n names=$(ps -A -o NAME) || blocked PROCESS_UNKNOWN\n [ -n \"$names\" ] || blocked PROCESS_UNKNOWN\n if printf '%s\\n' \"$names\" | grep -qE '^(rootd|root-bridge|esc-bridge|esc36449|cve36449)$'; then blocked PROCESS_ACTIVE; fi\n [ ! -L \"$marker\" ] && [ -f \"$marker\" ] || blocked MARKER_CHANGED\n current=$(sha256sum \"$marker\") || blocked MARKER_CHANGED\n [ \"${current%% *}\" = \"$digest\" ] || blocked MARKER_CHANGED\n}\nguard\nguard\n# A previous-boot process cannot survive the reboot; absent state precedes any backup/write.\n# Only this exact, unchanged regular marker is removed. Backups and install state are untouched.\nrm -f \"$marker\" || blocked REMOVE_DENIED\n[ ! -e \"$marker\" ] && [ ! -L \"$marker\" ] || blocked REMOVE_UNCONFIRMED\necho ALICEKIT_STALE_CLEARED\n";
 JSONObject previous()throws Exception{return previousResult(remote.shell(PREVIOUS_COMMAND,30000));}
 static JSONObject previousResult(String raw)throws Exception{
  if(!raw.startsWith("ALICEKIT_PREVIOUS_ROOT\n"))throw new IOException("Не получен ответ диагностики предыдущей установки");
  Map<String,String> f=fields(raw);JSONObject evidence=new JSONObject();for(String k:new String[]{"marker_file","state_file","status_file","lock","service","process","marker_operation_id","marker_stage","state_operation","state_phase","status_state"}){String v=f.get(k);if(v!=null&&v.matches("[A-Za-z0-9_.:-]{1,160}"))evidence.put(k,v);}
  String current=f.get("boot"),old=f.get("marker_boot_id");boolean oldBoot=current!=null&&old!=null&&current.matches("[a-f0-9]{8}(-[a-f0-9]{4}){3}-[a-f0-9]{12}")&&old.matches("[a-f0-9]{8}(-[a-f0-9]{4}){3}-[a-f0-9]{12}")&&!current.equals(old);
  if(current!=null&&old!=null)evidence.put("markerBoot",current.equals(old)?"current":"previous");
  boolean stale=oldBoot&&"readable".equals(f.get("marker_file"))&&"absent".equals(f.get("state_file"))&&"absent".equals(f.get("status_file"))&&"absent".equals(f.get("lock"))&&"absent".equals(f.get("service"))&&"not-seen".equals(f.get("process"))&&"1".equals(f.get("marker_version"))&&"install".equals(f.get("marker_stage"));
  String phase=evidence.optString("state_phase");boolean marker=!"absent".equals(f.get("marker_file")),blocked=marker||Arrays.asList("backup_complete","writing","files_written","verified","failed").contains(phase)||"present".equals(f.get("lock"))||"present".equals(f.get("service"))||!"not-seen".equals(f.get("process"));
  String kind="clear",action="Блокировка сейчас не обнаружена. Нажмите «Проверить доступ заново»: это проверка подключения, не запуск root.";
  if(blocked){kind="unconfirmed";action="Нажмите «Сохранить журнал для разбора» и отправьте его автору Hybrid Root в том же чате, где брали установщик. В журнале будут фаза и тип блокировки. Не запускайте установщик повторно и не удаляйте rootd-op.inflight вручную.";
   if(Arrays.asList("backup_complete","writing","files_written","verified","failed").contains(phase)){kind="needs-review";action="Предыдущая установка дошла до фазы "+phase+". Сохраните журнал и разберите состояние резервной копии с автором Hybrid Root. Для этих фаз исходный гайд требует status/rollback; простой повтор root не подходит.";}
   else if("present".equals(f.get("process"))){kind="active";action="На станции видна служба или процесс root. Если установка идёт в AliceBridge, дождитесь её окончания. Затем нажмите «Проверить доступ заново». Для нового снимка состояния есть «Обновить диагностику».";}
   else if(marker&&f.get("marker_boot_id")!=null&&f.get("boot")!=null){boolean same=f.get("marker_boot_id").equals(f.get("boot"));evidence.put("markerBoot",same?"current":"previous");action+=(same?" Маркер создан в текущем запуске: установщик может продолжать работать под именем sh.":" Маркер остался от прошлого запуска. Это ещё не разрешение на его удаление: сначала проверяется фаза и резервная копия.");}
  }
  if(stale){kind="stale";action="Остался след прерванной подготовки от прошлого запуска. Мастер проверит его и автоматически уберёт только устаревший маркер. Новая попытка root не запускается.";}
  return new JSONObject().put("state",blocked?"blocked":"none").put("kind",kind).put("guidance",action).put("evidence",evidence);
 }

 JSONObject preparePrevious()throws Exception{
  JSONObject before=previous();if(!"stale".equals(before.optString("kind")))return before;
  remote.progress("Исправляем незавершённую подготовку: проверяем прошлый запуск и отсутствие записи системы…");
  String answer;
  try{answer=remote.shell(CLEANUP_COMMAND,30000);}catch(Exception lost){JSONObject after=previous();if("none".equals(after.optString("state")))return repaired(after);throw new IOException("Не удалось подтвердить восстановление. Нажмите «Проверить доступ заново». Root повторно не запускался.");}
  JSONObject after=previous();if((answer.equals("ALICEKIT_STALE_CLEARED")||answer.equals("ALICEKIT_STALE_ABSENT"))&&"none".equals(after.optString("state")))return repaired(after);
  String reason=answer.startsWith("ALICEKIT_REPAIR_BLOCKED:")?answer.substring(24).trim():"UNCONFIRMED";
  String action="Не удалось подтвердить исправление. Нажмите «Проверить доступ заново». Если состояние не меняется — сохраните журнал: причина проверки уже записана.";
  if(reason.equals("WAIT_BOOT"))action="Станция недавно включилась. Оставьте её включённой на три минуты, затем нажмите «Проверить доступ заново».";
  else if(reason.equals("CURRENT_BOOT")||reason.equals("PROCESS_ACTIVE"))action="Предыдущая установка может ещё выполняться. Дождитесь её завершения в AliceBridge, затем нажмите «Проверить доступ заново».";
  else if(reason.equals("INSTALL_EVIDENCE"))action="Остались файлы установки или резервной копии. Очистка автоматически остановлена. Нажмите «Проверить доступ заново»; если root не подтверждён — «Сохранить журнал» для выбора восстановления.";
  else if(reason.startsWith("MARKER_"))action="Файл прежней попытки отличается от ожидаемого или изменился во время проверки. Мастер его сохранил. Нажмите «Сохранить журнал»: причина проверки будет в нём полностью.";
  return after.put("state",reason.equals("WAIT_BOOT")?"waiting":"blocked").put("kind","action-required").put("repairReason",reason.matches("[A-Z_]{1,40}")?reason:"UNCONFIRMED").put("guidance",action);
 }
 private static JSONObject repaired(JSONObject after)throws Exception{return after.put("state","repaired").put("kind","repaired").put("repairReason","OLD_BOOT_NO_STATE").put("guidance","Устаревшая блокировка снята. Переустанавливать AliceBridge не нужно. Продолжите настройку: файлы установщика сохраняются, новую попытку root вы подтвердите отдельно.");}
 static String summary(JSONObject result){JSONObject e=result.optJSONObject("evidence");StringBuilder out=new StringBuilder("state="+result.optString("state")+"; kind="+result.optString("kind"));for(String k:new String[]{"reason","exitCode","phase","repairReason"})if(result.has(k)&&!result.optString(k).isEmpty())out.append("; ").append(k.equals("repairReason")?"repair":k).append("=").append(result.optString(k));if(e!=null)for(String k:new String[]{"marker_file","markerBoot","state_file","state_phase","status_file","status_state","lock","service","process"})if(e.has(k))out.append("; ").append(k).append("=").append(e.optString(k));String detail=result.optString("diagnostic");if(!detail.isEmpty())out.append("; detail=").append(detail.substring(Math.max(0,detail.length()-180)).replace('\n',' '));return out.toString();}

 String preflight()throws Exception {
  String[] f=remote.shell("getprop ro.product.device; getprop ro.build.version.incremental; getprop ro.build.fingerprint; uname -r; cat /proc/sys/kernel/random/boot_id; cat /proc/uptime; id -u",30000).split("\n");
  if(f.length!=7||!compatible(f[0],f[1],f[2],f[3]))throw new IOException("ROOT_PROFILE_UNSUPPORTED: Этот root-профиль подходит только Max 5000000921 / ядро 4.9.113. Изменений нет.");
  if("0".equals(f[6]))throw new IOException("Root уже доступен: перейдите к установке панели");
  if(Double.parseDouble(f[5].split(" ")[0])<180)throw new IOException("ROOT_UPTIME_WAIT: После запуска станции должно пройти минимум три минуты");
  String status=remote.shell("cat /data/local/tmp/rootstatus 2>/dev/null || true",30000);if(status.contains("state=PROTECTED"))throw new IOException("Hybrid Root уже работает. Повторное получение не требуется");
  JSONObject history=preparePrevious();if("needs-review".equals(history.optString("kind")))throw new IOException("ROOT_OPERATION_PENDING: "+history.optString("guidance"));
  String conflict=remote.shell("if [ -e /data/local/tmp/rootd-op.inflight ] || [ -L /data/local/tmp/rootd-op.inflight ]; then echo ROOT_OPERATION_PENDING; elif [ -e /data/local/tmp/rootd-op.lock ]; then echo ROOT_OPERATION_LOCKED; elif [ -e /odm/bin/rootd ]; then echo ROOT_SERVICE_EXISTS; else names=$(ps -A -o NAME) || exit 3; if printf '%s\\n' \"$names\" | grep -qE '^(rootd|root-bridge|esc-bridge|esc36449|cve36449)$'; then echo ROOT_PROCESS_ACTIVE; else echo ROOT_PREFLIGHT_CLEAR; fi; fi",30000);
  if(!"ROOT_PREFLIGHT_CLEAR".equals(conflict))throw new IOException(conflict+": На станции обнаружена предыдущая операция или служба root. Повторный запуск заблокирован. Сначала проверьте доступ и сохранённый результат; не удаляйте служебные файлы вручную.");
  return f[4];
 }
 String install(String router,String dns)throws Exception {
  attempted=false;
  if(!ready())throw new IOException("ROOT_FILE_MISSING: Передайте оригинальный установщик в AliceKit");router=StationDiscovery.local(router);dns=RootNetwork.dnsAddress(dns);
  remote.progress("Проверяем точную прошивку и отсутствие другого root-процесса…");String boot=preflight();
  String id=UUID.randomUUID().toString().replace("-",""),operation="alicekit-"+id,stage="/data/local/tmp/station-studio-install-"+id;
  remote.shell("set -eu; umask 077; [ ! -L /data/local/tmp ]; mkdir "+stage+"; mkdir "+stage+"/hybrid",30000);
  for(String name:files.keySet()){remote.progress("Проверенный комплект: "+name);String dest=name.equals("bootstrap/root-bridge")?"root-bridge":name;remote.push(stage+"/"+dest,files.get(name));}
  remote.push(stage+"/install.conf",("ROUTER_IP="+router+"\nDNS_IP="+dns+"\nIPV6=drop\n").getBytes("US-ASCII"));
  StringBuilder sums=new StringBuilder();for(String name:files.keySet())sums.append(trusted.getString(name)).append("  ").append(name.equals("bootstrap/root-bridge")?"root-bridge":name).append('\n');remote.push(stage+"/SHA256SUMS",sums.toString().getBytes("US-ASCII"));
  remote.shell("set -eu; cd "+stage+"; sha256sum -c SHA256SUMS; chmod 755 root-bridge hybrid/permissive hybrid/install-run.sh hybrid/install-device.sh hybrid/rollback-run.sh hybrid/rollback-device.sh; printf 'format=1\\noperation_id="+operation+"\\nnonce="+id+"\\n' > .station-max-stage",30000);
  // Recheck after delivery. Atomically publish a persistent marker before the sole kernel attempt.
  if(!boot.equals(preflight()))throw new IOException("ROOT_BOOT_CHANGED: Станция перезагрузилась во время проверки; root не запускался");
  String marker="set -eu; [ \"$(cat /proc/sys/kernel/random/boot_id)\" = "+boot+" ]; set -C; umask 077; printf 'version=1\\noperation_id="+operation+"\\nboot_id="+boot+"\\nstaging_nonce="+id+"\\nstage=install\\ncreated=%s\\n' \"$(date '+%Y-%m-%dT%H:%M:%S')\" > /data/local/tmp/rootd-op.inflight";
  lastId=id;lastBoot=boot;remote.operation(id,boot);
  remote.shell(marker,30000);attempted=true;remote.progress("Получаем root и устанавливаем автозапуск. Не отключайте питание. Повторных попыток не будет.");
  String result;
  try{result=remote.shell("(cd "+stage+" && BASE="+stage+" OPERATION_ID="+operation+" STAGING_NONCE="+id+" INFLIGHT_OWNED=1 ./root-bridge esc-shell < hybrid/install-run.sh) > "+stage+"/install.log 2>&1; rc=$?; umask 077; printf 'operation_id="+operation+"\\nboot_id="+boot+"\\nexit=%s\\n' \"$rc\" > "+stage+"/receipt.new && mv "+stage+"/receipt.new "+stage+"/receipt; cat "+stage+"/install.log; exit \"$rc\"",300000);}
  catch(Exception interrupted){remote.progress("Читаем код завершения и причину из журнала установщика. Root повторно не запускается…");JSONObject outcome=null;try{outcome=inspectResult();}catch(Exception ignored){}if(outcome!=null){if("installed".equals(outcome.optString("state")))return boot;if("failed".equals(outcome.optString("state"))||"blocked".equals(outcome.optString("state")))throw new IOException(outcome.optString("guidance"),interrupted);}throw new IOException("Завершение установки пока не подтверждено. Нажмите «Обновить результат»: приложение прочитает состояние без повторного получения root.",interrupted);}
  if(!result.contains("INSTALL_OK backup="))throw new IOException("Установщик не подтвердил завершение. Повтор не выполняется. Диагностика: "+stage);
  remote.progress("Установщик подтвердил root. Резервная копия сохранена на станции. Для проверки автозапуска нужна перезагрузка.");return boot;
 }
}
