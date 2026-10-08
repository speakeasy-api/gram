import { Badge } from "@/components/ui/Badge";
import { Button } from "@/components/ui/Button";
import { Skeleton } from "@/components/ui/Skeleton";
import { Column, Table } from "@/components/ui/Table";
import { Text } from "@/components/ui/Text";
import { cn } from "@/lib/utils";
import type { ExplainedAccessLevel } from "@gram/client/models/components/explainedaccesslevel.js";
import type { ExplainedAccessRule } from "@gram/client/models/components/explainedaccessrule.js";
import { useExplainResourceAccess } from "@gram/client/react-query/explainResourceAccess.js";
import { ChevronDown, ChevronUp } from "lucide-react";
import { useState, type JSX } from "react";
import {
  accessDecision,
  directorySourceLabel,
  effectBadge,
  effectNote,
  explainedLevel,
  LEVEL_NAME,
  levelStatus,
  ruleIsBlock,
  ruleReachLabel,
  whySentence,
  groupRules,
  type AccessTone,
  type ExplainedLevel,
  type RuleGroup,
  type ServerVisibility,
} from "./explainAccess";
import { PrincipalBadge } from "./PrincipalBadge";
import { RoleLink } from "@/components/role-link";

const LEVELS: ExplainedLevel[] = ["use", "view", "manage"];

const TONE_MARK: Record<AccessTone, string> = {
  allowed: "bg-success-default",
  partial: "bg-warning-default",
  blocked: "bg-destructive",
  none: "border border-muted-foreground",
};

/** A small square in the tone of a decision, beside the words that state it. */
function ToneMark({ tone }: { tone: AccessTone }): JSX.Element {
  return (
    <span
      aria-hidden
      className={cn("inline-block h-2.5 w-2.5 shrink-0", TONE_MARK[tone])}
    />
  );
}

/**
 * One person's access to one server, as the server decides it: the headline,
 * the decision per level, and the rules behind the selected level.
 */
export function CheckAccessResult({
  resourceId,
  userId,
  memberName,
  serverName,
}: {
  resourceId: string;
  userId: string;
  memberName: string;
  serverName: string;
}): JSX.Element {
  const [selected, setSelected] = useState<ExplainedLevel>("use");
  const [showRules, setShowRules] = useState(false);
  const { data, isPending, isError } = useExplainResourceAccess(
    { resourceKind: "mcp", resourceId, userId },
    undefined,
    { throwOnError: false },
  );

  if (isPending) {
    return (
      <div className="border-t px-6 py-4">
        <Skeleton className="h-6 w-2/3" />
      </div>
    );
  }
  if (isError || !data) {
    return (
      <div className="border-t px-6 py-4">
        <Text muted small>
          Access for {memberName} could not be checked. Try again in a moment.
        </Text>
      </div>
    );
  }

  const decision = accessDecision(data, memberName, serverName);
  const level = explainedLevel(data, selected);

  return (
    <div className="border-t">
      <div className="flex flex-col md:flex-row">
        <div className="flex flex-1 items-center gap-4 px-6 py-4">
          <span className="text-eyebrow flex shrink-0 items-center gap-2">
            <ToneMark tone={decision.tone} />
            {decision.label}
          </span>
          <Text variant="body" className="text-base">
            {decision.sentence}
          </Text>
        </div>
        <div
          role="group"
          aria-label="Access level to explain"
          className="divide-border flex divide-x border-t md:border-t-0 md:border-l"
        >
          {LEVELS.map((name) => {
            const explained = explainedLevel(data, name);
            if (!explained) return null;
            return (
              <LevelTab
                key={name}
                level={explained}
                visibility={data.visibility}
                active={name === selected}
                onSelect={() => setSelected(name)}
              />
            );
          })}
        </div>
      </div>

      {level && (
        <LevelWhy
          level={level}
          visibility={data.visibility}
          memberName={memberName}
          serverName={serverName}
          showRules={showRules}
          onToggleRules={() => setShowRules((shown) => !shown)}
        />
      )}
    </div>
  );
}

function LevelTab({
  level,
  visibility,
  active,
  onSelect,
}: {
  level: ExplainedAccessLevel;
  visibility: ServerVisibility;
  active: boolean;
  onSelect: () => void;
}): JSX.Element {
  const status = levelStatus(level, visibility);
  return (
    <button
      type="button"
      aria-pressed={active}
      onClick={onSelect}
      className={cn(
        "flex min-w-32 flex-col items-start justify-center gap-1.5 border-b-2 px-5 py-3.5 text-left whitespace-nowrap transition-colors",
        // Only the bottom edge takes the active color: the side edges are the
        // group's hairline dividers.
        active
          ? "border-b-foreground bg-background"
          : "hover:bg-muted/20 border-b-transparent",
      )}
    >
      <span className="text-eyebrow">{LEVEL_NAME[level.level]}</span>
      <span className="flex items-center gap-2 text-sm">
        <ToneMark tone={status.tone} />
        {status.label}
      </span>
    </button>
  );
}

function LevelWhy({
  level,
  visibility,
  memberName,
  serverName,
  showRules,
  onToggleRules,
}: {
  level: ExplainedAccessLevel;
  visibility: ServerVisibility;
  memberName: string;
  serverName: string;
  showRules: boolean;
  onToggleRules: () => void;
}): JSX.Element {
  const groups = groupRules(level.rules, level.level);
  // Every grant counts, not every row: a row stacks a role's grants.
  const ruleCount = level.rules.length;
  const columns: Column<RuleGroup>[] = [
    {
      key: "source",
      header: "Source",
      width: "1fr",
      render: (group) => <RuleSource rule={group.rule} />,
    },
    {
      key: "rule",
      header: "Rule",
      width: "1.25fr",
      render: (group) => <RuleDescription group={group} />,
    },
    {
      key: "reach",
      header: "Applies to",
      width: "200px",
      render: (group) => (
        <Badge variant="neutral" size="sm" className="self-start">
          {ruleReachLabel(group.rule, serverName)}
        </Badge>
      ),
    },
    {
      key: "effect",
      header: "Effect",
      width: "1.25fr",
      render: (group) => (
        <RuleEffect
          rule={group.rule}
          level={level}
          memberName={memberName}
          serverName={serverName}
        />
      ),
    },
  ];

  return (
    <>
      <div className="bg-background flex items-center gap-6 border-t px-6 py-4">
        <span className="text-eyebrow w-32 shrink-0">
          Why · {LEVEL_NAME[level.level]}
        </span>
        <Text variant="body" className="flex-1 text-sm">
          {whySentence(level, memberName, serverName, visibility)}
        </Text>
        {ruleCount > 0 && (
          <Button variant="secondary" size="sm" onClick={onToggleRules}>
            <Button.Text>
              {showRules
                ? "Hide rules"
                : `Show ${ruleCount} rule${ruleCount === 1 ? "" : "s"}`}
            </Button.Text>
            <Button.RightIcon>
              {showRules ? (
                <ChevronUp className="h-4 w-4" />
              ) : (
                <ChevronDown className="h-4 w-4" />
              )}
            </Button.RightIcon>
          </Button>
        )}
      </div>
      {showRules && ruleCount > 0 && (
        <div className="border-t">
          <Table
            columns={columns}
            data={groups}
            rowKey={(group) => group.key}
            // The card already frames the table, and its cells line up with
            // the card's own gutter.
            className="border-0 [--cell-padding-inline:1.5rem]"
          />
        </div>
      )}
    </>
  );
}

function RuleSource({ rule }: { rule: ExplainedAccessRule }): JSX.Element {
  const directory = directorySourceLabel(rule);
  return (
    <div className="min-w-0 space-y-1 self-start">
      <div className="flex items-center gap-2">
        <PrincipalBadge kind={rule.kind} />
        <Text variant="body" className="truncate text-sm font-medium">
          {rule.kind === "role" ? (
            <RoleLink principalUrn={rule.principalUrn}>
              {rule.displayName}
            </RoleLink>
          ) : (
            rule.displayName
          )}
        </Text>
      </div>
      {directory && (
        <Text muted small className="break-words">
          {directory}
        </Text>
      )}
    </div>
  );
}

function RuleDescription({ group }: { group: RuleGroup }): JSX.Element {
  const { rule, labels } = group;
  const block = ruleIsBlock(rule);
  // A rule that lost still says what it would have done, struck through so
  // it is not read as access the person has.
  const lost = rule.effect === "blocked" || rule.effect === "overridden";
  return (
    <div className="flex items-start gap-3 self-start text-sm">
      <span className="text-eyebrow flex w-16 shrink-0 items-center gap-1.5 pt-0.5">
        <ToneMark tone={block ? "blocked" : "allowed"} />
        {block ? "Blocks" : "Grants"}
      </span>
      <span
        className={cn(
          "flex flex-col",
          lost && "text-muted-foreground line-through",
        )}
      >
        {labels.map((label) => (
          <span key={label}>{label}</span>
        ))}
      </span>
    </div>
  );
}

function RuleEffect({
  rule,
  level,
  memberName,
  serverName,
}: {
  rule: ExplainedAccessRule;
  level: ExplainedAccessLevel;
  memberName: string;
  serverName: string;
}): JSX.Element {
  const badge = effectBadge(rule);
  return (
    <div className="space-y-1 self-start">
      <Badge variant={badge.variant} size="sm">
        {badge.label}
      </Badge>
      <Text muted small className="break-words">
        {effectNote(rule, level, memberName, serverName)}
      </Text>
    </div>
  );
}
