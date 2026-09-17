import {
  mappingKey,
  statusLabels,
  type Draft,
  type Fact,
  type Mapping,
} from "./model";

export type ImportCatalog = {
  methods: { id: string; name: string }[];
  products: { id: string; name: string }[];
  capabilities: { id: string; name: string }[];
};

const columns = [
  "type",
  "method_id",
  "platform_id",
  "capability_id",
  "status",
  "note",
  "verify",
  "applicability",
  "conditions",
];
export const importCsvHeader = columns.join(",");
export const maxImportBytes = 2 * 1024 * 1024;

export function importPrompt(catalog: ImportCatalog): string {
  const list = (items: { id: string; name: string }[]) =>
    items.map((item) => `${item.id}: ${item.name}`).join("\n");
  return `Convert the support matrix I provide into a UTF-8 CSV file for import. Return only the CSV, with no Markdown fences or commentary.

Use this exact header:
${importCsvHeader}

Each row has one of three types:
- reference: a method-level capability claim. Fill method_id, capability_id, status, note, verify. Leave platform_id, applicability, conditions empty.
- mapping: whether a method applies to a platform. Fill method_id, platform_id, applicability, conditions. Leave capability_id, status, note, verify empty.
- coverage: an EXPLICIT capability claim for a particular method AND platform. Fill method_id, platform_id, capability_id, status, note, verify. Leave applicability and conditions empty. Include an applicable mapping row for it.

status must be supported, partial, unimplemented, impossible, na, or unknown. Map ✅ to supported, ❌ to unimplemented, ☠️ to impossible, -- to na, and ? or blank to unknown. WIP means partial and needs verification. Partial requires a note. verify must be true or false; set true for VERIFY, WIP, uncertain or ambiguous claims. Keep qualifications and original wording in note.

applicability must be applicable, na, or unknown. Use applicable for ✅, na for -- or a definite ☠️, and unknown for ❌, ?, blanks, or uncertain/VERIFY negatives. For mapping rows, preserve the original symbol and all qualifiers in conditions (including WIP, VERIFY, cost only, no hooks). Preserve plan eligibility and OS restrictions in the method's mapping conditions. The app derives platform-feature coverage from reference claims whenever a mapping is applicable. Do not emit redundant coverage rows for that combination. Explicit coverage rows override the derived result, including unknown overrides. Use the phrases cost only, session tracking only, no hooks, VERIFY, and WIP in conditions when the source states these restrictions; the app uses them to qualify derived coverage. Keep plan and OS restrictions in conditions too. For an explicit platform-specific exception not represented by these qualifiers, emit a coverage row with its status, note, and verify flag.

Use ONLY the IDs below. Do not create new IDs. If an item cannot be matched, ask me to resolve it before producing the file. Include each reference, mapping, or coverage key at most once. Omitted entries stay unchanged in the database; included entries replace their matching values. Do not include rows from examples unless present in my source.

Quote fields containing commas, double quotes, or newlines; escape a double quote as two double quotes. Always include all nine columns, including empty trailing fields. Do not use spreadsheet formulas.

Methods:
${list(catalog.methods)}

Platforms:
${list(catalog.products)}

Capabilities:
${list(catalog.capabilities)}

Now convert the source matrix I provide below:
`;
}

/** Read quoted CSV fields, including embedded newlines and escaped quotes. */
function readCsv(text: string): string[][] {
  const rows: string[][] = [];
  let row: string[] = [];
  let field = "";
  let quoted = false;
  let closed = false;
  const source = text.replace(/^\uFEFF/, "");
  for (let i = 0; i < source.length; i++) {
    const char = source[i]!;
    if (quoted) {
      if (char !== '"') field += char;
      else if (source[i + 1] === '"') {
        field += '"';
        i++;
      } else {
        quoted = false;
        closed = true;
      }
    } else if (char === "," || char === "\n" || char === "\r") {
      row.push(field);
      field = "";
      closed = false;
      if (char !== ",") {
        if (char === "\r" && source[i + 1] === "\n") i++;
        if (row.some((cell) => cell.trim())) rows.push(row);
        row = [];
      }
    } else if (char === '"' && !field && !closed) {
      quoted = true;
    } else {
      if (closed || char === '"')
        throw new Error(`Invalid CSV quoting near record ${rows.length + 1}.`);
      field += char;
    }
  }
  if (quoted) throw new Error("CSV contains an unclosed quoted field.");
  row.push(field);
  if (row.some((cell) => cell.trim())) rows.push(row);
  return rows;
}

export type CsvImport = {
  draft: Draft;
  counts: { reference: number; mapping: number; coverage: number };
};

export function parseMatrixImport(
  text: string,
  catalog: ImportCatalog,
  current: Draft,
): CsvImport {
  if (new TextEncoder().encode(text).length > maxImportBytes)
    throw new Error("CSV must be 2 MB or smaller.");
  const [header, ...rows] = readCsv(text);
  if (
    !header ||
    header.map((cell) => cell.trim()).join(",") !== importCsvHeader
  )
    throw new Error(`Expected header: ${importCsvHeader}`);
  if (!rows.length) throw new Error("CSV has no data rows.");
  const methods = new Set(catalog.methods.map((item) => item.id));
  const platforms = new Set(catalog.products.map((item) => item.id));
  const capabilities = new Set(catalog.capabilities.map((item) => item.id));
  const draft = structuredClone(current);
  const seen = new Set<string>();
  const counts = { reference: 0, mapping: 0, coverage: 0 };
  const coverageMappings = new Set<string>();
  rows.forEach((row, index) => {
    const fail = (message: string): never => {
      throw new Error(`Record ${index + 2}: ${message}`);
    };
    if (row.length !== columns.length)
      fail(`expected ${columns.length} columns, found ${row.length}.`);
    const [
      type,
      method,
      platform,
      capability,
      status,
      note,
      verify,
      applicability,
      conditions,
    ] = row.map((cell, i) => (i === 5 || i === 8 ? cell : cell.trim())) as [
      string,
      string,
      string,
      string,
      string,
      string,
      string,
      string,
      string,
    ];
    if (!methods.has(method)) fail(`unknown method_id "${method}".`);
    if (!["reference", "mapping", "coverage"].includes(type))
      fail(`unknown type "${type}".`);
    const key = JSON.stringify([type, method, platform, capability]);
    if (seen.has(key)) fail("duplicate entry; include each key only once.");
    seen.add(key);
    const mapKey = mappingKey(method, platform);
    if (type !== "reference" && !platforms.has(platform))
      fail(`unknown platform_id "${platform}".`);
    if (type === "mapping") {
      if (capability || status || note || verify)
        fail(
          "mapping rows must leave capability_id, status, note, and verify empty.",
        );
      if (!["applicable", "na", "unknown"].includes(applicability))
        fail("applicability must be applicable, na, or unknown.");
      if (new TextEncoder().encode(conditions).length > 10000)
        fail("conditions exceed 10000 bytes.");
      draft.mappings[mapKey] = {
        ...draft.mappings[mapKey],
        applicability: applicability as Mapping["applicability"],
        conditions,
        facts: draft.mappings[mapKey]?.facts ?? {},
      };
      counts.mapping++;
      return;
    }
    if (applicability || conditions)
      fail(
        "reference and coverage rows must leave applicability and conditions empty.",
      );
    if (type === "reference" && platform)
      fail("reference rows must leave platform_id empty.");
    if (!capabilities.has(capability))
      fail(`unknown capability_id "${capability}".`);
    if (!Object.hasOwn(statusLabels, status))
      fail(`unknown status "${status}".`);
    if (verify !== "true" && verify !== "false")
      fail("verify must be true or false.");
    if (status === "partial" && !note.trim())
      fail("partial coverage requires a note.");
    if (new TextEncoder().encode(note).length > 10000)
      fail("note exceeds 10000 bytes.");
    const fact: Fact = {
      status: status as Fact["status"],
      note,
      verify: verify === "true",
    };
    if (type === "reference") {
      draft.references[method] = {
        ...draft.references[method],
        [capability]: fact,
      };
      counts.reference++;
    } else {
      draft.mappings[mapKey] ??= {
        applicability: "unknown",
        conditions: "",
        facts: {},
      };
      draft.mappings[mapKey].facts[capability] = fact;
      coverageMappings.add(mapKey);
      counts.coverage++;
    }
  });
  for (const key of coverageMappings) {
    if (draft.mappings[key]?.applicability !== "applicable")
      throw new Error(
        `Coverage for ${key} requires an applicable mapping. Add a mapping row or remove the coverage rows.`,
      );
  }
  return { draft, counts };
}
