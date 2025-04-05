-- Modify "http_tool_definitions" table
ALTER TABLE "http_tool_definitions" DROP CONSTRAINT "http_tool_definitions_openapiv3_document_id_fkey";
-- Drop "openapiv3_documents" table
DROP TABLE "openapiv3_documents";
