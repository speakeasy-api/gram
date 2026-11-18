import { CLIENT_TYPES, clientTypeLabel } from "@/lib/auth-clients";

/**
 * Who may use which upstream credential, as rules a role, agent or person
 * holds. The backend calls a credential an auth client and these rules
 * mcp:auth / mcp:blocked_auth grants.
 *
 * Rules sit on three tiers: every credential, every credential of one kind,
 * and one credential. A rule on a wider tier reaches everything under it, now
 * and later. Anything that is none of those (a rule confined to a project, to
 * one upstream OAuth client, or to someone else's connection) is kept exactly
 * as it is and listed on its own.
 */

export const REPRESENTED_USER = "$represented_user";

export type CredentialScope = "mcp:auth" | "mcp:blocked_auth";

/** The selector fields a credential rule can carry. */
type CredentialSelector = {
  resourceKind: string;
  resourceId: string;
  clientType?: string | undefined;
  connectionOwner?: string | undefined;
  upstreamClient?: string | undefined;
  projectId?: string | undefined;
  tool?: string | undefined;
  disposition?: string | undefined;
  serverUrl?: string | undefined;
};

export type CredentialRule = {
  scope: CredentialScope;
  selector: CredentialSelector;
};

/** The parts of a credential the rules need. */
export type CredentialRef = {
  id: string;
  name: string;
  clientType: string;
  enabled: boolean;
  remoteSessionClientId?: string | undefined;
  servers: {
    id: string;
    name?: string | undefined;
    slug?: string | undefined;
    projectId: string;
  }[];
};

export type CredentialRow =
  | { kind: "all" }
  | { kind: "type"; clientType: string }
  | { kind: "client"; clientId: string; clientType: string };

type CredentialCellState =
  | "on"
  | "inherited"
  | "blocked"
  | "inherited-blocked"
  | "off";

export function isCredentialScope(scope: string): scope is CredentialScope {
  return scope === "mcp:auth" || scope === "mcp:blocked_auth";
}

export function credentialRowKey(row: CredentialRow): string {
  switch (row.kind) {
    case "all":
      return "all";
    case "type":
      return `type:${row.clientType}`;
    case "client":
      return `client:${row.clientId}`;
  }
}

const wild = (value: string | undefined) => value == null || value === "*";

/**
 * The tier a stored selector sits on, or null when it narrows further than a
 * tier can show. A personal allow written for the represented user is still a
 * tier rule: it is what the engine enforces for any personal credential.
 */
function credentialTierKey(selector: CredentialSelector): string | null {
  if (
    selector.resourceKind !== "mcp_auth_client" ||
    selector.projectId != null ||
    selector.upstreamClient != null ||
    selector.tool != null ||
    selector.disposition != null ||
    selector.serverUrl != null
  ) {
    return null;
  }
  // An owner constraint only ever matches a personal credential, so a rule
  // carrying one is a tier rule only where it names that kind.
  if (selector.connectionOwner != null) {
    if (
      selector.connectionOwner !== REPRESENTED_USER ||
      selector.clientType !== "remote_session"
    ) {
      return null;
    }
  }
  if (selector.resourceId === "*") {
    if (wild(selector.clientType)) return "all";
    // A kind this dashboard does not know has no row to show it on.
    return (CLIENT_TYPES as readonly string[]).includes(selector.clientType!)
      ? `type:${selector.clientType}`
      : null;
  }
  return `client:${selector.resourceId}`;
}

/** A stable identity for one rule, for diffing what changed. */
export function credentialRuleKey(rule: CredentialRule): string {
  const { selector } = rule;
  const keys = Object.keys(selector).sort() as (keyof CredentialSelector)[];
  const parts = keys
    .filter((key) => selector[key] != null)
    .map((key) => `${key}=${selector[key]}`);
  return `${rule.scope}|${parts.join("|")}`;
}

/** The selector a tier rule is written with. */
function tierSelector(
  row: CredentialRow,
  scope: CredentialScope,
): CredentialSelector {
  const personal =
    scope === "mcp:auth" &&
    ((row.kind === "type" && row.clientType === "remote_session") ||
      (row.kind === "client" && row.clientType === "remote_session"));
  const selector: CredentialSelector = {
    resourceKind: "mcp_auth_client",
    resourceId: row.kind === "client" ? row.clientId : "*",
  };
  if (row.kind === "type" || personal) selector.clientType = row.clientType;
  // A personal credential is allowed only for the person's own connection, so
  // the stored rule says what the engine enforces.
  if (personal) selector.connectionOwner = REPRESENTED_USER;
  return selector;
}

function ownRules(
  rules: CredentialRule[],
  row: CredentialRow,
  scope: CredentialScope,
): CredentialRule[] {
  const key = credentialRowKey(row);
  return rules.filter(
    (rule) => rule.scope === scope && credentialTierKey(rule.selector) === key,
  );
}

function holds(
  rules: CredentialRule[],
  row: CredentialRow,
  scope: CredentialScope,
): boolean {
  return ownRules(rules, row, scope).length > 0;
}

/** The wider rows a row inherits from, nearest first. */
function credentialAncestors(row: CredentialRow): CredentialRow[] {
  switch (row.kind) {
    case "all":
      return [];
    case "type":
      return [{ kind: "all" }];
    case "client":
      return [{ kind: "type", clientType: row.clientType }, { kind: "all" }];
  }
}

/** The rows one tier down. */
export function credentialChildren(
  row: CredentialRow,
  clients: CredentialRef[],
): CredentialRow[] {
  switch (row.kind) {
    case "all":
      return CLIENT_TYPES.map((clientType) => ({ kind: "type", clientType }));
    case "type":
      return clients
        .filter((client) => client.clientType === row.clientType)
        .map((client) => ({
          kind: "client",
          clientId: client.id,
          clientType: client.clientType,
        }));
    case "client":
      return [];
  }
}

export type CredentialRowState = {
  state: CredentialCellState;
  /** The wider row the state comes from, when it is inherited. */
  from?: CredentialRow;
};

export function credentialRowState(
  rules: CredentialRule[],
  row: CredentialRow,
): CredentialRowState {
  if (holds(rules, row, "mcp:blocked_auth")) return { state: "blocked" };
  const ancestors = credentialAncestors(row);
  const blockedBy = ancestors.find((a) => holds(rules, a, "mcp:blocked_auth"));
  if (blockedBy) return { state: "inherited-blocked", from: blockedBy };
  if (holds(rules, row, "mcp:auth")) return { state: "on" };
  const allowedBy = ancestors.find((a) => holds(rules, a, "mcp:auth"));
  if (allowedBy) return { state: "inherited", from: allowedBy };
  return { state: "off" };
}

function without(
  rules: CredentialRule[],
  row: CredentialRow,
  scope: CredentialScope,
): CredentialRule[] {
  const key = credentialRowKey(row);
  return rules.filter(
    (rule) =>
      !(rule.scope === scope && credentialTierKey(rule.selector) === key),
  );
}

function withRule(
  rules: CredentialRule[],
  row: CredentialRow,
  scope: CredentialScope,
): CredentialRule[] {
  if (holds(rules, row, scope)) return rules;
  return [...rules, { scope, selector: tierSelector(row, scope) }];
}

export function allowCredentialRow(
  rules: CredentialRule[],
  row: CredentialRow,
): CredentialRule[] {
  return withRule(rules, row, "mcp:auth");
}

/**
 * Takes the row's own allow away. A row allowed only from a wider row is
 * narrowed instead: the wider rule is split, one tier at a time, so that
 * everything else under it keeps the access it has now.
 */
export function revokeCredentialRow(
  rules: CredentialRule[],
  row: CredentialRow,
  clients: CredentialRef[],
): CredentialRule[] {
  let next = rules;
  for (const ancestor of [...credentialAncestors(row)].reverse()) {
    if (holds(next, ancestor, "mcp:auth")) {
      next = splitCredentialRow(next, ancestor, clients);
    }
  }
  return without(next, row, "mcp:auth");
}

export function blockCredentialRow(
  rules: CredentialRule[],
  row: CredentialRow,
): CredentialRule[] {
  // A block outranks the row's own allow, so the allow would only be noise.
  return withRule(without(rules, row, "mcp:auth"), row, "mcp:blocked_auth");
}

export function unblockCredentialRow(
  rules: CredentialRule[],
  row: CredentialRow,
): CredentialRule[] {
  return without(rules, row, "mcp:blocked_auth");
}

/**
 * Replaces a row's allow with one allow per row under it, so one of them can
 * be taken away without anyone losing the rest. Rows added under it later are
 * no longer covered.
 */
export function splitCredentialRow(
  rules: CredentialRule[],
  row: CredentialRow,
  clients: CredentialRef[],
): CredentialRule[] {
  if (!holds(rules, row, "mcp:auth")) return rules;
  let next = without(rules, row, "mcp:auth");
  for (const child of credentialChildren(row, clients)) {
    next = withRule(next, child, "mcp:auth");
  }
  return next;
}

/** Whether a row holds an allow that splitting would turn into narrower ones. */
export function canSplitCredentialRow(
  rules: CredentialRule[],
  row: CredentialRow,
  clients: CredentialRef[],
): boolean {
  return (
    holds(rules, row, "mcp:auth") && credentialChildren(row, clients).length > 0
  );
}

/** Rules no tier can show: kept as they are, listed on their own. */
export function otherCredentialRules(
  rules: CredentialRule[],
  clients: CredentialRef[] | null,
): CredentialRule[] {
  // Credentials with a row: those that exist and are of a kind shown here.
  const shown = clients
    ? new Set(
        clients
          .filter((client) =>
            (CLIENT_TYPES as readonly string[]).includes(client.clientType),
          )
          .map((client) => client.id),
      )
    : null;
  return rules.filter((rule) => {
    const key = credentialTierKey(rule.selector);
    if (key === null) return true;
    // A credential that no longer exists, or of an unknown kind, has no row
    // to show it on.
    return (
      shown !== null &&
      key.startsWith("client:") &&
      !shown.has(key.slice("client:".length))
    );
  });
}

function matches(
  selector: CredentialSelector,
  client: CredentialRef,
  projectId: string,
): boolean {
  if (
    selector.resourceKind !== "mcp_auth_client" &&
    selector.resourceKind !== "*"
  ) {
    return false;
  }
  if (!wild(selector.resourceId) && selector.resourceId !== client.id) {
    return false;
  }
  if (!wild(selector.clientType) && selector.clientType !== client.clientType) {
    return false;
  }
  if (!wild(selector.projectId) && selector.projectId !== projectId) {
    return false;
  }
  const personal = client.clientType === "remote_session";
  // Strict matching: a key the check does not carry never matches. Only a
  // personal credential's check carries an owner and an upstream client, and
  // the owner is always the person the request represents.
  if (selector.connectionOwner != null) {
    if (!personal) return false;
    if (
      selector.connectionOwner !== "*" &&
      selector.connectionOwner !== REPRESENTED_USER
    ) {
      return false;
    }
  }
  if (selector.upstreamClient != null) {
    if (!personal || !client.remoteSessionClientId) return false;
    if (
      selector.upstreamClient !== "*" &&
      selector.upstreamClient !== client.remoteSessionClientId
    ) {
      return false;
    }
  }
  return (
    selector.tool == null &&
    selector.disposition == null &&
    selector.serverUrl == null
  );
}

/**
 * Whether these rules let their holder use one credential on a server in
 * projectId, for their own connection where the credential is personal. Only
 * these rules are read: grants from elsewhere can still add to them.
 */
export function credentialUsable(
  rules: CredentialRule[],
  client: CredentialRef,
  projectId: string,
): "allowed" | "blocked" | "none" {
  if (
    rules.some(
      (rule) =>
        rule.scope === "mcp:blocked_auth" &&
        matches(rule.selector, client, projectId),
    )
  ) {
    return "blocked";
  }
  return rules.some(
    (rule) =>
      rule.scope === "mcp:auth" && matches(rule.selector, client, projectId),
  )
    ? "allowed"
    : "none";
}

/** The turned-on credentials a server offers. */
export function serverCredentials(
  clients: CredentialRef[],
  serverId: string,
): CredentialRef[] {
  return clients.filter(
    (client) =>
      client.enabled && client.servers.some((server) => server.id === serverId),
  );
}

/** What the save must add and take away so the stored rules match `next`. */
export function credentialRuleChanges(
  stored: CredentialRule[],
  next: CredentialRule[],
): { add: CredentialRule[]; remove: CredentialRule[] } {
  const storedKeys = new Set(stored.map(credentialRuleKey));
  const nextKeys = new Set(next.map(credentialRuleKey));
  return {
    add: next.filter((rule) => !storedKeys.has(credentialRuleKey(rule))),
    remove: stored.filter((rule) => !nextKeys.has(credentialRuleKey(rule))),
  };
}

/**
 * The form's rules after the stored rules were read again: what the person
 * added or took away stays that way, and everything else follows the store.
 */
export function rebaseCredentialRules(args: {
  current: CredentialRule[];
  baseline: CredentialRule[];
  stored: CredentialRule[];
}): CredentialRule[] {
  const { add, remove } = credentialRuleChanges(args.baseline, args.current);
  const removed = new Set(remove.map(credentialRuleKey));
  const kept = args.stored.filter(
    (rule) => !removed.has(credentialRuleKey(rule)),
  );
  const keptKeys = new Set(kept.map(credentialRuleKey));
  return [
    ...kept,
    ...add.filter((rule) => !keptKeys.has(credentialRuleKey(rule))),
  ];
}

export function credentialRulesKey(rules: CredentialRule[]): string {
  return rules.map(credentialRuleKey).sort().join("\n");
}

/** What one rule covers, in the words the credentials table uses. */
export function describeCredentialRule(
  rule: CredentialRule,
  clients: Map<string, { name: string }>,
  projects: Map<string, string>,
): string {
  const { selector: s } = rule;
  const what =
    s.resourceId !== "*"
      ? (clients.get(s.resourceId)?.name ?? "A removed credential")
      : s.clientType && s.clientType !== "*"
        ? `Every ${clientTypeLabel(s.clientType).toLowerCase()} credential`
        : "All credentials";
  const parts = [what];
  if (s.projectId) {
    parts.push(`in ${projects.get(s.projectId) ?? "a removed project"}`);
  }
  if (s.upstreamClient) parts.push("through one upstream app");
  if (s.connectionOwner && s.connectionOwner !== REPRESENTED_USER) {
    parts.push(
      s.connectionOwner === "*"
        ? "anyone's connection"
        : "one person's connection",
    );
  }
  return parts.join(" · ");
}

export type CredentialChangeLine = {
  key: string;
  verb: "Can use" | "Blocked" | "No longer allowed" | "Block lifted";
  what: string;
};

/** A save's credential changes, one line each, those that give access first. */
export function credentialChangeLines(
  stored: CredentialRule[],
  next: CredentialRule[],
  clients: Map<string, { name: string }>,
  projects: Map<string, string>,
): CredentialChangeLine[] {
  const { add, remove } = credentialRuleChanges(stored, next);
  const line = (
    rule: CredentialRule,
    verb: CredentialChangeLine["verb"],
  ): CredentialChangeLine => ({
    key: `${verb}|${credentialRuleKey(rule)}`,
    verb,
    what: describeCredentialRule(rule, clients, projects),
  });
  return [
    ...add.filter((r) => r.scope === "mcp:auth").map((r) => line(r, "Can use")),
    ...remove
      .filter((r) => r.scope === "mcp:blocked_auth")
      .map((r) => line(r, "Block lifted")),
    ...add
      .filter((r) => r.scope === "mcp:blocked_auth")
      .map((r) => line(r, "Blocked")),
    ...remove
      .filter((r) => r.scope === "mcp:auth")
      .map((r) => line(r, "No longer allowed")),
  ];
}

/**
 * Whether going from `stored` to `next` lets the holder use anything they
 * could not before. Splitting a rule adds narrower allows, but they only
 * restate what the wider one gave, so a split alone gives nothing new.
 */
export function credentialChangesGiveAccess(
  stored: CredentialRule[],
  next: CredentialRule[],
  clients: CredentialRef[],
): boolean {
  const { add, remove } = credentialRuleChanges(stored, next);
  if (remove.some((rule) => rule.scope === "mcp:blocked_auth")) return true;
  const byId = new Map(clients.map((client) => [client.id, client]));
  return add.some((rule) => {
    if (rule.scope !== "mcp:auth") return false;
    const key = credentialTierKey(rule.selector);
    if (key === null) return true;
    let row: CredentialRow;
    if (key === "all") row = { kind: "all" };
    else if (key.startsWith("type:")) {
      row = { kind: "type", clientType: key.slice("type:".length) };
    } else {
      const client = byId.get(key.slice("client:".length));
      if (!client) return true;
      row = {
        kind: "client",
        clientId: client.id,
        clientType: client.clientType,
      };
    }
    const before = credentialRowState(stored, row).state;
    return before !== "on" && before !== "inherited";
  });
}

/** Who holds the rules, for the words around them. */
export type CredentialHolder = "role" | "agent";

const HOLDER_COPY: Record<
  CredentialHolder,
  { description: string; personal: string }
> = {
  role: {
    description:
      "Which credentials people and agents in this role may sign in to upstream services with. A rule applies on every server that uses the credential; which servers they reach is set above.",
    personal:
      "Only ever the person's own connection. An agent in this role uses it only while acting for that person.",
  },
  agent: {
    description:
      "Which credentials this agent may sign in to upstream services with. A rule applies on every server that uses the credential; which servers it reaches is set by its keys.",
    personal:
      "Only while the agent acts for a person, and only that person's own connection.",
  },
};

/** The credentials table's words, for who holds the rules. */
export function credentialHolderCopy(holder: CredentialHolder): {
  description: string;
  personal: string;
} {
  return HOLDER_COPY[holder];
}
