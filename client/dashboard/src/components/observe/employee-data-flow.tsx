import { AgentProviderIcon } from "@/components/agent-providers/AgentProviderIcon";
import { Badge } from "@/components/ui/Badge";
import { Dialog } from "@/components/ui/Dialog";
import { Icon } from "@/components/ui/Icon";
import { type IconName } from "@/components/ui/Icon/names";
import {
  ArrowRight,
  Boxes,
  Globe,
  type LucideIcon,
  Laptop,
  Maximize2,
} from "lucide-react";
import { formatPlatform } from "@/lib/formatPlatform";
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@/components/ui/Tooltip";
import { Avatar, AvatarFallback, AvatarImage } from "@/components/ui/Avatar";
import { getInitials } from "@/lib/initials";
import { cn } from "@/lib/utils";
import type { EmployeeDataFlowNode } from "@gram/client/models/components/employeedataflownode.js";
import type { GetEmployeeDataFlowGraphResult } from "@gram/client/models/components/getemployeedataflowgraphresult.js";
import { useMemo, useState } from "react";
import {
  Background,
  BaseEdge,
  Controls,
  Handle,
  MarkerType,
  MiniMap,
  Position,
  ReactFlow,
  getBezierPath,
  type Edge,
  type EdgeProps,
  type Node,
  type NodeProps,
} from "@xyflow/react";
import "@xyflow/react/dist/style.css";

const DATA_FLOW_TIER_ORDER: DataFlowTier[] = [
  "user",
  "origin",
  "client",
  "server",
  "tool",
];
const DATA_FLOW_TIER_LABELS: Record<string, string> = {
  user: "Employee",
  origin: "Origin",
  client: "MCP Client",
  server: "MCP Server",
  tool: "Tool",
};
const DATA_FLOW_TIER_ICONS: Record<string, IconName> = {
  user: "user",
  origin: "monitor",
  client: "terminal",
  server: "server",
  tool: "wrench",
};
// Neutral chip surfaces with colored icon ink — semantics carried by text
// color, not tinted backgrounds, per the flat design language.
const DATA_FLOW_TIER_TONES: Record<string, string> = {
  user: "bg-card text-muted-foreground ring-border",
  origin:
    "bg-card ring-border text-[var(--color-feedback-blue-700)] dark:text-[var(--color-feedback-blue-500)]",
  client:
    "bg-card ring-border text-[var(--color-feedback-violet-600)] dark:text-[var(--color-feedback-violet-300)]",
  server:
    "bg-card ring-border text-[var(--color-feedback-orange-700)] dark:text-[var(--color-feedback-orange-500)]",
  tool: "bg-card ring-border text-[var(--color-feedback-green-700)] dark:text-[var(--color-feedback-green-500)]",
};
const SYNTHETIC_USER_NODE_ID = "synthetic:user";
const DATA_FLOW_TIER_MINIMAP_COLOR: Record<string, string> = {
  user: "var(--color-neutral-500)",
  origin: "var(--color-feedback-blue-500)",
  client: "var(--color-feedback-violet-600)",
  server: "var(--color-feedback-orange-500)",
  tool: "var(--color-feedback-green-500)",
};
const DATA_FLOW_EDGE_COLOR = "var(--color-muted-foreground)";
const DATA_FLOW_EDGE_MARKER = {
  type: MarkerType.ArrowClosed,
  width: 9,
  height: 9,
  color: DATA_FLOW_EDGE_COLOR,
};
const DATA_FLOW_NODE_TYPES = { dataFlow: DataFlowNodeCard };
const DATA_FLOW_EDGE_TYPES = { dataFlow: DataFlowEdgeLine };
type DataFlowTier = EmployeeDataFlowNode["tier"] | "user";

type DataFlowSourceNode = Omit<EmployeeDataFlowNode, "tier"> & {
  tier: DataFlowTier;
  photoUrl?: string;
};

type DataFlowNodeMetric = {
  value: number;
  successValue: number;
  failureValue: number;
};

type DataFlowNodeData = {
  node: DataFlowSourceNode;
  variant?: "detail" | "summary";
  tierCount?: number;
  metric?: DataFlowNodeMetric;
  serverClassCounts?: Partial<
    Record<NonNullable<EmployeeDataFlowNode["serverClass"]>, number>
  >;
};

type DataFlowEdgeData = {
  callCount: number;
  successCount: number;
  failureCount: number;
};

type DataFlowGraphNode = Node<DataFlowNodeData, "dataFlow">;
type DataFlowGraphEdge = Edge<DataFlowEdgeData, "dataFlow">;

/**
 * The path a person's traffic takes: them, the origin they worked from, the
 * MCP client, the servers it reached and the tools it called. Ported from the
 * Employee Enrollment detail page, where it was the one panel that showed the
 * shape of someone's activity rather than its size.
 */
export function IdentityDataFlowGraphCard({
  graph,
  userName,
  userPhotoUrl,
}: {
  graph: GetEmployeeDataFlowGraphResult;
  userName: string;
  userPhotoUrl?: string;
}): JSX.Element {
  const [expandedOpen, setExpandedOpen] = useState(false);
  const sourceGraph = useMemo(
    () => augmentGraphWithUser(graph, userName, userPhotoUrl),
    [graph, userName, userPhotoUrl],
  );
  const summaryLayout = useMemo(
    () => buildCollapsedDataFlowLayout(sourceGraph),
    [sourceGraph],
  );
  const detailLayout = useMemo(
    () => buildDataFlowLayout(sourceGraph),
    [sourceGraph],
  );
  const hasData =
    detailLayout.nodes.length > 0 && detailLayout.edges.length > 0;

  return (
    <section className="border p-4">
      <DataFlowEdgeAnimationStyles />
      <div className="flex items-start justify-between gap-4">
        <div>
          <h3 className="text-eyebrow">Data Flow</h3>
          <p className="text-muted-foreground mt-1 text-sm">
            From devices to MCP clients, servers, and the tools they use.
          </p>
        </div>
        {hasData && (
          <button
            type="button"
            onClick={() => setExpandedOpen(true)}
            className="text-muted-foreground hover:text-foreground p-0.5 transition-colors"
            aria-label="Expand graph"
          >
            <Maximize2 className="size-4" />
          </button>
        )}
      </div>

      {!hasData ? (
        <div className="text-muted-foreground flex h-[280px] items-center justify-center text-sm">
          No MCP tool-call flow data for selected time range
        </div>
      ) : (
        <div className="bg-muted/20 mt-4 h-[240px] overflow-hidden border">
          <ReactFlow<DataFlowGraphNode, DataFlowGraphEdge>
            className="employee-data-flow-graph"
            nodes={summaryLayout.nodes}
            edges={summaryLayout.edges}
            nodeTypes={DATA_FLOW_NODE_TYPES}
            edgeTypes={DATA_FLOW_EDGE_TYPES}
            fitView
            fitViewOptions={{ padding: 0.3 }}
            minZoom={0.5}
            maxZoom={1.2}
            zoomOnScroll={false}
            zoomOnPinch={false}
            panOnScroll={false}
            panOnDrag={false}
            nodesDraggable={false}
            nodesConnectable={false}
            elementsSelectable={false}
          >
            <Background gap={24} size={1} />
          </ReactFlow>
        </div>
      )}
      <Dialog open={expandedOpen} onOpenChange={setExpandedOpen}>
        <Dialog.Content className="flex h-[90vh] max-h-[90vh] w-[calc(100vw-2rem)] max-w-[calc(100vw-2rem)] flex-col gap-4 p-4 sm:max-w-[calc(100vw-2rem)]">
          <Dialog.Header>
            <div className="flex items-start justify-between gap-4">
              <div>
                <Dialog.Title>Data Flow</Dialog.Title>
                <Dialog.Description>
                  From devices to MCP clients, servers, and the tools they use.
                </Dialog.Description>
              </div>
              <div className="mr-8 flex shrink-0 gap-2">
                <ServerClassBadge serverClass="gram" />
                <ServerClassBadge serverClass="external" />
                <ServerClassBadge serverClass="local" />
              </div>
            </div>
          </Dialog.Header>
          <div className="bg-muted/20 min-h-0 flex-1 overflow-hidden border">
            <ReactFlow<DataFlowGraphNode, DataFlowGraphEdge>
              className="employee-data-flow-graph"
              nodes={detailLayout.nodes}
              edges={detailLayout.edges}
              nodeTypes={DATA_FLOW_NODE_TYPES}
              edgeTypes={DATA_FLOW_EDGE_TYPES}
              fitView
              fitViewOptions={{ padding: 0.16, maxZoom: 1.25 }}
              minZoom={0.2}
              maxZoom={1.6}
              nodesDraggable={false}
              nodesConnectable={false}
              elementsSelectable={false}
            >
              <Background gap={24} size={1} />
              <MiniMap
                pannable
                zoomable
                ariaLabel="Data flow minimap"
                className="bg-card! border-border! border"
                maskColor="hsl(0 0% 50% / 0.12)"
                nodeColor={getDataFlowMiniMapColor}
                nodeStrokeWidth={2}
              />
              <Controls showInteractive={false} />
            </ReactFlow>
          </div>
        </Dialog.Content>
      </Dialog>
    </section>
  );
}

function DataFlowEdgeAnimationStyles() {
  return (
    <style>{`
      @keyframes employee-data-flow-edge-dash {
        to {
          stroke-dashoffset: -11;
        }
      }

      /* Interactions are disabled on the graph, which makes React Flow set
         pointer-events: none on nodes. Re-enable it so node badge tooltips
         can receive hover. */
      .employee-data-flow-graph .react-flow__node {
        pointer-events: auto !important;
      }

      .employee-data-flow-graph .react-flow__handle {
        opacity: 0;
        pointer-events: none;
      }

      @media (prefers-reduced-motion: reduce) {
        .employee-data-flow-edge {
          animation: none !important;
        }
      }
    `}</style>
  );
}

function DataFlowNodeCard({ data }: NodeProps<DataFlowGraphNode>) {
  const node = data.node;
  const isSummary = data.variant === "summary";
  const isServer = node.tier === "server";
  const icon = DATA_FLOW_TIER_ICONS[node.tier] ?? "circle";
  const tone =
    DATA_FLOW_TIER_TONES[node.tier] ??
    "bg-muted text-muted-foreground ring-border";
  const serverClassCounts = data.serverClassCounts
    ? (Object.entries(data.serverClassCounts).filter(([, count]) =>
        Boolean(count),
      ) as [NonNullable<EmployeeDataFlowNode["serverClass"]>, number][])
    : [];

  return (
    <div
      className={cn(
        "bg-card/95 border-border border backdrop-blur",
        isSummary ? "max-w-64 min-w-56 p-4" : "max-w-56 min-w-48 p-3",
      )}
    >
      <Handle
        type="target"
        position={Position.Left}
        className="border-background! bg-muted-foreground!"
      />
      <div className="mb-2 flex items-center gap-2">
        <DataFlowNodeVisual
          node={node}
          isSummary={isSummary}
          tone={tone}
          icon={icon}
        />
        <div className="text-muted-foreground text-[11px] font-medium tracking-wide uppercase">
          {DATA_FLOW_TIER_LABELS[node.tier] ?? node.tier}
        </div>
      </div>
      <div className="truncate text-sm font-semibold" title={node.label}>
        {isSummary ? node.label : formatDataFlowNodeLabel(node)}
      </div>
      {(data.metric ||
        (isSummary ? serverClassCounts.length > 0 : isServer)) && (
        <div className="mt-2 flex flex-wrap items-center gap-1.5">
          {data.metric && <DataFlowMetricBadge metric={data.metric} />}
          {isSummary
            ? serverClassCounts.map(([serverClass, count]) => (
                <ServerClassBadge
                  key={serverClass}
                  serverClass={serverClass}
                  count={count}
                />
              ))
            : isServer && (
                <ServerClassBadge
                  serverClass={node.serverClass ?? "external"}
                />
              )}
        </div>
      )}
      <Handle
        type="source"
        position={Position.Right}
        className="border-background! bg-muted-foreground!"
      />
    </div>
  );
}

function DataFlowMetricBadge({ metric }: { metric: DataFlowNodeMetric }) {
  const tooltip = `${metric.value.toLocaleString()} calls received (${metric.successValue.toLocaleString()} ok / ${metric.failureValue.toLocaleString()} blocked)`;

  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <Badge variant="neutral" background>
          <Badge.LeftIcon>
            <ArrowRight className="h-3.5 w-3.5" />
          </Badge.LeftIcon>
          <Badge.Text>{metric.value.toLocaleString()}</Badge.Text>
        </Badge>
      </TooltipTrigger>
      <TooltipContent>{tooltip}</TooltipContent>
    </Tooltip>
  );
}

function DataFlowNodeVisual({
  node,
  isSummary,
  tone,
  icon,
}: {
  node: DataFlowSourceNode;
  isSummary: boolean;
  tone: string;
  icon: IconName;
}) {
  if (node.tier === "user") {
    return (
      <Avatar className="size-7">
        {node.photoUrl && <AvatarImage src={node.photoUrl} alt={node.label} />}
        <AvatarFallback className="text-[10px] font-semibold">
          {getInitials(node.label)}
        </AvatarFallback>
      </Avatar>
    );
  }

  // Individual MCP client nodes show their product logo (Cursor, Claude, etc.).
  if (node.tier === "client" && !isSummary) {
    return (
      <span className="border-border bg-background inline-flex size-7 items-center justify-center border">
        <AgentProviderIcon source={node.label} className="size-4" />
      </span>
    );
  }

  return (
    <span
      className={cn(
        "inline-flex size-7 items-center justify-center ring-1",
        tone,
      )}
    >
      <Icon name={icon} className="size-3.5" />
    </span>
  );
}

const SERVER_CLASS_BADGE_META: Record<
  NonNullable<EmployeeDataFlowNode["serverClass"]>,
  {
    variant: "information" | "warning" | "success";
    icon: LucideIcon;
    tooltip: string;
  }
> = {
  gram: {
    variant: "information",
    icon: Boxes,
    tooltip: "Speakeasy-hosted MCP server",
  },
  external: {
    variant: "warning",
    icon: Globe,
    tooltip: "Third-party external MCP server",
  },
  local: {
    variant: "success",
    icon: Laptop,
    tooltip: "Local MCP server running on the employee's device",
  },
};

function ServerClassBadge({
  serverClass,
  count,
}: {
  serverClass: NonNullable<EmployeeDataFlowNode["serverClass"]>;
  count?: number;
}) {
  const meta = SERVER_CLASS_BADGE_META[serverClass];
  const ClassIcon = meta.icon;
  const tooltip =
    count !== undefined
      ? `${count.toLocaleString()} ${serverClass} ${count === 1 ? "server" : "servers"} — ${meta.tooltip}`
      : meta.tooltip;

  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <Badge variant={meta.variant} background aria-label={meta.tooltip}>
          <Badge.LeftIcon>
            <ClassIcon className="h-3.5 w-3.5" />
          </Badge.LeftIcon>
          {count !== undefined && (
            <Badge.Text>{count.toLocaleString()}</Badge.Text>
          )}
        </Badge>
      </TooltipTrigger>
      <TooltipContent>{tooltip}</TooltipContent>
    </Tooltip>
  );
}

function DataFlowEdgeLine({
  id,
  sourceX,
  sourceY,
  targetX,
  targetY,
  sourcePosition,
  targetPosition,
  markerEnd,
  style,
}: EdgeProps<DataFlowGraphEdge>) {
  const [edgePath] = getBezierPath({
    sourceX,
    sourceY,
    sourcePosition,
    targetX,
    targetY,
    targetPosition,
  });
  const edgeStyle = {
    ...style,
    strokeLinecap: "round" as const,
    animation: "employee-data-flow-edge-dash 900ms linear infinite",
  };

  return (
    <BaseEdge
      id={id}
      path={edgePath}
      markerEnd={markerEnd}
      style={edgeStyle}
      className="employee-data-flow-edge"
      interactionWidth={28}
    />
  );
}

type DataFlowSourceGraph = {
  nodes: DataFlowSourceNode[];
  edges: GetEmployeeDataFlowGraphResult["edges"];
};

function augmentGraphWithUser(
  graph: GetEmployeeDataFlowGraphResult,
  userName: string,
  userPhotoUrl?: string,
): DataFlowSourceGraph {
  // The backend already prunes nodes that aren't reachable from an origin, so
  // here we only attach the synthetic "user" node that fronts the origins.
  const nodes: DataFlowSourceNode[] = graph.nodes.map((node) => ({ ...node }));
  const edges = graph.edges.map((edge) => ({ ...edge }));

  const origins = nodes.filter((node) => node.tier === "origin");
  if (origins.length === 0) return { nodes, edges };

  const outcomeByOrigin = new Map<
    string,
    { success: number; failure: number }
  >();
  for (const edge of graph.edges) {
    const outcome = outcomeByOrigin.get(edge.source) ?? {
      success: 0,
      failure: 0,
    };
    outcome.success += edge.successCount;
    outcome.failure += edge.failureCount;
    outcomeByOrigin.set(edge.source, outcome);
  }

  const totalCalls = origins.reduce((sum, node) => sum + node.totalCalls, 0);
  nodes.push({
    id: SYNTHETIC_USER_NODE_ID,
    label: userName || "Employee",
    tier: "user",
    totalCalls,
    photoUrl: userPhotoUrl,
  });

  for (const origin of origins) {
    const outcome = outcomeByOrigin.get(origin.id) ?? {
      success: origin.totalCalls,
      failure: 0,
    };
    edges.push({
      id: `synthetic:user->${origin.id}`,
      source: SYNTHETIC_USER_NODE_ID,
      target: origin.id,
      callCount: origin.totalCalls,
      successCount: outcome.success,
      failureCount: outcome.failure,
    });
  }

  return { nodes, edges };
}

function buildCollapsedDataFlowLayout(graph: DataFlowSourceGraph): {
  nodes: DataFlowGraphNode[];
  edges: DataFlowGraphEdge[];
} {
  const nodesByTier = groupDataFlowNodesByTier(graph.nodes);
  const visibleTiers = DATA_FLOW_TIER_ORDER.filter((tier) =>
    nodesByTier.has(tier),
  );
  const tierXGap = 280;

  const edgeCountsByTierPair = new Map<
    string,
    { callCount: number; successCount: number; failureCount: number }
  >();
  const tierByNodeId = new Map(graph.nodes.map((node) => [node.id, node.tier]));
  for (const edge of graph.edges) {
    const sourceTier = tierByNodeId.get(edge.source);
    const targetTier = tierByNodeId.get(edge.target);
    if (!sourceTier || !targetTier || sourceTier === targetTier) continue;

    const key = getTierPairKey(sourceTier, targetTier);
    const counts = edgeCountsByTierPair.get(key) ?? {
      callCount: 0,
      successCount: 0,
      failureCount: 0,
    };
    counts.callCount += edge.callCount;
    counts.successCount += edge.successCount;
    counts.failureCount += edge.failureCount;
    edgeCountsByTierPair.set(key, counts);
  }

  const nodes: DataFlowGraphNode[] = visibleTiers.map((tier, index) => {
    const tierNodes = nodesByTier.get(tier) ?? [];
    const totalCalls = tierNodes.reduce(
      (sum, node) => sum + node.totalCalls,
      0,
    );
    const isUser = tier === "user";
    const firstNode = tierNodes[0];
    const previousTier = index > 0 ? visibleTiers[index - 1] : undefined;
    const incoming = previousTier
      ? edgeCountsByTierPair.get(getTierPairKey(previousTier, tier))
      : undefined;

    const metric: DataFlowNodeMetric | undefined =
      !isUser && incoming
        ? {
            value: incoming.callCount,
            successValue: incoming.successCount,
            failureValue: incoming.failureCount,
          }
        : undefined;

    return {
      id: getAggregateDataFlowNodeId(tier),
      type: "dataFlow",
      position: {
        x: index * tierXGap,
        y: 0,
      },
      data: {
        node: {
          id: getAggregateDataFlowNodeId(tier),
          label: isUser
            ? (firstNode?.label ?? "Employee")
            : formatAggregateTierLabel(tier, tierNodes.length),
          tier,
          totalCalls,
          photoUrl: isUser ? firstNode?.photoUrl : undefined,
        },
        variant: "summary",
        tierCount: tierNodes.length,
        metric,
        serverClassCounts:
          tier === "server" ? getServerClassCounts(tierNodes) : undefined,
      },
      sourcePosition: Position.Right,
      targetPosition: Position.Left,
    };
  });

  const aggregateEdges = visibleTiers.slice(0, -1).map((tier, index) => {
    const nextTier = visibleTiers[index + 1]!;
    const counts = edgeCountsByTierPair.get(getTierPairKey(tier, nextTier)) ?? {
      callCount: 0,
      successCount: 0,
      failureCount: 0,
    };

    return {
      id: `aggregate:${tier}->${nextTier}`,
      source: getAggregateDataFlowNodeId(tier),
      target: getAggregateDataFlowNodeId(nextTier),
      callCount: counts.callCount,
      successCount: counts.successCount,
      failureCount: counts.failureCount,
    };
  });
  const maxCalls = Math.max(...aggregateEdges.map((edge) => edge.callCount), 1);
  const edges: DataFlowGraphEdge[] = aggregateEdges.map((edge) => ({
    id: edge.id,
    source: edge.source,
    target: edge.target,
    type: "dataFlow",
    markerEnd: DATA_FLOW_EDGE_MARKER,
    style: getDataFlowEdgeStyle(edge.callCount, maxCalls, 2),
    data: {
      callCount: edge.callCount,
      successCount: edge.successCount,
      failureCount: edge.failureCount,
    },
  }));

  return { nodes, edges };
}

function buildDataFlowLayout(graph: DataFlowSourceGraph): {
  nodes: DataFlowGraphNode[];
  edges: DataFlowGraphEdge[];
} {
  const nodesByTier = groupDataFlowNodesByTier(graph.nodes);
  const visibleTiers = DATA_FLOW_TIER_ORDER.filter((tier) =>
    nodesByTier.has(tier),
  );
  // Tiers with many nodes (typically tools) wrap into multiple sub-columns so
  // the graph stays compact instead of one very tall, sparse column.
  const maxRowsPerColumn = 6;
  const subColumnGap = 264;
  const tierGap = 336;
  const nodeYGap = 132;

  const incomingSourcesByTarget = new Map<string, string[]>();
  const incomingCallsByTarget = new Map<
    string,
    { callCount: number; successCount: number; failureCount: number }
  >();
  for (const edge of graph.edges) {
    const sources = incomingSourcesByTarget.get(edge.target) ?? [];
    sources.push(edge.source);
    incomingSourcesByTarget.set(edge.target, sources);

    const counts = incomingCallsByTarget.get(edge.target) ?? {
      callCount: 0,
      successCount: 0,
      failureCount: 0,
    };
    counts.callCount += edge.callCount;
    counts.successCount += edge.successCount;
    counts.failureCount += edge.failureCount;
    incomingCallsByTarget.set(edge.target, counts);
  }

  // Order each tier so connected nodes line up vertically with their sources
  // (barycenter heuristic), which reduces edge crossings and empty-space edges.
  const rowIndexByNode = new Map<string, number>();
  const orderedNodesByTier = new Map<string, DataFlowSourceNode[]>();
  const barycenter = (nodeId: string) => {
    const sources = incomingSourcesByTarget.get(nodeId) ?? [];
    const indices = sources
      .map((source) => rowIndexByNode.get(source))
      .filter((index): index is number => index !== undefined);
    if (indices.length === 0) return Number.POSITIVE_INFINITY;
    return indices.reduce((sum, index) => sum + index, 0) / indices.length;
  };

  visibleTiers.forEach((tier, tierIndex) => {
    const tierNodes = (nodesByTier.get(tier) ?? []).slice();
    tierNodes.sort((a, b) => {
      if (tierIndex > 0) {
        const diff = barycenter(a.id) - barycenter(b.id);
        if (Number.isFinite(diff) && diff !== 0) return diff;
      }
      return b.totalCalls - a.totalCalls || a.label.localeCompare(b.label);
    });
    tierNodes.forEach((node, index) => {
      void rowIndexByNode.set(node.id, index);
    });
    orderedNodesByTier.set(tier, tierNodes);
  });

  // Pre-compute horizontal placement for each tier, accounting for tiers that
  // wrap into multiple sub-columns (so later tiers shift right accordingly).
  let cursorX = 0;
  const placementByTier = new Map<
    string,
    { baseX: number; rowsPerColumn: number }
  >();
  for (const tier of visibleTiers) {
    const count = orderedNodesByTier.get(tier)?.length ?? 0;
    const subColumns = Math.max(1, Math.ceil(count / maxRowsPerColumn));
    const rowsPerColumn = Math.max(1, Math.ceil(count / subColumns));
    placementByTier.set(tier, { baseX: cursorX, rowsPerColumn });
    cursorX += (subColumns - 1) * subColumnGap + tierGap;
  }

  const nodes: DataFlowGraphNode[] = visibleTiers.flatMap((tier) => {
    const tierNodes = orderedNodesByTier.get(tier) ?? [];
    const placement = placementByTier.get(tier)!;
    const offset = ((placement.rowsPerColumn - 1) * nodeYGap) / 2;

    return tierNodes.map((node, index) => {
      const isUser = node.tier === "user";
      const incoming = incomingCallsByTarget.get(node.id);
      const metric: DataFlowNodeMetric | undefined =
        !isUser && incoming
          ? {
              value: incoming.callCount,
              successValue: incoming.successCount,
              failureValue: incoming.failureCount,
            }
          : undefined;

      const column = Math.floor(index / placement.rowsPerColumn);
      const row = index % placement.rowsPerColumn;

      return {
        id: node.id,
        type: "dataFlow" as const,
        position: {
          x: placement.baseX + column * subColumnGap,
          y: row * nodeYGap - offset,
        },
        data: { node, variant: "detail" as const, metric },
        sourcePosition: Position.Right,
        targetPosition: Position.Left,
      };
    });
  });

  const maxCalls = Math.max(...graph.edges.map((edge) => edge.callCount), 1);
  const edges: DataFlowGraphEdge[] = graph.edges.map((edge) => ({
    id: edge.id,
    source: edge.source,
    target: edge.target,
    type: "dataFlow",
    markerEnd: DATA_FLOW_EDGE_MARKER,
    style: getDataFlowEdgeStyle(edge.callCount, maxCalls, 1.5),
    data: {
      callCount: edge.callCount,
      successCount: edge.successCount,
      failureCount: edge.failureCount,
    },
  }));

  return { nodes, edges };
}

function getDataFlowMiniMapColor(node: DataFlowGraphNode) {
  return (
    DATA_FLOW_TIER_MINIMAP_COLOR[node.data.node.tier] ??
    "var(--color-neutral-400)"
  );
}

function getDataFlowEdgeStyle(
  callCount: number,
  maxCalls: number,
  minStrokeWidth: number,
) {
  return {
    stroke: DATA_FLOW_EDGE_COLOR,
    strokeDasharray: "5 6",
    strokeWidth: Math.max(
      minStrokeWidth,
      Math.min(3, (callCount / maxCalls) * 3),
    ),
    opacity: 1,
  };
}

function groupDataFlowNodesByTier(nodes: DataFlowSourceNode[]) {
  const nodesByTier = new Map<string, DataFlowSourceNode[]>();
  for (const node of nodes) {
    const tierNodes = nodesByTier.get(node.tier) ?? [];
    tierNodes.push(node);
    nodesByTier.set(node.tier, tierNodes);
  }

  return nodesByTier;
}

function getAggregateDataFlowNodeId(tier: string) {
  return `aggregate:${tier}`;
}

function getTierPairKey(sourceTier: string, targetTier: string) {
  return `${sourceTier}->${targetTier}`;
}

function formatAggregateTierLabel(tier: string, count: number) {
  const label = DATA_FLOW_TIER_LABELS[tier] ?? tier;
  const suffix = count === 1 ? label : pluralizeDataFlowTierLabel(label);
  return `${count.toLocaleString()} ${suffix}`;
}

function pluralizeDataFlowTierLabel(label: string) {
  if (label.endsWith("y")) return `${label.slice(0, -1)}ies`;
  return `${label}s`;
}

function getServerClassCounts(nodes: DataFlowSourceNode[]) {
  return nodes.reduce<
    Partial<Record<NonNullable<EmployeeDataFlowNode["serverClass"]>, number>>
  >((counts, node) => {
    const serverClass = node.serverClass ?? "external";
    counts[serverClass] = (counts[serverClass] ?? 0) + 1;
    return counts;
  }, {});
}

function formatToolUrn(value: string) {
  const parts = value.split(/[/:]/).filter(Boolean);
  return parts[parts.length - 1] ?? value;
}

function formatDataFlowNodeLabel(node: DataFlowSourceNode) {
  if (node.tier === "client") return formatPlatform(node.label);
  if (node.tier === "tool") return formatToolUrn(node.label);
  if (node.tier === "origin") return formatOriginLabel(node.label);
  if (node.tier === "server") return formatServerLabel(node);
  return node.label;
}

const UUID_LIKE_PATTERN =
  /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;

function formatServerLabel(node: DataFlowSourceNode) {
  if (UUID_LIKE_PATTERN.test(node.label)) {
    const serverClass = node.serverClass ?? "external";
    const shortId = node.label.slice(0, 8);
    const prefix =
      serverClass === "gram"
        ? "Speakeasy server"
        : serverClass === "local"
          ? "Local server"
          : "MCP server";
    return `${prefix} ${shortId}`;
  }
  return node.label;
}

function formatOriginLabel(value: string) {
  if (value === "local") return "local";
  if (/^https?:\/\//.test(value)) {
    try {
      return new URL(value).hostname || value;
    } catch {
      return value;
    }
  }
  return value;
}
