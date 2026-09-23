import metaschemas from "./metaschemas.json" with { type: "json" };
// Source filters: mcp-registry c5873aaaad0a2988e39f1ed7c237006bbfb05826 Schema.ts.
// The URI grammar remains in record.schema.json; this checks bracketed IP literals.
const uri = (value) => {
  const literal = /^[^:]+:\/\/(?:[^@/?#]*@)?(\[[^\]]+\])/.exec(value)?.[1];
  return (
    literal === undefined ||
    /^\[[vV]/.test(literal) ||
    URL.canParse(`http://${literal}`)
  );
};
const timestamp = (value) => {
  const p =
    /^(\d{4})-(0[1-9]|1[0-2])-(0[1-9]|[12]\d|3[01])[tT]([01]\d|2[0-3]):([0-5]\d):([0-5]\d|60)(?:\.\d+)?([zZ]|[+-](?:[01]\d|2[0-3]):[0-5]\d)$/.exec(
      value,
    );
  if (!p) return false;
  const [year, month, day, hour, minute] = p.slice(1, 6).map(Number);
  const leap = year % 4 === 0 && (year % 100 !== 0 || year % 400 === 0);
  const days = [31, leap ? 29 : 28, 31, 30, 31, 30, 31, 31, 30, 31, 30, 31];
  if (day > days[month - 1]) return false;
  if (p[6] !== "60") return true;
  const zone = p[7];
  const offset =
    zone.length === 1
      ? 0
      : (Number(zone.slice(1, 3)) * 60 + Number(zone.slice(4))) *
        (zone[0] === "+" ? 1 : -1);
  const utcMinutes = hour * 60 + minute - offset;
  return (
    (utcMinutes === -1 && day === 1) ||
    (utcMinutes === 1439 && day === days[month - 1])
  );
};
export function compile(schema, Validator, formats) {
  if (
    schema?.$schema !== undefined &&
    schema.$schema !== "https://json-schema.org/draft/2020-12/schema"
  )
    throw new Error("Unsupported contract dialect");
  const meta = new Validator(metaschemas[0], "2020-12", false);
  for (const resource of metaschemas.slice(1)) meta.addSchema(resource);
  if (!meta.validate(schema).valid) throw new Error("Invalid contract schema");
  const supported = {
    "gram-source-uri": uri,
    "gram-source-date-time": timestamp,
  };
  const visit = (node) => {
    if (!node || typeof node !== "object") return;
    if (
      typeof node.format === "string" &&
      !Object.hasOwn(supported, node.format)
    )
      throw new Error("Unsupported contract format");
    for (const keyword of [
      "$defs",
      "definitions",
      "properties",
      "patternProperties",
      "dependentSchemas",
    ])
      for (const child of Object.values(node[keyword] ?? {})) visit(child);
    for (const keyword of ["allOf", "anyOf", "oneOf", "prefixItems"])
      for (const child of node[keyword] ?? []) visit(child);
    for (const keyword of [
      "items",
      "contains",
      "additionalProperties",
      "propertyNames",
      "not",
      "if",
      "then",
      "else",
      "unevaluatedItems",
      "unevaluatedProperties",
    ])
      visit(node[keyword]);
  };
  visit(schema);
  Object.assign(formats, supported);
  return new Validator(schema, "2020-12", false);
}
