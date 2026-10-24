-- Finuance database bootstrap on CloudNativePG (PLAN task 16.2): the parts of deploy/db/bootstrap.sql
-- that CloudNativePG doesn't do itself. Run once by `bootstrap.initdb.postInitApplicationSQLRefs`,
-- as the superuser, in the finuance database, right after initdb created it with its owner
-- (finuance_owner). Logins are not set up here: the cluster's managed roles do that.
-- tools/check-db-bootstrap.sh keeps this file equal to deploy/db/bootstrap.sql (minus the role
-- and database creation, plus the app role created bare).

CREATE ROLE finuance_app LOGIN;     -- used by api + worker; subject to RLS (login set by the managed role)

-- Only the two Finuance roles may connect.
REVOKE ALL ON DATABASE finuance FROM PUBLIC;
GRANT CONNECT, TEMPORARY ON DATABASE finuance TO finuance_owner, finuance_app;

ALTER SCHEMA public OWNER TO finuance_owner;
REVOKE CREATE ON SCHEMA public FROM PUBLIC;
CREATE EXTENSION IF NOT EXISTS pg_trgm;
CREATE EXTENSION IF NOT EXISTS citext;

-- Let LIKE serve as an index condition under row-level security (Phase 7; docs/perf.md). The flag
-- lives in the catalog: a restore from this cluster's backups keeps it, a logical restore into a
-- new cluster needs it again (docs/RUNBOOK.md).
ALTER FUNCTION pg_catalog.textlike(text, text) LEAKPROOF;

GRANT USAGE ON SCHEMA public TO finuance_app;
ALTER DEFAULT PRIVILEGES FOR ROLE finuance_owner IN SCHEMA public
  GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO finuance_app;
ALTER DEFAULT PRIVILEGES FOR ROLE finuance_owner IN SCHEMA public
  GRANT USAGE, SELECT ON SEQUENCES TO finuance_app;
