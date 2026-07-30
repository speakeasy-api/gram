import { cn } from "@/lib/utils";
import { Badge, Icon, type IconName } from "@speakeasy-api/moonshine";
import {
  Background,
  BaseEdge,
  Handle,
  MarkerType,
  Position,
  ReactFlow,
  getBezierPath,
  type Edge,
  type EdgeProps,
  type Node,
  type NodeProps,
} from "@xyflow/react";
import "@xyflow/react/dist/style.css";
import { useMemo } from "react";

// Flow tier definitions
type FlowTier = "issuer" | "client" | "server" | "session";

const FLOW_TIER_LABELS: Record<FlowTier, string> = {
  issuer: "Identity Provider",
  client: "OAuth Client",
  server: "MCP Server",
  session: "User Session",
};

const FLOW_TIER_ICONS: Record<FlowTier, IconName> = {
  issuer: "fingerprint",
  client: "key",
  server: "server",
  session: "users",
};

const FLOW_TIER_TONES: Record<FlowTier, string> = {
  issuer: "bg-blue-500/10 text-blue-600 ring-blue-500/20 dark:text-blue-400",
  client:
    "bg-purple-500/10 text-purple-600 ring-purple-500/20 dark:text-purple-400",
  server:
    "bg-amber-500/10 text-amber-600 ring-amber-500/20 dark:text-amber-400",
  session:
    "bg-emerald-500/10 text-emerald-600 ring-emerald-500/20 dark:text-emerald-400",
};

const FLOW_EDGE_COLOR = "var(--color-muted-foreground)";
const FLOW_EDGE_MARKER = {
  type: MarkerType.ArrowClosed,
  width: 9,
  height: 9,
  color: FLOW_EDGE_COLOR,
};

// Node data types
export type FlowNodeData = {
  tier: FlowTier;
  label: string;
  count?: number;
  isHighlighted?: boolean;
  subtitle?: string;
};

type FlowGraphNode = Node<FlowNodeData>;
type FlowGraphEdge = Edge<{ animated?: boolean }>;

// Node component
function FlowNodeCard({ data }: NodeProps<FlowGraphNode>) {
  const icon = FLOW_TIER_ICONS[data.tier];
  const tone = FLOW_TIER_TONES[data.tier];

  return (
    <div
      className={cn(
        "bg-card/95 border-border rounded-lg border shadow-sm backdrop-blur",
        "min-w-40 max-w-48 p-3",
        data.isHighlighted && "ring-primary/50 ring-2",
      )}
    >
      <Handle
        type="target"
        position={Position.Left}
        className="border-background! bg-muted-foreground!"
      />
      <div className="mb-2 flex items-center gap-2">
        <div
          className={cn(
            "flex h-7 w-7 shrink-0 items-center justify-center rounded-md ring-1 ring-inset",
            tone,
          )}
        >
          <Icon name={icon} className="h-4 w-4" />
        </div>
        <div className="text-muted-foreground text-[10px] font-medium tracking-wide uppercase">
          {FLOW_TIER_LABELS[data.tier]}
        </div>
      </div>
      <div className="truncate text-sm font-semibold" title={data.label}>
        {data.label}
      </div>
      {data.subtitle && (
        <div
          className="text-muted-foreground mt-0.5 truncate text-xs"
          title={data.subtitle}
        >
          {data.subtitle}
        </div>
      )}
      {data.count !== undefined && (
        <div className="mt-2">
          <Badge variant="neutral" background size="sm">
            <Badge.Text>
              {data.count} {data.count === 1 ? "active" : "active"}
            </Badge.Text>
          </Badge>
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

// Edge component with animation
function FlowEdge({
  id,
  sourceX,
  sourceY,
  targetX,
  targetY,
  sourcePosition,
  targetPosition,
  data,
}: EdgeProps<FlowGraphEdge>) {
  const [edgePath] = getBezierPath({
    sourceX,
    sourceY,
    sourcePosition,
    targetX,
    targetY,
    targetPosition,
  });

  return (
    <BaseEdge
      id={id}
      path={edgePath}
      className="oauth-flow-edge"
      style={{
        stroke: FLOW_EDGE_COLOR,
        strokeWidth: 1.5,
        strokeDasharray: data?.animated ? "5 6" : undefined,
        animation: data?.animated
          ? "oauth-flow-edge-dash 900ms linear infinite"
          : undefined,
      }}
      markerEnd={`url(#${MarkerType.ArrowClosed})`}
    />
  );
}

const FLOW_NODE_TYPES = { flowNode: FlowNodeCard };
const FLOW_EDGE_TYPES = { flowEdge: FlowEdge };

// Animation styles
function FlowEdgeAnimationStyles() {
  return (
    <style>{`
      @keyframes oauth-flow-edge-dash {
        to {
          stroke-dashoffset: -11;
        }
      }

      .oauth-flow-graph .react-flow__node {
        pointer-events: auto !important;
      }

      .oauth-flow-graph .react-flow__handle {
        opacity: 0;
        pointer-events: none;
      }

      @media (prefers-reduced-motion: reduce) {
        .oauth-flow-edge {
          animation: none !important;
        }
      }
    `}</style>
  );
}

// Layout helpers
const NODE_WIDTH = 180;
const NODE_GAP_X = 80;

function layoutNodes(nodes: FlowNodeData[]): FlowGraphNode[] {
  return nodes.map((data, index) => ({
    id: `node-${data.tier}`,
    type: "flowNode",
    position: { x: index * (NODE_WIDTH + NODE_GAP_X), y: 0 },
    data,
  }));
}

function layoutEdges(nodes: FlowNodeData[]): FlowGraphEdge[] {
  const edges: FlowGraphEdge[] = [];
  for (let i = 0; i < nodes.length - 1; i++) {
    const currentNode = nodes[i];
    const nextNode = nodes[i + 1];
    if (currentNode && nextNode) {
      edges.push({
        id: `edge-${currentNode.tier}-${nextNode.tier}`,
        source: `node-${currentNode.tier}`,
        target: `node-${nextNode.tier}`,
        type: "flowEdge",
        data: { animated: true },
        markerEnd: FLOW_EDGE_MARKER,
      });
    }
  }
  return edges;
}

// Props for different flow types
export type IssuerFlowData = {
  issuerName: string;
  issuerUrl: string;
  clientCount: number;
  mcpServerCount?: number;
  sessionCount?: number;
};

export type ClientFlowData = {
  issuerName: string;
  issuerUrl: string;
  clientName: string;
  clientId: string;
  mcpServerCount: number;
  sessionCount: number;
};

type OAuthFlowDiagramProps =
  | {
      variant: "issuer";
      data: IssuerFlowData;
    }
  | {
      variant: "client";
      data: ClientFlowData;
    };

export function OAuthFlowDiagram({
  variant,
  data,
}: OAuthFlowDiagramProps): JSX.Element {
  const { nodes, edges } = useMemo(() => {
    if (variant === "issuer") {
      const issuerData = data as IssuerFlowData;
      const nodeData: FlowNodeData[] = [
        {
          tier: "issuer",
          label: issuerData.issuerName,
          subtitle: issuerData.issuerUrl,
          isHighlighted: true,
        },
        {
          tier: "client",
          label:
            issuerData.clientCount === 1
              ? "1 OAuth Client"
              : `${issuerData.clientCount} OAuth Clients`,
          count:
            issuerData.clientCount > 0 ? issuerData.clientCount : undefined,
        },
        {
          tier: "server",
          label:
            issuerData.mcpServerCount === undefined
              ? "MCP Servers"
              : issuerData.mcpServerCount === 1
                ? "1 MCP Server"
                : `${issuerData.mcpServerCount} MCP Servers`,
          count:
            issuerData.mcpServerCount !== undefined &&
            issuerData.mcpServerCount > 0
              ? issuerData.mcpServerCount
              : undefined,
        },
        {
          tier: "session",
          label:
            issuerData.sessionCount === undefined
              ? "User Sessions"
              : issuerData.sessionCount === 1
                ? "1 Session"
                : `${issuerData.sessionCount} Sessions`,
          count:
            issuerData.sessionCount !== undefined && issuerData.sessionCount > 0
              ? issuerData.sessionCount
              : undefined,
        },
      ];
      return { nodes: layoutNodes(nodeData), edges: layoutEdges(nodeData) };
    }

    // Client variant
    const clientData = data as ClientFlowData;
    const nodeData: FlowNodeData[] = [
      {
        tier: "issuer",
        label: clientData.issuerName,
        subtitle: clientData.issuerUrl,
      },
      {
        tier: "client",
        label: clientData.clientName,
        subtitle: clientData.clientId,
        isHighlighted: true,
      },
      {
        tier: "server",
        label:
          clientData.mcpServerCount === 1
            ? "1 MCP Server"
            : `${clientData.mcpServerCount} MCP Servers`,
        count:
          clientData.mcpServerCount > 0 ? clientData.mcpServerCount : undefined,
      },
      {
        tier: "session",
        label:
          clientData.sessionCount === 1
            ? "1 Active Session"
            : `${clientData.sessionCount} Active Sessions`,
        count:
          clientData.sessionCount > 0 ? clientData.sessionCount : undefined,
      },
    ];
    return { nodes: layoutNodes(nodeData), edges: layoutEdges(nodeData) };
  }, [variant, data]);

  return (
    <div className="bg-muted/20 h-[140px] overflow-hidden rounded-lg border">
      <FlowEdgeAnimationStyles />
      <ReactFlow<FlowGraphNode, FlowGraphEdge>
        className="oauth-flow-graph"
        nodes={nodes}
        edges={edges}
        nodeTypes={FLOW_NODE_TYPES}
        edgeTypes={FLOW_EDGE_TYPES}
        fitView
        fitViewOptions={{ padding: 0.2 }}
        minZoom={0.5}
        maxZoom={1.2}
        zoomOnScroll={false}
        zoomOnPinch={false}
        panOnScroll={false}
        panOnDrag={false}
        nodesDraggable={false}
        nodesConnectable={false}
        elementsSelectable={false}
        proOptions={{ hideAttribution: true }}
      >
        <Background gap={24} size={1} />
        <svg>
          <defs>
            <marker
              id={MarkerType.ArrowClosed}
              viewBox="0 0 10 10"
              refX="8"
              refY="5"
              markerWidth="9"
              markerHeight="9"
              orient="auto-start-reverse"
            >
              <path
                d="M 0 0 L 10 5 L 0 10 z"
                fill={FLOW_EDGE_COLOR}
                strokeWidth="0"
              />
            </marker>
          </defs>
        </svg>
      </ReactFlow>
    </div>
  );
}
