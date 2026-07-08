import { Type } from "@/components/ui/type";
import type { McpServer } from "@gram/client/models/components/mcpserver.js";
import { useMcpEndpoints } from "@gram/client/react-query/mcpEndpoints.js";
import { Badge } from "@speakeasy-api/moonshine";

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
        <Type small className="font-mono" title={server.id}>
          {shortId}...
        </Type>
        <Badge variant="neutral">
          <Badge.Text>{server.visibility}</Badge.Text>
        </Badge>
      </div>
      {isLoading ? (
        <Type small muted>
          Loading endpoints...
        </Type>
      ) : isError ? (
        <Type small muted>
          Unable to load endpoints
        </Type>
      ) : endpoints && endpoints.mcpEndpoints.length > 0 ? (
        <Type small muted>
          {endpoints.mcpEndpoints.length} endpoint
          {endpoints.mcpEndpoints.length === 1 ? "" : "s"}:{" "}
          {endpoints.mcpEndpoints.map((e) => e.slug).join(", ")}
        </Type>
      ) : (
        <Type small muted>
          No endpoints attached
        </Type>
      )}
    </li>
  );
}
