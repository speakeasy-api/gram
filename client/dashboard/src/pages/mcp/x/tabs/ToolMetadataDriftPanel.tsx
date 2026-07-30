import { RequireScope } from "@/components/require-scope";
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@/components/ui/tooltip";
import { Type } from "@/components/ui/type";
import { cn } from "@/lib/utils";
import { Badge, Button } from "@speakeasy-api/moonshine";
import { Loader2, RefreshCw } from "lucide-react";
import {
  type FieldChange,
  type MetadataField,
  type ToolDrift,
} from "./toolMetadataSync";

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
 * The session is authoritative, so every row reads as "what syncing would do".
 * Newly advertised tools are recorded automatically and never appear here; what
 * is left is the destructive half — overwriting hints that changed upstream and
 * removing tools the session stopped advertising — which only happens when the
 * user asks for it.
 */
export function ToolMetadataDriftPanel({
  drift,
  mcpServerId,
  onSync,
  isSyncing,
}: {
  drift: ToolDrift[];
  mcpServerId: string | undefined;
  onSync: () => void;
  isSyncing: boolean;
}): JSX.Element | null {
  if (drift.length === 0) return null;

  const removing = drift.filter((d) => d.kind === "removed").length;

  return (
    <div className="border-border mb-5 rounded-lg border">
      <div className="flex flex-wrap items-center justify-between gap-x-3 gap-y-1 border-b px-4 py-2">
        <Type small as="p" className="text-muted-foreground">
          <span className="text-foreground font-medium">
            {drift.length} {drift.length === 1 ? "tool" : "tools"}
          </span>{" "}
          {drift.length === 1 ? "differs" : "differ"} from what this server
          advertises
          {removing > 0 ? (
            <>
              {" · "}
              <span className="text-destructive">
                syncing removes {removing}
              </span>
            </>
          ) : null}
        </Type>
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
      </div>

      <ul className="divide-border divide-y">
        {drift.map((entry) => (
          <DriftRow key={entry.toolName} entry={entry} />
        ))}
      </ul>
    </div>
  );
}

/**
 * One tool per line: marker, name, then what syncing changes. The three columns
 * use the same track sizes on every row so they read as a table, and the detail
 * column scrolls rather than wrapping so a tool with several changed hints
 * still occupies a single line.
 */
function DriftRow({ entry }: { entry: ToolDrift }): JSX.Element {
  return (
    <li className="hover:bg-muted/50 grid grid-cols-[0.75rem_minmax(0,14rem)_minmax(0,1fr)] items-baseline gap-x-3 px-4 py-1.5 transition-colors">
      <DriftMarker kind={entry.kind} />
      <Type
        mono
        small
        as="span"
        className={cn(
          "truncate",
          // The name itself is struck through when the tool is going away, so
          // the row reads as a deletion without needing to spell it out.
          entry.kind === "removed" && "text-muted-foreground line-through",
        )}
        title={entry.toolName}
      >
        {entry.toolName}
      </Type>

      {entry.kind === "changed" ? (
        <span className="flex gap-x-3 overflow-x-auto whitespace-nowrap">
          {entry.changes.map((change) => (
            <ChangeChip key={change.field} change={change} />
          ))}
        </span>
      ) : entry.kind === "new" ? (
        // Worth saying, because it already happened without the user asking.
        // A removal needs no gloss — the marker is the whole story.
        <Type muted small as="span" className="truncate">
          recorded automatically
        </Type>
      ) : null}
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

function DriftMarker({ kind }: { kind: ToolDrift["kind"] }): JSX.Element {
  const { symbol, label, className } = DRIFT_MARKERS[kind];

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
