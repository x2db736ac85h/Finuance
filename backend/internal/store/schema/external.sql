-- Tables created by tools rather than by our migrations. sqlc reads this file to type-check
-- queries against them; it is never executed.

-- goose's version table (github.com/pressly/goose/v3).
CREATE TABLE goose_db_version (
    id         serial PRIMARY KEY,
    version_id bigint    NOT NULL,
    is_applied boolean   NOT NULL,
    tstamp     timestamp NOT NULL DEFAULT now()
);
