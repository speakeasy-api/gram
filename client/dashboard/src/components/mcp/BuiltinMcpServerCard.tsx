import { Badge } from "@/components/ui/Badge";
import { CopyButton } from "@/components/ui/CopyButton";
import { DotRow } from "@/components/ui/DotRow";
import { Text } from "@/components/ui/Text";
import { useRoutes } from "@/routes";
import type { BuiltinMcpServer } from "@gram/client/models/components/builtinmcpserver.js";
import { ArrowRight, Blocks } from "lucide-react";
import { Link } from "react-router";
import { McpEntityCard } from "./McpEntityCard";

/** One card per built-in server in the /mcp grid, beside your own servers. */
export function BuiltinMcpServerCard({
  server,
}: {
  server: BuiltinMcpServer;
}): JSX.Element | null {
  const routes = useRoutes();
  return (
    <Link
      to={routes.mcp.builtin.overview.href(server.key)}
      className="focus-visible:ring-ring block h-full no-underline focus-visible:ring-2 focus-visible:ring-offset-2 focus-visible:outline-none"
    >
      <McpEntityCard icon={<Blocks className="text-muted-foreground size-8" />}>
        <Text
          variant="subheading"
          as="div"
          className="text-md group-hover:text-primary truncate transition-colors"
          title={server.name}
        >
          {server.name}
        </Text>
        <Text small muted className="mt-1 line-clamp-2">
          {server.description}
        </Text>
        <div className="mt-auto flex items-center justify-between gap-2 pt-2">
          <div className="flex min-w-0 items-center gap-2">
            <Badge variant="neutral">
              <Badge.Text>Built-in</Badge.Text>
            </Badge>
            <Text small muted className="truncate">
              {server.tools.length} tools
            </Text>
          </div>
          <div className="text-muted-foreground group-hover:text-primary flex items-center gap-1 text-sm transition-colors">
            <span>Open</span>
            <ArrowRight className="size-3.5" />
          </div>
        </div>
      </McpEntityCard>
    </Link>
  );
}

export function BuiltinMcpServerTableRow({
  server,
}: {
  server: BuiltinMcpServer;
}): JSX.Element | null {
  const routes = useRoutes();
  return (
    <DotRow
      href={routes.mcp.builtin.overview.href(server.key)}
      ariaLabel={`Open ${server.name}`}
      icon={<Blocks className="text-muted-foreground size-5" />}
    >
      <td className="px-3 py-3">
        <Text variant="subheading" as="div" className="text-sm">
          {server.name}
        </Text>
      </td>
      <td className="px-3 py-3">
        <Badge variant="neutral">
          <Badge.Text>Built-in</Badge.Text>
        </Badge>
      </td>
      <td className="max-w-xs px-3 py-3">
        <div
          className="relative z-20 flex items-center gap-1.5"
          onClick={(event) => event.stopPropagation()}
        >
          <Text small muted className="truncate font-mono text-xs">
            {server.url.replace(/^https?:\/\//, "")}
          </Text>
          <CopyButton text={server.url} size="sm" tooltip="Copy URL" />
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
