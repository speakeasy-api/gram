import { formatShortDate } from "@/components/access/shadow-mcp-utils";
import { AIToolIcon } from "@/components/ai-tools/AIToolIcon";
import { CollapsibleSectionHeader } from "@/components/collapsible-section-header";
import { nextAIToolDecision } from "@/components/shadow-ai/ai-tool-decision";
import { Alert, AlertDescription } from "@/components/ui/Alert";
import { Badge } from "@/components/ui/Badge";
import { Card } from "@/components/ui/Card";
import { SegmentedControl } from "@/components/ui/SegmentedControl";
import { SkeletonTable } from "@/components/ui/Skeleton";
import { type Column, Table } from "@/components/ui/Table";
import { Text } from "@/components/ui/Text";
import { SimpleTooltip } from "@/components/ui/Tooltip";
import { TARGET_CATEGORIES } from "@/pages/device-agent/ai-scan-target-draft";
import { CircleDashed } from "lucide-react";
import { useState } from "react";
import { SectionHeader } from "../policy-editor-shell";
import type { ShadowMCPDisposition } from "../policy-shadow-mcp-setup";
import {
  type AccessDecision,
  type AccessRow,
  effectiveDecision,
} from "./access-rows";

const DECISION_OPTIONS: { value: "approved" | "blocked"; label: string }[] = [
  { value: "approved", label: "Approve" },
  { value: "blocked", label: "Deny" },
];

const INVENTORY_ONLY_REASON =
  "This tool never identifies itself to the MCP gateway, so a decision about it would have nothing to enforce. It is listed for inventory.";

// Harnesses and assistants open: they are what the policy is about. Open
// models are inventory only, so twenty of them would bury the decisions.
const DEFAULT_EXPANDED: Record<string, boolean> = {
  harness: true,
  assistant: true,
  local_model: false,
};

/**
 * Approve | Deny for one target, written to the editor's draft. A target
 * with neither pressed is unreviewed, shown by the dashed marker rather than
 * a third option; pressing the option in force withdraws it.
 */
function DecisionControl({
  row,
  decision,
  disabled,
  onDecide,
}: {
  row: AccessRow;
  decision: AccessDecision;
  disabled: boolean;
  onDecide: (targetId: string, decision: AccessDecision) => void;
}): JSX.Element {
  if (!row.enforceable) {
    return (
      <SimpleTooltip tooltip={INVENTORY_ONLY_REASON}>
        <Text small muted>
          Inventory only
        </Text>
      </SimpleTooltip>
    );
  }
  return (
    <div className="flex items-center gap-3">
      <SegmentedControl
        ariaLabel={`Access decision for ${row.target.displayName}`}
        className="h-7"
        disabled={disabled}
        value={decision === "unreviewed" ? null : decision}
        onChange={(clicked) =>
          onDecide(row.target.id, nextAIToolDecision(decision, clicked))
        }
        options={DECISION_OPTIONS}
      />
      {decision === "unreviewed" ? (
        <SimpleTooltip tooltip="No decision. The tool connects until you deny it.">
          <span className="text-muted-foreground inline-flex items-center gap-1 text-xs">
            <CircleDashed
              className="text-default-warning h-3.5 w-3.5"
              aria-hidden="true"
            />
            Unreviewed
          </span>
        </SimpleTooltip>
      ) : null}
    </div>
  );
}

function columns(
  draft: ReadonlyMap<string, AccessDecision>,
  disabled: boolean,
  onDecide: (targetId: string, decision: AccessDecision) => void,
): Column<AccessRow>[] {
  return [
    {
      key: "tool",
      header: "Tool",
      width: "1.6fr",
      render: (row) => (
        <div className="flex min-w-0 items-center gap-3">
          <AIToolIcon
            targetId={row.target.id}
            displayName={row.target.displayName}
            className="h-6 w-6 shrink-0"
          />
          <Text className="truncate font-medium">{row.target.displayName}</Text>
          {row.target.origin === "organization" ? (
            <Badge variant="neutral">
              <Badge.Text>Custom</Badge.Text>
            </Badge>
          ) : null}
        </div>
      ),
    },
    {
      key: "users",
      header: "Users",
      width: "0.8fr",
      render: (row) =>
        row.detection ? (
          <Text small className="whitespace-nowrap tabular-nums">
            {row.detection.userCount}
          </Text>
        ) : (
          <Text small muted className="whitespace-nowrap">
            Not seen yet
          </Text>
        ),
    },
    {
      key: "lastSeen",
      header: "Last seen",
      width: "0.7fr",
      render: (row) => (
        <Text small muted={!row.detection} className="whitespace-nowrap">
          {row.detection ? formatShortDate(row.detection.lastSeen) : "–"}
        </Text>
      ),
    },
    {
      key: "action",
      header: "Action",
      width: "1.4fr",
      render: (row) => (
        <DecisionControl
          row={row}
          decision={effectiveDecision(row, draft)}
          disabled={disabled}
          onDecide={onDecide}
        />
      ),
    },
  ];
}

/** What "unreviewed" means under this policy, said once above the groups.
 *  The server reads a missing disposition as block_all, as the standard
 *  editor does. */
function PostureNote({
  disposition,
}: {
  disposition: ShadowMCPDisposition;
}): JSX.Element {
  if (disposition === "allow_all") {
    return (
      <Text small muted>
        Unreviewed tools stay allowed until you deny them. Decisions apply when
        the policy is created or saved.
      </Text>
    );
  }
  return (
    <Alert variant="info" alignTop>
      <AlertDescription>
        Unreviewed MCP servers are blocked by this policy's default, so allow
        the ones your teams need on the Shadow MCP inventory. Unreviewed
        harnesses and assistants still connect until you deny them. Decisions
        apply when the policy is saved.
      </AlertDescription>
    </Alert>
  );
}

/**
 * The Access step: the organization's whole scan target library, grouped by
 * kind, each target with the device agent's evidence and its decision. Rows
 * are configuration only: the Approve | Deny control is the one thing to
 * press. The decisions live in the editor's draft until Create or Save; MCP
 * servers are decided on the Shadow MCP inventory, not here.
 */
export function ShadowAIAccessStep({
  rows,
  isPending,
  libraryFailed,
  detectionsFailed,
  draft,
  disabled = false,
  onDecide,
  disposition,
}: {
  rows: AccessRow[];
  isPending: boolean;
  /** The library read failed: there is nothing to decide over. */
  libraryFailed: boolean;
  /** The detections read failed: evidence and decisions on file are unknown. */
  detectionsFailed: boolean;
  draft: ReadonlyMap<string, AccessDecision>;
  /** Locks the controls while the draft is being written. */
  disabled?: boolean;
  onDecide: (targetId: string, decision: AccessDecision) => void;
  disposition: ShadowMCPDisposition;
}): JSX.Element {
  const [expanded, setExpanded] =
    useState<Record<string, boolean>>(DEFAULT_EXPANDED);
  const toggle = (key: string) =>
    setExpanded((current) => ({ ...current, [key]: !current[key] }));
  const cols = columns(draft, disabled, onDecide);

  return (
    <Card>
      <SectionHeader
        title="Access"
        description="Approve or deny each AI tool in the organization's target library. A denied tool is refused when it connects to the MCP gateway. Harness and assistant decisions apply to everyone in the organization; MCP servers are decided on the Shadow MCP inventory."
      />
      <PostureNote disposition={disposition} />
      {libraryFailed ? (
        <Alert variant="error">
          <AlertDescription>
            The target library could not be loaded. Reload to try again.
          </AlertDescription>
        </Alert>
      ) : (
        <>
          {detectionsFailed ? (
            <Alert variant="error">
              <AlertDescription>
                Detections could not be loaded, so evidence and decisions on
                file are not shown. Reload to try again.
              </AlertDescription>
            </Alert>
          ) : null}
          <div className="divide-border border divide-y">
            {TARGET_CATEGORIES.map((category) => {
              const group = rows.filter(
                (row) => row.target.category === category.value,
              );
              return (
                <div key={category.value}>
                  <CollapsibleSectionHeader
                    label={category.plural}
                    description={category.description}
                    count={isPending ? undefined : group.length}
                    expanded={!!expanded[category.value]}
                    onToggle={() => toggle(category.value)}
                  />
                  {expanded[category.value] ? (
                    <div className="border-border border-t">
                      {isPending ? (
                        <SkeletonTable />
                      ) : group.length === 0 ? (
                        <div className="px-4 py-6">
                          <Text small muted>
                            No {category.plural.toLowerCase()} in the library.
                          </Text>
                        </div>
                      ) : (
                        <Table
                          // About ten rows, then the group scrolls inside
                          // itself so a long kind never grows the page.
                          className="max-h-[28rem] overflow-y-auto"
                          columns={cols}
                          data={group}
                          rowKey={(row) => row.target.id}
                        />
                      )}
                    </div>
                  ) : null}
                </div>
              );
            })}
          </div>
        </>
      )}
    </Card>
  );
}
