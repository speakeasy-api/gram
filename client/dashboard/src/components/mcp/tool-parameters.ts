/**
 * Reading a tool's JSON Schema as a flat list of named arguments, for the MCP
 * surfaces that browse tools they do not own. Lives beside the components that
 * render it rather than inside them, so a schema can also be read without
 * rendering anything.
 */

export type ToolParameter = {
  name: string;
  type: string;
  description?: string;
  required: boolean;
};

/** Flatten a tool's JSON Schema into a flat list of named parameters. */
export function extractToolParameters(inputSchema: unknown): ToolParameter[] {
  const schema = readJsonSchema(inputSchema);
  if (!schema) return [];

  const properties = isRecord(schema.properties) ? schema.properties : {};
  const required = new Set(
    Array.isArray(schema.required)
      ? schema.required.filter((r): r is string => typeof r === "string")
      : [],
  );

  return Object.entries(properties).map(([name, def]) => ({
    name,
    type: jsonSchemaTypeLabel(def),
    description:
      isRecord(def) && typeof def.description === "string"
        ? def.description
        : undefined,
    required: required.has(name),
  }));
}

/**
 * Accepts the shapes the callers actually hold: a live MCP client hands over
 * the schema object itself, some AI SDK paths stow it on `.jsonSchema`, and the
 * built-in catalog serves it as a JSON string.
 */
function readJsonSchema(
  inputSchema: unknown,
): Record<string, unknown> | undefined {
  if (typeof inputSchema === "string") {
    try {
      return readJsonSchema(JSON.parse(inputSchema));
    } catch {
      return undefined;
    }
  }
  if (!isRecord(inputSchema)) return undefined;
  if (isRecord(inputSchema.jsonSchema)) return inputSchema.jsonSchema;
  return inputSchema;
}

/** A short, human-readable type label for a JSON Schema property definition. */
function jsonSchemaTypeLabel(def: unknown): string {
  if (!isRecord(def)) return "unknown";

  if (Array.isArray(def.enum) && def.enum.length > 0) {
    return def.enum.map((value) => JSON.stringify(value)).join(" | ");
  }

  const type = def.type;
  if (Array.isArray(type)) {
    // "null | string" is how a schema spells optional; the parameter list
    // already says whether a value is required, so the null adds nothing.
    const names = type.filter(
      (t): t is string => typeof t === "string" && t !== "null",
    );
    return names.length > 0 ? names.join(" | ") : "unknown";
  }
  if (type === "array") {
    const items = def.items;
    const itemType =
      isRecord(items) && typeof items.type === "string"
        ? items.type
        : undefined;
    return itemType ? `${itemType}[]` : "array";
  }

  return typeof type === "string" ? type : "unknown";
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}
