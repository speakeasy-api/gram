import { RequireScope } from "@/components/require-scope";
import { Field, FieldLabel } from "@/components/ui/field";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { toolVariationsGroupDisplayName } from "@/lib/toolVariationGroups";
import type { McpServer } from "@gram/client/models/components";
import {
  invalidateAllGetMcpServer,
  invalidateAllMcpServers,
  invalidateAllToolVariationGroups,
  useCreateGlobalToolVariationGroupMutation,
  useToolVariationGroups,
  useUpdateMcpServerMutation,
} from "@gram/client/react-query/index.js";
import { Button } from "@speakeasy-api/moonshine";
import { useQueryClient } from "@tanstack/react-query";
import { Loader2 } from "lucide-react";
import { useEffect, useState } from "react";
import { toast } from "sonner";
import { FooterSaveButtonContent, SettingsSection } from "../SettingsSection";

// Radix Select disallows an empty-string value, so the "Disabled" option needs
// a sentinel that maps back to null (filtering off) when persisted.
const DISABLED_VALUE = "__disabled__";

export function ToolFilteringSection({
  mcpServer,
}: {
  mcpServer: McpServer;
}): JSX.Element {
  const queryClient = useQueryClient();
  const groupsQuery = useToolVariationGroups();
  const groups = groupsQuery.data?.groups ?? [];

  const currentValue = mcpServer.toolVariationsGroupId ?? DISABLED_VALUE;
  const [draft, setDraft] = useState(currentValue);

  // Re-sync the draft when the persisted value changes underneath us.
  useEffect(() => {
    setDraft(currentValue);
  }, [currentValue]);

  const notifyError = (error: unknown) =>
    toast.error(
      error instanceof Error
        ? error.message
        : "Failed to update tool filtering settings",
    );

  const updateMcpServer = useUpdateMcpServerMutation({
    onSuccess: async () => {
      await Promise.all([
        invalidateAllGetMcpServer(queryClient, { refetchType: "all" }),
        invalidateAllMcpServers(queryClient, { refetchType: "all" }),
      ]);
      toast.success("Tool filtering settings updated");
    },
    onError: notifyError,
  });

  // mcpServers.update is a full-record replace for the optional UUID
  // references, so every field has to be re-sent or it gets nulled.
  const applyGroup = (groupId: string | null) => {
    updateMcpServer.mutate({
      request: {
        updateMcpServerForm: {
          id: mcpServer.id,
          name: mcpServer.name ?? undefined,
          remoteMcpServerId: mcpServer.remoteMcpServerId ?? undefined,
          tunneledMcpServerId: mcpServer.tunneledMcpServerId ?? undefined,
          toolsetId: mcpServer.toolsetId ?? undefined,
          environmentId: mcpServer.environmentId ?? undefined,
          userSessionIssuerId: mcpServer.userSessionIssuerId ?? undefined,
          visibility: mcpServer.visibility,
          toolVariationsGroupId: groupId ?? undefined,
        },
      },
    });
  };

  const createGroup = useCreateGlobalToolVariationGroupMutation({
    onSuccess: async (data) => {
      await invalidateAllToolVariationGroups(queryClient, {
        refetchType: "all",
      });
      // Enabling for the first time both materializes the project-default
      // group and assigns it to this server, so filtering is actually on in a
      // single click rather than leaving the user on "Disabled".
      applyGroup(data.group.id);
    },
    onError: (error) =>
      toast.error(
        error instanceof Error ? error.message : "Failed to create tool group",
      ),
  });

  const isSaving = updateMcpServer.isPending || createGroup.isPending;
  const dirty = draft !== currentValue;
  const hasGroups = groups.length > 0;
  let enableButtonContent = <Button.Text>Enable</Button.Text>;
  if (createGroup.isPending) {
    enableButtonContent = (
      <>
        <Button.LeftIcon>
          <Loader2 className="size-4 animate-spin" />
        </Button.LeftIcon>
        <Button.Text>Enabling</Button.Text>
      </>
    );
  }

  return (
    <SettingsSection>
      <SettingsSection.Header>
        <SettingsSection.Title>Tool Filtering</SettingsSection.Title>
        <SettingsSection.Description>
          Filter the tools exposed by this server based on their tags. All tools
          are returned by default unless filtering is enabled and a `tags` query
          parameter is provided.
        </SettingsSection.Description>
      </SettingsSection.Header>
      <SettingsSection.Panel>
        <SettingsSection.Body>
          <Field>
            <FieldLabel htmlFor="mcp-server-tool-filtering" className="sr-only">
              Tool filtering
            </FieldLabel>
            {hasGroups ? (
              <RequireScope scope="mcp:write" level="component">
                <Select
                  value={draft}
                  disabled={isSaving}
                  onValueChange={(value) => setDraft(value)}
                >
                  <SelectTrigger
                    id="mcp-server-tool-filtering"
                    className="w-72"
                  >
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value={DISABLED_VALUE}>Disabled</SelectItem>
                    {groups.map((group) => (
                      <SelectItem key={group.id} value={group.id}>
                        {toolVariationsGroupDisplayName(group.name)}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </RequireScope>
            ) : (
              <RequireScope scope="mcp:write" level="component">
                <Button
                  variant="secondary"
                  size="md"
                  disabled={isSaving || groupsQuery.isLoading}
                  onClick={() => createGroup.mutate({})}
                >
                  {enableButtonContent}
                </Button>
              </RequireScope>
            )}
          </Field>
        </SettingsSection.Body>
        <SettingsSection.Footer>
          <SettingsSection.FooterHint>
            Filtering applies to every endpoint on this server.
          </SettingsSection.FooterHint>
          {hasGroups && (
            <SettingsSection.FooterActions>
              <RequireScope scope="mcp:write" level="component">
                <Button
                  variant="primary"
                  size="md"
                  disabled={!dirty || isSaving}
                  onClick={() =>
                    applyGroup(draft === DISABLED_VALUE ? null : draft)
                  }
                >
                  <FooterSaveButtonContent
                    pending={updateMcpServer.isPending}
                  />
                </Button>
              </RequireScope>
            </SettingsSection.FooterActions>
          )}
        </SettingsSection.Footer>
      </SettingsSection.Panel>
    </SettingsSection>
  );
}
