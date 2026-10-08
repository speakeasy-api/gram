import { RequireScope } from "@/components/require-scope";
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@/components/ui/Tooltip";
import { Text } from "@/components/ui/Text";
import { cn } from "@/lib/utils";
import { Badge } from "@/components/ui/Badge";
import { Button } from "@/components/ui/Button";
import { Dialog } from "@/components/ui/Dialog";
import { Loader2, RefreshCw } from "lucide-react";
import { useState, type ReactNode } from "react";
import {
  type FieldChange,
  type MetadataField,
  type ToolDrift,
} from "./toolMetadataSync";
import type { ToolMetadataActions } from "./useSyncToolMetadata";

const FIELD_LABELS: Record<MetadataField, string> = {
  title: "Title",
  readOnlyHint: "Read-only",
  destructiveHint: "Destructive",
  idempotentHint: "Idempotent",
  openWorldHint: "Open world",
};

/**
 * Diff colours, as explicit palette values rather than the semantic tokens.
 * Moonshine defines --success and --warning as BACKGROUND tokens (green-100 /
 * orange-100 in light mode, with separate -foreground counterparts), so
 * `text-success` paints near-white text; only --destructive is text-weight.
 * Using the palette directly keeps the before/after pair legible and balanced
 * in both themes.
 */
/**
 * The leading marker for each row: one character, coloured by drift kind.
 *
 * Mind which token is the text colour — `--success` and `--warning` are
 * BACKGROUND tokens (green-100 / orange-100 in light mode) whose readable
 * counterpart is `-foreground`, while `--destructive` is itself text-weight and
 * its `-foreground` is the pale colour for text sitting ON destructive fill.
 * Hence the asymmetry below; both halves are design-system tokens.
 */
const DRIFT_MARKERS: Record<
  ToolDrift["kind"],
  { symbol: string; label: string; className: string }
> = {
  new: { symbol: "+", label: "New", className: "text-success-foreground" },
  changed: {
    symbol: "~",
    label: "Changed",
    className: "text-warning-foreground",
  },
  removed: { symbol: "−", label: "Removed", className: "text-destructive" },
};

/** Render an unset hint as an em dash so it reads differently from `false`. */
function formatValue(value: string | boolean | undefined): string {
  if (value === undefined) return "—";
  if (typeof value === "boolean") return String(value);
  return value;
}

/**
 * Shows how Speakeasy's stored tool metadata differs from what the live MCP session
 * advertises, and offers to reconcile it.
 *
 * With `onSync` (remote servers) the session is authoritative, so every row
 * reads as "what syncing would do". Newly advertised tools are recorded
 * automatically and never appear here; what is left is the destructive half —
 * overwriting hints that changed upstream and removing tools the session
 * stopped advertising — which only happens when the user asks for it.
 *
 * With `toolActions` (tunneled servers) the session is only what this viewer
 * can see, so nothing is reconciled in bulk: each row offers one write to one
 * tool, and a tool the session doesn't show is never presented as gone.
 */
export function ToolMetadataDriftPanel({
  drift,
  mcpServerId,
  onSync,
  isSyncing,
  toolActions,
}: {
  drift: ToolDrift[];
  mcpServerId: string | undefined;
  onSync: (() => void) | undefined;
  isSyncing: boolean;
  toolActions?: ToolMetadataActions;
}): JSX.Element | null {
  // The confirmation names the server it was opened for, so it never applies
  // to another server this panel is later reused for.
  const [removing, setRemoving] = useState<{
    mcpServerId: string | undefined;
    toolName: string;
  } | null>(null);
  if (drift.length === 0) return null;

  if (toolActions) {
    return (
      <div className="border-border mb-5 border">
        <div className="border-b px-4 py-2">
          <Text small as="p" className="text-muted-foreground">
            <span className="text-foreground font-medium">
              {drift.length} {drift.length === 1 ? "tool" : "tools"}
            </span>{" "}
            {drift.length === 1 ? "differs" : "differ"} from what this server
            listed for you. Other people may see different tools, so stored
            annotations change one tool at a time.
          </Text>
        </div>
        <ul className="divide-border divide-y">
          {drift.map((entry) => (
            <DriftRow
              key={entry.toolName}
              entry={entry}
              action={
                <DriftRowAction
                  entry={entry}
                  mcpServerId={mcpServerId}
                  actions={toolActions}
                  onRemove={() =>
                    setRemoving({ mcpServerId, toolName: entry.toolName })
                  }
                />
              }
            />
          ))}
        </ul>
        {removing !== null &&
        removing.mcpServerId === mcpServerId &&
        drift.some(
          (entry) =>
            entry.kind === "removed" && entry.toolName === removing.toolName,
        ) ? (
          <RemoveStoredToolDialog
            toolName={removing.toolName}
            onConfirm={() => {
              toolActions.remove(removing.toolName);
              setRemoving(null);
            }}
            onClose={() => setRemoving(null)}
          />
        ) : null}
      </div>
    );
  }

  const removingCount = drift.filter((d) => d.kind === "removed").length;

  return (
    <div className="border-border mb-5 border">
      <div className="flex flex-wrap items-center justify-between gap-x-3 gap-y-1 border-b px-4 py-2">
        <Text small as="p" className="text-muted-foreground">
          <span className="text-foreground font-medium">
            {drift.length} {drift.length === 1 ? "tool" : "tools"}
          </span>{" "}
          {drift.length === 1 ? "differs" : "differ"} from what this server
          advertises
          {removingCount > 0 ? (
            <>
              {" · "}
              <span className="text-destructive">
                syncing removes {removingCount}
              </span>
            </>
          ) : null}
        </Text>
        {onSync ? (
          <SyncButton
            mcpServerId={mcpServerId}
            onSync={onSync}
            isSyncing={isSyncing}
          />
        ) : null}
      </div>

      <ul className="divide-border divide-y">
        {drift.map((entry) => (
          <DriftRow key={entry.toolName} entry={entry} />
        ))}
      </ul>
    </div>
  );
}

function SyncButton({
  mcpServerId,
  onSync,
  isSyncing,
}: {
  mcpServerId: string | undefined;
  onSync: () => void;
  isSyncing: boolean;
}): JSX.Element {
  return (
    <RequireScope
      scope="mcp:write"
      resourceId={mcpServerId}
      level="component"
      reason="You need write access to this MCP server to sync tool metadata."
    >
      {({ disabled }) => (
        <Tooltip>
          <TooltipTrigger asChild>
            <Button
              variant="tertiary"
              size="sm"
              className="p-2"
              disabled={disabled || isSyncing}
              onClick={onSync}
            >
              <Button.LeftIcon>
                {isSyncing ? (
                  <Loader2 className="size-4 animate-spin" />
                ) : (
                  <RefreshCw className="size-4" />
                )}
              </Button.LeftIcon>
              {/* The panel header already explains what syncing does, so
                  the label is for screen readers and the tooltip. */}
              <Button.Text className="sr-only">
                {isSyncing ? "Syncing annotations" : "Sync annotations"}
              </Button.Text>
            </Button>
          </TooltipTrigger>
          <TooltipContent>Sync annotations</TooltipContent>
        </Tooltip>
      )}
    </RequireScope>
  );
}

const DRIFT_ACTION_LABELS: Record<ToolDrift["kind"], string> = {
  new: "Record",
  changed: "Apply",
  removed: "Remove",
};

/** The one write a tunneled server's drift row offers for its tool. */
function DriftRowAction({
  entry,
  mcpServerId,
  actions,
  onRemove,
}: {
  entry: ToolDrift;
  mcpServerId: string | undefined;
  actions: ToolMetadataActions;
  onRemove: () => void;
}): JSX.Element {
  const run = () => {
    switch (entry.kind) {
      case "new":
        actions.record(entry.toolName);
        return;
      case "changed":
        actions.apply(entry.toolName);
        return;
      case "removed":
        onRemove();
        return;
    }
  };
  const label = DRIFT_ACTION_LABELS[entry.kind];

  return (
    <RequireScope
      scope="mcp:write"
      resourceId={mcpServerId}
      level="component"
      reason="You need write access to this MCP server to change its stored tool metadata."
    >
      {({ disabled }) => (
        <Button
          variant="tertiary"
          size="xs"
          disabled={disabled || actions.pendingTool !== undefined}
          onClick={run}
        >
          <Button.Text>
            {label}
            <span className="sr-only"> {entry.toolName}</span>
          </Button.Text>
        </Button>
      )}
    </RequireScope>
  );
}

/**
 * Confirms removing one tool's stored metadata. The listing it came from is
 * only one viewer's, so the copy says what is actually removed and who it
 * affects.
 */
function RemoveStoredToolDialog({
  toolName,
  onConfirm,
  onClose,
}: {
  toolName: string;
  onConfirm: () => void;
  onClose: () => void;
}): JSX.Element {
  return (
    <Dialog
      open
      onOpenChange={(open) => {
        if (!open) onClose();
      }}
    >
      <Dialog.Content className="sm:max-w-md">
        <Dialog.Header>
          <Dialog.Title>Remove stored metadata for {toolName}?</Dialog.Title>
          <Dialog.Description>
            This removes Speakeasy&rsquo;s recorded annotations for this tool on
            this server. The tool itself is unaffected.
          </Dialog.Description>
        </Dialog.Header>
        <ul className="text-muted-foreground list-disc space-y-1 py-2 pl-5 text-sm">
          <li>
            It wasn&rsquo;t in your listing, but other people may still see it.
          </li>
          <li>
            Annotation rules stop reaching it for everyone: allow rules no
            longer grant it, and annotation-based exclusions no longer block it,
            which can widen access another rule grants. Rules naming only the
            tool keep working.
          </li>
        </ul>
        <div className="flex justify-end gap-2">
          <Button variant="secondary" onClick={onClose}>
            <Button.Text>Cancel</Button.Text>
          </Button>
          <Button variant="destructive-primary" onClick={onConfirm}>
            <Button.Text>Remove metadata</Button.Text>
          </Button>
        </div>
      </Dialog.Content>
    </Dialog>
  );
}

/**
 * One tool per line: marker, name, then what syncing changes. The three columns
 * use the same track sizes on every row so they read as a table, and the detail
 * column scrolls rather than wrapping so a tool with several changed hints
 * still occupies a single line.
 */
function DriftRow({
  entry,
  action,
}: {
  entry: ToolDrift;
  /** A per-row write, for servers whose drift is resolved one tool at a time. */
  action?: ReactNode;
}): JSX.Element {
  return (
    <li
      className={cn(
        "hover:bg-muted/50 grid items-baseline gap-x-3 px-4 py-1.5 transition-colors",
        action
          ? "grid-cols-[0.75rem_minmax(0,14rem)_minmax(0,1fr)_auto]"
          : "grid-cols-[0.75rem_minmax(0,14rem)_minmax(0,1fr)]",
      )}
    >
      <DriftMarker kind={entry.kind} neutral={!!action} />
      <Text
        mono
        small
        as="span"
        className={cn(
          "truncate",
          // The name itself is struck through when the tool is going away, so
          // the row reads as a deletion without needing to spell it out.
          entry.kind === "removed" &&
            !action &&
            "text-muted-foreground line-through",
        )}
        title={entry.toolName}
      >
        {entry.toolName}
      </Text>

      {entry.kind === "changed" ? (
        <span className="flex gap-x-3 overflow-x-auto whitespace-nowrap">
          {entry.changes.map((change) => (
            <ChangeChip key={change.field} change={change} />
          ))}
        </span>
      ) : entry.kind === "new" ? (
        // Worth saying, because it already happened without the user asking.
        // A removal needs no gloss — the marker is the whole story.
        <Text muted small as="span" className="truncate">
          {action ? "not recorded yet" : "recorded automatically"}
        </Text>
      ) : action ? (
        // Only this viewer's listing lacks it, so it isn't shown as deleted.
        <Text muted small as="span" className="truncate">
          not in your listing
        </Text>
      ) : null}
      {action}
    </li>
  );
}

/**
 * `field old new` — the superseded value struck through and untinted so it
 * recedes, the incoming value filled so the outcome is what catches the eye.
 */
function ChangeChip({ change }: { change: FieldChange }): JSX.Element {
  return (
    <span className="flex shrink-0 items-baseline gap-1 text-xs">
      <span className="text-muted-foreground">
        {FIELD_LABELS[change.field]}
      </span>
      <Badge variant="destructive" size="sm">
        <Badge.Text className="font-mono line-through">
          {formatValue(change.stored)}
        </Badge.Text>
      </Badge>
      <Badge variant="success" size="sm" background>
        <Badge.Text className="font-mono">
          {formatValue(change.advertised)}
        </Badge.Text>
      </Badge>
    </span>
  );
}

/**
 * A tool the viewer's listing lacks is not known to be gone when listings
 * differ per caller, so it is marked neutrally there rather than as removed.
 */
const NOT_IN_LISTING_MARKER = {
  symbol: "?",
  label: "Not in your listing",
  className: "text-muted-foreground",
};

function DriftMarker({
  kind,
  neutral,
}: {
  kind: ToolDrift["kind"];
  /** Per-caller listings: a missing tool is not shown as removed. */
  neutral: boolean;
}): JSX.Element {
  const { symbol, label, className } =
    neutral && kind === "removed" ? NOT_IN_LISTING_MARKER : DRIFT_MARKERS[kind];

  return (
    <span
      aria-label={label}
      title={label}
      className={cn(
        "shrink-0 text-center font-mono text-sm font-medium",
        className,
      )}
    >
      {symbol}
    </span>
  );
}
