import { useState } from "react";
import { useNavigate } from "react-router";
import { useQueryClient } from "@tanstack/react-query";
import type { McpServer } from "@gram/client/models/components/mcpserver";
import { SettingsSection } from "@/components/detail/settings-section";
import { Button } from "@/components/ui/Button";
import { FieldError } from "@/components/ui/Field";
import { useSdkClient } from "@/contexts/Sdk";
import { invalidateMcpServerQueries } from "@/lib/mcp-server-visibility";
import { clearProviderProgress } from "@/pages/mcp/x/setup/provider-setup";
import { mcpServerRouteParam } from "@/lib/sources";
import { useRoutes } from "@/routes";

/**
 * Reopens guided setup on a server whose setup is marked done, at its first
 * step. Saved credentials and the connection stay as they are; the done mark
 * and this tab's saved Setup progress clear. The Settings page decides when it
 * shows: guided setup marked done, for people who can edit the server.
 */
export function RestartSetupSection({
  mcpServer,
}: {
  mcpServer: McpServer;
}): JSX.Element {
  const sdk = useSdkClient();
  const queryClient = useQueryClient();
  const navigate = useNavigate();
  const routes = useRoutes();
  const [pending, setPending] = useState(false);
  const [failed, setFailed] = useState(false);

  const restart = async () => {
    setPending(true);
    setFailed(false);
    try {
      await sdk.mcpServers.setSetupGuideCompleted({
        setSetupGuideCompletedRequestBody: {
          id: mcpServer.id,
          completed: false,
        },
      });
    } catch {
      setFailed(true);
      setPending(false);
      return;
    }
    // Setup restores the last step it saved; start again from the first.
    clearProviderProgress(mcpServer.id);
    // Setup reads the done mark when it opens, so refresh it before going.
    await invalidateMcpServerQueries(queryClient);
    await navigate(
      routes.mcp.details.setup.href(mcpServerRouteParam(mcpServer)),
    );
  };

  return (
    <SettingsSection>
      <SettingsSection.Header>
        <SettingsSection.Title>Guided setup</SettingsSection.Title>
        <SettingsSection.Description>
          Walk through this server’s guided setup again. Saved credentials and
          the connection stay in place until you change them.
        </SettingsSection.Description>
      </SettingsSection.Header>
      <SettingsSection.Panel>
        <SettingsSection.Body>
          <div className="flex flex-col items-start gap-2">
            <Button
              variant="secondary"
              disabled={pending}
              onClick={() => void restart()}
            >
              <Button.Text>
                {pending ? "Restarting…" : "Restart setup"}
              </Button.Text>
            </Button>
            {failed && (
              <FieldError>Unable to restart setup. Try again.</FieldError>
            )}
          </div>
        </SettingsSection.Body>
      </SettingsSection.Panel>
    </SettingsSection>
  );
}
