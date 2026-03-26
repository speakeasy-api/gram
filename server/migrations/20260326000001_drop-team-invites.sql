-- atlas:txtar

-- checks/destructive.sql --
-- Table was never used in production; safe to drop unconditionally.
SELECT TRUE AS team_invites_ok;

-- migration.sql --
-- Drop team_invites table — feature no longer needed
DROP TABLE IF EXISTS team_invites;
