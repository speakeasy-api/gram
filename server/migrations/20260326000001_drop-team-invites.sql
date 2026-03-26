-- atlas:txtar

-- checks/destructive.sql --
SELECT NOT EXISTS (SELECT 1 FROM team_invites) AS team_invites_empty;

-- migration.sql --
DROP TABLE IF EXISTS team_invites;
