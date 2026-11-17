import { DetailSidebarInfoLabel } from "@/components/detail/detail-sidebar-nav";
import { CopyButton } from "@/components/ui/CopyButton";
import { Text } from "@/components/ui/Text";
import {
  MCPServerAvailabilityToggle,
  MCPServerStatusDropdown,
} from "@/pages/mcp/x/MCPServerDetails";
import { useMcpServerVisibilityUpdate } from "@/pages/mcp/x/mcpServerVisibility";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/Dropdown";
import { DETAIL_HEADER_CONTROL_BUTTON } from "@/components/detail/detail-frame";
import { StatusDot } from "@/components/ui/StatusDot";
import { UpdateMcpServerFormVisibility as KnownUpdateMcpServerFormVisibility } from "@gram/client/models/components/updatemcpserverform.js";
import { ConfirmDialog } from "@/components/ui/ConfirmDialog";
import type { McpServer } from "@gram/client/models/components/mcpserver.js";
import { Check, ChevronDown, Loader2 } from "lucide-react";
import {
  HoverCard,
  HoverCardContent,
  HoverCardTrigger,
} from "@/components/ui/HoverCard";
import { cn } from "@/lib/utils";
import { isKnownEnumValue } from "@/lib/enum";
import * as React from "react";

/**
 * A labelled URL in the rail. The value is long and the rail is narrow, so it
 * sits on one clipped line and the whole thing appears on hover.
 *
 * The reveal is a hover card rather than an absolutely-positioned child:
 * SidebarContent scrolls (overflow-auto), which clips any descendant that
 * tries to escape the rail no matter what z-index it carries. Radix portals
 * the content to the body, so it floats over the page as intended.
 */
export function SidebarUrlRow({
  label,
  url,
  copyTooltip,
}: {
  label: string;
  url: string;
  copyTooltip: string;
}): React.JSX.Element {
  const display = url.replace(/^https?:\/\//, "");
  return (
    <div className="flex flex-col gap-1">
      {/* The copy button sits beside the label, not the URL: the hover card
        that reveals the full URL would otherwise cover it. */}
      <div className="flex items-center gap-1">
        <DetailSidebarInfoLabel>{label}</DetailSidebarInfoLabel>
        <CopyButton text={url} size="xs" tooltip={copyTooltip} />
      </div>
      <div className="min-w-0">
        {/* No delay: this is a reveal of text already on screen, not a
          disclosure of extra information. */}
        <HoverCard openDelay={0}>
          <HoverCardTrigger asChild>
            {/* The padding belongs to the trigger, not the text inside it:
              the card aligns to the trigger's box, and matching insets are
              what land the two texts on each other. The negative margin
              cancels the indent so the URL stays flush with its label. */}
            <span
              // Focusable so keyboard users can open the full URL too.
              tabIndex={0}
              className="focus-visible:ring-ring/50 -mx-2 block px-2 py-1 outline-none focus-visible:ring-2"
            >
              <Text
                variant="small"
                muted
                data-slot="sidebar-url-line"
                className="block truncate font-mono text-xs"
              >
                {display}
              </Text>
            </span>
          </HoverCardTrigger>
          {/* The card carries the same insets as the line, so aligning their
            boxes aligns their text: pull up by exactly the trigger's height
            and the two land on each other. */}
          <HoverCardContent
            align="start"
            side="bottom"
            sideOffset={-24}
            data-slot="sidebar-url-full"
            className="w-auto max-w-[calc(100vw-2rem)] px-2 py-1 font-mono text-xs break-all duration-75"
          >
            {display}
          </HoverCardContent>
        </HoverCard>
      </div>
    </div>
  );
}

export function McpServerCardStatus({
  server,
}: {
  server: McpServer;
}): React.JSX.Element | null {
  if (server.remoteMcpServerId) {
    return <MCPServerAvailabilityToggle server={server} />;
  }

  // Clients connect straight to the vendor, so visibility has nothing to gate.
  if (server.unproxiedMcpServerId) {
    return null;
  }

  return (
    <div className="flex flex-col gap-1.5">
      <DetailSidebarInfoLabel>Visibility</DetailSidebarInfoLabel>
      <MCPServerStatusDropdown server={server} />
    </div>
  );
}

const STATUS_OPTIONS = [
  {
    on: true,
    label: "On",
    description: "Agents can connect and use its tools.",
  },
  {
    on: false,
    label: "Off",
    description: "Agents can't reach it or its tools.",
  },
] as const;

/**
 * The header's on/off control. A remote server is a switch, and turning it
 * off asks first, since every connection to it then fails; other kinds keep
 * the visibility picker, which has its own confirmations.
 */
export function McpServerHeaderStatus({
  server,
}: {
  server: McpServer;
}): React.JSX.Element | null {
  const { canWrite, updateVisibility, updating, displayVisibility } =
    useMcpServerVisibilityUpdate(server);
  const [confirmOff, setConfirmOff] = React.useState(false);
  // Clients reach an unproxied server's vendor directly, so there is
  // nothing to switch; the row still says so.
  if (server.unproxiedMcpServerId) {
    return (
      <span
        className={cn(
          DETAIL_HEADER_CONTROL_BUTTON,
          "hover:bg-card cursor-default",
        )}
      >
        <StatusDot tone="success" />
        Always on
      </span>
    );
  }
  if (!server.remoteMcpServerId) {
    return <MCPServerStatusDropdown server={server} />;
  }
  // Shows the requested state while the change saves, so the control
  // answers at once; the spinner says it hasn't landed yet.
  const on = displayVisibility === "private" || displayVisibility === "public";
  // A state this build doesn't know is neither on nor off: say so, and
  // leave the switch alone until a refresh.
  const known = isKnownEnumValue(
    KnownUpdateMcpServerFormVisibility,
    server.visibility,
  );
  const name = server.name || "this server";
  return (
    <>
      <DropdownMenu>
        <DropdownMenuTrigger asChild disabled={!known || !canWrite}>
          <button
            type="button"
            className={DETAIL_HEADER_CONTROL_BUTTON}
            aria-busy={updating}
            title={
              known
                ? undefined
                : "Refresh the page to see whether this server is on."
            }
          >
            {updating ? (
              <Loader2
                aria-label="Saving"
                className="text-muted-foreground size-3 shrink-0 animate-spin"
              />
            ) : (
              <StatusDot tone={on ? "success" : "destructive"} />
            )}
            <span className="min-w-0 flex-1 truncate text-left">
              {!known ? "Unknown" : on ? "On" : "Off"}
            </span>
            <ChevronDown className="text-muted-foreground size-3.5 shrink-0" />
          </button>
        </DropdownMenuTrigger>
        <DropdownMenuContent align="end" className="w-[280px] p-1">
          {STATUS_OPTIONS.map((option) => (
            <DropdownMenuItem
              key={option.label}
              // Visibly unavailable while a change saves, rather than a
              // menu whose choices do nothing.
              disabled={updating}
              className="flex items-start gap-2 py-2"
              onSelect={() => {
                if (updating || option.on === on) return;
                if (option.on) updateVisibility("private");
                else setConfirmOff(true);
              }}
            >
              <StatusDot
                tone={option.on ? "success" : "destructive"}
                className="mt-1.5"
              />
              <span className="flex min-w-0 flex-1 flex-col">
                <span className="text-sm font-medium">{option.label}</span>
                <span className="text-muted-foreground text-xs">
                  {option.description}
                </span>
              </span>
              {option.on === on ? (
                <Check className="mt-0.5 size-4 shrink-0" />
              ) : null}
            </DropdownMenuItem>
          ))}
        </DropdownMenuContent>
      </DropdownMenu>
      <ConfirmDialog
        open={confirmOff}
        onOpenChange={setConfirmOff}
        title={`Turn off ${name}?`}
        description="Agents can't reach it or its tools until it's turned on again."
        confirmLabel="Turn off"
        pendingLabel="Turning off…"
        isPending={updating}
        onConfirm={() => {
          updateVisibility("disabled");
          setConfirmOff(false);
        }}
      />
    </>
  );
}
