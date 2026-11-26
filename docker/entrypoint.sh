#!/bin/sh
set -eu

# Apply optional UMASK (e.g. 002 or 022)
if [ -n "${UMASK:-}" ]; then
  umask "$UMASK"
fi

# Default command handling (keeps `command: ["-data", "/data", ...]` backwards-compatible)
if [ "$#" -eq 0 ]; then
  set -- /usr/local/bin/ambra -data /data
elif [ "${1#-}" != "$1" ]; then
  set -- /usr/local/bin/ambra "$@"
fi

# Detect data directory from CLI arguments (-data /path, --data /path, -data=/path, --data=/path)
DATA_DIR="/data"
prev_was_data=0
for arg in "$@"; do
  if [ "$prev_was_data" = "1" ]; then
    DATA_DIR="$arg"
    prev_was_data=0
    continue
  fi
  case "$arg" in
    -data|--data)
      prev_was_data=1
      ;;
    -data=*)
      DATA_DIR="${arg#-data=}"
      ;;
    --data=*)
      DATA_DIR="${arg#--data=}"
      ;;
  esac
done

# If container was started as a non-root user (e.g. `docker run --user` / Compose `user:`),
# skip ownership fix and privilege drop and exec directly.
CUR_UID="$(id -u)"
if [ "$CUR_UID" != "0" ]; then
  CUR_GID="$(id -g 2>/dev/null || echo unknown)"
  export HOME="$DATA_DIR"
  echo "ambra entrypoint: running as uid=${CUR_UID} gid=${CUR_GID} (non-root container user)" >&2
  exec "$@"
fi

# Resolve desired UID/GID:
# Supports PUID/PGID (LinuxServer/NAS convention) and exported UID/GID (official Emby convention).
# Defaults to 1000:1000; set PUID=0 and PGID=0 (or UID=0 and GID=0) for mixed-ownership media directories.
ENV_UID="$(printenv UID 2>/dev/null || true)"
ENV_GID="$(printenv GID 2>/dev/null || true)"
TARGET_UID="${PUID:-${ENV_UID:-1000}}"
TARGET_GID="${PGID:-${ENV_GID:-1000}}"

case "$TARGET_UID" in
  ''|*[!0-9]*)
    echo "ambra entrypoint: invalid PUID/UID '${TARGET_UID}', must be a non-negative integer" >&2
    exit 1
    ;;
esac

case "$TARGET_GID" in
  ''|*[!0-9]*)
    echo "ambra entrypoint: invalid PGID/GID '${TARGET_GID}', must be a non-negative integer" >&2
    exit 1
    ;;
esac

# Ensure data directory exists and fix ownership only when needed (e.g. upgrade from root or PUID change)
mkdir -p "$DATA_DIR"
if [ "$TARGET_UID" != "0" ] || [ "$TARGET_GID" != "0" ]; then
  needs_chown=0
  for p in \
    "$DATA_DIR" \
    "$DATA_DIR/config" \
    "$DATA_DIR/config/ambra.json" \
    "$DATA_DIR/logs" \
    "$DATA_DIR/cache" \
    "$DATA_DIR/cache/images" \
    "$DATA_DIR/cache/chapters" \
    "$DATA_DIR/cache/bif" \
    "$DATA_DIR/cache/subtitles" \
    "$DATA_DIR/cache/transcoding-temp" \
    "$DATA_DIR/metadata" \
    "$DATA_DIR/plugins" \
    "$DATA_DIR/bin" \
    "$DATA_DIR/ambra.db" \
    "$DATA_DIR/ambra.db-wal" \
    "$DATA_DIR/ambra.db-shm"
  do
    if [ -e "$p" ]; then
      cur_owner="$(stat -c '%u:%g' "$p" 2>/dev/null || true)"
      if [ -n "$cur_owner" ] && [ "$cur_owner" != "${TARGET_UID}:${TARGET_GID}" ]; then
        needs_chown=1
        break
      fi
    fi
  done
  if [ "$needs_chown" = "1" ]; then
    if ! chown -R "${TARGET_UID}:${TARGET_GID}" "$DATA_DIR"; then
      echo "ambra entrypoint: warning: failed to chown '${DATA_DIR}' to ${TARGET_UID}:${TARGET_GID}" >&2
    fi
  fi
fi

# Collect supplementary groups:
# Preserve non-root groups passed via Docker `group_add` (e.g. video/render for /dev/dri)
# plus any comma/space-separated GIDs or group names configured via GIDLIST.
SUPP_GROUPS=""
add_group_id() {
  raw_gid="$1"
  gid="$raw_gid"
  if [ -z "$gid" ]; then
    return 0
  fi
  case "$gid" in
    *[!0-9]*)
      gid="$(awk -F: -v name="$gid" '$1 == name { print $3; exit }' /etc/group 2>/dev/null || true)"
      if [ -z "$gid" ]; then
        echo "ambra entrypoint: warning: unable to resolve group '${raw_gid}', skipping" >&2
        return 0
      fi
      ;;
  esac
  case "$gid" in
    ''|*[!0-9]*)
      echo "ambra entrypoint: warning: invalid group id '${raw_gid}', skipping" >&2
      return 0
      ;;
  esac
  if [ "$gid" = "$TARGET_GID" ]; then
    return 0
  fi
  case ",${SUPP_GROUPS}," in
    *",${gid},"*) return 0 ;;
  esac
  if [ -z "$SUPP_GROUPS" ]; then
    SUPP_GROUPS="$gid"
  else
    SUPP_GROUPS="${SUPP_GROUPS},${gid}"
  fi
}

for g in $(id -G 2>/dev/null || true); do
  if [ "$g" != "0" ] || [ "$TARGET_GID" = "0" ]; then
    add_group_id "$g"
  fi
done

if [ -n "${GIDLIST:-}" ]; then
  OLD_IFS="$IFS"
  IFS=', '
  set -f
  for g in $GIDLIST; do
    add_group_id "$g"
  done
  set +f
  IFS="$OLD_IFS"
fi

echo "ambra entrypoint: running as uid=${TARGET_UID} gid=${TARGET_GID} (override via PUID/PGID or UID/GID)" >&2

# Run directly as root when PUID=0 and PGID=0 (ideal for mixed-permission media mounts)
if [ "$TARGET_UID" = "0" ] && [ "$TARGET_GID" = "0" ]; then
  if [ -n "${GIDLIST:-}" ] && [ -n "$SUPP_GROUPS" ]; then
    exec setpriv --reuid=0 --regid=0 --groups="0,${SUPP_GROUPS}" -- "$@"
  fi
  exec "$@"
fi

export HOME="$DATA_DIR"

if [ -n "$SUPP_GROUPS" ]; then
  exec setpriv --reuid="$TARGET_UID" --regid="$TARGET_GID" --groups="$SUPP_GROUPS" -- "$@"
else
  exec setpriv --reuid="$TARGET_UID" --regid="$TARGET_GID" --clear-groups -- "$@"
fi
