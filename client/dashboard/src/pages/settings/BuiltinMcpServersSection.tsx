import { SettingsSection } from "@/components/detail/settings-section";
import { useBuiltinMcpServers } from "@/components/mcp/useBuiltinMcpServers";
import { useUpdateBuiltinMcpServer } from "@/components/mcp/useUpdateBuiltinMcpServer";
import { Button } from "@/components/ui/Button";
import { Switch } from "@/components/ui/Switch";
import { Text } from "@/components/ui/Text";
import { useRoutes } from "@/routes";
import { Link } from "react-router";

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
          Show or hide built-in servers in project discovery. Hiding is visual
          only: addresses, permissions, and gateway membership do not change.
        </SettingsSection.Description>
      </SettingsSection.Header>
      <SettingsSection.Panel>
        <SettingsSection.Body>
          {isLoading ? (
            <Text muted>Loading built-in servers…</Text>
          ) : isError ? (
            <div
              role="alert"
              className="flex items-center justify-between gap-4"
            >
              <Text muted>Could not load built-in servers.</Text>
              <Button variant="secondary" onClick={() => void refetch()}>
                Retry
              </Button>
            </div>
          ) : (
            servers.map((server) => (
              <div
                key={server.id}
                className="flex items-center justify-between gap-4"
              >
                <div>
                  <Link
                    to={routes.mcp.builtin.href(server.key)}
                    className="font-medium hover:underline"
                  >
                    {server.name}
                  </Link>
                  <Text small muted>
                    Show in MCP Servers and Command K
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
