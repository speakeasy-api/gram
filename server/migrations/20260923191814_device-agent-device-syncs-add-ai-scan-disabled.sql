-- Modify "device_agent_device_syncs" table
ALTER TABLE "device_agent_device_syncs" ADD COLUMN "ai_scan_disabled" boolean NOT NULL DEFAULT false;
