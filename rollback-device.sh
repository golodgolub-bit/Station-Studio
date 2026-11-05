#!/system/bin/sh
set -eu
[ "$(id -u)" = 0 ]
[ "$(getprop ro.product.device)" = yandexstation_2 ]
[ "$(getprop ro.build.version.incremental)" = 5000000921 ]
backup=/data/local/tmp/station-panel/install-backup
[ -d "$backup" ]
# Restore only files recorded by this installer. Keep audio, animations, pairing and plug-ins.
mount -o remount,rw /odm
trap 'mount -o remount,ro /odm' EXIT
for target in bin/station-panel-v2 bin/station-matrix etc/init/station-panel.rc etc/init/station-matrix.rc priv-app/StationStudio/StationStudio.apk etc/permissions/privapp-permissions-station-studio.xml; do
 name=$(echo "$target" | tr / _)
 if [ -f "$backup/$name" ]; then cp "$backup/$name" "/odm/$target";elif [ -f "$backup/$name.absent" ]; then rm -f "/odm/$target";fi
done
if [ -f "$backup/rootd.rc" ]; then cp "$backup/rootd.rc" /odm/etc/init/rootd.rc;fi
sync
mount -o remount,ro /odm
trap - EXIT
echo STATION_STUDIO_ROLLED_BACK
