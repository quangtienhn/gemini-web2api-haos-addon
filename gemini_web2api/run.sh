#!/usr/bin/with-contenv bashio
set -Eeuo pipefail

umask 077
mkdir -p /data

# ── Đọc cấu hình từ Add-on options ──────────────────────────────────────────

admin_password="$(bashio::config 'admin_password')"
api_key="$(bashio::config 'api_key')"
adapter_api_key="$(bashio::config 'adapter_api_key')"
disable_media="$(bashio::config 'disable_media_generation')"
disable_url="$(bashio::config 'disable_url_fetch')"
disable_acct="$(bashio::config 'disable_account_pool')"
disable_proxy="$(bashio::config 'disable_proxy_pool')"
raw_cors="$(bashio::config 'cors_allow_origins')"

# ── Validation ──────────────────────────────────────────────────────────────

# Fail early if admin password missing/short.
if [[ -z "${admin_password}" || "${admin_password}" == "null" || ${#admin_password} -lt 8 ]]; then
  echo "[FATAL] Configure an admin password of at least 8 characters in the Add-on options." >&2
  exit 1
fi

# API key is required for Web2API clients and as the upstream credential for the adapter.
if [[ -z "${api_key}" || "${api_key}" == "null" || ${#api_key} -lt 8 ]]; then
  echo "[FATAL] Configure an API key of at least 8 characters in the Add-on options." >&2
  exit 1
fi

# Adapter API key is separate from the client API key. Generate once, persist.
adapter_api_key_file="/data/adapter_api_key"
if [[ -n "${adapter_api_key}" && "${adapter_api_key}" != "null" ]]; then
  printf '%s' "${adapter_api_key}" > "${adapter_api_key_file}"
  chmod 0600 "${adapter_api_key_file}"
elif [[ ! -s "${adapter_api_key_file}" ]]; then
  python3 -c "import secrets; print(secrets.token_hex(32))" > "${adapter_api_key_file}"
  chmod 0600 "${adapter_api_key_file}"
fi
ADAPTER_API_KEY="$(cat "${adapter_api_key_file}")"
if [[ ${#ADAPTER_API_KEY} -lt 8 ]]; then
  echo "[FATAL] Internal adapter API key is too short." >&2
  exit 1
fi

# Internal admin token: generate once, persist in /data, never expose.
admin_token_file="/data/admin_token"
if [[ ! -s "${admin_token_file}" ]]; then
  python3 -c "import secrets; print(secrets.token_hex(32))" > "${admin_token_file}"
  chmod 0600 "${admin_token_file}"
fi
admin_token="$(cat "${admin_token_file}")"
if [[ ${#admin_token} -lt 32 ]]; then
  echo "[FATAL] Internal admin token is too short." >&2
  exit 1
fi

# ── Chuẩn hóa các giá trị boolean ───────────────────────────────────────────

to_bool() {
  case "$1" in
    true|True|TRUE|1|yes) echo true ;;
    *) echo false ;;
  esac
}
disable_media="$(to_bool "$disable_media")"
disable_url="$(to_bool "$disable_url")"
disable_acct="$(to_bool "$disable_acct")"
disable_proxy="$(to_bool "$disable_proxy")"

# ── Chuẩn hóa cors_allow_origins ────────────────────────────────────────────

if [[ -z "${raw_cors}" || "${raw_cors}" == "null" ]]; then
  cors_json='[]'
else
  if ! printf '%s' "${raw_cors}" | jq -e 'type == "array" and all(.[]; type == "string")' >/dev/null 2>&1; then
    echo "[FATAL] cors_allow_origins must be a JSON array of strings." >&2
    exit 1
  fi
  cors_json="${raw_cors}"
fi

# ── Ghi config cho Web2API ──────────────────────────────────────────────────

jq -n \
  --arg admin_token "$admin_token" \
  --argjson disable_media "$disable_media" \
  --argjson disable_url "$disable_url" \
  --argjson disable_acct "$disable_acct" \
  --argjson disable_proxy "$disable_proxy" \
  --argjson cors "$cors_json" \
  '{
    port: 8083,
    host: "0.0.0.0",
    db_path: "/data/gemini.db",
    admin_token: $admin_token,
    admin_enabled: true,
    cors_allow_origins: $cors,
    disable_media_generation: $disable_media,
    disable_url_fetch: $disable_url,
    disable_account_pool: $disable_acct,
    disable_proxy_pool: $disable_proxy
  }' > /data/config.json
chmod 0600 /data/config.json

# ── Chuẩn hóa quyền sở hữu trước khi drop privileges ────────────────────────

chown gemini:gemini /data
for path in /data/gemini.db* /data/secret.key /data/config.json /data/admin_token /data/admin_password.hash /data/admin_password.lock /data/admin_password.reset /data/admin_password.recovery_required /data/admin_password.tx /data/admin_auth.json /data/adapter_api_key; do
  if [[ -e "$path" ]]; then
    chown gemini:gemini "$path"
  fi
done
chmod 0700 /data

# ── Môi trường riêng cho từng process (secret isolation) ────────────────────
# Secrets are NOT exported into the global shell environment. Each process is
# launched with env -i and an explicit allowlist so secrets cannot leak across
# process boundaries via /proc/<pid>/environ.

BASE_PATH="/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"
BASE_HOME="/data"

WEB2API_ENV=(
  "env" "-i"
  "PATH=${BASE_PATH}"
  "HOME=${BASE_HOME}"
  "WEB2API_ADMIN_TOKEN=${admin_token}"
  "WEB2API_ADMIN_PASSWORD=${admin_password}"
  "WEB2API_API_KEY=${api_key}"
  "WEB2API_DB_PATH=/data/gemini.db"
  "WEB2API_PORT=8083"
  "WEB2API_HOST=0.0.0.0"
)

ADAPTER_ENV=(
  "env" "-i"
  "PATH=${BASE_PATH}"
  "HOME=${BASE_HOME}"
  "ADAPTER_HOST=0.0.0.0"
  "ADAPTER_PORT=8084"
  "ADAPTER_API_KEY=${ADAPTER_API_KEY}"
  "UPSTREAM_BASE_URL=http://127.0.0.1:8083/v1"
  "UPSTREAM_API_KEY=${api_key}"
  "UPSTREAM_MODEL=gemini-image"
  "UPSTREAM_TIMEOUT=120"
)

# ── Hàm cleanup và graceful shutdown ────────────────────────────────────────

cleanup() {
    local exit_code=${1:-1}
    echo "[run.sh] Shutting down..." >&2

    # Gửi SIGTERM đến cả hai process.
    if [[ -n "${WEB_PID:-}" ]] && kill -0 "$WEB_PID" 2>/dev/null; then
        kill -TERM "$WEB_PID" 2>/dev/null || true
    fi
    if [[ -n "${ADAPTER_PID:-}" ]] && kill -0 "$ADAPTER_PID" 2>/dev/null; then
        kill -TERM "$ADAPTER_PID" 2>/dev/null || true
    fi

    # Chờ grace period (10 giây) để cả hai process thoát sạch.
    local waited=0
    while [[ $waited -lt 10 ]]; do
        local web_alive=false
        local adapter_alive=false
        [[ -n "${WEB_PID:-}" ]] && kill -0 "$WEB_PID" 2>/dev/null && web_alive=true
        [[ -n "${ADAPTER_PID:-}" ]] && kill -0 "$ADAPTER_PID" 2>/dev/null && adapter_alive=true

        if ! $web_alive && ! $adapter_alive; then
            break
        fi
        sleep 1
        waited=$((waited + 1))
    done

    # SIGKILL nếu vẫn còn process sống sau grace period.
    if [[ -n "${WEB_PID:-}" ]] && kill -0 "$WEB_PID" 2>/dev/null; then
        echo "[run.sh] Web2API did not exit gracefully, sending SIGKILL" >&2
        kill -KILL "$WEB_PID" 2>/dev/null || true
    fi
    if [[ -n "${ADAPTER_PID:-}" ]] && kill -0 "$ADAPTER_PID" 2>/dev/null; then
        echo "[run.sh] Image adapter did not exit gracefully, sending SIGKILL" >&2
        kill -KILL "$ADAPTER_PID" 2>/dev/null || true
    fi

    # Reap tất cả process con để tránh zombie.
    wait 2>/dev/null || true

    exit "$exit_code"
}

trap 'cleanup 1' SIGTERM SIGINT

# ── Khởi động Web2API (process PID 1 con) ───────────────────────────────────

su-exec gemini:gemini "${WEB2API_ENV[@]}" /usr/bin/gemini-web2api --config /data/config.json &
WEB_PID=$!

# Chờ Web2API healthy (tối đa 30 giây).
for i in $(seq 1 30); do
  if wget -qO- http://127.0.0.1:8083/health >/dev/null 2>&1; then
    break
  fi
  if ! kill -0 "$WEB_PID" 2>/dev/null; then
    echo "[FATAL] Gemini Web2API exited during startup." >&2
    cleanup 1
  fi
  sleep 1
done
if ! wget -qO- http://127.0.0.1:8083/health >/dev/null 2>&1; then
  echo "[FATAL] Gemini Web2API health check failed." >&2
  cleanup 1
fi

# ── Khởi động Image Adapter (process PID 1 con) ─────────────────────────────

su-exec gemini:gemini "${ADAPTER_ENV[@]}" python3 /opt/gemini-image-adapter/app.py &
ADAPTER_PID=$!

# Chờ adapter healthy (tối đa 30 giây). Startup fail-closed: nếu adapter không
# healthy, container thoát để Supervisor khởi động lại.
for i in $(seq 1 30); do
  if wget -qO- http://127.0.0.1:8084/health >/dev/null 2>&1; then
    break
  fi
  if ! kill -0 "$ADAPTER_PID" 2>/dev/null; then
    echo "[FATAL] Image adapter exited during startup." >&2
    cleanup 1
  fi
  sleep 1
done
if ! wget -qO- http://127.0.0.1:8084/health >/dev/null 2>&1; then
  echo "[FATAL] Image adapter health check failed." >&2
  cleanup 1
fi

echo "[run.sh] Both services healthy. Entering supervision loop." >&2

# ── Vòng lặp giám sát liên tục ──────────────────────────────────────────────

ADAPTER_RESTARTS=0
LAST_HEALTH_CHECK=0
HEALTH_INTERVAL=10

while true; do
    now=$(date +%s)

    # Kiểm tra Web2API process còn sống.
    if [[ -n "${WEB_PID:-}" ]] && ! kill -0 "$WEB_PID" 2>/dev/null; then
        echo "[FATAL] Gemini Web2API process died." >&2
        cleanup 1
    fi

    # Kiểm tra Adapter process còn sống.
    if [[ -n "${ADAPTER_PID:-}" ]] && ! kill -0 "$ADAPTER_PID" 2>/dev/null; then
        ADAPTER_RESTARTS=$((ADAPTER_RESTARTS + 1))
        if [[ ${ADAPTER_RESTARTS} -gt 3 ]]; then
            echo "[FATAL] Image adapter failed repeatedly; giving up." >&2
            cleanup 1
        fi
        echo "[WARN] Image adapter process died; restarting (attempt ${ADAPTER_RESTARTS})." >&2
        su-exec gemini:gemini "${ADAPTER_ENV[@]}" python3 /opt/gemini-image-adapter/app.py &
        ADAPTER_PID=$!
        # Chờ health sau restart.
        for i in $(seq 1 15); do
            if wget -qO- http://127.0.0.1:8084/health >/dev/null 2>&1; then
                break
            fi
            if ! kill -0 "$ADAPTER_PID" 2>/dev/null; then
                break
            fi
            sleep 1
        done
        if ! wget -qO- http://127.0.0.1:8084/health >/dev/null 2>&1; then
            echo "[FATAL] Image adapter failed health check after restart." >&2
            cleanup 1
        fi
    fi

    # Health polling liên tục (mỗi HEALTH_INTERVAL giây).
    if [[ $((now - LAST_HEALTH_CHECK)) -ge ${HEALTH_INTERVAL} ]]; then
        LAST_HEALTH_CHECK=$now

        # Kiểm tra Web2API health endpoint.
        if [[ -n "${WEB_PID:-}" ]] && kill -0 "$WEB_PID" 2>/dev/null; then
            if ! wget -qO- --timeout=5 http://127.0.0.1:8083/health >/dev/null 2>&1; then
                echo "[WARN] Web2API health check failed." >&2
                sleep 2
                if ! wget -qO- --timeout=5 http://127.0.0.1:8083/health >/dev/null 2>&1; then
                    echo "[FATAL] Web2API unhealthy after retry." >&2
                    cleanup 1
                fi
            fi
        fi

        # Kiểm tra Image Adapter health endpoint.
        if [[ -n "${ADAPTER_PID:-}" ]] && kill -0 "$ADAPTER_PID" 2>/dev/null; then
            if ! wget -qO- --timeout=5 http://127.0.0.1:8084/health >/dev/null 2>&1; then
                echo "[WARN] Image adapter health check failed." >&2
                sleep 2
                if ! wget -qO- --timeout=5 http://127.0.0.1:8084/health >/dev/null 2>&1; then
                    echo "[FATAL] Image adapter unhealthy after retry." >&2
                    cleanup 1
                fi
            fi
        fi
    fi

    sleep 5
done
