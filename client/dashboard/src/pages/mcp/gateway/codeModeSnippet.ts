import { toArguments, type ToolField } from "../inspect/toolSchemaFields";

/** What the Code Mode editor starts with: list the tools the program can call. */
export const DEFAULT_CODE_MODE_SOURCE = 'await tools.search("")';

/**
 * A Code Mode call to one member tool, in the shape the gateway's execute
 * description teaches: `tools.call(path, arguments)`, then the data when the
 * call succeeded. Arguments come from the form values: empty optional ones are
 * left out, and an empty required one is a named placeholder such as
 * `"<owner/repo>"`, so the snippet says what still needs filling in.
 */
export function codeModeCallSnippet(
  path: string,
  fields: ToolField[],
  values: Record<string, string>,
): string {
  const entries: [string, string][] = [];
  // A whole-arguments field that is empty or invalid becomes one placeholder.
  let argsPlaceholder: string | undefined;
  for (const field of fields) {
    if (field.kind === "arguments") {
      const parsed = toArguments([field], values);
      if (parsed.ok) {
        for (const [key, value] of Object.entries(parsed.args))
          entries.push([key, toPython(value)]);
      } else {
        argsPlaceholder = JSON.stringify(placeholderFor(field));
      }
      continue;
    }
    const raw = values[field.name] ?? "";
    const empty =
      field.kind !== "const" && raw.trim() === "" && field.kind !== "boolean";
    if (empty) {
      if (field.required)
        entries.push([field.name, JSON.stringify(placeholderFor(field))]);
      continue;
    }
    if (field.kind === "boolean" && raw === "" && !field.required) continue;
    const parsed = toArguments([field], { [field.name]: raw });
    entries.push([
      field.name,
      parsed.ok
        ? toPython(parsed.args[field.name])
        : JSON.stringify(placeholderFor(field)),
    ]);
  }
  const args =
    argsPlaceholder ??
    (entries.length === 0
      ? "{}"
      : `{\n${entries.map(([key, value]) => `    ${JSON.stringify(key)}: ${value},`).join("\n")}\n}`);
  return `result = await tools.call(${JSON.stringify(path)}, ${args})\nresult["data"] if result["ok"] else result`;
}

/** Text naming what an empty argument wants, e.g. `<owner/repo>`. */
function placeholderFor(field: ToolField): string {
  const schema = isRecord(field.schema) ? field.schema : {};
  const example = Array.isArray(schema.examples)
    ? schema.examples.find((value) => typeof value === "string")
    : undefined;
  if (typeof example === "string" && example.length <= 40)
    return `<${example}>`;
  // A shape spelled out in the description, such as "owner/repo".
  const shape = field.description?.match(
    /\b[A-Za-z][\w.-]*\/[A-Za-z][\w.-]*\b/,
  )?.[0];
  if (shape) return `<${shape}>`;
  if (typeof schema.format === "string") return `<${schema.format}>`;
  return `<${field.name}>`;
}

/** A JSON value as a Python literal. JSON strings are valid Python strings. */
function toPython(value: unknown): string {
  if (value === null || value === undefined) return "None";
  if (value === true) return "True";
  if (value === false) return "False";
  if (typeof value === "number")
    return Number.isFinite(value) ? String(value) : "None";
  if (typeof value === "string") return JSON.stringify(value);
  if (Array.isArray(value)) return `[${value.map(toPython).join(", ")}]`;
  if (isRecord(value)) {
    const entries = Object.entries(value).map(
      ([key, item]) => `${JSON.stringify(key)}: ${toPython(item)}`,
    );
    return `{${entries.join(", ")}}`;
  }
  return "None";
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}
