/** One top-level argument of a tool, ready to render as a form field. */
export type ToolField = {
  name: string;
  description?: string;
  required: boolean;
  /** Short type label shown next to the name, e.g. `string`, `string[]`. */
  typeLabel: string;
  /** The argument's own JSON Schema, for people who need the exact shape. */
  schema?: unknown;
  /** A sample value built from the schema, for the input's placeholder. */
  example?: string;
} & (
  | { kind: "string" | "number" | "boolean" | "json" }
  /** Plain text, or JSON when it parses (a union such as string | string[]). */
  | { kind: "text-or-json" }
  | { kind: "enum"; options: string[] }
  /** Fixed by the schema; sent as-is and never shown. */
  | { kind: "const"; value: unknown }
);

/** Flattens a tool's JSON Schema into its top-level arguments. */
export function extractFields(inputSchema: unknown): ToolField[] {
  const schema = readJsonSchema(inputSchema);
  if (!schema) return [];
  const properties = isRecord(schema.properties) ? schema.properties : {};
  const required = new Set(
    Array.isArray(schema.required)
      ? schema.required.filter((r): r is string => typeof r === "string")
      : [],
  );

  return Object.entries(properties).map(([name, def]): ToolField => {
    const base = {
      name,
      required: required.has(name),
      description:
        isRecord(def) && typeof def.description === "string"
          ? def.description
          : undefined,
      schema: def,
    };
    if (!isRecord(def)) return { ...base, kind: "json", typeLabel: "any" };
    if ("const" in def) {
      return { ...base, kind: "const", value: def.const, typeLabel: "const" };
    }

    if (
      Array.isArray(def.enum) &&
      def.enum.length > 0 &&
      def.enum.every((value) => typeof value === "string")
    ) {
      return {
        ...base,
        kind: "enum",
        options: def.enum as string[],
        typeLabel: "enum",
      };
    }

    // A union reads as its variants; one that takes a string takes text.
    const variants = unionVariants(def);
    if (variants) {
      const labels = variants.map(typeLabelOf);
      const takesText = variants.some((v) => typeOf(v) === "string");
      return {
        ...base,
        kind: takesText ? "text-or-json" : "json",
        typeLabel: [...new Set(labels)].join(" | "),
        example: exampleText(
          variants.find((v) => typeOf(v) !== "string") ?? variants[0],
        ),
      };
    }

    // An untyped property is treated as an object.
    switch (typeOf(def) ?? "object") {
      case "string":
        return { ...base, kind: "string", typeLabel: "string" };
      case "integer":
      case "number":
        return { ...base, kind: "number", typeLabel: typeLabelOf(def) };
      case "boolean":
        return { ...base, kind: "boolean", typeLabel: "boolean" };
      default:
        return {
          ...base,
          kind: "json",
          typeLabel: typeLabelOf(def),
          example: exampleText(def),
        };
    }
  });
}

/**
 * Turns form values back into tool arguments. Empty optional fields are left
 * out, so the server applies its own defaults.
 */
export function toArguments(
  fields: ToolField[],
  values: Record<string, string>,
): { ok: true; args: Record<string, unknown> } | { ok: false; error: string } {
  const args: Record<string, unknown> = {};
  for (const field of fields) {
    if (field.kind === "const") {
      args[field.name] = field.value;
      continue;
    }
    const raw = values[field.name] ?? "";
    if (raw.trim() === "" && field.kind !== "boolean") {
      if (field.required)
        return { ok: false, error: `${field.name} is required.` };
      continue;
    }
    switch (field.kind) {
      case "boolean":
        if (raw === "" && !field.required) continue;
        args[field.name] = raw === "true";
        break;
      case "number": {
        const number = Number(raw);
        if (Number.isNaN(number))
          return { ok: false, error: `${field.name} must be a number.` };
        args[field.name] = number;
        break;
      }
      case "json":
        try {
          args[field.name] = JSON.parse(raw);
        } catch {
          return {
            ok: false,
            error: `${field.name} must be valid JSON${field.example ? `, like ${field.example}` : ""}.`,
          };
        }
        break;
      case "text-or-json": {
        // Lists and objects arrive as JSON; anything else is the text itself.
        const trimmed = raw.trim();
        if (trimmed.startsWith("[") || trimmed.startsWith("{")) {
          try {
            args[field.name] = JSON.parse(trimmed);
            break;
          } catch {
            // Not JSON after all: send the text.
          }
        }
        args[field.name] = raw;
        break;
      }
      case "string":
      case "enum":
        args[field.name] = raw;
    }
  }
  return { ok: true, args };
}

function unionVariants(
  def: Record<string, unknown>,
): Record<string, unknown>[] | undefined {
  const list = Array.isArray(def.anyOf)
    ? def.anyOf
    : Array.isArray(def.oneOf)
      ? def.oneOf
      : Array.isArray(def.type) &&
          def.type.filter((t) => t !== "null").length > 1
        ? def.type
            .filter((t): t is string => typeof t === "string" && t !== "null")
            .map((type) => ({ ...def, type }))
        : undefined;
  if (!list) return undefined;
  const variants = list.filter(isRecord).filter((v) => typeOf(v) !== "null");
  return variants.length > 0 ? variants : undefined;
}

function typeOf(def: Record<string, unknown>): string | undefined {
  if (Array.isArray(def.type)) {
    const first = def.type.find((t) => t !== "null");
    return typeof first === "string" ? first : undefined;
  }
  if (typeof def.type === "string") return def.type;
  if (isRecord(def.properties)) return "object";
  if (def.items !== undefined) return "array";
  return undefined;
}

function typeLabelOf(def: Record<string, unknown>): string {
  const type = typeOf(def);
  if (type === "array") {
    const items = isRecord(def.items) ? typeLabelOf(def.items) : "any";
    return items.includes(" ") ? `(${items})[]` : `${items}[]`;
  }
  return type ?? "object";
}

/** A short sample of the shape the schema wants, as JSON text. */
function exampleText(
  def: Record<string, unknown> | undefined,
): string | undefined {
  if (!def) return undefined;
  const text = JSON.stringify(exampleOf(def, 0));
  return text && text.length <= 120 ? text : undefined;
}

function exampleOf(def: Record<string, unknown>, depth: number): unknown {
  if (depth > 3) return null;
  if (def.default !== undefined) return def.default;
  if (Array.isArray(def.examples) && def.examples.length > 0)
    return def.examples[0];
  if (Array.isArray(def.enum) && def.enum.length > 0) return def.enum[0];
  const variants = unionVariants(def);
  if (variants) return exampleOf(variants[0]!, depth + 1);
  switch (typeOf(def) ?? "unknown") {
    case "string":
      return "…";
    case "integer":
    case "number":
      return 0;
    case "boolean":
      return false;
    case "array":
      return isRecord(def.items) ? [exampleOf(def.items, depth + 1)] : [];
    case "object": {
      const props = isRecord(def.properties) ? def.properties : {};
      return Object.fromEntries(
        Object.entries(props)
          .slice(0, 4)
          .map(([key, value]) => [
            key,
            isRecord(value) ? exampleOf(value, depth + 1) : null,
          ]),
      );
    }
    default:
      return null;
  }
}

/** Some AI SDK paths wrap the schema as `{ jsonSchema }`; unwrap it. */
function readJsonSchema(
  inputSchema: unknown,
): Record<string, unknown> | undefined {
  if (!isRecord(inputSchema)) return undefined;
  if (isRecord(inputSchema.jsonSchema)) return inputSchema.jsonSchema;
  return inputSchema;
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}
