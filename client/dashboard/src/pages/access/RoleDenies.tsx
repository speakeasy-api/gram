import { Alert } from "@/components/ui/Alert";
import { Button } from "@/components/ui/Button";
import { Checkbox } from "@/components/ui/Checkbox";
import { ConfirmDialog } from "@/components/ui/ConfirmDialog";
import type { Scope } from "@gram/client/models/components/rolegrant.js";
import { Ban } from "lucide-react";
import { useState, type JSX } from "react";

import { dispositionCopy } from "./mcpAccessModel";
import { computeRuleLabel } from "./roleDialogState";
import {
  denyCountLabel,
  LEVEL_LABEL,
  LEVELS,
  type McpRowView,
  mcpRowKey,
  removeOtherMcpRule,
  type RoleDeny,
  setMcpBlock,
  setMcpToolBlocked,
  setPlatformAreaDenied,
  unblockPlatformScope,
} from "./roleMatrixModel";
import type { DenyTarget, Grants, ProjectRef } from "./roleMatrixParts";
import { isProjectScopedResourceType } from "./types";

/**
 * Confirms a deny before it is added. A deny outranks every grant a member
 * holds elsewhere, so it is written as its own sentence with its target
 * fixed, and needs an acknowledgement before it can be added.
 */
export function DenyDialog({
  target,
  memberCount,
  onConfirm,
  onClose,
}: {
  target: DenyTarget | null;
  /** How many members hold the role, for the acknowledgement. */
  memberCount: number;
  onConfirm: (target: DenyTarget) => void;
  onClose: () => void;
}): JSX.Element {
  const [acknowledged, setAcknowledged] = useState(false);
  // The dialog keeps its last target while it animates out.
  const [shown, setShown] = useState<DenyTarget | null>(target);
  if (target && target !== shown) {
    setShown(target);
    setAcknowledged(false);
  }
  const holders =
    memberCount === 0
      ? "everyone who is given this role"
      : `${memberCount} ${memberCount === 1 ? "member" : "members"}`;
  return (
    <ConfirmDialog
      open={!!target}
      onOpenChange={(open) => {
        if (!open) onClose();
      }}
      title={`Deny ${shown?.what ?? "access"} for this role`}
      description="A deny is not a level. It is a standing refusal attached to this role that overrides every grant its members hold elsewhere."
      confirmLabel="Add deny"
      confirmDisabled={!acknowledged}
      onConfirm={() => {
        if (target) onConfirm(target);
      }}
    >
      <p className="text-sm">
        Deny <strong>{shown?.what}</strong> for this role on{" "}
        <strong>{shown?.where}</strong>.
      </p>
      <Alert variant="warning" title={null} className="text-sm">
        A deny wins over every grant, including grants made directly to a
        person, and no other role can lift it. Most roles never need one:
        leaving something ungranted already keeps this role from giving it.
      </Alert>
      <label className="flex cursor-pointer items-start gap-3 text-sm">
        <Checkbox
          checked={acknowledged}
          onCheckedChange={(checked) => setAcknowledged(checked === true)}
          className="mt-0.5"
        />
        <span>
          I understand this blocks {holders} regardless of their other roles and
          grants
        </span>
      </label>
    </ConfirmDialog>
  );
}

/** What a deny refuses and where, in the words the list shows. */
function describeDeny(
  deny: RoleDeny,
  rowName: (deny: RoleDeny & { kind: "mcp-row" | "mcp-tool" }) => string,
  projects: ProjectRef[],
): { what: string; where: string } {
  switch (deny.kind) {
    case "mcp-row":
      return {
        what:
          deny.levels.length === LEVELS.length
            ? "MCP access"
            : `MCP access · ${deny.levels.map((l) => LEVEL_LABEL[l]).join(" and ")}`,
        where: rowName(deny),
      };
    case "mcp-tool":
      return { what: `Tool ${deny.tool}`, where: rowName(deny) };
    case "mcp-other": {
      const { selector: s, level } = deny.rule;
      const project = s.projectId
        ? (projects.find((p) => p.id === s.projectId)?.name ??
          "a removed project")
        : undefined;
      const parts = [
        s.resourceId === "*"
          ? project
            ? `every server in ${project}`
            : "every server"
          : "a removed server",
      ];
      if (s.tool) parts.push(`the ${s.tool} tool`);
      if (s.disposition) {
        parts.push(
          `${dispositionCopy(s.disposition).label.toLowerCase()} tools`,
        );
      }
      if (s.serverUrl) parts.push(s.serverUrl);
      return {
        what: `MCP access · ${LEVEL_LABEL[level]}`,
        where: parts.join(" · "),
      };
    }
    case "area":
      return {
        what: deny.area.label,
        where: isProjectScopedResourceType(
          deny.area.resourceType as Parameters<
            typeof isProjectScopedResourceType
          >[0],
        )
          ? "all projects"
          : "the whole organization",
      };
    case "scope":
      return { what: deny.scope.label, where: "everywhere" };
    case "rule":
      return {
        what: deny.scope.label,
        where: computeRuleLabel(
          deny.rule.selectors,
          deny.scope.resourceType as Parameters<typeof computeRuleLabel>[1],
          projects,
        ),
      };
  }
}

/**
 * Every deny the role stores, in one place: a deny set from a row or card far
 * up the page is still found, and taken back, here.
 */
export function ExplicitDenies({
  denies,
  rowViews,
  projects,
  onChange,
  onRemoveRule,
}: {
  denies: RoleDeny[];
  rowViews: McpRowView[];
  projects: ProjectRef[];
  onChange: (change: (prev: Grants) => Grants) => void;
  onRemoveRule: (scope: Scope, ruleIndex: number) => void;
}): JSX.Element | null {
  if (denies.length === 0) return null;
  const views = new Map(rowViews.map((view) => [mcpRowKey(view.row), view]));
  const rowName = (deny: RoleDeny & { kind: "mcp-row" | "mcp-tool" }) => {
    const view = views.get(mcpRowKey(deny.row));
    if (!view) return "a removed server";
    if (deny.row.kind === "all") return "all servers";
    return deny.row.kind === "project"
      ? `every server in ${view.name}`
      : view.name;
  };
  const remove = (deny: RoleDeny) => {
    switch (deny.kind) {
      case "mcp-row":
        return onChange((prev) =>
          deny.levels.reduce(
            (next, level) => setMcpBlock(next, deny.row, level, false),
            prev,
          ),
        );
      case "mcp-tool":
        return onChange((prev) =>
          setMcpToolBlocked(prev, deny.row, deny.tool, false),
        );
      case "mcp-other":
        return onChange((prev) => removeOtherMcpRule(prev, deny.rule));
      case "area":
        return onChange((prev) =>
          setPlatformAreaDenied(prev, deny.area, false),
        );
      case "scope":
        return onChange((prev) =>
          unblockPlatformScope(prev, deny.scope.slug as Scope),
        );
      case "rule":
        return onRemoveRule(deny.scope.slug as Scope, deny.ruleIndex);
    }
  };
  return (
    <section
      aria-label="Explicit denies"
      className="border-destructive bg-card flex flex-col gap-2 border p-4"
    >
      <div className="flex flex-wrap items-baseline gap-x-3 gap-y-1">
        <h3 className="text-destructive flex items-center gap-2 font-medium">
          <Ban aria-hidden="true" className="size-4" />
          {denyCountLabel(denies.length)}
        </h3>
        <p className="text-muted-foreground text-sm">
          Standing refusals. They win over every grant, including grants made
          directly to a person, and no other role can lift them.
        </p>
      </div>
      <ul className="divide-border divide-y">
        {denies.map((deny) => {
          const { what, where } = describeDeny(deny, rowName, projects);
          return (
            <li
              key={deny.key}
              className="flex flex-wrap items-center justify-between gap-3 py-2 text-sm"
            >
              <span>
                <span className="font-medium">{what}</span>{" "}
                <span className="text-muted-foreground">on {where}</span>
              </span>
              <Button
                variant="tertiary"
                size="xs"
                onClick={() => remove(deny)}
                aria-label={`Remove deny: ${what} on ${where}`}
              >
                <Button.Text>Remove</Button.Text>
              </Button>
            </li>
          );
        })}
      </ul>
    </section>
  );
}
