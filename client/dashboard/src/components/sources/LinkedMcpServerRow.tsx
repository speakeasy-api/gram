import { Text } from "@/components/ui/Text";
import type { McpServer } from "@gram/client/models/components/mcpserver.js";
import { useMcpEndpoints } from "@gram/client/react-query/mcpEndpoints.js";
import { Badge } from "@/components/ui/Badge";

export function LinkedMcpServerRow({
  server,
}: {
  server: McpServer;
}): JSX.Element {
  const {
    data: endpoints,
    isError,
    isLoading,
  } = useMcpEndpoints({
    mcpServerId: server.id,
  });
  const shortId = server.id.slice(0, 8);

  return (
    <li className="flex flex-col gap-1 px-3 py-2">
      <div className="flex items-center gap-2">
        <Text small className="font-mono" title={server.id}>
          {shortId}...
        </Text>
        <Badge variant="neutral">
          <Badge.Text>{server.visibility}</Badge.Text>
        </Badge>
      </div>
      {isLoading ? (
        <Text small muted>
          Loading endpoints...
        </Text>
      ) : isError ? (
        <Text small muted>
          Unable to load endpoints
        </Text>
      ) : endpoints && endpoints.mcpEndpoints.length > 0 ? (
        <Text small muted>
          {endpoints.mcpEndpoints.length} endpoint
          {endpoints.mcpEndpoints.length === 1 ? "" : "s"}:{" "}
          {endpoints.mcpEndpoints.map((e) => e.slug).join(", ")}
        </Text>
      ) : (
        <Text small muted>
          No endpoints attached
        </Text>
      )}
    </li>
  );
}
