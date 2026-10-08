import { Badge } from "@/components/ui/Badge";
import { MoreActions, type Action } from "@/components/ui/MoreActions";
import { Network } from "lucide-react";
import type { JSX } from "react";

import {
  toolLimitBadges,
  type ToolLimit,
  type ToolLimitKind,
} from "./mcpAccessModel";

/**
 * A server's mark: the network glyph MCP server lists fall back to when a
 * server has no logo.
 */
export function ServerMark(): JSX.Element {
  return (
    <Network
      aria-hidden="true"
      className="text-muted-foreground size-5 shrink-0"
    />
  );
}

/** What a tool limit allows, as badges that open the tool access sheet. */
export function ToolLimitBadges({
  limit,
  onOpen,
}: {
  limit: ToolLimit;
  onOpen: () => void;
}): JSX.Element | null {
  const badges = toolLimitBadges(limit);
  if (badges.length === 0) return null;
  return (
    <span className="flex flex-wrap items-center gap-1.5">
      {badges.map((label) => (
        <Badge key={label} variant="neutral" size="md" background asChild>
          <button
            type="button"
            onClick={onOpen}
            title="Edit tool access"
            className="cursor-pointer"
          >
            {label}
          </button>
        </Badge>
      ))}
    </span>
  );
}

const TOOL_OPTIONS: {
  kind: ToolLimitKind;
  label: string;
  hint: string;
}[] = [
  { kind: "all", label: "All tools", hint: "Every tool on the server" },
  { kind: "tools", label: "Edit by tool…", hint: "Choose individual tools" },
  {
    kind: "annotations",
    label: "Edit by annotation…",
    hint: "Allow tools by their annotation",
  },
];

/**
 * The ellipsis on a server row (and on All servers): pick how its tools are
 * limited, or forbid the server.
 */
export function ToolLimitMenu({
  label,
  offerByTool,
  toolsDisabled = false,
  onPick,
  onForbid,
}: {
  /** Names the menu for assistive tech, e.g. "More options for Linear". */
  label: string;
  /** All servers has no tool list to pick from. */
  offerByTool: boolean;
  /** Tool access cannot change, e.g. administrative access covers it. */
  toolsDisabled?: boolean;
  onPick: (kind: ToolLimitKind) => void;
  onForbid?: () => void;
}): JSX.Element {
  const actions: Action[] = TOOL_OPTIONS.filter(
    (option) => offerByTool || option.kind !== "tools",
  ).map((option) => ({
    label: option.label,
    description: option.hint,
    disabled: toolsDisabled,
    onClick: () => onPick(option.kind),
  }));
  if (onForbid) {
    actions.push({
      label: "Forbid server",
      icon: "ban",
      destructive: true,
      separatorBefore: true,
      onClick: onForbid,
    });
  }
  return <MoreActions actions={actions} triggerAriaLabel={label} />;
}
