-- Remove token length limit since tokens now include base64-encoded workspace slug
ALTER TABLE team_invites DROP CONSTRAINT team_invites_token_check;
ALTER TABLE team_invites ADD CONSTRAINT team_invites_token_check CHECK (token <> ''::text);
