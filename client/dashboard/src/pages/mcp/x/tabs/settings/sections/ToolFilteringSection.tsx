import { RequireScope } from "@/components/require-scope";
import { Field, FieldLabel } from "@/components/ui/Field";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/Select";
import { Toolset } from "@/lib/toolTypes";
import { toolVariationsGroupDisplayName } from "@/lib/toolVariationGroups";
import type { McpServer } from "@gram/client/models/components/mcpserver.js";
import { useCreateGlobalToolVariationGroupMutation } from "@gram/client/react-query/createGlobalToolVariationGroup.js";
import { invalidateAllGetMcpServer } from "@gram/client/react-query/getMcpServer.js";
import { invalidateAllMcpServers } from "@gram/client/react-query/mcpServers.js";
import { useSetToolsetToolVariationsGroupMutation } from "@gram/client/react-query/setToolsetToolVariationsGroup.js";
import {
  invalidateAllToolVariationGroups,
  useToolVariationGroups,
} from "@gram/client/react-query/toolVariationGroups.js";
import { invalidateAllToolset } from "@gram/client/react-query/toolset.js";
import { useUpdateMcpServerMutation } from "@gram/client/react-query/updateMcpServer.js";
import { Button } from "@/components/ui/Button";
import { useQueryClient } from "@tanstack/react-query";
import { Loader2 } from "lucide-react";
import { useEffect, useState } from "react";
import { toast } from "sonner";
import { FooterSaveButton, SettingsSection } from "../SettingsSection";

// Radix Select disallows an empty-string value, so the "Disabled" option needs
// a sentinel that maps back to null (filtering off) when persisted.
const DISABLED_VALUE = "__disabled__";

export function ToolFilteringSection({
  mcpServer,
  backingToolset,
}: {
  mcpServer: McpServer;
  /**
   * When set, the variations group lives on the backing toolset: reads come
   * from — and writes go to — the toolset, matching where existing hosted
   * servers keep their filtering config. The server-side runtime resolves the
   * wrapper's own group first and falls back to the toolset's.
   */
  backingToolset?: Toolset;
}): JSX.Element {
  const queryClient = useQueryClient();
  const groupsQuery = useToolVariationGroups();
  const groups = groupsQuery.data?.groups ?? [];

  const persistedGroupId = backingToolset
    ? (mcpServer.toolVariationsGroupId ??
      backingToolset.toolVariationsGroupId ??
      null)
    : (mcpServer.toolVariationsGroupId ?? null);
  const currentValue = persistedGroupId ?? DISABLED_VALUE;
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

  const setToolsetGroup = useSetToolsetToolVariationsGroupMutation({
    onSuccess: async () => {
      await Promise.all([
        invalidateAllToolset(queryClient, { refetchType: "all" }),
        invalidateAllGetMcpServer(queryClient, { refetchType: "all" }),
        invalidateAllMcpServers(queryClient, { refetchType: "all" }),
      ]);
      toast.success("Tool filtering settings updated");
    },
    onError: notifyError,
  });

  const applyGroup = (groupId: string | null) => {
    if (backingToolset) {
      setToolsetGroup.mutate({
        request: {
          slug: backingToolset.slug,
          setToolVariationsGroupRequestBody: {
            toolVariationsGroupId: groupId ?? undefined,
          },
        },
      });
      return;
    }

    // mcpServers.update is a full-record replace for the optional UUID
    // references, so every field has to be re-sent or it gets nulled.
    updateMcpServer.mutate({
      request: {
        updateMcpServerForm: {
          id: mcpServer.id,
          name: mcpServer.name ?? undefined,
          remoteMcpServerId: mcpServer.remoteMcpServerId ?? undefined,
          tunneledMcpServerId: mcpServer.tunneledMcpServerId ?? undefined,
          toolsetId: mcpServer.toolsetId ?? undefined,
          environmentId: mcpServer.environmentId ?? undefined,
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

  const isSaving =
    updateMcpServer.isPending ||
    setToolsetGroup.isPending ||
    createGroup.isPending;
  const dirty = draft !== currentValue;
  const hasGroups = groups.length > 0;

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
                  {createGroup.isPending && (
                    <Button.LeftIcon>
                      <Loader2 className="size-4 animate-spin" />
                    </Button.LeftIcon>
                  )}
                  <Button.Text>
                    {createGroup.isPending ? "Enabling" : "Enable"}
                  </Button.Text>
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
                <FooterSaveButton
                  pending={
                    updateMcpServer.isPending || setToolsetGroup.isPending
                  }
                  disabled={!dirty || isSaving}
                  onClick={() =>
                    applyGroup(draft === DISABLED_VALUE ? null : draft)
                  }
                />
              </RequireScope>
            </SettingsSection.FooterActions>
          )}
        </SettingsSection.Footer>
      </SettingsSection.Panel>
    </SettingsSection>
  );
}
