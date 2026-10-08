import { FormPage } from "@/components/page-templates";
import { Alert } from "@/components/ui/Alert";
import { Button } from "@/components/ui/Button";
import { Stack } from "@/components/ui/Stack";
import { Text } from "@/components/ui/Text";
import { formatTunneledMcpDisplay, mcpServerRouteParam } from "@/lib/sources";
import { GatewayAttachmentStatus } from "@/pages/mcp/gateway/GatewayAttachmentStatus";
import type { GatewayCreationFlow } from "@/pages/mcp/gateway/useGatewayCreation";
import { MCP_SERVER_URL_SECTION_ID } from "@/pages/mcp/x/tabs/settings/sections/ServerUrlSection";
import { NewServerGuardrailOutcomeAlert } from "@/pages/security/server-guardrails/NewServerGuardrailOutcomeAlert";
import type { NewServerGuardrailOutcome } from "@/pages/security/server-guardrails/useNewServerGuardrail";
import { useRoutes } from "@/routes";
import type { McpServer } from "@gram/client/models/components/mcpserver.js";
import type { TunneledMcpServer } from "@gram/client/models/components/tunneledmcpserver.js";
import { useNavigate } from "react-router";

const CONNECTION_SUMMARY: Record<
  TunneledMcpServer["connectionStatus"],
  string
> = {
  connected: "Its tunnel agent is connected.",
  inactive:
    "Its tunnel agent is offline; the server is reachable once the agent reconnects.",
  never_connected:
    "No tunnel agent has connected yet; see Agent Setup in the server settings.",
};

/**
 * Confirmation after adding an MCP server to an existing tunnel. No key is
 * issued: the tunnel's existing key and agent serve the new server too.
 */
export function ExistingTunnelCreated({
  mcpServer,
  tunnel,
  endpointCreated,
  flow,
  guardrailOutcome,
}: {
  mcpServer: McpServer;
  tunnel: TunneledMcpServer;
  endpointCreated: boolean;
  flow: GatewayCreationFlow;
  guardrailOutcome: NewServerGuardrailOutcome | null;
}): JSX.Element {
  const routes = useRoutes();
  const navigate = useNavigate();
  const serverParam = mcpServerRouteParam(mcpServer);

  return (
    <FormPage
      scope="mcp:write"
      width="wide"
      title="MCP server added to tunnel"
      description={`Created on the existing tunnel ${formatTunneledMcpDisplay(tunnel)}. No new key or agent is needed.`}
    >
      <Stack gap={6}>
        <GatewayAttachmentStatus flow={flow} />
        <NewServerGuardrailOutcomeAlert
          outcome={guardrailOutcome}
          onOpenGuardrails={() => routes.mcp.x.guardrails.goTo(serverParam)}
        />
        <Alert variant="info" dismissible={false}>
          The server starts disabled. Enable it from its settings when it is
          ready to serve traffic. {CONNECTION_SUMMARY[tunnel.connectionStatus]}
        </Alert>
        {endpointCreated ? null : (
          <Alert variant="warning" dismissible={false}>
            <Text small>
              The server was created, but its default endpoint was not. Add an
              endpoint from the Server URL section of its settings.
            </Text>
            <div className="mt-2">
              <Button
                variant="secondary"
                onClick={() =>
                  void navigate(
                    `${routes.mcp.x.settings.href(serverParam)}#${MCP_SERVER_URL_SECTION_ID}`,
                  )
                }
              >
                <Button.Text>Open Server URL settings</Button.Text>
              </Button>
            </div>
          </Alert>
        )}
        <Stack direction="horizontal" gap={2}>
          {flow.gatewayId ? (
            <>
              <Button
                variant="primary"
                disabled={flow.isAttaching || flow.attachmentRefused}
                onClick={() => {
                  void flow.complete(mcpServer.id).catch(() => {});
                }}
              >
                <Button.Text>Add to gateway</Button.Text>
              </Button>
              <Button
                variant="secondary"
                disabled={flow.isAttaching}
                onClick={() => routes.mcp.x.overview.goTo(serverParam)}
              >
                <Button.Text>Open MCP server</Button.Text>
              </Button>
            </>
          ) : (
            <Button
              variant="primary"
              onClick={() => routes.mcp.x.overview.goTo(serverParam)}
            >
              <Button.Text>Open MCP server</Button.Text>
            </Button>
          )}
        </Stack>
      </Stack>
    </FormPage>
  );
}
