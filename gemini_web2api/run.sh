#!/usr/bin/with-contenv bashio
set -Eeuo pipefail

# Keep generated config and secrets private. The Supervisor options file is read
# while this entrypoint still runs as root.
umask 077
mkdir -p /data

admin_token="$(bashio::config 'admin_token')"
api_key="$(bashio::config 'api_key')"
disable_media="$(bashio::config 'disable_media_generation')"
disable_url="$(bashio::config 'disable_url_fetch')"
disable_acct="$(bashio::config 'disable_account_pool')"
disable_proxy="$(bashio::config 'disable_proxy_pool')"
raw_cors="$(bashio::config 'cors_allow_origins')"

# Fail early rather than install an Add-on whose Admin UI cannot be accessed.
if [[ -z "${admin_token}" || "${admin_token}" == "null" || ${#admin_token} -lt 32 ]]; then
  echo "[FATAL] Configure an Admin token of at least 32 characters in the Add-on options." >&2
  exit 1
fi

# Normalize booleans that bashio returns as strings.
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

# Normalize cors_allow_origins into a valid JSON array of strings.
# HAOS may return an empty/null/non-JSON value when the option is unset.
if [[ -z "${raw_cors}" || "${raw_cors}" == "null" ]]; then
  cors_json='[]'
else
  if ! printf '%s' "${raw_cors}" | jq -e 'type == "array" and all(.[]; type == "string")' >/dev/null 2>&1; then
    echo "[FATAL] cors_allow_origins must be a JSON array of strings (e.g. [\"https://example.com\"]). Got: ${raw_cors}" >&2
    exit 1
  fi
  cors_json="${raw_cors}"
fi

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

# Preserve ownership of existing database/key files across upgrades, then run
# the Go service as an unprivileged account.
chown gemini:gemini /data
for path in /data/gemini.db* /data/secret.key /data/config.json; do
  if [[ -e "$path" ]]; then
    chown gemini:gemini "$path"
  fi
done
chmod 0700 /data

# The application supports API_KEY as an environment variable. Avoid passing
# the secret as a CLI argument, where it could appear in process listings.
export ADMIN_TOKEN="$admin_token"
export API_KEY="$api_key"
exec su-exec gemini:gemini /usr/bin/gemini-web2api --config /data/config.json
