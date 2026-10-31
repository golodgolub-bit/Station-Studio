"""Station Studio bootstrap. Python 3.10+, Android Platform Tools. No saved credentials."""
from pathlib import Path
import argparse,hashlib,ipaddress,json,os,re,shlex,shutil,subprocess,sys,tempfile,time,urllib.request,uuid

ROOT=Path(__file__).resolve().parent
YAM_URL='https://github.com/nyakokitsu/yam/releases/download/yam-v0.2.3/yam-aarch64-unknown-linux-musl'
YAM_SHA='0ca82606e4e989d81d9e7fb7f9916e8dfbfcb24d38ceb0d122a0ae8039cfe7c8'
parser=argparse.ArgumentParser(description='Проверка и установка Station Studio на Station Max 5000000921')
parser.add_argument('--adb',default=shutil.which('adb')or shutil.which('adb.exe')or'adb')
parser.add_argument('--serial',required=True,help='ADB-серийный номер или локальный IP:порт')
parser.add_argument('--install',action='store_true');parser.add_argument('--rollback',action='store_true')
parser.add_argument('--pin-root',action='store_true',help='Закрепить уже установленный Hybrid Root 1.2.4 на CPU 0')
parser.add_argument('--reboot',action='store_true',help='Перезагрузить после проверенной установки')
args=parser.parse_args()
if args.install and args.rollback:parser.error('Выберите установку или откат')
if ':'in args.serial:
 host,port=args.serial.rsplit(':',1)
 try:ip=ipaddress.ip_address(host);assert ip.version==4 and ip.is_private and not ip.is_loopback;assert 1<=int(port)<=65535
 except Exception:parser.error('Нужен локальный IPv4-адрес станции и порт ADB')
 subprocess.run([args.adb,'connect',args.serial],capture_output=True,timeout=10)
def adb(*values,timeout=25):
 result=subprocess.run([args.adb,'-s',args.serial,*map(str,values)],capture_output=True,timeout=timeout)
 if result.returncode:raise RuntimeError(result.stderr.decode(errors='replace')or result.stdout.decode(errors='replace'))
 return result.stdout.decode(errors='replace').replace('\r\n','\n').strip()
def shell(command,timeout=25):return adb('shell',command,timeout=timeout)
def root_queue(script,wait=True):
 request='studio-'+uuid.uuid4().hex[:16];boot=shell('cat /proc/sys/kernel/random/boot_id')
 with tempfile.TemporaryDirectory(prefix='station-studio-')as folder:
  local=Path(folder)/'request.sh';local.write_text(script+'\n',encoding='utf-8',newline='\n')
  staging='/data/local/tmp/.'+request+'.tmp';adb('push',local,staging)
 shell('chown 2000:2000 '+staging+' && chmod 0660 '+staging+' && mv '+staging+' /data/local/tmp/rootcmd-'+request)
 if not wait:return
 until=time.monotonic()+180
 while time.monotonic()<until:
  data=shell('if [ -f /data/local/tmp/rootres.'+request+' ]; then cat /data/local/tmp/rootres.'+request+'; fi')
  if data:
   fields=dict(line.split('=',1)for line in data.splitlines()if'='in line)
   if fields.get('id')!=request or fields.get('boot_id')!=boot or fields.get('status')!='executed' or fields.get('result')!='exited' or fields.get('exit')!='0' or fields.get('signal')!='0' or fields.get('truncated')!='0':raise RuntimeError('Rootd не подтвердил завершение операции: '+data)
   return
  time.sleep(.5)
 raise RuntimeError('Нет подтверждения rootd. Не повторяйте установку, пока не проверен результат опубликованного запроса.')

try:
 model=shell('getprop ro.product.device');build=shell('getprop ro.build.version.incremental');sdk=shell('getprop ro.build.version.sdk');uid=shell('id -u');permissive=shell('cat /sys/fs/selinux/enforce')
 print('Модель:',model,'Сборка:',build,'Android API:',sdk,'ADB UID:',uid,'Permissive:',permissive=='0')
 if (model,build,sdk)!=('yandexstation_2','5000000921','28'):raise RuntimeError('Для этой модели или прошивки нет проверенного профиля. Изменения не внесены.')
 rootd=shell('if [ -f /data/local/tmp/rootstatus ]; then cat /data/local/tmp/rootstatus; fi')
 queue=uid!='0'and 'state=PROTECTED'in rootd and'profile=0921'in rootd
 if uid!='0'and not queue:raise RuntimeError('Сначала получите root через совместимый AliceBridge/Hybrid Root. Инструкция и ссылки находятся в README. Повторный эксплойт при работающем root не запускается.')
 if permissive!='0':raise RuntimeError('Root/Permissive ещё не готовы. Проверьте AliceBridge/Hybrid Root и rootstatus.')
 print('Профиль совместим. Root-канал:', 'rootd'if queue else'прямой ADB')
 if not args.install and not args.rollback:
  print('Проверка завершена без изменений. Для установки: --install --reboot; для закрепления существующего Hybrid Root: также --pin-root.');sys.exit(0)
 if args.rollback:
  stage='/data/local/tmp/station-studio-install-'+uuid.uuid4().hex[:12];shell('mkdir -p '+stage)
  adb('push',ROOT/'rollback-device.sh',stage+'/rollback-device.sh')
  script='set -eu\n/system/bin/sh '+stage+'/rollback-device.sh'
 else:
  payload=ROOT/'payload';manifest=json.loads((ROOT/'manifest.json').read_text(encoding='utf-8'))
  required={'station-panel-v2','station-matrix','matrix-probe','station-tv.apk','controller.apk','controller.json','station-tv.json','station-panel.rc','station-matrix.rc','rootd-cpu0.rc','yandexstation_2.yaml'}
  if set(manifest)!=required:raise RuntimeError('Неполный или неизвестный состав выпуска')
  for name,digest in manifest.items():
   if not re.fullmatch(r'[a-zA-Z0-9_.-]+',name):raise RuntimeError('Неверный файл выпуска')
   if hashlib.sha256((payload/name).read_bytes()).hexdigest()!=digest:raise RuntimeError('Контрольная сумма не совпала: '+name)
  # Fetch YAM from the original author rather than bundling an unlicensed third-party binary.
  yam=payload/'yam'
  if not yam.exists()or hashlib.sha256(yam.read_bytes()).hexdigest()!=YAM_SHA:
   print('Загружается официальный YAM 0.2.3…')
   with urllib.request.urlopen(YAM_URL,timeout=40)as response:binary=response.read(24*1024*1024)
   if hashlib.sha256(binary).hexdigest()!=YAM_SHA:raise RuntimeError('YAM: контрольная сумма не совпала')
   yam.write_bytes(binary)
  stage='/data/local/tmp/station-studio-install-'+uuid.uuid4().hex[:12];shell('mkdir -p '+stage)
  sums=[]
  for name,digest in {**manifest,'yam':YAM_SHA}.items():adb('push',payload/name,stage+'/'+name,timeout=80);sums.append(digest+' '+name)
  with tempfile.TemporaryDirectory(prefix='station-studio-')as folder:
   check=Path(folder)/'SHA256SUMS';check.write_text('\n'.join(sums)+'\n',encoding='ascii',newline='\n');adb('push',check,stage+'/SHA256SUMS')
  adb('push',ROOT/'install-device.sh',stage+'/install-device.sh')
  script='set -eu\n/system/bin/sh '+stage+'/install-device.sh '+stage+' '+('1'if args.pin_root else'0')
 if queue:root_queue(script)
 else:print(shell(script,timeout=180))
 print('Операция подтверждена. Аккаунты, музыка, рисунки и настройки сохранены.')
 if args.reboot:
  previous=shell('cat /proc/sys/kernel/random/boot_id')
  if queue:root_queue('setprop sys.powerctl reboot',wait=False)
  else:
   try:adb('reboot',timeout=8)
   except Exception:pass
  print('Станция перезагружается…')
  until=time.monotonic()+180;ready=False
  while time.monotonic()<until:
   time.sleep(3)
   try:
    if ':'in args.serial:subprocess.run([args.adb,'connect',args.serial],capture_output=True,timeout=5)
    boot=shell('cat /proc/sys/kernel/random/boot_id',timeout=5)
    if boot!=previous and shell('getprop sys.boot_completed',timeout=5)=='1'and shell('cat /sys/fs/selinux/enforce',timeout=5)=='0':ready=True;break
   except Exception:pass
  if not ready:raise RuntimeError('Не удалось подтвердить запуск после перезагрузки. Проверьте станцию; автоматическая повторная установка не выполняется.')
  adb('shell','am start -n local.station.studio/.TvActivity')
  print('Запуск после перезагрузки подтверждён. На станции: Система → Подключить телефон.')
 else:print('Перезагрузите станцию, чтобы Android прочитал новые службы автозапуска.')
except Exception as e:
 print('Остановлено:',e,file=sys.stderr);sys.exit(1)
