import type { ExplainedAccessLevel } from "@gram/client/models/components/explainedaccesslevel.js";
import type { ExplainedAccessRule } from "@gram/client/models/components/explainedaccessrule.js";
import type { ExplainResourceAccessResult } from "@gram/client/models/components/explainresourceaccessresult.js";
import type { Badge } from "@/components/ui/Badge";
import type { ComponentProps } from "react";
import { narrowingLabel } from "./serverAudience";

/**
 * Wording for an access explanation. The server decides every outcome and
 * classifies every rule's effect; nothing here re-derives precedence. These
 * helpers only turn those answers into sentences and labels.
 */

export type ExplainedLevel = ExplainedAccessLevel["level"];
export type ServerVisibility = ExplainResourceAccessResult["visibility"];
type RuleEffect = ExplainedAccessRule["effect"];
type BadgeVariant = ComponentProps<typeof Badge>["variant"];

/** The tone a decision or level is shown in. */
export type AccessTone = "allowed" | "partial" | "blocked" | "none";

export const LEVEL_NAME: Record<ExplainedLevel, string> = {
  use: "Connect",
  view: "View",
  manage: "Manage",
};

/** The access a rule names, whether it gives it or takes it away. */
const RULE_LEVEL_NAME: Record<ExplainedAccessRule["level"], string> = {
  use: "Connect",
  view: "View",
  manage: "Manage",
  blocked: "Connect",
  blocked_view: "View",
  blocked_manage: "Manage",
  all: "Full access",
};

export function explainedLevel(
  result: ExplainResourceAccessResult,
  level: ExplainedLevel,
): ExplainedAccessLevel | undefined {
  return result.levels.find((explained) => explained.level === level);
}

function rulesWith(
  level: ExplainedAccessLevel,
  effect: RuleEffect,
): ExplainedAccessRule[] {
  return level.rules.filter((rule) => rule.effect === effect);
}

/** The distinct names of rules, in order, as an English list. */
function listNames(rules: ExplainedAccessRule[]): string {
  const names = [...new Set(rules.map((rule) => rule.displayName))];
  if (names.length <= 1) return names[0] ?? "";
  return `${names.slice(0, -1).join(", ")} and ${names.at(-1)}`;
}

function distinctCount(rules: ExplainedAccessRule[]): number {
  return new Set(rules.map((rule) => rule.displayName)).size;
}

export interface AccessDecision {
  tone: AccessTone;
  label: string;
  sentence: string;
}

/** The headline answer: can this person use the server at all? */
export function accessDecision(
  result: ExplainResourceAccessResult,
  memberName: string,
  serverName: string,
): AccessDecision {
  switch (result.visibility) {
    case "disabled":
      return {
        tone: "blocked",
        label: "Disabled",
        sentence: `Nobody can connect to ${serverName} while it is disabled.`,
      };
    case "public":
      return {
        tone: "allowed",
        label: "Public",
        sentence: `Anyone can connect to ${serverName}. It is public, so connecting does not check access rules.`,
      };
    case "private":
      break;
  }

  const use = explainedLevel(result, "use");
  if (use?.allowed && use.toolAccess === "some") {
    return {
      tone: "partial",
      label: "Partial",
      sentence: `${memberName} can use ${serverName} with some of its tools.`,
    };
  }
  if (use?.allowed) {
    return {
      tone: "allowed",
      label: "Allowed",
      sentence: `${memberName} can use ${serverName} with all tools.`,
    };
  }
  if (use && rulesWith(use, "blocks").length > 0) {
    return {
      tone: "blocked",
      label: "Blocked",
      sentence: `${memberName} can't connect to ${serverName}.`,
    };
  }
  return {
    tone: "blocked",
    label: "Blocked by default",
    sentence: `${memberName} can't use ${serverName}.`,
  };
}

/**
 * One level's short status, e.g. "All tools" or "Blocked". Connecting to a
 * public server skips the rules and a disabled one serves nobody, so for
 * Connect the server's visibility answers before the rules do.
 */
export function levelStatus(
  level: ExplainedAccessLevel,
  visibility: ServerVisibility,
): {
  tone: AccessTone;
  label: string;
} {
  if (level.level === "use" && visibility === "public") {
    return { tone: "allowed", label: "Public" };
  }
  if (level.level === "use" && visibility === "disabled") {
    return { tone: "blocked", label: "Disabled" };
  }
  if (level.allowed) {
    switch (level.level) {
      case "use":
        if (level.toolAccess === "some") {
          return { tone: "partial", label: "Some tools" };
        }
        return { tone: "allowed", label: "All tools" };
      case "view":
        return { tone: "allowed", label: "Can view" };
      case "manage":
        return { tone: "allowed", label: "Can manage" };
    }
  }
  if (rulesWith(level, "blocks").length > 0) {
    return { tone: "blocked", label: "Blocked" };
  }
  return { tone: "none", label: "No access" };
}

/**
 * Why one level came out the way it did, in a sentence or two. For Connect on
 * a public or disabled server, the visibility decides first, and the rules are
 * what would apply once that changes.
 */
export function whySentence(
  level: ExplainedAccessLevel,
  memberName: string,
  serverName: string,
  visibility: ServerVisibility,
): string {
  const rules = rulesSentence(level, memberName, serverName);
  if (level.level === "use" && visibility === "public") {
    return `${serverName} is public, so anyone can connect without a rule. If it were private: ${rules}`;
  }
  if (level.level === "use" && visibility === "disabled") {
    return `${serverName} is disabled, so nobody can connect. Once it is enabled: ${rules}`;
  }
  return rules;
}

function rulesSentence(
  level: ExplainedAccessLevel,
  memberName: string,
  serverName: string,
): string {
  const access = LEVEL_NAME[level.level];
  const overrides = rulesWith(level, "overrides");
  const blocks = rulesWith(level, "blocks");
  const allows = rulesWith(level, "allows");
  const limits = rulesWith(level, "limits");
  const blocked = rulesWith(level, "blocked");

  if (overrides.length > 0) {
    // A direct grant naming some tools overrides a block for those tools
    // only; the block still takes the rest away.
    if (limits.length > 0) {
      return `A grant made directly to ${memberName} on ${serverName} overrides blocks from roles and everyone, but only for the tools it names. ${listNames(limits)} still ${distinctCount(limits) === 1 ? "takes" : "take"} the other tools away.`;
    }
    const overridden = rulesWith(level, "overridden");
    const consequence =
      overridden.length > 0
        ? `, so the ${listNames(overridden)} block does not apply`
        : "";
    return `A grant made directly to ${memberName} on ${serverName} overrides blocks from roles and everyone${consequence}.`;
  }

  if (blocks.length > 0) {
    if (blocks.some((rule) => rule.kind === "user")) {
      return `${memberName} is blocked from ${access} on ${serverName} by name. A person's own block applies even over their own grants.`;
    }
    const blocking = `${listNames(blocks)} ${distinctCount(blocks) === 1 ? "blocks" : "block"} ${access}`;
    if (blocked.some((rule) => rule.reason === "wildcard_direct_grant")) {
      return `Blocks win over grants. ${blocking}, and ${memberName}'s own grant covers every server, so it cannot override the block. Only a grant naming ${serverName} can.`;
    }
    if (blocked.length > 0) {
      return `Blocks win over grants. ${blocking}, so ${listNames(blocked)}'s grant does not apply.`;
    }
    return `${blocking} on ${serverName}.`;
  }

  if (allows.length > 0) {
    const granted = `${listNames(allows)} ${distinctCount(allows) === 1 ? "grants" : "grant"} ${access} on ${serverName}.`;
    if (limits.length > 0) {
      return `${granted} ${listNames(limits)} ${distinctCount(limits) === 1 ? "limits" : "limit"} which tools ${memberName} can call.`;
    }
    if (level.toolAccess === "some") {
      return `${granted} It covers only some of the tools.`;
    }
    return granted;
  }

  return `Nothing grants ${memberName} ${access} on ${serverName}. Access is off until a rule grants it.`;
}

/** What a rule does, e.g. "Connect · All tools". */
export function ruleAccessLabel(rule: ExplainedAccessRule): string {
  const access = RULE_LEVEL_NAME[rule.level];
  if (rule.level !== "use" && rule.level !== "blocked") return access;
  const narrowing = narrowingLabel(rule);
  return `${access} · ${narrowing.charAt(0).toUpperCase()}${narrowing.slice(1)}`;
}

export function ruleIsBlock(rule: ExplainedAccessRule): boolean {
  return rule.level.startsWith("blocked");
}

/** Where a rule applies: this server, its project, or every server. */
export function ruleReachLabel(
  rule: ExplainedAccessRule,
  serverName: string,
): string {
  switch (rule.appliesTo) {
    case "resource":
      return serverName;
    case "project":
      return "All servers in this project";
    case "all_resources":
      return "All servers";
  }
}

const EFFECT_BADGE: Record<
  RuleEffect,
  { label: string; variant: BadgeVariant }
> = {
  allows: { label: "Allows", variant: "success" },
  overrides: { label: "Overrides", variant: "success" },
  blocks: { label: "Blocks", variant: "destructive" },
  limits: { label: "Limits", variant: "warning" },
  blocked: { label: "Blocked", variant: "neutral" },
  overridden: { label: "Overridden", variant: "neutral" },
};

export function effectBadge(rule: ExplainedAccessRule): {
  label: string;
  variant: BadgeVariant;
} {
  return EFFECT_BADGE[rule.effect];
}

/**
 * A short note under a rule's effect saying why it had that effect. The level
 * supplies the blocks a blocked rule names as its cause.
 */
export function effectNote(
  rule: ExplainedAccessRule,
  level: ExplainedAccessLevel,
  memberName: string,
  serverName: string,
): string {
  switch (rule.effect) {
    case "allows":
      if (rule.appliesTo === "resource") return "Defined on this server.";
      return "Inherited from a rule covering more servers.";
    case "overrides":
      return `Direct grants that name ${serverName} override blocks from roles and everyone.`;
    case "blocks":
      if (rule.kind === "user") {
        return `${memberName}'s own block applies even over their own grants.`;
      }
      return "Blocks win over grants from other rules.";
    case "limits":
      return "Takes these tools away; the rest stay reachable.";
    case "overridden":
      return `Overridden by a grant made directly to ${memberName}.`;
    case "blocked":
      switch (rule.reason) {
        case "wildcard_direct_grant":
          return `Covers every server, so it cannot override a block. Only a grant naming ${serverName} can.`;
        case "own_exclusion":
          return `${memberName}'s own block applies.`;
        case "narrower_direct_grant":
          return "Does not reach this tool.";
        case undefined:
          return blockedByNote(level);
      }
  }
}

/**
 * Names the blocks that kept a matching grant from counting. When a direct
 * grant overrides them, they still block every other grant, so an overridden
 * block is named the same way.
 */
function blockedByNote(level: ExplainedAccessLevel): string {
  const blocks = level.rules.filter(
    (rule) => rule.effect === "blocks" || rule.effect === "overridden",
  );
  if (blocks.length === 0) return "Blocked by a block on this access.";
  return `Blocked by ${listNames(blocks)}.`;
}

/**
 * How a role reached this person through the directory, when it did. A role
 * held directly and through a mapping says so, since taking it away means
 * undoing both.
 */
export function directorySourceLabel(
  rule: ExplainedAccessRule,
): string | undefined {
  const sources = rule.directorySources ?? [];
  const described = sources
    .map((source) => {
      if (source.sourceKind === "group") {
        return `directory group ${source.directoryGroupName ?? "(unnamed)"}`;
      }
      return `${source.attributeKey ?? "attribute"} = ${source.attributeValue ?? ""}`;
    })
    .join(", ");
  if (!rule.viaDirectoryMapping) {
    return sources.length > 0 ? `Also mapped from ${described}` : undefined;
  }
  if (sources.length === 0) return "Mapped from the directory";
  return `Mapped from ${described}`;
}
