-- Modify "http_tool_definitions" table
ALTER TABLE "http_tool_definitions" ADD CONSTRAINT "http_tool_definitions_openapiv3_document_id_fkey" FOREIGN KEY ("openapiv3_document_id") REFERENCES "deployments_openapiv3_assets" ("id") ON UPDATE NO ACTION ON DELETE RESTRICT;
