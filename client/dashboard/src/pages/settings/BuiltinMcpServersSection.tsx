import { SettingsSection } from "@/components/detail/settings-section";
import { useBuiltinMcpServers } from "@/components/mcp/useBuiltinMcpServers";
import { useUpdateBuiltinMcpServer } from "@/components/mcp/useUpdateBuiltinMcpServer";
import { Button } from "@/components/ui/Button";
import { Skeleton } from "@/components/ui/Skeleton";
import { Switch } from "@/components/ui/Switch";
import { Text } from "@/components/ui/Text";
import { useRoutes } from "@/routes";
import { Link } from "react-router";

/**
 * Which Speakeasy-maintained servers this project lists. Hiding one is a
 * view preference — it stays reachable at its own address and keeps whatever
 * gateway membership it has — so the switch applies as soon as it is flipped.
 */
export function BuiltinMcpServersSection(): JSX.Element | null {
  const { servers, isAdmin, isLoading, isError, refetch } =
    useBuiltinMcpServers();
  const update = useUpdateBuiltinMcpServer();
  const routes = useRoutes();
  if (!isAdmin) return null;
  return (
    <SettingsSection id="builtin-mcp-servers">
      <SettingsSection.Header>
        <SettingsSection.Title>Built-in MCP servers</SettingsSection.Title>
        <SettingsSection.Description>
          Speakeasy maintains these servers in every project. Hide the ones this
          project does not use to keep them out of its MCP server list and
          search results.
        </SettingsSection.Description>
      </SettingsSection.Header>
      <SettingsSection.Panel>
        <SettingsSection.Body>
          {isLoading ? (
            <div role="status" aria-live="polite">
              <span className="sr-only">Loading built-in servers…</span>
              <Skeleton className="h-9 w-full" />
            </div>
          ) : isError ? (
            <div
              role="alert"
              className="flex items-center justify-between gap-4"
            >
              <Text muted>
                Couldn&rsquo;t load built-in servers. Nothing has changed.
              </Text>
              <Button variant="secondary" onClick={() => void refetch()}>
                <Button.Text>Try again</Button.Text>
              </Button>
            </div>
          ) : (
            servers.map((server) => (
              <div
                key={server.id}
                className="flex items-center justify-between gap-4"
              >
                <div className="min-w-0">
                  <Link
                    to={routes.mcp.builtin.overview.href(server.key)}
                    className="font-medium hover:underline"
                  >
                    {server.name}
                  </Link>
                  <Text small muted>
                    {server.tools.length} tools
                  </Text>
                </div>
                <Switch
                  aria-label={`Show ${server.name} in MCP Servers`}
                  checked={!server.hidden}
                  disabled={update.isPending}
                  onCheckedChange={(shown) =>
                    update.save({
                      key: server.key,
                      expectedVersion: server.version,
                      hidden: !shown,
                      environmentId: server.environmentId,
                    })
                  }
                />
              </div>
            ))
          )}
        </SettingsSection.Body>
      </SettingsSection.Panel>
    </SettingsSection>
  );
}
