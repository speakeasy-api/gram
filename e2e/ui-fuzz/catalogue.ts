import fs from "node:fs";
import path from "node:path";

// The page catalogue, read from client/dashboard/src/routes.tsx so the fuzz
// run covers whatever the dashboard ships without a hand-kept list.
//
// routes.tsx is a nested object literal that imports every page component,
// so it cannot be imported from here. Instead this walks its text with a
// tokenizer that understands strings, comments, and braces, and treats
// `key: {` as a route entry when it sits directly inside a route table and
// `subPages: {` as a nested table. Anything else in braces (arrow functions,
// JSX attributes) is skipped by depth. The unit test checks the result
// against routes the dashboard is known to have.

export type CatalogueRoute = {
  // Full path with `:orgSlug` and `:projectSlug` placeholders, e.g.
  // "/:orgSlug/projects/:projectSlug/mcp".
  path: string;
  // Dotted key path in the table, e.g. "mcp.add.remote".
  key: string;
  title: string;
  unauthenticated: boolean;
  external: boolean;
  legacyRedirect: boolean;
};

export const PROJECT_PARENT = "/:orgSlug/projects/:projectSlug";
export const ORG_PARENT = "/:orgSlug";

type Frame =
  | { kind: "routes"; parent: string; keyPrefix: string }
  | { kind: "entry"; node: Node; parent: string; keyPrefix: string }
  | { kind: "other" };

type Node = {
  key: string;
  url?: string;
  title?: string;
  flags: Set<string>;
};

type Token =
  | { kind: "open" }
  | { kind: "close" }
  | { kind: "string"; value: string }
  | { kind: "key"; name: string }
  | { kind: "word"; value: string };

// Yields the tokens the walker cares about and nothing else. Strings are
// returned whole; comments, regex-free punctuation and JSX are dropped.
export function* tokenize(src: string): Generator<Token> {
  let i = 0;
  const n = src.length;
  while (i < n) {
    const c = src[i]!;
    if (c === "/" && src[i + 1] === "/") {
      const end = src.indexOf("\n", i);
      i = end === -1 ? n : end;
      continue;
    }
    if (c === "/" && src[i + 1] === "*") {
      const end = src.indexOf("*/", i + 2);
      i = end === -1 ? n : end + 2;
      continue;
    }
    if (c === '"' || c === "'" || c === "`") {
      let j = i + 1;
      let value = "";
      while (j < n && src[j] !== c) {
        if (src[j] === "\\") {
          value += src[j + 1] ?? "";
          j += 2;
          continue;
        }
        value += src[j];
        j += 1;
      }
      yield { kind: "string", value };
      i = j + 1;
      continue;
    }
    if (c === "{") {
      yield { kind: "open" };
      i += 1;
      continue;
    }
    if (c === "}") {
      yield { kind: "close" };
      i += 1;
      continue;
    }
    if (/[A-Za-z_$]/.test(c)) {
      let j = i;
      while (j < n && /[A-Za-z0-9_$]/.test(src[j]!)) j += 1;
      const word = src.slice(i, j);
      let k = j;
      while (k < n && (src[k] === " " || src[k] === "\t")) k += 1;
      if (src[k] === ":") {
        yield { kind: "key", name: word };
        i = k + 1;
      } else {
        yield { kind: "word", value: word };
        i = j;
      }
      continue;
    }
    i += 1;
  }
}

const FLAG_KEYS = new Set(["unauthenticated", "external", "legacyRedirect"]);

// Walks one route table. `src` is positioned so that the first `{` opens
// the table.
function walkTable(
  tokens: Token[],
  start: number,
  parent: string,
  keyPrefix: string,
): CatalogueRoute[] {
  const out: CatalogueRoute[] = [];
  const stack: Frame[] = [];
  let pendingKey: string | undefined;
  let pendingFlag: string | undefined;
  let i = start;
  for (; i < tokens.length; i += 1) {
    const t = tokens[i]!;
    const top = stack[stack.length - 1];
    if (t.kind === "key") {
      pendingKey = t.name;
      pendingFlag =
        FLAG_KEYS.has(t.name) && top?.kind === "entry" ? t.name : undefined;
      continue;
    }
    if (t.kind === "open") {
      if (stack.length === 0) {
        stack.push({ kind: "routes", parent, keyPrefix });
      } else if (top?.kind === "routes" && pendingKey) {
        stack.push({
          kind: "entry",
          node: { key: pendingKey, flags: new Set() },
          parent: top.parent,
          keyPrefix: top.keyPrefix,
        });
      } else if (top?.kind === "entry" && pendingKey === "subPages") {
        const own = entryPath(top);
        stack.push({
          kind: "routes",
          parent: own,
          keyPrefix: `${top.keyPrefix}${top.node.key}.`,
        });
      } else {
        stack.push({ kind: "other" });
      }
      pendingKey = undefined;
      pendingFlag = undefined;
      continue;
    }
    if (t.kind === "close") {
      const frame = stack.pop();
      if (frame?.kind === "entry") {
        if (frame.node.url !== undefined) {
          out.push({
            path: entryPath(frame),
            key: `${frame.keyPrefix}${frame.node.key}`,
            title: frame.node.title ?? frame.node.key,
            unauthenticated: frame.node.flags.has("unauthenticated"),
            external: frame.node.flags.has("external"),
            legacyRedirect: frame.node.flags.has("legacyRedirect"),
          });
        }
      }
      if (stack.length === 0) break;
      pendingKey = undefined;
      pendingFlag = undefined;
      continue;
    }
    if (t.kind === "string" && top?.kind === "entry") {
      if (pendingKey === "url") top.node.url = t.value;
      if (pendingKey === "title") top.node.title = t.value;
      pendingKey = undefined;
      continue;
    }
    if (
      t.kind === "word" &&
      top?.kind === "entry" &&
      pendingFlag &&
      t.value === "true"
    ) {
      top.node.flags.add(pendingFlag);
      pendingFlag = undefined;
      pendingKey = undefined;
    }
  }
  return out;
}

function entryPath(frame: Extract<Frame, { kind: "entry" }>): string {
  const url = frame.node.url ?? "";
  if (url.startsWith("/")) return url;
  if (url === "") return frame.parent;
  return `${frame.parent}/${url}`;
}

function findTable(tokens: Token[], name: string): number {
  for (let i = 0; i < tokens.length - 1; i += 1) {
    const t = tokens[i]!;
    if (
      t.kind === "word" &&
      t.value === name &&
      tokens[i + 1]?.kind === "open"
    ) {
      return i + 1;
    }
  }
  throw new Error(`routes.tsx: no \`${name} = {\` found`);
}

export function parseCatalogue(src: string): CatalogueRoute[] {
  const tokens = [...tokenize(src)];
  return [
    ...walkTable(
      tokens,
      findTable(tokens, "ROUTE_STRUCTURE"),
      PROJECT_PARENT,
      "",
    ),
    // Org-level keys get a prefix so `home` here and `home` above stay apart.
    ...walkTable(
      tokens,
      findTable(tokens, "ORG_ROUTE_STRUCTURE"),
      ORG_PARENT,
      "org.",
    ),
  ];
}

export const ROUTES_FILE = path.resolve(
  import.meta.dirname,
  "..",
  "..",
  "client",
  "dashboard",
  "src",
  "routes.tsx",
);

export function loadCatalogue(file = ROUTES_FILE): CatalogueRoute[] {
  return parseCatalogue(fs.readFileSync(file, "utf8"));
}

// Phase 1: routes that can be visited with nothing but an org and a project
// slug. Parameterised and splat routes need an instance and come later;
// unauthenticated, external and redirect-only entries are not screens.
export function staticRoutes(routes: CatalogueRoute[]): CatalogueRoute[] {
  return routes.filter((r) => {
    if (r.unauthenticated || r.external || r.legacyRedirect) return false;
    const rest = r.path.replace(PROJECT_PARENT, "").replace(ORG_PARENT, "");
    return !rest.includes(":") && !rest.includes("*");
  });
}

export function resolvePath(
  route: CatalogueRoute,
  orgSlug: string,
  projectSlug: string,
): string {
  return route.path
    .replace(":orgSlug", orgSlug)
    .replace(":projectSlug", projectSlug);
}
