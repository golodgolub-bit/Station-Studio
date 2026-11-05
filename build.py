"""Portable source build. Set ANDROID_HOME, JAVA_HOME, STUDIO_KEYSTORE and STUDIO_KEY_PASS."""
from pathlib import Path
import argparse, os, shutil, subprocess, zipfile, sys

ROOT=Path(__file__).resolve().parent
parser=argparse.ArgumentParser();parser.add_argument('--android',action='store_true');args=parser.parse_args()
out=ROOT/'dist';out.mkdir(exist_ok=True)
env={**os.environ,'GOOS':'linux','GOARCH':'arm64','CGO_ENABLED':'0'}
subprocess.run(['go','build','-trimpath','-ldflags','-s -w','-o',str(out/'station-panel-v2'),'.'],cwd=ROOT/'panel',env=env,check=True)
for folder,files,target in [('matrix-runtime',['main.go','audio.go'],'station-matrix'),('matrix-probe',['main.go'],'matrix-probe')]:
 subprocess.run(['go','build','-trimpath','-ldflags','-s -w','-o',str(out/target),*files],cwd=ROOT/folder,env=env,check=True)
if args.android:
 sdk=Path(os.environ['ANDROID_HOME']);jdk=Path(os.environ['JAVA_HOME'])/'bin';key=Path(os.environ['STUDIO_KEYSTORE'])
 if not os.environ.get('STUDIO_KEY_PASS'):raise SystemExit('Set STUDIO_KEY_PASS for your own signing key')
 platform=Path(os.environ.get('STUDIO_ANDROID_JAR',str(sdk/'platforms/android-35/android.jar')))
 versions=sorted((sdk/'build-tools').iterdir(),key=lambda p:tuple(int(s)for s in p.name.split('.')if s.isdigit()))
 bt=versions[-1];windows=os.name=='nt';exe='.exe'if windows else''
 def run(values):subprocess.run([str(v)for v in values],check=True)
 for folder,name in [('controller','Station-Studio.apk'),('voice','Station-Studio-TV.apk')]:
  src=ROOT/'panel'/folder;build=out/('build-'+folder);build.mkdir(exist_ok=True)
  assets=build/'assets';assets.mkdir(exist_ok=True)
  deps=[]
  if folder=='voice':
   run([sys.executable,ROOT/'fetch_android_media_deps.py'])
   deps=[ROOT/'android-media-deps'/(name+'.jar') for name in ['media','core','versionedparcelable','collection','media3-session','media3-common','media3-datasource','media3-database','exifinterface','lifecycle-java','annotation-java','guava','failureaccess']]
   notices=assets/'third-party';shutil.copytree(ROOT/'third-party/android-media',notices,dirs_exist_ok=True)
  run([jdk/('javac'+exe),'-source','8','-target','8','-encoding','UTF-8','-classpath',os.pathsep.join(map(str,[platform,*deps])),'-d',build,*src.glob('*.java')])
  run([bt/('d8.bat'if windows else'd8'),'--lib',platform,'--min-api','23','--output',build,*build.rglob('*.class'),*deps])
  unsigned=build/'unsigned.apk'
  run([bt/('aapt'+exe),'package','-f','-M',src/'AndroidManifest.xml','-A',assets,'-S',src/'res','-I',platform,'-F',unsigned])
  with zipfile.ZipFile(unsigned,'a')as archive:
   for dex in build.glob('classes*.dex'):archive.write(dex,dex.name)
  run([jdk/('java'+exe),'-jar',bt/'lib/apksigner.jar','sign','--ks',key,'--ks-pass','env:STUDIO_KEY_PASS','--out',out/name,unsigned])
  run([jdk/('java'+exe),'-jar',bt/'lib/apksigner.jar','verify',out/name])
print('Builds saved in dist. Private signing keys and connected-device data are not included.')
