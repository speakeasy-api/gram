import { Badge } from "@/components/ui/Badge";
import {
  MCP_SERVER_KIND_LABEL,
  type McpServerKind,
} from "@/components/mcp/mcp-server-kind";

export type McpBadgeKind = McpServerKind | "gateway";

const LABEL: Record<McpBadgeKind, string> = {
  ...MCP_SERVER_KIND_LABEL,
  gateway: "Gateway",
};

// One fixed brand hue per kind, so a kind reads the same on every card and row.
// Red and orange are left out: the Badge's destructive and warning variants own
// them, and an "Off" or "OAuth Required" badge sits right beside these. Built-in
// stays neutral as Speakeasy's own. Classes are literal so Tailwind emits them;
// the dark column sinks the ground and lifts the text, like the identity tints.
const TINT: Record<McpBadgeKind, string | undefined> = {
  gateway:
    "bg-[hsl(280,30%,93%)] text-[hsl(280,40%,32%)] border-[hsl(280,26%,80%)] dark:bg-[hsl(280,22%,20%)] dark:text-[hsl(280,40%,82%)] dark:border-[hsl(280,22%,34%)]",
  hosted:
    "bg-[hsl(214,40%,93%)] text-[hsl(214,50%,30%)] border-[hsl(214,34%,80%)] dark:bg-[hsl(214,26%,20%)] dark:text-[hsl(214,50%,82%)] dark:border-[hsl(214,26%,34%)]",
  remote:
    "bg-[hsl(174,32%,91%)] text-[hsl(174,48%,24%)] border-[hsl(174,28%,76%)] dark:bg-[hsl(174,24%,18%)] dark:text-[hsl(174,40%,78%)] dark:border-[hsl(174,24%,30%)]",
  tunneled:
    "bg-[hsl(334,36%,93%)] text-[hsl(334,42%,32%)] border-[hsl(334,30%,80%)] dark:bg-[hsl(334,22%,20%)] dark:text-[hsl(334,40%,82%)] dark:border-[hsl(334,22%,34%)]",
  unproxied:
    "bg-[hsl(68,30%,89%)] text-[hsl(68,40%,24%)] border-[hsl(68,26%,72%)] dark:bg-[hsl(68,20%,18%)] dark:text-[hsl(68,34%,76%)] dark:border-[hsl(68,20%,30%)]",
  built_in: undefined,
};

/** The source/kind badge on MCP server and gateway cards and table rows. */
export function McpKindBadge({ kind }: { kind: McpBadgeKind }): JSX.Element {
  return (
    <Badge variant="neutral" className={TINT[kind]}>
      <Badge.Text>{LABEL[kind]}</Badge.Text>
    </Badge>
  );
}
