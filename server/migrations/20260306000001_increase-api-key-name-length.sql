-- Increase API key name length limit from 40 to 255 characters to support URL-based names
ALTER TABLE api_keys DROP CONSTRAINT IF EXISTS api_keys_name_check;
ALTER TABLE api_keys ADD CONSTRAINT api_keys_name_check CHECK (name <> '' AND CHAR_LENGTH(name) <= 255);
