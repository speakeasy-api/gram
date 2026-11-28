import { readCsv } from "@/lib/csv";

// The server's bound on one bulk disable (constants.MaxBulkDisableOrganizations).
// Checked here too, so an oversized file is refused before anything is sent.
export const MAX_BULK_DISABLE_ORGANIZATIONS = 1000;

// Far above a thousand ids or slugs, and small enough to read in one go.
export const MAX_BULK_DISABLE_CSV_BYTES = 1024 * 1024;

export type BulkDisableTargets = {
  ids: string[];
  slugs: string[];
};

// Headers are compared after lowercasing and dropping spaces, dashes and
// underscores, so "Org ID", "organization_id" and "id" all name the id column.
const ID_HEADERS = new Set(["id", "orgid", "organizationid"]);
const SLUG_HEADERS = new Set(["slug", "orgslug", "organizationslug"]);

function normalizeHeader(cell: string): string {
  return cell
    .trim()
    .toLowerCase()
    .replace(/[\s_-]+/g, "");
}

function findColumn(
  header: string[],
  names: Set<string>,
  label: string,
): number {
  const matches = header.flatMap((cell, index) =>
    names.has(normalizeHeader(cell)) ? [index] : [],
  );
  if (matches.length > 1) {
    throw new Error(`CSV has more than one ${label} column. Keep one.`);
  }
  return matches[0] ?? -1;
}

function dedupe(values: string[]): string[] {
  return [...new Set(values)];
}

/**
 * Reads the organizations a bulk disable names. The file needs an id column, a
 * slug column, or both; other columns are ignored, so an export of the
 * organizations list can be uploaded as it is. Each row names one
 * organization: by its id when the row has one, by its slug otherwise.
 *
 * Throws with a message the operator can act on when the file cannot be used.
 */
export function parseBulkDisableCsv(text: string): BulkDisableTargets {
  const [header, ...rows] = readCsv(text);
  if (!header) throw new Error("CSV is empty.");

  const idColumn = findColumn(header, ID_HEADERS, "id");
  const slugColumn = findColumn(header, SLUG_HEADERS, "slug");
  if (idColumn === -1 && slugColumn === -1) {
    const found = header.map((cell) => cell.trim()).filter(Boolean);
    throw new Error(
      `CSV needs an "id" or "slug" column in its first row. Found: ${found.length ? found.join(", ") : "no column names"}.`,
    );
  }
  if (!rows.length) throw new Error("CSV has no data rows.");

  const ids: string[] = [];
  const slugs: string[] = [];
  rows.forEach((row, index) => {
    const id = idColumn === -1 ? "" : (row[idColumn] ?? "").trim();
    const slug = slugColumn === -1 ? "" : (row[slugColumn] ?? "").trim();
    if (id) ids.push(id);
    else if (slug) slugs.push(slug);
    else {
      // Row numbers count the header as row 1, as a spreadsheet does.
      const column =
        idColumn !== -1 && slugColumn !== -1
          ? "an id or a slug"
          : idColumn !== -1
            ? "an id"
            : "a slug";
      throw new Error(`Row ${index + 2} has no value for ${column}.`);
    }
  });

  const targets = { ids: dedupe(ids), slugs: dedupe(slugs) };
  const count = targets.ids.length + targets.slugs.length;
  if (count > MAX_BULK_DISABLE_ORGANIZATIONS) {
    throw new Error(
      `CSV names ${count} organizations. At most ${MAX_BULK_DISABLE_ORGANIZATIONS} can be disabled at once; split the file.`,
    );
  }
  return targets;
}
