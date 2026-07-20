// Shared CSV plumbing for the cost explorer's exports. The dimension table
// (EntityProfile) and the session list (SessionTable) each own their column
// shape but serialize and download through here, so the formula-injection guard
// below can never be forgotten by one of them.

// A cell whose first meaningful character is one of these is evaluated as a
// formula by Excel/Sheets (CWE-1236). Importers skip leading whitespace before
// deciding, so the payload " =SUM(A1:A10)" is still live — match past it rather
// than only at position 0.
const FORMULA_LEAD = /^\s*[=+\-@]/;
// Tab, CR and LF are themselves formula triggers in OWASP's guidance, so a value
// leading with one is neutralized even without a following =/+/-/@.
const CONTROL_LEAD = /^[\t\r\n]/;

// Serialize one CSV field. Two concerns:
//   1. Formula injection (CWE-1236). Directory-sync values (names, emails) are
//      attacker-influenced, so a cell that would evaluate gets a leading
//      apostrophe. Numbers are exempt: they can't be formulas, and guarding
//      them would corrupt a negative value into the text "'-5".
//   2. RFC 4180 quoting: wrap in quotes (doubling internal quotes) when the
//      value contains a comma, quote, CR or LF.
function csvField(value: string | number): string {
  if (typeof value === "number") return String(value);
  let s = value;
  if (FORMULA_LEAD.test(s) || CONTROL_LEAD.test(s)) s = `'${s}`;
  return /["\r\n,]/.test(s) ? `"${s.replace(/"/g, '""')}"` : s;
}

/**
 * Serialize a header row plus body rows into an RFC 4180 CSV string. Records are
 * CRLF-separated per the spec — Excel and Sheets accept either, but strict
 * parsers don't.
 */
export function toCsv(header: string[], body: (string | number)[][]): string {
  return [header, ...body]
    .map((cols) => cols.map(csvField).join(","))
    .join("\r\n");
}

/** Trigger a client-side download of a CSV string. */
export function downloadCsv(filename: string, csv: string): void {
  const blob = new Blob([csv], { type: "text/csv;charset=utf-8;" });
  const url = URL.createObjectURL(blob);
  const link = document.createElement("a");
  link.href = url;
  link.download = filename;
  link.click();
  URL.revokeObjectURL(url);
}

/** Filename-safe slug for an entity/range label. */
export function slugify(value: string): string {
  return (
    value
      .toLowerCase()
      .replace(/[^a-z0-9]+/g, "-")
      .replace(/(^-|-$)/g, "") || "all-costs"
  );
}
