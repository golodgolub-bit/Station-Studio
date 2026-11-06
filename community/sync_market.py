"""Mirror public metadata/packages to GitHub Releases; never execute packages.

Runs only in the project's GitHub Action with its short-lived repository token.
Author authentication, ownership and daily publication quotas remain on Sites.
"""
import datetime, hashlib, json, os, pathlib, re, subprocess, sys, tempfile, urllib.request, urllib.parse

ORIGIN='https://station-studio-community.shaaaape.chatgpt.site'
UUID=re.compile(r'^[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}$')
SHA=re.compile(r'^[a-f0-9]{64}$')
UA='StationStudio/2.4 (+https://github.com/golodgolub-bit/Station-Studio)'

class OnlySource(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, req, fp, code, msg, headers, url):
        if urllib.parse.urlsplit(url).netloc!=urllib.parse.urlsplit(ORIGIN).netloc or not url.startswith('https://'):
            raise RuntimeError('Untrusted source redirect')
        return super().redirect_request(req,fp,code,msg,headers,url)

opener=urllib.request.build_opener(OnlySource())
def fetch(path,limit):
    req=urllib.request.Request(ORIGIN+path,headers={'User-Agent':UA,'Accept':'application/json, application/octet-stream'})
    with opener.open(req,timeout=60) as response:data=response.read(limit+1)
    if len(data)>limit:raise RuntimeError('Source response too large')
    return data

def gh(*args):
    result=subprocess.run(['gh',*args],check=True,capture_output=True,text=True)
    return result.stdout

def main():
    items=[];cursor=None;seen=set()
    for page in range(1000):
        payload=json.loads(fetch('/api/catalog'+('?before='+urllib.parse.quote(cursor,safe='') if cursor else ''),1<<20))
        for item in payload['items']:
            if not UUID.fullmatch(item['id']) or not SHA.fullmatch(item['sha256']) or not 0<item['bytes']<=32<<20:
                raise RuntimeError('Invalid public package descriptor')
            if item['id'] in seen:continue
            seen.add(item['id']);item['description']=item['description'][:600];items.append(item)
        cursor=payload.get('next')
        if not cursor:break
    else:raise RuntimeError('Catalog pagination exceeded limit')
    if '--check-source' in sys.argv:
        print('Public source reachable:',len(items),'packages');return
    repo=os.environ['GITHUB_REPOSITORY']
    if not re.fullmatch(r'[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+',repo):raise RuntimeError('Invalid repository')
    pages=json.loads(gh('api','--paginate','--slurp','repos/'+repo+'/releases?per_page=100'))
    releases={r['tag_name']:r for page in pages for r in page if re.fullmatch(r'community-(index|[a-f0-9]{2})',r['tag_name'])}
    def release(tag):
        if tag not in releases:
            gh('release','create',tag,'--repo',repo,'--target',os.environ['GITHUB_SHA'],'--title','Station Studio · '+tag,'--notes','Public plugin distribution. Author and permission review is required in Station Studio.','--latest=false')
            releases[tag]=json.loads(gh('api','repos/'+repo+'/releases/tags/'+tag))
        return releases[tag]
    desired={}
    with tempfile.TemporaryDirectory(prefix='studio-market-') as folder:
        root=pathlib.Path(folder)
        for item in items:
            tag='community-'+item['id'][:2];name=item['id']+'-'+item['sha256']+'.yampkg';desired.setdefault(tag,set()).add(name)
            info=release(tag)
            existing={a['name']:a for a in info['assets']}
            if name not in existing:
                raw=fetch('/api/download/'+item['id']+'?sha256='+item['sha256'],32<<20)
                if len(raw)!=item['bytes'] or hashlib.sha256(raw).hexdigest()!=item['sha256']:raise RuntimeError('Archive integrity mismatch')
                target=root/name;target.write_bytes(raw)
                gh('release','upload',tag,str(target),'--repo',repo)
            elif existing[name].get('digest') not in (None,'sha256:'+item['sha256']):
                raise RuntimeError('Existing release asset digest mismatch')
        index={'items':items,'syncedAt':datetime.datetime.now(datetime.timezone.utc).isoformat(),'source':ORIGIN,'intervalMinutes':15}
        target=root/'catalog.json';data=json.dumps(index,ensure_ascii=False,separators=(',',':')).encode()
        if len(data)>16<<20:raise RuntimeError('Catalog exceeds device memory budget')
        target.write_bytes(data);release('community-index');gh('release','upload','community-index',str(target),'--repo',repo,'--clobber')
        # Replace the index first. Only our old package assets are removed;
        # app releases and unrelated files are never part of this cleanup.
        for tag,info in releases.items():
            if tag=='community-index':continue
            for asset in info['assets']:
                name=asset['name']
                if re.fullmatch(r'[a-f0-9-]{36}-[a-f0-9]{64}\.yampkg',name) and name not in desired.get(tag,set()):
                    gh('api','--method','DELETE','repos/'+repo+'/releases/assets/'+str(asset['id']))
    print('Mirrored public packages:',len(items))

if __name__=='__main__':main()
