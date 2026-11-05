"""Pinned AndroidX media client dependencies from Google's Maven repository."""
from pathlib import Path
import hashlib,json,urllib.request,zipfile
root=Path(__file__).resolve().parent
folder=root/'android-media-deps';folder.mkdir(exist_ok=True)
spec=[('androidx/media/media/1.7.0/media-1.7.0.aar','media'),('androidx/core/core/1.12.0/core-1.12.0.aar','core'),('androidx/versionedparcelable/versionedparcelable/1.1.1/versionedparcelable-1.1.1.aar','versionedparcelable'),('androidx/collection/collection/1.2.0/collection-1.2.0.jar','collection'),('androidx/annotation/annotation/1.6.0/annotation-1.6.0.jar','annotation')]
spec += [('androidx/media3/'+a+'/1.8.0/'+a+'-1.8.0.aar',a)for a in ['media3-session','media3-common','media3-datasource','media3-database']]
spec += [('androidx/exifinterface/exifinterface/1.3.6/exifinterface-1.3.6.aar','exifinterface'),('androidx/lifecycle/lifecycle-common/2.6.2/lifecycle-common-2.6.2.jar','lifecycle-common'),('androidx/annotation/annotation-jvm/1.6.0/annotation-jvm-1.6.0.jar','annotation-jvm'),('https://repo.maven.apache.org/maven2/com/google/guava/guava/33.3.1-android/guava-33.3.1-android.jar','guava'),('https://repo.maven.apache.org/maven2/com/google/guava/failureaccess/1.0.2/failureaccess-1.0.2.jar','failureaccess')]
spec=[entry for entry in spec if entry[1]not in ['annotation','annotation-jvm','lifecycle-common']]
spec += [('androidx/annotation/annotation/1.5.0/annotation-1.5.0.jar','annotation-java'),('androidx/lifecycle/lifecycle-common/2.2.0/lifecycle-common-2.2.0.jar','lifecycle-java')]
manifest=folder/'dependencies.json'  ;known=json.loads(manifest.read_text())if manifest.exists()else{}
for relative,name in spec:
 url=relative if relative.startswith('https://') else 'https://dl.google.com/dl/android/maven2/'+relative;target=folder/relative.rsplit('/',1)[1]
 if not target.exists():
  with urllib.request.urlopen(url,timeout=45)as r:target.write_bytes(r.read(10*1024*1024))
 digest=hashlib.sha256(target.read_bytes()).hexdigest()
 if name in known and known[name]['sha256']!=digest:raise RuntimeError('Dependency hash mismatch: '+name)
 known[name]={'url':url,'sha256':digest,'license':'Apache-2.0'}
 output=folder/(name+'.jar')
 if target.suffix=='.aar':
  with zipfile.ZipFile(target)as z:output.write_bytes(z.read('classes.jar'))
 else:output.write_bytes(target.read_bytes())
 print(name,target.stat().st_size,flush=True)
manifest.write_text(json.dumps(known,indent=2),encoding='utf-8')
