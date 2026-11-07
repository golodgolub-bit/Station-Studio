"""Execute the exact Android cleanup command against disposable Linux fixtures."""
from pathlib import Path
import tempfile,subprocess,os,json,re
base=Path(__file__).resolve().parents[1]
source=(base/'panel/controller/RootInstaller.java').read_text(encoding='utf-8')
script=json.loads(re.search(r'static final String CLEANUP_COMMAND=(.*);',source).group(1))
assert script==(base/'panel/controller/assets/root-stale-cleanup.sh').read_text(encoding='utf-8')
old='bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb';current='aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa'
marker='version=1\noperation_id=alicekit-0123456789abcdef0123456789abcdef\nboot_id='+old+'\nstage=install\ncreated=2026-11-07T10:00:00\n'

def run(case,modify=None,expected='ALICEKIT_REPAIR_BLOCKED'):
 with tempfile.TemporaryDirectory(prefix='alicekit-repair-') as td:
  root=Path(td);tmp=root/'data';odm=root/'odm';tmp.mkdir();(odm/'bin').mkdir(parents=True);(odm/'etc/init').mkdir(parents=True)
  boot=root/'boot';uptime=root/'uptime';boot.write_text(current+'\n');uptime.write_text('240.00 20.0\n')
  guard=tmp/'rootd-op.inflight';guard.write_text(marker);guard.chmod(0o600)
  protected=root/'keep.txt';protected.write_text('untouched')
  prefix='''getprop() { case "$1" in ro.product.device) echo yandexstation_2;; ro.build.version.incremental) echo 5000000921;; ro.build.fingerprint) echo Yandex/yandexstation_2/yandexstation_2:9/PPR1.180610.011/5000000921:user/release-keys;; sys.boot_completed) echo 1;; esac; }
uname() { echo 4.9.113; }
ps() { printf 'NAME\\nsystem_server\\n'; }
'''
  code=script.replace('ROOT=/data/local/tmp','ROOT='+str(tmp)).replace('ODM=/odm','ODM='+str(odm)).replace('BOOT_FILE=/proc/sys/kernel/random/boot_id','BOOT_FILE='+str(boot)).replace('UPTIME_FILE=/proc/uptime','UPTIME_FILE='+str(uptime)).replace('SHELL_UID=2000','SHELL_UID='+str(os.getuid()))
  if modify:prefix,code=modify(root,tmp,odm,guard,boot,uptime,prefix,code)
  result=subprocess.run(['sh','-c',prefix+code],capture_output=True,text=True,timeout=5)
  assert result.returncode==0,(case,result.stderr)
  assert result.stdout.strip().startswith(expected),(case,result.stdout,result.stderr)
  assert protected.read_text()=='untouched',case
  if expected=='ALICEKIT_STALE_CLEARED':assert not guard.exists(),case
  elif expected!='ALICEKIT_STALE_ABSENT':assert guard.exists() or guard.is_symlink(),case
  print(case,'PASS',result.stdout.strip())

def change(fn):
 def apply(root,tmp,odm,guard,boot,uptime,prefix,code):fn(root,tmp,odm,guard,boot,uptime);return prefix,code
 return apply

run('old boot, no state, legacy marker',expected='ALICEKIT_STALE_CLEARED')
run('current boot retained',change(lambda r,t,o,g,b,u:b.write_text(old+'\n')))
run('recent boot retained',change(lambda r,t,o,g,b,u:u.write_text('25.0 1.0\n')))
for name in ['rootd-op.state','rootd-op.lock','rootstatus']:
 run(name+' retained',change(lambda r,t,o,g,b,u,n=name:(t/n).write_text('phase=writing\n')))
for name in ['bin/rootd','bin/permissive','etc/init/rootd.rc','etc/rootd.conf']:
 run('installed '+name+' retained',change(lambda r,t,o,g,b,u,n=name:(o/n).write_text('preserve')))
run('malformed marker retained',change(lambda r,t,o,g,b,u:g.write_text(marker+'boot_id='+old+'\n')))
run('unsafe mode retained',change(lambda r,t,o,g,b,u:g.chmod(0o644)))
def symlink(r,t,o,g,b,u):g.unlink();g.symlink_to(r/'keep.txt')
run('symlink and target retained',change(symlink))
def active(r,t,o,g,b,u,p,c):return p+"ps() { printf 'NAME\\nroot-bridge\\n'; }\n",c
run('active process retained',active)
def wrong(r,t,o,g,b,u,p,c):return p+'uname() { echo other; }\n',c
run('unknown kernel retained',wrong)
def race(r,t,o,g,b,u,p,c):return p+"ps() { printf 'NAME\\nsystem_server\\n'; printf 'changed' >> '"+str(g)+"'; }\n",c
run('changed marker retained',race)
run('missing marker idempotent',change(lambda r,t,o,g,b,u:g.unlink()),expected='ALICEKIT_STALE_ABSENT')
print('Exact cleanup command: successful removal limited to old pre-write marker; all unsafe fixtures retained')
