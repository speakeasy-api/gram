-- Modify "channels_access_exceptions" table
ALTER TABLE "channels_access_exceptions" DROP CONSTRAINT "channels_access_exceptions_project_id_fkey", ADD CONSTRAINT "channels_access_exceptions_project_fkey" FOREIGN KEY ("organization_id", "project_id") REFERENCES "projects" ("organization_id", "id") ON UPDATE NO ACTION ON DELETE CASCADE;
-- Modify "channels_conversation_endpoints" table
ALTER TABLE "channels_conversation_endpoints" DROP CONSTRAINT "channels_conversation_endpoints_project_id_fkey", ADD CONSTRAINT "channels_conversation_endpoints_project_fkey" FOREIGN KEY ("organization_id", "project_id") REFERENCES "projects" ("organization_id", "id") ON UPDATE NO ACTION ON DELETE CASCADE;
-- Modify "channels_conversation_participants" table
ALTER TABLE "channels_conversation_participants" DROP CONSTRAINT "channels_conversation_participants_project_id_fkey", ADD CONSTRAINT "channels_conversation_participants_project_fkey" FOREIGN KEY ("organization_id", "project_id") REFERENCES "projects" ("organization_id", "id") ON UPDATE NO ACTION ON DELETE CASCADE;
-- Modify "channels_conversations" table
ALTER TABLE "channels_conversations" DROP CONSTRAINT "channels_conversations_project_id_fkey", ADD CONSTRAINT "channels_conversations_project_fkey" FOREIGN KEY ("organization_id", "project_id") REFERENCES "projects" ("organization_id", "id") ON UPDATE NO ACTION ON DELETE CASCADE;
-- Modify "channels_instances" table
ALTER TABLE "channels_instances" DROP CONSTRAINT "channels_instances_project_id_fkey", ADD CONSTRAINT "channels_instances_project_fkey" FOREIGN KEY ("organization_id", "project_id") REFERENCES "projects" ("organization_id", "id") ON UPDATE NO ACTION ON DELETE CASCADE;
-- Modify "channels_messages" table
ALTER TABLE "channels_messages" DROP CONSTRAINT "channels_messages_project_id_fkey", ADD CONSTRAINT "channels_messages_project_fkey" FOREIGN KEY ("organization_id", "project_id") REFERENCES "projects" ("organization_id", "id") ON UPDATE NO ACTION ON DELETE CASCADE;
-- Modify "channels_settings" table
ALTER TABLE "channels_settings" DROP CONSTRAINT "channels_settings_project_id_fkey", ADD CONSTRAINT "channels_settings_project_fkey" FOREIGN KEY ("organization_id", "project_id") REFERENCES "projects" ("organization_id", "id") ON UPDATE NO ACTION ON DELETE CASCADE;
-- Modify "channels_state" table
ALTER TABLE "channels_state" DROP CONSTRAINT "channels_state_organization_id_fkey", DROP CONSTRAINT "channels_state_project_id_fkey", ADD CONSTRAINT "channels_state_organization_id_fkey" FOREIGN KEY ("organization_id") REFERENCES "organization_metadata" ("id") ON UPDATE NO ACTION ON DELETE CASCADE, ADD CONSTRAINT "channels_state_project_fkey" FOREIGN KEY ("organization_id", "project_id") REFERENCES "projects" ("organization_id", "id") ON UPDATE NO ACTION ON DELETE CASCADE;
-- Modify "channels_thread_preferences" table
ALTER TABLE "channels_thread_preferences" DROP CONSTRAINT "channels_thread_preferences_project_id_fkey", ADD CONSTRAINT "channels_thread_preferences_project_fkey" FOREIGN KEY ("organization_id", "project_id") REFERENCES "projects" ("organization_id", "id") ON UPDATE NO ACTION ON DELETE CASCADE;
