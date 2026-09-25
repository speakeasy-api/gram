import type { DiagnosticsOptions } from "monaco-editor/languages/features/json/register.js";
import schema from "../../../../../server/internal/mcpregistry/contract/record.schema.json";

// The same offline asset embedded by Go; imported only through the lazy editor.
// Monaco assists editing; Go's format and mutation checks remain authoritative.
export const registryJsonDiagnostics: DiagnosticsOptions = {
  validate: true,
  allowComments: false,
  comments: "error",
  trailingCommas: "error",
  enableSchemaRequest: false,
  schemaValidation: "error",
  schemas: [
    {
      uri: "gram-registry://schema/record.schema.json",
      fileMatch: ["gram-registry://draft/*.json"],
      schema,
    },
  ],
};
