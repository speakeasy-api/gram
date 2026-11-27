-- Modify "skill_edit_suggestion_changes" table
ALTER TABLE "skill_edit_suggestion_changes" ADD COLUMN "path" text NULL;
-- Set comment to column: "path" on table: "skill_edit_suggestion_changes"
COMMENT ON COLUMN "skill_edit_suggestion_changes"."path" IS 'Skill-relative path of the file this change edits, such as references/api.md. NULL means SKILL.md.
@access: opaque-confidential';
