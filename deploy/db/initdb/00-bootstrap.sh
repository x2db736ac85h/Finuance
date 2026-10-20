#!/usr/bin/env bash
# Runs once, from /docker-entrypoint-initdb.d/, when the Postgres volume is first created.
# Passes the Finuance role passwords from env into deploy/db/bootstrap.sql as psql variables.
set -euo pipefail

: "${FINUANCE_OWNER_PASSWORD:?FINUANCE_OWNER_PASSWORD must be set}"
: "${FINUANCE_APP_PASSWORD:?FINUANCE_APP_PASSWORD must be set}"

psql -v ON_ERROR_STOP=1 \
  --username "$POSTGRES_USER" --dbname postgres \
  -v owner_password="$FINUANCE_OWNER_PASSWORD" \
  -v app_password="$FINUANCE_APP_PASSWORD" \
  -f /finuance/bootstrap.sql
