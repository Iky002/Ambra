#!/bin/sh
set -eu

SCRIPT_DIR="$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)"
ENTRYPOINT="${SCRIPT_DIR}/entrypoint.sh"
ORIG_PATH="${PATH:-/usr/bin:/bin}"

TMP_DIR="$(mktemp -d)"
trap 'rm -rf "$TMP_DIR"' EXIT

MOCK_BIN="${TMP_DIR}/bin"
mkdir -p "$MOCK_BIN"

cat > "${MOCK_BIN}/id" <<'EOF'
#!/bin/sh
case "${1:-}" in
  -u) printf '%s\n' "${MOCK_ID_U:-0}" ;;
  -g) printf '%s\n' "${MOCK_ID_G:-0}" ;;
  -G) printf '%s\n' "${MOCK_ID_GROUPS:-0}" ;;
  *) printf 'uid=%s gid=%s\n' "${MOCK_ID_U:-0}" "${MOCK_ID_G:-0}" ;;
esac
EOF

cat > "${MOCK_BIN}/setpriv" <<'EOF'
#!/bin/sh
printf 'SETPRIV|HOME=%s|ARGS=%s\n' "${HOME:-}" "$*" > "${MOCK_OUT}"
exit 0
EOF

cat > "${MOCK_BIN}/stat" <<'EOF'
#!/bin/sh
if [ "${1:-}" = "-c" ] && [ "${2:-}" = "%u:%g" ]; then
  target="${3:-}"
  if [ -n "${MOCK_MISMATCH_PATH:-}" ] && [ "$target" = "$MOCK_MISMATCH_PATH" ]; then
    printf '%s\n' "${MOCK_MISMATCH_OWNER:-0:0}"
  else
    printf '%s\n' "${MOCK_STAT_OWNER:-1000:1000}"
  fi
  exit 0
fi
exit 1
EOF

cat > "${MOCK_BIN}/chown" <<'EOF'
#!/bin/sh
printf 'CHOWN|%s\n' "$*" >> "${MOCK_CHOWN_LOG}"
if [ "${MOCK_CHOWN_FAIL:-0}" = "1" ]; then
  exit 1
fi
exit 0
EOF

cat > "${MOCK_BIN}/fake-ambra" <<'EOF'
#!/bin/sh
printf 'DIRECT|HOME=%s|ARGS=%s\n' "${HOME:-}" "$*" > "${MOCK_OUT}"
exit 0
EOF

chmod +x "${MOCK_BIN}/id" "${MOCK_BIN}/setpriv" "${MOCK_BIN}/stat" "${MOCK_BIN}/chown" "${MOCK_BIN}/fake-ambra"

assert_contains() {
  haystack="$1"
  needle="$2"
  label="$3"
  case "$haystack" in
    *"$needle"*) ;;
    *)
      printf 'FAIL [%s]: expected to contain %s, got:\n%s\n' "$label" "$needle" "$haystack" >&2
      exit 1
      ;;
  esac
}

run_case() {
  case_name="$1"
  shift
  OUT_FILE="${TMP_DIR}/${case_name}.out"
  ERR_FILE="${TMP_DIR}/${case_name}.err"
  CHOWN_FILE="${TMP_DIR}/${case_name}.chown"
  DATA_DIR="${TMP_DIR}/data_${case_name}"
  rm -f "$OUT_FILE" "$ERR_FILE" "$CHOWN_FILE"
  mkdir -p "$DATA_DIR"
  : > "$CHOWN_FILE"
  env -i \
    PATH="${MOCK_BIN}:${ORIG_PATH}" \
    MOCK_OUT="$OUT_FILE" \
    MOCK_CHOWN_LOG="$CHOWN_FILE" \
    "$@" \
    sh "$ENTRYPOINT" fake-ambra -data "$DATA_DIR" 2>"$ERR_FILE"
}

# 1. No env vars -> fallback 1000:1000, HOME=$DATA_DIR
run_case default_fallback MOCK_ID_U=0 MOCK_ID_G=0 MOCK_ID_GROUPS=0
out="$(cat "${TMP_DIR}/default_fallback.out")"
err="$(cat "${TMP_DIR}/default_fallback.err")"
assert_contains "$out" "SETPRIV|HOME=${TMP_DIR}/data_default_fallback|ARGS=--reuid=1000 --regid=1000 --clear-groups -- fake-ambra -data ${TMP_DIR}/data_default_fallback" "default_fallback exec"
assert_contains "$err" "ambra entrypoint: running as uid=1000 gid=1000" "default_fallback log"

# 2. PUID=0 PGID=0 -> 0:0 direct exec
run_case puid_root MOCK_ID_U=0 MOCK_ID_G=0 MOCK_ID_GROUPS=0 PUID=0 PGID=0
out="$(cat "${TMP_DIR}/puid_root.out")"
err="$(cat "${TMP_DIR}/puid_root.err")"
assert_contains "$out" "DIRECT|" "puid_root exec"
assert_contains "$err" "ambra entrypoint: running as uid=0 gid=0" "puid_root log"

# 3. UID=0 GID=0 (without PUID/PGID) -> 0:0 direct exec
run_case env_uid_root MOCK_ID_U=0 MOCK_ID_G=0 MOCK_ID_GROUPS=0 UID=0 GID=0
out="$(cat "${TMP_DIR}/env_uid_root.out")"
err="$(cat "${TMP_DIR}/env_uid_root.err")"
assert_contains "$out" "DIRECT|" "env_uid_root exec"
assert_contains "$err" "ambra entrypoint: running as uid=0 gid=0" "env_uid_root log"

# 4. PUID=1026 PGID=100 GIDLIST=44,109 -> 1026:100 with supplementary groups 44,109
run_case custom_uid_gidlist MOCK_ID_U=0 MOCK_ID_G=0 MOCK_ID_GROUPS=0 PUID=1026 PGID=100 GIDLIST=44,109
out="$(cat "${TMP_DIR}/custom_uid_gidlist.out")"
err="$(cat "${TMP_DIR}/custom_uid_gidlist.err")"
assert_contains "$out" "SETPRIV|HOME=${TMP_DIR}/data_custom_uid_gidlist|ARGS=--reuid=1026 --regid=100 --groups=44,109 -- fake-ambra -data ${TMP_DIR}/data_custom_uid_gidlist" "custom_uid_gidlist exec"
assert_contains "$err" "ambra entrypoint: running as uid=1026 gid=100" "custom_uid_gidlist log"

# 5. GIDLIST glob safety & unknown group warning
run_case gidlist_warn MOCK_ID_U=0 MOCK_ID_G=0 MOCK_ID_GROUPS=0 PUID=1026 PGID=100 GIDLIST="44,no_such_group_xyz,*"
out="$(cat "${TMP_DIR}/gidlist_warn.out")"
err="$(cat "${TMP_DIR}/gidlist_warn.err")"
assert_contains "$out" "--groups=44 --" "gidlist_warn valid group kept"
assert_contains "$err" "unable to resolve group 'no_such_group_xyz'" "gidlist_warn unknown group"
assert_contains "$err" "unable to resolve group '*'" "gidlist_warn glob not expanded"

# 6. needs_chown detects nested cache/images mismatch & warns when chown fails
DATA_NESTED="${TMP_DIR}/data_nested_chown"
mkdir -p "${DATA_NESTED}/cache/images"
run_case nested_chown \
  MOCK_ID_U=0 MOCK_ID_G=0 MOCK_ID_GROUPS=0 \
  PUID=1000 PGID=1000 \
  MOCK_STAT_OWNER=1000:1000 \
  MOCK_MISMATCH_PATH="${DATA_NESTED}/cache/images" \
  MOCK_MISMATCH_OWNER=0:0 \
  MOCK_CHOWN_FAIL=1
chown_log="$(cat "${TMP_DIR}/nested_chown.chown")"
err="$(cat "${TMP_DIR}/nested_chown.err")"
assert_contains "$chown_log" "CHOWN|-R 1000:1000 ${DATA_NESTED}" "nested_chown triggered"
assert_contains "$err" "warning: failed to chown '${DATA_NESTED}' to 1000:1000" "nested_chown failure warning"

# 7. Non-root startup logs uid/gid and sets HOME=$DATA_DIR
run_case non_root MOCK_ID_U=1026 MOCK_ID_G=100 MOCK_ID_GROUPS="100 44"
out="$(cat "${TMP_DIR}/non_root.out")"
err="$(cat "${TMP_DIR}/non_root.err")"
assert_contains "$out" "DIRECT|HOME=${TMP_DIR}/data_non_root|ARGS=-data ${TMP_DIR}/data_non_root" "non_root exec"
assert_contains "$err" "ambra entrypoint: running as uid=1026 gid=100" "non_root log"

# 8. Invalid PUID fails fast with error
ERR_INVALID="${TMP_DIR}/invalid_puid.err"
if env -i PATH="${MOCK_BIN}:${ORIG_PATH}" MOCK_ID_U=0 MOCK_ID_G=0 PUID=notanumber \
  sh "$ENTRYPOINT" fake-ambra -data "${TMP_DIR}/data_invalid" 2>"$ERR_INVALID"; then
  printf 'FAIL [invalid_puid]: expected non-zero exit status\n' >&2
  exit 1
fi
assert_contains "$(cat "$ERR_INVALID")" "invalid PUID/UID 'notanumber'" "invalid_puid error"

printf 'PASS: all entrypoint tests passed\n'
