export type UserSearchTerm = {
  field: "any" | "name" | "email" | "org";
  value: string;
  start: number;
  end: number;
};

export type ParsedUserSearch =
  | { ok: true; terms: UserSearchTerm[] }
  | { ok: false; message: string; start: number; end: number };

// Unicode White_Space matches Go's unicode.IsSpace (unlike JavaScript's \s).
const whitespace = /\p{White_Space}/u;
const unsupported = (value: string) =>
  /^(OR|NOT)$/i.test(value) ||
  /[()]/.test(value) ||
  /^[!-]/.test(value) ||
  (value.startsWith("/") && value.endsWith("/"));

export function parseUserSearch(query: string): ParsedUserSearch {
  if (new TextEncoder().encode(query).length > 2048) {
    return {
      ok: false,
      message: "search must be at most 2048 UTF-8 bytes",
      start: 0,
      end: query.length,
    };
  }
  const terms: UserSearchTerm[] = [];
  let i = 0;
  while (i < query.length) {
    if (whitespace.test(query[i]!)) {
      i++;
      continue;
    }
    const start = i;
    const error = (message: string): ParsedUserSearch => {
      let end = i;
      while (end < query.length && !whitespace.test(query[end]!)) end++;
      return { ok: false, message, start, end };
    };
    if (terms.length === 20) return error("search supports at most 20 terms");
    let field: UserSearchTerm["field"] = "any";
    while (
      i < query.length &&
      !whitespace.test(query[i]!) &&
      query[i] !== '"' &&
      query[i] !== ":"
    )
      i++;
    if (query[i] === ":") {
      const prefix = query.slice(start, i).toLowerCase();
      if (/[()]/.test(prefix) || /^[!-]/.test(prefix))
        return error(
          "unsupported search syntax; quote it to search for literal text",
        );
      if (prefix !== "name" && prefix !== "email" && prefix !== "org") {
        return error(
          "unknown search field; supported fields are name, email, and org",
        );
      }
      field = prefix;
      i++;
    } else i = start;
    let value = "";
    const quoted = query[i] === '"';
    if (quoted) {
      i++;
      while (i < query.length && query[i] !== '"') {
        if (
          query[i] === "\\" &&
          (query[i + 1] === "\\" || query[i + 1] === '"')
        )
          i++;
        value += query[i++];
      }
      if (i === query.length) return error("unclosed quote in search term");
      i++;
      if (i < query.length && !whitespace.test(query[i]!))
        return error(
          "separate search terms with whitespace; quote the whole value",
        );
    } else {
      while (i < query.length && !whitespace.test(query[i]!)) {
        if (query[i] === '"')
          return error(
            "separate search terms with whitespace; quote the whole value",
          );
        value += query[i++];
      }
    }
    if (value === "") return error("search terms must have a value");
    if (!quoted && unsupported(value))
      return error(
        "unsupported search syntax; quote it to search for literal text",
      );
    if ([...value].length > 256)
      return error("search terms must be at most 256 Unicode code points");
    terms.push({ field, value, start, end: i });
  }
  return { ok: true, terms };
}

export function serializeUserSearch(
  terms: Pick<UserSearchTerm, "field" | "value">[],
): string {
  return terms
    .map(({ field, value }) => {
      const quote =
        whitespace.test(value) ||
        /"/.test(value) ||
        unsupported(value) ||
        (field === "any" && value.includes(":"));
      // Unknown escapes stay literal; protect only quotes, paired slashes, and
      // a terminal slash that would otherwise escape the closing quote.
      const encoded = quote
        ? '"' + value.replace(/"|\\(?=["\\]|$)/g, "\\$&") + '"'
        : value;
      return (field === "any" ? "" : field + ":") + encoded;
    })
    .join(" ");
}
