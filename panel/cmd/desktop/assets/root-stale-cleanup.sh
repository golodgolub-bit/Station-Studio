# AliceKit: clear only an old-boot pre-install marker. Never run an exploit here.
ROOT=/data/local/tmp
ODM=/odm
BOOT_FILE=/proc/sys/kernel/random/boot_id
UPTIME_FILE=/proc/uptime
SHELL_UID=2000
blocked() { printf 'ALICEKIT_REPAIR_BLOCKED:%s\n' "$1"; exit 0; }
[ "$(id -u)" = "$SHELL_UID" ] || blocked SHELL_ID
[ "$(getprop ro.product.device)" = yandexstation_2 ] || blocked PROFILE
[ "$(getprop ro.build.version.incremental)" = 5000000921 ] || blocked PROFILE
[ "$(getprop ro.build.fingerprint)" = 'Yandex/yandexstation_2/yandexstation_2:9/PPR1.180610.011/5000000921:user/release-keys' ] || blocked PROFILE
[ "$(uname -r)" = 4.9.113 ] || blocked PROFILE
[ "$(getprop sys.boot_completed)" = 1 ] || blocked WAIT_BOOT
read -r uptime rest < "$UPTIME_FILE" || blocked WAIT_BOOT
case "$uptime" in ''|*[!0-9.]*) blocked WAIT_BOOT;; esac
uptime=${uptime%%.*}
[ "$uptime" -ge 180 ] || blocked WAIT_BOOT
boot=$(cat "$BOOT_FILE") || blocked BOOT_UNKNOWN
case "$boot" in ????????-????-????-????-????????????) ;; *) blocked BOOT_UNKNOWN;; esac
case "$boot" in *[!a-f0-9-]*) blocked BOOT_UNKNOWN;; esac
marker=$ROOT/rootd-op.inflight
if [ ! -e "$marker" ] && [ ! -L "$marker" ]; then echo ALICEKIT_STALE_ABSENT; exit 0; fi
[ ! -L "$ROOT" ] && [ -d "$ROOT" ] || blocked PATH_TYPE
[ ! -L "$marker" ] && [ -f "$marker" ] && [ -r "$marker" ] || blocked MARKER_TYPE
[ "$(stat -c %u "$marker")" = "$SHELL_UID" ] || blocked MARKER_OWNER
[ "$(stat -c %a "$marker")" = 600 ] || blocked MARKER_MODE
bytes=$(stat -c %s "$marker") || blocked MARKER_TYPE
case "$bytes" in ''|*[!0-9]*) blocked MARKER_TYPE;; esac
[ "$bytes" -gt 0 ] && [ "$bytes" -le 1024 ] || blocked MARKER_FORMAT
digest=$(sha256sum "$marker") || blocked MARKER_READ
digest=${digest%% *}
version= operation= marker_boot= stage= created= nonce=
seen='|'
while IFS='=' read -r key value || [ -n "$key$value" ]; do
 case "$seen" in *"|$key|"*) blocked MARKER_FORMAT;; esac
 seen=$seen$key'|'
 case "$value" in ''|*[!A-Za-z0-9._:-]*) blocked MARKER_FORMAT;; esac
 [ "${#value}" -le 64 ] || blocked MARKER_FORMAT
 case "$key" in
  version) version=$value;; operation_id) operation=$value;; boot_id) marker_boot=$value;;
  stage) stage=$value;; created) created=$value;; staging_nonce) nonce=$value;;
  *) blocked MARKER_FORMAT;;
 esac
done < "$marker"
[ "$version" = 1 ] && [ "$stage" = install ] && [ -n "$operation" ] && [ -n "$created" ] || blocked MARKER_FORMAT
case "$operation" in *[!A-Za-z0-9._-]*) blocked MARKER_FORMAT;; esac
case "$marker_boot" in ????????-????-????-????-????????????) ;; *) blocked MARKER_FORMAT;; esac
case "$marker_boot" in *[!a-f0-9-]*) blocked MARKER_FORMAT;; esac
[ "$marker_boot" != "$boot" ] || blocked CURRENT_BOOT
guard() {
 [ "$(cat "$BOOT_FILE")" = "$boot" ] || blocked BOOT_CHANGED
 for file in "$ROOT/rootd-op.state" "$ROOT/rootd-op.lock" "$ROOT/rootstatus" "$ODM/bin/rootd" "$ODM/bin/permissive" "$ODM/etc/init/rootd.rc" "$ODM/etc/rootd.conf"; do
  if [ -e "$file" ] || [ -L "$file" ]; then blocked INSTALL_EVIDENCE; fi
 done
 names=$(ps -A -o NAME) || blocked PROCESS_UNKNOWN
 [ -n "$names" ] || blocked PROCESS_UNKNOWN
 if printf '%s\n' "$names" | grep -qE '^(rootd|root-bridge|esc-bridge|esc36449|cve36449)$'; then blocked PROCESS_ACTIVE; fi
 [ ! -L "$marker" ] && [ -f "$marker" ] || blocked MARKER_CHANGED
 current=$(sha256sum "$marker") || blocked MARKER_CHANGED
 [ "${current%% *}" = "$digest" ] || blocked MARKER_CHANGED
}
guard
guard
# A previous-boot process cannot survive the reboot; absent state precedes any backup/write.
# Only this exact, unchanged regular marker is removed. Backups and install state are untouched.
rm -f "$marker" || blocked REMOVE_DENIED
[ ! -e "$marker" ] && [ ! -L "$marker" ] || blocked REMOVE_UNCONFIRMED
echo ALICEKIT_STALE_CLEARED
