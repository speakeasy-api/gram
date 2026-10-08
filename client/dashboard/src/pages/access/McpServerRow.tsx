import { Button } from "@/components/ui/Button";
import { Checkbox } from "@/components/ui/Checkbox";
import {
  HoverCard,
  HoverCardContent,
  HoverCardTrigger,
} from "@/components/ui/HoverCard";
import { cn } from "@/lib/utils";
import { Text } from "@/components/ui/Text";
import { Lock } from "lucide-react";
import { useId, useState, type JSX } from "react";

import { ServerMark, ToolLimitBadges, ToolLimitMenu } from "./McpAccessParts";
import { LIST_ROW, LIST_ROW_SELECTED } from "./mcpAccessStyles";
import {
  serverHandle,
  type McpAdminScope,
  type ServerWithProject,
  type ToolLimit,
  type ToolLimitKind,
} from "./mcpAccessModel";

/** Elements inside a row that act on their own rather than open the sheet. */
const ROW_CONTROLS =
  "button, a, input, label, [role=checkbox], [role=menuitem], [data-slot=checkbox]";

/** One server in the picker: tick it to grant it, pencil to limit its tools. */
export function McpServerRow({
  entry,
  limit,
  lockedBy,
  onToggle,
  onPickLimit,
  onOpenSheet,
  onForbid,
  onShowPlatformAccess,
}: {
  entry: ServerWithProject;
  /** Undefined while the server is not granted. */
  limit: ToolLimit | undefined;
  /** Set when administrative access already connects the role with every tool. */
  lockedBy: McpAdminScope | undefined;
  onToggle: (checked: boolean) => void;
  onPickLimit: (kind: ToolLimitKind) => void;
  onOpenSheet: () => void;
  onForbid: () => void;
  onShowPlatformAccess: () => void;
}): JSX.Element {
  const inputId = useId();
  const nameId = `${inputId}-name`;
  const handleId = `${inputId}-handle`;
  const { server } = entry;
  const locked = !!lockedBy;
  const checked = locked || !!limit;

  return (
    // The whole row opens the tool access sheet, except the controls inside
    // it. The name is the keyboard way in; the row is for the pointer.
    <div
      className={cn(
        LIST_ROW,
        checked && LIST_ROW_SELECTED,
        !locked && "cursor-pointer",
      )}
      onClick={(event) => {
        if (locked) return;
        const target = event.target;
        // Clicks in a portalled menu or card still bubble here through React.
        if (
          !(target instanceof Node) ||
          !event.currentTarget.contains(target)
        ) {
          return;
        }
        const control =
          target instanceof Element ? target.closest(ROW_CONTROLS) : null;
        if (control && event.currentTarget.contains(control)) return;
        onOpenSheet();
      }}
    >
      <Checkbox
        id={inputId}
        aria-labelledby={nameId}
        aria-describedby={handleId}
        checked={checked}
        disabled={locked}
        onCheckedChange={(next) => onToggle(next === true)}
      />
      <ServerMark />
      <span className="flex min-w-0 flex-[1_1_16rem] flex-col items-start">
        {locked ? (
          <Text as="span" id={nameId} className="truncate text-sm font-medium">
            {server.name}
          </Text>
        ) : (
          <button
            type="button"
            onClick={onOpenSheet}
            className="max-w-full truncate text-left text-sm font-medium hover:underline"
          >
            <span id={nameId}>{server.name}</span>
            <span className="sr-only">, edit tool access</span>
          </button>
        )}
        <Text as="span" id={handleId} mono small muted className="truncate">
          {serverHandle(server)}
        </Text>
      </span>
      {limit && !locked && (
        <span className="mr-auto">
          <ToolLimitBadges limit={limit} onOpen={onOpenSheet} />
        </span>
      )}
      <span className="ml-auto flex items-center gap-1">
        {lockedBy && (
          <AlwaysOnCard
            serverName={server.name}
            scope={lockedBy}
            onShowPlatformAccess={onShowPlatformAccess}
          />
        )}
        <ToolLimitMenu
          label={`More options for ${server.name}`}
          offerByTool
          toolsDisabled={locked}
          onPick={onPickLimit}
          onForbid={onForbid}
        />
      </span>
    </div>
  );
}

/** Why a row cannot be unticked: administrative access includes connecting. */
function AlwaysOnCard({
  serverName,
  scope,
  onShowPlatformAccess,
}: {
  serverName: string;
  scope: McpAdminScope;
  onShowPlatformAccess: () => void;
}): JSX.Element {
  // Hover opens it for the pointer; the lock toggles it for the keyboard,
  // which a hover card otherwise ignores.
  const [open, setOpen] = useState(false);
  return (
    <HoverCard open={open} onOpenChange={setOpen} openDelay={100}>
      <HoverCardTrigger asChild>
        <Button
          variant="tertiary"
          size="sm"
          aria-expanded={open}
          onClick={() => setOpen((wasOpen) => !wasOpen)}
          aria-label={`${serverName} is always on: administrative access through ${scope}`}
        >
          <Button.LeftIcon>
            <Lock className="h-4 w-4" />
          </Button.LeftIcon>
        </Button>
      </HoverCardTrigger>
      <HoverCardContent align="end" className="w-72 space-y-2">
        <Text className="font-medium">Always on</Text>
        <Text muted small>
          This role has administrative access to {serverName} through{" "}
          <code className="font-mono">{scope}</code>, which includes connecting
          with every tool. It can&rsquo;t be turned off here.
        </Text>
        <Button variant="secondary" size="sm" onClick={onShowPlatformAccess}>
          <Button.Text>Manage in Platform access</Button.Text>
        </Button>
      </HoverCardContent>
    </HoverCard>
  );
}
