// Shared CSV plumbing for the cost explorer's exports. The dimension table
// (EntityProfile) and the session list (SessionTable) each own their column
// shape but serialize and download through here, so the formula-injection guard
// below can never be forgotten by one of them.

// Serialize one CSV field. Two concerns:
//   1. Formula injection (CWE-1236): a cell starting with = + - @ (or a control
//      char) is treated as a formula by Excel/Sheets. Directory-sync values
//      (names, emails) are attacker-influenced, so neutralize with a leading
//      apostrophe before quoting.
//   2. RFC 4180 quoting: wrap in quotes (doubling internal quotes) when the
//      value contains a comma, quote, or newline.
function csvField(value: string | number): string {
  let s = String(value);
  if (/^[=+\-@\t\r]/.test(s)) s = `'${s}`;
  return /[",\n]/.test(s) ? `"${s.replace(/"/g, '""')}"` : s;
}

/** Serialize a header row plus body rows into an RFC 4180 CSV string. */
export function toCsv(header: string[], body: (string | number)[][]): string {
  return [header, ...body]
    .map((cols) => cols.map(csvField).join(","))
    .join("\n");
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
