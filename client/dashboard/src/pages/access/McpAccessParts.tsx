import { Badge } from "@/components/ui/Badge";
import { Button } from "@/components/ui/Button";
import {
  DropdownMenu,
  DropdownMenuCheckboxItem,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/Dropdown";
import { cn } from "@/lib/utils";
import { Ban, Network, Pencil } from "lucide-react";
import type { JSX } from "react";

import {
  toolLimitBadges,
  type ToolLimit,
  type ToolLimitKind,
} from "./mcpAccessModel";

/** The square hairline tile a server's mark sits in. */
export function ServerTile({ className }: { className?: string }): JSX.Element {
  return (
    <span
      aria-hidden="true"
      className={cn(
        "bg-card text-muted-foreground flex size-8 shrink-0 items-center justify-center border",
        className,
      )}
    >
      <Network className="size-4" />
    </span>
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
            className="hover:border-foreground cursor-pointer"
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
 * The pencil on a server row (and on All servers): pick how its tools are
 * limited, or forbid the server.
 */
export function ToolLimitMenu({
  label,
  current,
  offerByTool,
  toolsDisabled = false,
  onPick,
  onForbid,
}: {
  /** Names the menu for assistive tech, e.g. "More options for Linear". */
  label: string;
  /** The limit in force, or null when the server is not granted. */
  current: ToolLimitKind | null;
  /** All servers has no tool list to pick from. */
  offerByTool: boolean;
  /** Tool access cannot change, e.g. administrative access covers it. */
  toolsDisabled?: boolean;
  onPick: (kind: ToolLimitKind) => void;
  onForbid?: () => void;
}): JSX.Element {
  const options = TOOL_OPTIONS.filter(
    (option) => offerByTool || option.kind !== "tools",
  );
  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button variant="tertiary" size="sm" aria-label={label} title="Edit">
          <Button.LeftIcon>
            <Pencil className="h-4 w-4" />
          </Button.LeftIcon>
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" className="w-64">
        <DropdownMenuLabel className="text-eyebrow">Tools</DropdownMenuLabel>
        {options.map((option) => (
          <DropdownMenuCheckboxItem
            key={option.kind}
            checked={current === option.kind}
            disabled={toolsDisabled}
            onSelect={() => onPick(option.kind)}
          >
            <span className="flex flex-col">
              <span>{option.label}</span>
              <span className="text-muted-foreground text-xs">
                {option.hint}
              </span>
            </span>
          </DropdownMenuCheckboxItem>
        ))}
        {onForbid && (
          <>
            <DropdownMenuSeparator />
            <DropdownMenuItem
              onSelect={onForbid}
              className="text-default-destructive"
            >
              <Ban />
              Forbid server
            </DropdownMenuItem>
          </>
        )}
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
