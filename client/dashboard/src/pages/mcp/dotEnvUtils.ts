export interface DotEnvEntry {
  key: string;
  value: string;
}

export interface DotEnvParseResult {
  entries: DotEnvEntry[];
  invalidLineNumbers: number[];
}

const DOT_ENV_ASSIGNMENT =
  /^(?:export\s+)?([A-Za-z_][A-Za-z0-9_.-]*)\s*=\s*(.*)$/;
const QUOTED_VALUE = /^(["'])(.*)\1(?:\s*#.*)?$/;

/**
 * Parses the common, single-line subset of dotenv files used for environment
 * variable forms. Comments and blank lines are ignored; malformed lines are
 * reported so callers do not silently drop pasted content.
 */
export function parseDotEnv(contents: string): DotEnvParseResult {
  const entries: DotEnvEntry[] = [];
  const invalidLineNumbers: number[] = [];

  for (const [index, originalLine] of contents.split(/\r?\n/).entries()) {
    const line = originalLine.replace(/^\uFEFF/, "").trim();
    if (line === "" || line.startsWith("#")) continue;

    const assignment = line.match(DOT_ENV_ASSIGNMENT);
    if (!assignment) {
      invalidLineNumbers.push(index + 1);
      continue;
    }

    const [, key, rawValue] = assignment;
    const value = parseDotEnvValue(rawValue);
    if (value === null) {
      invalidLineNumbers.push(index + 1);
      continue;
    }

    entries.push({ key, value });
  }

  return { entries, invalidLineNumbers };
}

function parseDotEnvValue(rawValue: string): string | null {
  const trimmed = rawValue.trim();
  if (trimmed === "") return "";

  if (trimmed.startsWith('"') || trimmed.startsWith("'")) {
    const quoted = trimmed.match(QUOTED_VALUE);
    if (!quoted) return null;

    const [, quote, value] = quoted;
    return quote === '"'
      ? value.replaceAll("\\n", "\n").replaceAll("\\r", "\r")
      : value;
  }

  return trimmed.replace(/\s+#.*$/, "").trim();
}
