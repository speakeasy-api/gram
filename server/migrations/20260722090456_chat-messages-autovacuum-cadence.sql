-- Set "autovacuum_vacuum_insert_scale_factor" storage parameter on table: "chat_messages"
ALTER TABLE "chat_messages" SET (autovacuum_vacuum_insert_scale_factor = 0);
-- Set "autovacuum_vacuum_insert_threshold" storage parameter on table: "chat_messages"
ALTER TABLE "chat_messages" SET (autovacuum_vacuum_insert_threshold = 250000);
-- Set "autovacuum_vacuum_cost_limit" storage parameter on table: "chat_messages"
ALTER TABLE "chat_messages" SET (autovacuum_vacuum_cost_limit = 2000);
-- Set "autovacuum_vacuum_scale_factor" storage parameter on table: "chat_messages"
ALTER TABLE "chat_messages" SET (autovacuum_vacuum_scale_factor = 0.02);
