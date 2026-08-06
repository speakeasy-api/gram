import { RequireScope } from "@/components/require-scope";
import { Checkbox } from "@/components/ui/Checkbox";
import { Text } from "@/components/ui/Text";
import { usePublishing } from "@/pages/mcp/usePublishing";
import type { McpEndpoint } from "@gram/client/models/components/mcpendpoint.js";
import type { McpServer } from "@gram/client/models/components/mcpserver.js";
import { Button } from "@/components/ui/Button";
import { Stack } from "@/components/ui/Stack";
import { FooterSaveButton, SettingsSection } from "../SettingsSection";

export function PublishingSection({
  mcpServer,
  endpoints,
}: {
  mcpServer: McpServer;
  endpoints: McpEndpoint[];
}): JSX.Element {
  // Unproxied servers are addressed directly by their vendor URL and never
  // have an mcp_endpoints row (there's no Gram-managed endpoint to create for
  // them, see ServerUrlSection, which is hidden for these servers), so they
  // don't need one to be publishable.
  const isUnproxied = !!mcpServer.unproxiedMcpServerId;
  // A server is publishable once it serves traffic (visibility != disabled)
  // and, for Gram-addressed servers, has at least one endpoint, mirroring the
  // server-side attach validation in collections.attachServerToCollection.
  const canPublish =
    mcpServer.visibility !== "disabled" &&
    (isUnproxied || endpoints.length > 0);
  const disabledMessage =
    mcpServer.visibility === "disabled"
      ? "Enable this MCP server before publishing it to a collection."
      : "Add an endpoint to this MCP server before publishing it to a collection.";

  const {
    collections,
    effectiveSelected,
    hasChanges,
    isSaving,
    isLoading,
    toggleCollection,
    handleSave,
    handleDiscard,
  } = usePublishing({ kind: "mcpServer", mcpServerId: mcpServer.id });

  let body: React.ReactNode;
  if (!canPublish) {
    body = (
      <Text muted small>
        {disabledMessage}
      </Text>
    );
  } else if (isLoading) {
    body = (
      <Text muted small>
        Loading collections...
      </Text>
    );
  } else if (collections.length === 0) {
    body = (
      <Text muted small>
        No collections available.
      </Text>
    );
  } else {
    body = (
      <Stack direction="vertical" gap={2}>
        {collections.map((collection) => (
          <label
            key={collection.id}
            className="flex cursor-pointer items-center gap-3"
          >
            <Checkbox
              checked={effectiveSelected.has(collection.id)}
              disabled={isSaving}
              onCheckedChange={() => toggleCollection(collection.id)}
            />
            <Stack direction="vertical" gap={0}>
              <Text small className="font-medium">
                {collection.name}
              </Text>
              {collection.description && (
                <Text muted small>
                  {collection.description}
                </Text>
              )}
            </Stack>
          </label>
        ))}
      </Stack>
    );
  }

  // Publishing attaches the server to an org-level collection, which the
  // collections service authorizes as org:admin (see AttachServer /
  // DetachServer). Gate to match: non-admins see the card greyed out with a
  // permission tooltip rather than controls that would 403.
  return (
    <RequireScope
      scope="org:admin"
      level="component"
      className="w-full"
      reason="Only organization admins can publish servers to collections."
    >
      <SettingsSection>
        <SettingsSection.Header>
          <SettingsSection.Title>Publishing</SettingsSection.Title>
          <SettingsSection.Description>
            Publish this server to collections so it can be discovered and
            installed by others in your organization.
          </SettingsSection.Description>
        </SettingsSection.Header>
        <SettingsSection.Panel>
          <SettingsSection.Body>{body}</SettingsSection.Body>
          <SettingsSection.Footer>
            <SettingsSection.FooterHint>
              Published collections list this server for discovery and install.
            </SettingsSection.FooterHint>
            {hasChanges && (
              <SettingsSection.FooterActions>
                <Button
                  variant="secondary"
                  size="md"
                  disabled={isSaving}
                  onClick={handleDiscard}
                >
                  <Button.Text>Discard</Button.Text>
                </Button>
                <FooterSaveButton
                  pending={isSaving}
                  disabled={isSaving}
                  onClick={() => void handleSave()}
                />
              </SettingsSection.FooterActions>
            )}
          </SettingsSection.Footer>
        </SettingsSection.Panel>
      </SettingsSection>
    </RequireScope>
  );
}
