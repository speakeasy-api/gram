import {
  FooterSaveButton,
  SettingsSection,
} from "@/components/detail/settings-section";
import { useReportUnsavedChanges } from "@/components/detail/settings-layout";
import { RequireScope } from "@/components/require-scope";
import { useRBAC } from "@/hooks/useRBAC";
import { VerifyRemoteMcpUrlButton } from "@/pages/sources/remote-mcp/VerifyRemoteMcpUrlButton";
import type { RemoteMcpServer } from "@gram/client/models/components/remotemcpserver.js";
import { toast } from "sonner";
import { UpstreamUrlField } from "./UpstreamUrlField";
import { useUpstreamUrlDraft } from "./useUpstreamUrlDraft";
import * as authz from "@/lib/authz.gen";

export const MCP_UPSTREAM_URL_SECTION_ID = "upstream-url";

/**
 * Where Speakeasy reaches a remote server. It sits in Connection beside the
 * address clients use, so both URLs are in one place.
 */
export function UpstreamUrlSection({
  remoteMcpServer,
}: {
  remoteMcpServer: RemoteMcpServer;
}): JSX.Element {
  const upstream = useUpstreamUrlDraft(remoteMcpServer);
  const { can } = useRBAC();
  const writeCheck = authz.mcpServers(remoteMcpServer.projectId).write();
  useReportUnsavedChanges(can(writeCheck) && upstream.dirty);

  const handleSave = async () => {
    try {
      await upstream.save();
      toast.success("Remote URL updated");
    } catch (error) {
      toast.error(
        error instanceof Error ? error.message : "Failed to update remote URL",
      );
    }
  };

  return (
    <SettingsSection id={MCP_UPSTREAM_URL_SECTION_ID}>
      <SettingsSection.Header>
        <SettingsSection.Title>Remote URL</SettingsSection.Title>
        <SettingsSection.Description>
          Where Speakeasy reaches this server. Clients connect to the server URL
          above, not to this one.
        </SettingsSection.Description>
      </SettingsSection.Header>
      <SettingsSection.Panel>
        <SettingsSection.Body>
          <UpstreamUrlField upstream={upstream} />
        </SettingsSection.Body>
        <SettingsSection.Footer>
          <SettingsSection.FooterHint>
            {upstream.dirty
              ? "Verify before saving to confirm the remote URL answers as an MCP server."
              : null}
          </SettingsSection.FooterHint>
          <SettingsSection.FooterActions>
            <RequireScope check={writeCheck} level="component">
              <VerifyRemoteMcpUrlButton
                state={upstream.verify}
                url={upstream.draft}
                disabled={upstream.pending || upstream.invalid}
              />
              <FooterSaveButton
                pending={upstream.pending}
                disabled={
                  !upstream.dirty || upstream.invalid || upstream.pending
                }
                onClick={() => void handleSave()}
              />
            </RequireScope>
          </SettingsSection.FooterActions>
        </SettingsSection.Footer>
      </SettingsSection.Panel>
    </SettingsSection>
  );
}
