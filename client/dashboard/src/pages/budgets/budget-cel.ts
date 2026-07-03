// A small, self-contained CEL-flavored expression layer for the Budgets /
// Spend Control prototype. It mirrors the security CEL authoring experience
// (Monaco editor + reference + live validation) but targets a mocked
// directory-synced *actor* attribute schema instead of the risk message model.
//
// This is prototype scaffolding: the parser/evaluator here is intentionally a
// tiny subset of CEL (boolean predicates over actor attributes) so the mock can
// validate expressions and compute "matched actors" entirely in the browser
// with no backend. The real feature will compile these against the server-side
// CEL infra (see server/internal/risk/celenv) with an actor-shaped env.

/** A directory-synced attribute an actor rule can be written against. */
export interface ActorAttribute {
  name: string;
  type: "string" | "list";
  description: string;
  /** Representative values, surfaced in the editor reference. */
  samples: string[];
}

/**
 * The actor attributes available to spend-control rules. These map to the
 * WorkOS directory-sync attributes Gram already ingests (department_name,
 * job_title, employee_type, division_name, cost_center_name) plus identity
 * dimensions (email, groups, roles). Keep in sync with the telemetry attribute
 * allowlist when this graduates from a mock.
 */
export const ACTOR_ATTRIBUTES: ActorAttribute[] = [
  {
    name: "department_name",
    type: "string",
    description: "Directory department the actor belongs to.",
    samples: ["Engineering", "Data Science", "Design", "Support", "Finance"],
  },
  {
    name: "job_title",
    type: "string",
    description: "Directory job title.",
    samples: ["Software Engineer", "Staff Engineer", "Manager", "Analyst"],
  },
  {
    name: "employee_type",
    type: "string",
    description: "Employment classification.",
    samples: ["full_time", "contractor", "intern"],
  },
  {
    name: "division_name",
    type: "string",
    description: "Directory division / business unit.",
    samples: ["R&D", "Platform", "Go-To-Market"],
  },
  {
    name: "cost_center_name",
    type: "string",
    description: "Finance cost center the actor rolls up to.",
    samples: ["CC-1001", "CC-2043", "CC-3100"],
  },
  {
    name: "email",
    type: "string",
    description: "Actor email address.",
    samples: ["ada@acme.com", "grace@acme.com"],
  },
  {
    name: "groups",
    type: "list",
    description: "IdP / directory group memberships.",
    samples: ["eng-frontier", "ml-team", "interns", "leadership"],
  },
  {
    name: "roles",
    type: "list",
    description: "Gram org roles assigned to the actor.",
    samples: ["admin", "member", "viewer"],
  },
];

const ATTRIBUTE_BY_NAME = new Map(ACTOR_ATTRIBUTES.map((a) => [a.name, a]));

/** String-valued matchers usable via `attr.matcher("...")`. */
export const STRING_MATCHERS = [
  {
    name: "startsWith",
    signature: 'string.startsWith("...")',
    description: "True when the value starts with the given prefix.",
  },
  {
    name: "endsWith",
    signature: 'string.endsWith("...")',
    description: "True when the value ends with the given suffix.",
  },
  {
    name: "contains",
    signature: 'string.contains("...")',
    description: "True when the value contains the given substring.",
  },
  {
    name: "matches",
    signature: 'string.matches("regex")',
    description: "True when the value matches the given RE2/JS regex.",
  },
] as const;

const STRING_MATCHER_NAMES = new Set<string>(
  STRING_MATCHERS.map((m) => m.name),
);

/** Insertable example predicates shown beneath the editor. */
export const BUDGET_CEL_EXAMPLES: { label: string; expr: string }[] = [
  { label: "One department", expr: 'department_name == "Engineering"' },
  {
    label: "Engineers, not managers",
    expr: 'department_name == "Engineering" && job_title != "Manager"',
  },
  { label: "Interns", expr: 'employee_type == "intern"' },
  { label: "In a group", expr: '"ml-team" in groups' },
  {
    label: "Two divisions",
    expr: 'division_name == "R&D" || division_name == "Platform"',
  },
  { label: "By email domain", expr: 'email.endsWith("@acme.com")' },
  { label: "Admins", expr: '"admin" in roles' },
];

/* -------------------------------------------------------------------------- */
/*  Tiny expression engine: tokenizer -> Pratt-ish parser -> evaluator         */
/* -------------------------------------------------------------------------- */

type TokenType =
  | "string"
  | "ident"
  | "op"
  | "lparen"
  | "rparen"
  | "lbracket"
  | "rbracket"
  | "comma"
  | "dot";

interface Token {
  type: TokenType;
  value: string;
  pos: number;
}

class CelError extends Error {}

function tokenize(src: string): Token[] {
  const tokens: Token[] = [];
  let i = 0;
  const n = src.length;
  while (i < n) {
    const c = src[i]!;
    if (/\s/.test(c)) {
      i++;
      continue;
    }
    if (c === '"' || c === "'") {
      const quote = c;
      let j = i + 1;
      let value = "";
      while (j < n && src[j] !== quote) {
        if (src[j] === "\\" && j + 1 < n) {
          value += src[j + 1];
          j += 2;
          continue;
        }
        value += src[j];
        j++;
      }
      if (j >= n) throw new CelError("Unterminated string literal");
      tokens.push({ type: "string", value, pos: i });
      i = j + 1;
      continue;
    }
    if (c === "(") {
      tokens.push({ type: "lparen", value: c, pos: i });
      i++;
      continue;
    }
    if (c === ")") {
      tokens.push({ type: "rparen", value: c, pos: i });
      i++;
      continue;
    }
    if (c === "[") {
      tokens.push({ type: "lbracket", value: c, pos: i });
      i++;
      continue;
    }
    if (c === "]") {
      tokens.push({ type: "rbracket", value: c, pos: i });
      i++;
      continue;
    }
    if (c === ",") {
      tokens.push({ type: "comma", value: c, pos: i });
      i++;
      continue;
    }
    if (c === ".") {
      tokens.push({ type: "dot", value: c, pos: i });
      i++;
      continue;
    }
    const two = src.slice(i, i + 2);
    if (two === "&&" || two === "||" || two === "==" || two === "!=") {
      tokens.push({ type: "op", value: two, pos: i });
      i += 2;
      continue;
    }
    if (c === "!") {
      tokens.push({ type: "op", value: "!", pos: i });
      i++;
      continue;
    }
    if (/[A-Za-z_]/.test(c)) {
      let j = i;
      while (j < n && /[A-Za-z0-9_]/.test(src[j]!)) j++;
      const word = src.slice(i, j);
      if (word === "in") {
        tokens.push({ type: "op", value: "in", pos: i });
      } else {
        tokens.push({ type: "ident", value: word, pos: i });
      }
      i = j;
      continue;
    }
    throw new CelError(`Unexpected character '${c}'`);
  }
  return tokens;
}

type Node =
  | { t: "or"; left: Node; right: Node }
  | { t: "and"; left: Node; right: Node }
  | { t: "not"; expr: Node }
  | { t: "cmp"; op: "==" | "!="; left: Node; right: Node }
  | { t: "in"; left: Node; right: Node }
  | { t: "call"; recv: Node; method: string; args: Node[] }
  | { t: "ident"; name: string }
  | { t: "str"; value: string }
  | { t: "list"; items: Node[] };

class Parser {
  private i = 0;
  private readonly tokens: Token[];
  constructor(tokens: Token[]) {
    this.tokens = tokens;
  }

  private peek(): Token | undefined {
    return this.tokens[this.i];
  }
  private next(): Token {
    const t = this.tokens[this.i];
    if (!t) throw new CelError("Unexpected end of expression");
    this.i++;
    return t;
  }
  private expect(type: TokenType): Token {
    const t = this.next();
    if (t.type !== type) throw new CelError(`Expected ${type}`);
    return t;
  }

  parse(): Node {
    const node = this.parseOr();
    if (this.peek()) throw new CelError("Unexpected trailing input");
    return node;
  }

  private parseOr(): Node {
    let left = this.parseAnd();
    while (this.peek()?.type === "op" && this.peek()?.value === "||") {
      this.next();
      left = { t: "or", left, right: this.parseAnd() };
    }
    return left;
  }
  private parseAnd(): Node {
    let left = this.parseNot();
    while (this.peek()?.type === "op" && this.peek()?.value === "&&") {
      this.next();
      left = { t: "and", left, right: this.parseNot() };
    }
    return left;
  }
  private parseNot(): Node {
    if (this.peek()?.type === "op" && this.peek()?.value === "!") {
      this.next();
      return { t: "not", expr: this.parseNot() };
    }
    return this.parseComparison();
  }
  private parseComparison(): Node {
    const left = this.parsePostfix();
    const op = this.peek();
    if (op?.type === "op" && (op.value === "==" || op.value === "!=")) {
      this.next();
      return {
        t: "cmp",
        op: op.value,
        left,
        right: this.parsePostfix(),
      };
    }
    if (op?.type === "op" && op.value === "in") {
      this.next();
      return { t: "in", left, right: this.parsePostfix() };
    }
    return left;
  }
  private parsePostfix(): Node {
    let node = this.parseAtom();
    while (this.peek()?.type === "dot") {
      this.next();
      const method = this.expect("ident").value;
      this.expect("lparen");
      const args: Node[] = [];
      if (this.peek()?.type !== "rparen") {
        args.push(this.parseOr());
        while (this.peek()?.type === "comma") {
          this.next();
          args.push(this.parseOr());
        }
      }
      this.expect("rparen");
      node = { t: "call", recv: node, method, args };
    }
    return node;
  }
  private parseAtom(): Node {
    const t = this.next();
    if (t.type === "string") return { t: "str", value: t.value };
    if (t.type === "ident") return { t: "ident", name: t.value };
    if (t.type === "lparen") {
      const inner = this.parseOr();
      this.expect("rparen");
      return inner;
    }
    if (t.type === "lbracket") {
      const items: Node[] = [];
      if (this.peek()?.type !== "rbracket") {
        items.push(this.parseOr());
        while (this.peek()?.type === "comma") {
          this.next();
          items.push(this.parseOr());
        }
      }
      this.expect("rbracket");
      return { t: "list", items };
    }
    throw new CelError(`Unexpected token '${t.value}'`);
  }
}

/** A directory actor the mock evaluates rules against. The index type
 *  admits `number` so richer mock actor records (e.g. with a spend field) are
 *  assignable; the evaluator only reads string/list attributes. */
export type ActorRecord = { [key: string]: string | string[] | number };

type Value =
  | { kind: "string"; v: string }
  | { kind: "list"; v: string[] }
  | { kind: "bool"; v: boolean };

function evalNode(node: Node, actor: ActorRecord): Value {
  switch (node.t) {
    case "str":
      return { kind: "string", v: node.value };
    case "list":
      return {
        kind: "list",
        v: node.items.map((item) => {
          const val = evalNode(item, actor);
          if (val.kind !== "string")
            throw new CelError("List items must be strings");
          return val.v;
        }),
      };
    case "ident": {
      const attr = ATTRIBUTE_BY_NAME.get(node.name);
      if (!attr) throw new CelError(`Unknown attribute '${node.name}'`);
      const raw = actor[node.name];
      if (attr.type === "list") {
        return { kind: "list", v: Array.isArray(raw) ? raw : [] };
      }
      return { kind: "string", v: typeof raw === "string" ? raw : "" };
    }
    case "not": {
      const inner = evalNode(node.expr, actor);
      if (inner.kind !== "bool")
        throw new CelError("'!' expects a true/false value");
      return { kind: "bool", v: !inner.v };
    }
    case "and":
    case "or": {
      const l = evalNode(node.left, actor);
      const r = evalNode(node.right, actor);
      if (l.kind !== "bool" || r.kind !== "bool")
        throw new CelError(
          `'${node.t === "and" ? "&&" : "||"}' expects true/false values`,
        );
      return { kind: "bool", v: node.t === "and" ? l.v && r.v : l.v || r.v };
    }
    case "cmp": {
      const l = evalNode(node.left, actor);
      const r = evalNode(node.right, actor);
      if (l.kind !== "string" || r.kind !== "string")
        throw new CelError("Comparisons expect string values");
      return { kind: "bool", v: node.op === "==" ? l.v === r.v : l.v !== r.v };
    }
    case "in": {
      const l = evalNode(node.left, actor);
      const r = evalNode(node.right, actor);
      if (l.kind !== "string")
        throw new CelError("Left side of 'in' must be a string");
      if (r.kind !== "list")
        throw new CelError("Right side of 'in' must be a list");
      return { kind: "bool", v: r.v.includes(l.v) };
    }
    case "call": {
      if (!STRING_MATCHER_NAMES.has(node.method))
        throw new CelError(`Unknown matcher '${node.method}'`);
      const recv = evalNode(node.recv, actor);
      if (recv.kind !== "string")
        throw new CelError(`'${node.method}' can only be called on a string`);
      if (node.args.length !== 1)
        throw new CelError(`'${node.method}' takes exactly one argument`);
      const argNode = node.args[0]!;
      const arg = evalNode(argNode, actor);
      if (arg.kind !== "string")
        throw new CelError(`'${node.method}' expects a string argument`);
      switch (node.method) {
        case "startsWith":
          return { kind: "bool", v: recv.v.startsWith(arg.v) };
        case "endsWith":
          return { kind: "bool", v: recv.v.endsWith(arg.v) };
        case "contains":
          return { kind: "bool", v: recv.v.includes(arg.v) };
        case "matches":
          try {
            return { kind: "bool", v: new RegExp(arg.v).test(recv.v) };
          } catch {
            throw new CelError("Invalid regex in matches()");
          }
        default:
          throw new CelError(`Unknown matcher '${node.method}'`);
      }
    }
  }
}

function parse(expr: string): Node {
  return new Parser(tokenize(expr)).parse();
}

/** A representative actor with every attribute present, used to type-check. */
function sampleActor(): ActorRecord {
  const actor: ActorRecord = {};
  for (const attr of ACTOR_ATTRIBUTES) {
    actor[attr.name] =
      attr.type === "list" ? [...attr.samples] : (attr.samples[0] ?? "");
  }
  return actor;
}

/**
 * Validate a rule target expression. Returns null when it parses and
 * evaluates to a boolean, otherwise a human-readable error message. Mirrors the
 * client-side compile-status the security CEL editor shows.
 */
export function validateBudgetCel(expr: string): string | null {
  const trimmed = expr.trim();
  if (!trimmed) return "Add an expression";
  let ast: Node;
  try {
    ast = parse(trimmed);
  } catch (err) {
    return err instanceof Error ? err.message : "Invalid expression";
  }
  try {
    const result = evalNode(ast, sampleActor());
    if (result.kind !== "bool")
      return "Expression must evaluate to true or false";
  } catch (err) {
    return err instanceof Error ? err.message : "Invalid expression";
  }
  return null;
}

/**
 * Evaluate a target expression against a set of actors, returning the ones it
 * matches. Returns null when the expression can't be evaluated (so callers can
 * show a "can't preview" state rather than an empty match set).
 */
export function matchActors<T extends ActorRecord>(
  expr: string,
  actors: T[],
): T[] | null {
  const trimmed = expr.trim();
  if (!trimmed) return null;
  let ast: Node;
  try {
    ast = parse(trimmed);
  } catch {
    return null;
  }
  const matched: T[] = [];
  for (const actor of actors) {
    try {
      const result = evalNode(ast, actor);
      if (result.kind === "bool" && result.v) matched.push(actor);
    } catch {
      return null;
    }
  }
  return matched;
}
