-- Add source column to chats table to distinguish between Elements, Playground, and ClaudeCode sessions
ALTER TABLE chats ADD COLUMN IF NOT EXISTS source TEXT;
