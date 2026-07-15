import { RequireScope } from "@/components/require-scope";
import { Checkbox } from "@/components/ui/checkbox";
import { Type } from "@/components/ui/type";
import { usePublishing } from "@/pages/mcp/usePublishing";
import type { McpEndpoint } from "@gram/client/models/components/mcpendpoint.js";
import type { McpServer } from "@gram/client/models/components/mcpserver.js";
import { Stack } from "@/components/ui/stack";
import { Button } from "@/components/ui/button";
import { FooterSaveButtonContent, SettingsSection } from "../SettingsSection";

export function PublishingSection({
  mcpServer,
  endpoints,
}: {
  mcpServer: McpServer;
  endpoints: McpEndpoint[];
}): JSX.Element {
  // A server is publishable once it serves traffic (visibility != disabled) and
  // has at least one endpoint to address it — mirroring the server-side attach
  // validation in collections.attachServerToCollection.
  const canPublish =
    mcpServer.visibility !== "disabled" && endpoints.length > 0;
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
      <Type muted small>
        {disabledMessage}
      </Type>
    );
  } else if (isLoading) {
    body = (
      <Type muted small>
        Loading collections...
      </Type>
    );
  } else if (collections.length === 0) {
    body = (
      <Type muted small>
        No collections available.
      </Type>
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
              <Type small className="font-medium">
                {collection.name}
              </Type>
              {collection.description && (
                <Type muted small>
                  {collection.description}
                </Type>
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
                <Button
                  variant="primary"
                  size="md"
                  disabled={isSaving}
                  onClick={() => void handleSave()}
                >
                  <FooterSaveButtonContent pending={isSaving} />
                </Button>
              </SettingsSection.FooterActions>
            )}
          </SettingsSection.Footer>
        </SettingsSection.Panel>
      </SettingsSection>
    </RequireScope>
  );
}
