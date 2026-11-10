-- Finuance database bootstrap: roles, database, extensions, default privileges.
-- Run ONCE as a superuser. This is not a goose migration (it needs superuser rights).
-- Passwords are injected by the runner as psql variables:
--   psql -v ON_ERROR_STOP=1 -v owner_password=… -v app_password=… -f bootstrap.sql
-- In Compose, deploy/db/initdb/00-bootstrap.sh does this; on k8s, CloudNativePG managed roles replace the CREATE ROLE lines.

CREATE ROLE finuance_owner LOGIN PASSWORD :'owner_password';   -- owns schema, runs migrations
CREATE ROLE finuance_app   LOGIN PASSWORD :'app_password';     -- used by api + worker; subject to RLS
CREATE DATABASE finuance OWNER finuance_owner;

-- Only the two Finuance roles may connect, plus monitoring (members of the built-in pg_monitor,
-- e.g. CloudNativePG's cnpg_metrics_exporter, read statistics only: no table grants, and RLS).
REVOKE ALL ON DATABASE finuance FROM PUBLIC;
GRANT CONNECT, TEMPORARY ON DATABASE finuance TO finuance_owner, finuance_app;
GRANT CONNECT ON DATABASE finuance TO pg_monitor;

\connect finuance

ALTER SCHEMA public OWNER TO finuance_owner;
REVOKE CREATE ON SCHEMA public FROM PUBLIC;
CREATE EXTENSION IF NOT EXISTS pg_trgm;
CREATE EXTENSION IF NOT EXISTS citext;

-- Let LIKE serve as an index condition under row-level security (Phase 7, the maintainer's decision; docs/perf.md).
-- PostgreSQL only uses leakproof operators in index scans on RLS tables. textlike can raise an error
-- only for a malformed pattern, never because of the row's value, so it can't reveal other users'
-- rows. The flag lives in this database's catalog: repeat it after a dump/restore into a new cluster.
ALTER FUNCTION pg_catalog.textlike(text, text) LEAKPROOF;

GRANT USAGE ON SCHEMA public TO finuance_app;
ALTER DEFAULT PRIVILEGES FOR ROLE finuance_owner IN SCHEMA public
  GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO finuance_app;
ALTER DEFAULT PRIVILEGES FOR ROLE finuance_owner IN SCHEMA public
  GRANT USAGE, SELECT ON SEQUENCES TO finuance_app;
