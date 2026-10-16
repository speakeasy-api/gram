import {
  FooterSaveButton,
  SettingsSection,
} from "@/components/detail/settings-section";
import { RequireScope } from "@/components/require-scope";
import { Alert } from "@/components/ui/Alert";
import { Button } from "@/components/ui/Button";
import { Text } from "@/components/ui/Text";
import { useRBAC } from "@/hooks/useRBAC";
import { toError } from "@/lib/errors";
import { useTunneledHeaderDrafts } from "@/lib/remote-identity";
import type { TunneledMcpServer } from "@gram/client/models/components/tunneledmcpserver.js";
import { invalidateAllPlugin } from "@gram/client/react-query/plugin.js";
import { invalidateAllPlugins } from "@gram/client/react-query/plugins.js";
import { useMcpServers } from "@gram/client/react-query/mcpServers.js";
import { useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { HeadersSection } from "./HeadersSection";

const MCP_TUNNELED_HEADERS_SECTION_ID = "tunnel-headers";

/**
 * Headers the tunnel sends to the upstream MCP server on every request.
 *
 * They are stored on the tunnel, not on this MCP server, so every server on
 * the tunnel sends them, public ones included. The gate is the tunnel's
 * project, the same check the API makes, so a principal who can edit only
 * this one server cannot change what its siblings send.
 */
export function TunneledHeadersSection({
  tunneledMcpServer,
  mcpServerId,
}: {
  tunneledMcpServer: TunneledMcpServer;
  mcpServerId: string;
}): JSX.Element {
  const projectId = tunneledMcpServer.projectId;
  const queryClient = useQueryClient();
  const { hasScope, isLoading: rbacLoading } = useRBAC();
  const canWrite = !rbacLoading && hasScope("mcp:write", projectId, projectId);

  const siblingsQuery = useMcpServers(
    { tunneledMcpServerId: tunneledMcpServer.id },
    undefined,
    { throwOnError: false },
  );
  const siblingMcpServers = (siblingsQuery.data?.mcpServers ?? []).filter(
    (server) =>
      server.tunneledMcpServerId === tunneledMcpServer.id &&
      server.id !== mcpServerId,
  );

  const drafts = useTunneledHeaderDrafts({
    tunneledMcpServerId: tunneledMcpServer.id,
    readOnly: !canWrite,
  });

  const save = async () => {
    try {
      if (await drafts.save()) toast.success("Headers saved");
    } catch (error) {
      toast.error(toError(error).message || "Failed to save headers");
    } finally {
      // A pass-through header changes whether plugins backed by this tunnel
      // can be distributed, and a failed save may still have written some
      // rows.
      await Promise.all([
        invalidateAllPlugins(queryClient),
        invalidateAllPlugin(queryClient),
      ]);
    }
  };

  return (
    <SettingsSection id={MCP_TUNNELED_HEADERS_SECTION_ID}>
      <SettingsSection.Header>
        <SettingsSection.Title>Upstream Headers</SettingsSection.Title>
        <SettingsSection.Description>
          Headers the tunnel adds to every request it forwards to your MCP
          server, such as a tenant or environment selector. A header can carry a
          fixed value, stored encrypted when marked secret, or copy a header
          from the caller's request. Speakeasy credentials, tunnel fields and
          MCP protocol headers are reserved.
        </SettingsSection.Description>
      </SettingsSection.Header>
      <SettingsSection.Panel>
        <SettingsSection.Body>
          {drafts.loadError ? (
            <Alert variant="error" dismissible={false}>
              Could not load this tunnel's headers. Editing is disabled.
            </Alert>
          ) : null}
          <HeadersSection
            state={drafts}
            resourceId={projectId}
            projectId={projectId}
            siblingMcpServers={siblingMcpServers}
            sharedNotice={<SharedTunnelNotice />}
          />
          <Text muted small>
            Headers are sent on the HTTP requests the tunnel agent forwards. An
            agent that runs your MCP server as a local command over stdio cannot
            pass them to that process.
          </Text>
        </SettingsSection.Body>
        {drafts.isDirty ? (
          <SettingsSection.Footer>
            {drafts.reportErrors && drafts.validationError ? (
              <Text small warning>
                {drafts.validationError}
              </Text>
            ) : null}
            <SettingsSection.FooterActions>
              <RequireScope
                scope="mcp:write"
                resourceId={projectId}
                projectId={projectId}
                level="component"
              >
                <Button
                  variant="tertiary"
                  size="md"
                  disabled={drafts.saving}
                  onClick={drafts.discard}
                >
                  <Button.Text>Discard</Button.Text>
                </Button>
                <FooterSaveButton
                  pending={drafts.saving}
                  disabled={
                    !canWrite ||
                    drafts.saving ||
                    drafts.loadError ||
                    drafts.validationError !== null
                  }
                  onClick={() => void save()}
                />
              </RequireScope>
            </SettingsSection.FooterActions>
          </SettingsSection.Footer>
        ) : null}
      </SettingsSection.Panel>
    </SettingsSection>
  );
}

function SharedTunnelNotice(): JSX.Element {
  return (
    <Text small>
      These headers are stored on the tunnel and apply to every MCP server on
      it, including public ones, which send the same headers and secrets on
      behalf of anonymous callers.
    </Text>
  );
}
