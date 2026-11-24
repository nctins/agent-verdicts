-- The db image moved to ParadeDB 0.27.0: a database created on an older image
-- keeps the old extension SQL until updated. A no-op on a fresh database.
ALTER EXTENSION vector UPDATE;
ALTER EXTENSION pg_search UPDATE;
