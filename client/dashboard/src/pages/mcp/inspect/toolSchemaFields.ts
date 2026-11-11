/** One choice of an enum field: what the list shows and what is sent. */
interface ToolFieldOption {
  label: string;
  /** The value as the schema types it, e.g. the number 2, not "2". */
  value: unknown;
}

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
  | { kind: "string" | "boolean" | "json" }
  | { kind: "number"; integer?: boolean }
  /** Plain text, or JSON when it parses (a union such as string | string[]). */
  | { kind: "text-or-json" }
  | { kind: "enum"; options: ToolFieldOption[] }
  /** Fixed by the schema; sent as-is and never shown. */
  | { kind: "const"; value: unknown }
  /**
   * The whole arguments object as one JSON editor, for schemas whose shape
   * can't be split into top-level fields (allOf/anyOf/oneOf at the root).
   */
  | { kind: "arguments" }
);

/** The form value an enum option is stored under. */
export function optionKey(value: unknown): string {
  return JSON.stringify(value) ?? "null";
}

/** Flattens a tool's JSON Schema into its top-level arguments. */
export function extractFields(inputSchema: unknown): ToolField[] {
  const schema = readJsonSchema(inputSchema);
  if (!schema) return [];
  const properties = isRecord(schema.properties) ? schema.properties : {};
  if (isComposed(schema, properties)) {
    return [
      {
        name: "arguments",
        required: true,
        kind: "arguments",
        typeLabel: "object",
        schema,
        example: exampleText(schema),
      },
    ];
  }
  const required = new Set(
    Array.isArray(schema.required)
      ? schema.required.filter((r): r is string => typeof r === "string")
      : [],
  );

  return Object.entries(properties).map(([name, def]) =>
    fieldFor(name, def, required.has(name)),
  );
}

/**
 * True when the top level can't be read as a plain property list: a root
 * allOf/anyOf/oneOf/$ref, or arbitrary keys described by a schema.
 */
function isComposed(
  schema: Record<string, unknown>,
  properties: Record<string, unknown>,
): boolean {
  if (
    Array.isArray(schema.allOf) ||
    Array.isArray(schema.anyOf) ||
    Array.isArray(schema.oneOf) ||
    typeof schema.$ref === "string"
  )
    return true;
  return (
    Object.keys(properties).length === 0 &&
    (isRecord(schema.additionalProperties) ||
      isRecord(schema.patternProperties))
  );
}

function fieldFor(name: string, def: unknown, required: boolean): ToolField {
  const base = {
    name,
    required,
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

  // null stays a choice, so a required nullable enum can still send null.
  const enumValues: unknown[] = Array.isArray(def.enum) ? def.enum : [];
  if (
    enumValues.some((value) => value !== null) &&
    enumValues.every((value) => value === null || isScalar(value))
  ) {
    return {
      ...base,
      kind: "enum",
      options: enumValues.map((value) => ({ label: labelOf(value), value })),
      typeLabel: "enum",
    };
  }

  const variants = unionVariants(def);
  if (variants) {
    // A nullable type (`anyOf: [X, { type: "null" }]`) reads as X itself.
    if (variants.length === 1) {
      const { anyOf: _anyOf, oneOf: _oneOf, ...rest } = def;
      const field = fieldFor(name, { ...rest, ...variants[0] }, required);
      return { ...field, description: base.description ?? field.description };
    }
    // A union of fixed values is a list of choices.
    if (variants.every((v) => "const" in v && isScalar(v.const))) {
      return {
        ...base,
        kind: "enum",
        options: variants.map((v) => ({
          label: typeof v.title === "string" ? v.title : labelOf(v.const),
          value: v.const,
        })),
        typeLabel: "enum",
      };
    }
    // Any other union reads as its variants; one that takes a string takes text.
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
      return { ...base, kind: "number", integer: true, typeLabel: "integer" };
    case "number":
      return { ...base, kind: "number", typeLabel: "number" };
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
}

function isScalar(value: unknown): value is string | number | boolean {
  return (
    typeof value === "string" ||
    typeof value === "number" ||
    typeof value === "boolean"
  );
}

function labelOf(value: unknown): string {
  return typeof value === "string" ? value : optionKey(value);
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
    if (field.kind === "arguments") {
      if (raw.trim() === "") {
        if (field.required)
          return { ok: false, error: "Arguments are required." };
        continue;
      }
      let parsed: unknown;
      try {
        parsed = JSON.parse(raw);
      } catch {
        return { ok: false, error: "Arguments must be valid JSON." };
      }
      if (!isRecord(parsed))
        return { ok: false, error: "Arguments must be a JSON object." };
      Object.assign(args, parsed);
      continue;
    }
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
        if (!Number.isFinite(number))
          return { ok: false, error: `${field.name} must be a number.` };
        if (field.integer && !Number.isInteger(number))
          return { ok: false, error: `${field.name} must be a whole number.` };
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
      case "enum": {
        const option = field.options.find((o) => optionKey(o.value) === raw);
        if (!option)
          return {
            ok: false,
            error: `${field.name} must be one of the choices.`,
          };
        args[field.name] = option.value;
        break;
      }
      case "string":
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
