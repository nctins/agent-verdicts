-- When a person last signed in with their password (spec
-- docs/vi/specs/2026-11-22-admin-ui-redesign.md A1). NULL: not since this column exists.
ALTER TABLE login ADD COLUMN last_login_at timestamptz;
