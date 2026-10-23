import { Badge } from "@/components/ui/Badge";
import { CopyButton } from "@/components/ui/CopyButton";
import { DotRow } from "@/components/ui/DotRow";
import { Text } from "@/components/ui/Text";
import { useRoutes } from "@/routes";
import type { BuiltinMcpServer } from "@gram/client/models/components/builtinmcpserver.js";
import { ArrowRight, Blocks } from "lucide-react";
import { McpEntityCard } from "./McpEntityCard";

export function BuiltinMcpServerCard({ server }: { server: BuiltinMcpServer }) {
  const routes = useRoutes();
  return (
    <McpEntityCard
      className="cursor-pointer"
      onClick={() => routes.mcp.builtin.goTo(server.key)}
      icon={<Blocks className="size-8 text-muted-foreground" />}
    >
      <Text variant="subheading" as="div" className="truncate">
        {server.name}
      </Text>
      <Text small muted className="mt-1 line-clamp-2">
        {server.description}
      </Text>
      <div className="mt-auto flex items-center justify-between gap-2 pt-2">
        <Badge variant="neutral">
          <Badge.Text>Built-in</Badge.Text>
        </Badge>
        <div className="text-muted-foreground flex items-center gap-1 text-sm">
          <span>Open</span>
          <ArrowRight className="size-3.5" />
        </div>
      </div>
    </McpEntityCard>
  );
}

export function BuiltinMcpServerTableRow({
  server,
}: {
  server: BuiltinMcpServer;
}) {
  const routes = useRoutes();
  return (
    <DotRow
      href={routes.mcp.builtin.href(server.key)}
      ariaLabel={`Open built-in ${server.name}`}
      icon={<Blocks className="size-5 text-muted-foreground" />}
    >
      <td className="px-3 py-3">
        <Text variant="subheading" as="div" className="text-sm">
          {server.name}
        </Text>
      </td>
      <td className="px-3 py-3">
        <Badge variant="neutral">Built-in</Badge>
      </td>
      <td className="max-w-xs px-3 py-3">
        <div
          className="relative z-20 flex items-center gap-1.5"
          onClick={(event) => event.stopPropagation()}
        >
          <Text small muted className="truncate font-mono text-xs">
            {server.url.replace(/^https?:\/\//, "")}
          </Text>
          <CopyButton text={server.url} size="sm" tooltip="Copy MCP URL" />
        </div>
      </td>
      <td className="px-3 py-3">
        <Text small muted>
          {server.tools.length} tools
        </Text>
      </td>
    </DotRow>
  );
}
