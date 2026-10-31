#!/system/bin/sh
set -eu
stage=$1
pin_root=${2:-0}
case "$stage" in /data/local/tmp/station-studio-install-*) ;; *) exit 2;; esac
suffix=${stage#/data/local/tmp/station-studio-install-}
case "$suffix" in ''|*[!0-9a-f]*) exit 2;; esac
[ ${#suffix} -ge 12 ] && [ ${#suffix} -le 32 ]
[ "$(id -u)" = 0 ]
[ "$(getprop ro.product.device)" = yandexstation_2 ]
[ "$(getprop ro.build.version.incremental)" = 5000000921 ]
[ "$(getprop ro.build.version.sdk)" = 28 ]
[ "$(cat /sys/fs/selinux/enforce)" = 0 ]
set -- $(df -k /data | tail -n 1)
[ "$4" -ge 135000 ] || { echo 'Keep at least 135 MB free in /data before installation';exit 5; }
set -- $(df -k /odm | tail -n 1)
[ "$4" -ge 25000 ] || { echo 'Not enough ODM space';exit 5; }
while read -r sum file; do
 case "$file" in station-panel-v2|station-matrix|matrix-probe|station-tv.apk|controller.apk|controller.json|station-tv.json|station-panel.rc|station-matrix.rc|rootd-cpu0.rc|yam|yandexstation_2.yaml) ;; *) exit 3;; esac
 [ "$(sha256sum "$stage/$file" | cut -d ' ' -f1)" = "$sum" ]
done < "$stage/SHA256SUMS"
base=/data/local/tmp/station-panel
backup=$base/install-backup
mkdir -p "$base" "$backup" /data/local/station-matrix /data/local/station-yam/bin /data/local/station-yam/platforms /data/local/station-yam/run
chmod 750 "$base"
chmod 700 "$backup" /data/local/station-matrix /data/local/station-yam /data/local/station-yam/run
# Only replace our named services. Other launchers, root implementations and apps remain independent.
if [ -f /odm/etc/init/station-panel.rc ]; then
 grep -q '/odm/bin/station-panel-v2 --base ' /odm/etc/init/station-panel.rc || { echo 'Existing panel init is not Station Studio v2; preserve its access bootstrap and migrate it first'; exit 4; }
fi
for target in bin/station-panel-v2 bin/station-matrix etc/init/station-panel.rc etc/init/station-matrix.rc; do
 name=$(echo "$target" | tr / _)
 if [ ! -e "$backup/$name.saved" ]; then
  if [ -f "/odm/$target" ]; then cp "/odm/$target" "$backup/$name"; else touch "$backup/$name.absent"; fi
  touch "$backup/$name.saved"
 fi
done
if [ "$pin_root" = 1 ]; then
 [ "$(sha256sum /odm/bin/rootd | cut -d ' ' -f1)" = 30ee1e1f92623322ccdc77a3ff1d548ebab4f3ee003d5fc3258b473b490a927f ]
 [ -f /odm/etc/rootd.conf ]
 if [ -f /odm/etc/init/rootd.rc ]; then
  sum=$(sha256sum /odm/etc/init/rootd.rc | cut -d ' ' -f1)
  case "$sum" in 0940cfb282167e54193a10126de0c9cc1e62a2390201884220a42788d0b1bbf4|f16b385f611e93e02faa7fcdc8015cc61f708ca22a2a5c673eb47c8d8393d4cd) ;; *) echo 'Unknown rootd init configuration';exit 4;; esac
  [ -f "$backup/rootd.rc" ] || cp /odm/etc/init/rootd.rc "$backup/rootd.rc"
 fi
fi
mount -o remount,rw /odm
trap 'mount -o remount,ro /odm' EXIT
put(){
 src=$1;target=$2;mode=$3;label=$4
 cp "$stage/$src" "$target.studio-new"
 chmod "$mode" "$target.studio-new"
 chown 0:0 "$target.studio-new"
 chcon "$label" "$target.studio-new"
 mv "$target.studio-new" "$target"
}
put station-panel-v2 /odm/bin/station-panel-v2 755 u:object_r:adbd_exec:s0
put station-matrix /odm/bin/station-matrix 755 u:object_r:adbd_exec:s0
put station-panel.rc /odm/etc/init/station-panel.rc 644 u:object_r:vendor_configs_file:s0
put station-matrix.rc /odm/etc/init/station-matrix.rc 644 u:object_r:vendor_configs_file:s0
[ "$pin_root" = 0 ] || put rootd-cpu0.rc /odm/etc/init/rootd.rc 644 u:object_r:vendor_configs_file:s0
sync
mount -o remount,ro /odm
trap - EXIT
cp "$stage/matrix-probe" /data/local/station-matrix/probe
chmod 700 /data/local/station-matrix/probe
if [ ! -f /data/local/station-yam/bin/yam ]; then cp "$stage/yam" /data/local/station-yam/bin/yam;chmod 700 /data/local/station-yam/bin/yam;fi
if [ ! -f /data/local/station-yam/platforms/yandexstation_2.yaml ]; then cp "$stage/yandexstation_2.yaml" /data/local/station-yam/platforms/yandexstation_2.yaml;chmod 600 /data/local/station-yam/platforms/yandexstation_2.yaml;fi
mkdir -p "$base/releases"
chmod 700 "$base/releases"
for file in controller.apk station-tv.apk controller.json station-tv.json; do cp "$stage/$file" "$base/releases/$file";chmod 600 "$base/releases/$file";done
pm install -r "$stage/station-tv.apk" | grep Success
am start-foreground-service --user 0 -n local.station.studio/.VisualService --es operation observe >/dev/null
rm -rf "$stage"
# Init parses new service definitions during the next boot.
echo STATION_STUDIO_INSTALLED
