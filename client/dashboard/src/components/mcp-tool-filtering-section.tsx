import { RequireScope } from "@/components/require-scope";
import { Heading } from "@/components/ui/heading";
import { toastError } from "@/lib/toast-error";
import { toolVariationsGroupDisplayName } from "@/lib/toolVariationGroups";
import { cn } from "@/lib/utils";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Type } from "@/components/ui/type";
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
import { Button } from "@/components/ui/button";
import { useQueryClient } from "@tanstack/react-query";
import { Loader2 } from "lucide-react";
import { toast } from "sonner";

// Radix Select disallows an empty-string value, so the "Disabled" option needs
// a sentinel that maps back to null (filtering off) when persisted.
const DISABLED_VALUE = "__disabled__";

// The variations group is assigned to either a toolset (via
// toolsets.setToolVariationsGroup) or an mcp_server (via mcpServers.update).
// The caller passes the target; this component owns the persistence so both
// settings pages stay a single drop-in.
export type MCPToolFilteringTarget =
  | { kind: "toolset"; slug: string; currentGroupId: string | null | undefined }
  | { kind: "mcpServer"; mcpServer: McpServer };

export function MCPToolFilteringSection({
  target,
  className,
}: {
  target: MCPToolFilteringTarget;
  className?: string;
}): JSX.Element {
  const queryClient = useQueryClient();
  const groupsQuery = useToolVariationGroups();
  const groups = groupsQuery.data?.groups ?? [];

  const notifySaved = () => toast.success("Tool filtering settings updated");
  const notifyError = (error: unknown) =>
    toastError(error, "Failed to update tool filtering settings");

  const setToolsetGroup = useSetToolsetToolVariationsGroupMutation({
    onSuccess: async () => {
      await invalidateAllToolset(queryClient, { refetchType: "all" });
      notifySaved();
    },
    onError: notifyError,
  });
  const updateMcpServer = useUpdateMcpServerMutation({
    onSuccess: async () => {
      await Promise.all([
        invalidateAllGetMcpServer(queryClient, { refetchType: "all" }),
        invalidateAllMcpServers(queryClient, { refetchType: "all" }),
      ]);
      notifySaved();
    },
    onError: notifyError,
  });

  const applyGroup = (groupId: string | null) => {
    if (target.kind === "toolset") {
      setToolsetGroup.mutate({
        request: {
          slug: target.slug,
          setToolVariationsGroupRequestBody: {
            toolVariationsGroupId: groupId ?? undefined,
          },
        },
      });
      return;
    }

    // mcpServers.update is a full-record replace for the optional UUID
    // references, so every field has to be re-sent or it gets nulled.
    const server = target.mcpServer;
    updateMcpServer.mutate({
      request: {
        updateMcpServerForm: {
          id: server.id,
          name: server.name ?? undefined,
          remoteMcpServerId: server.remoteMcpServerId ?? undefined,
          toolsetId: server.toolsetId ?? undefined,
          environmentId: server.environmentId ?? undefined,
          visibility: server.visibility,
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
      // group and assigns it to this target, so filtering is actually on in a
      // single click rather than leaving the user on "Disabled".
      applyGroup(data.group.id);
    },
    onError: (error) => toastError(error, "Failed to create tool group"),
  });

  const currentGroupId =
    target.kind === "toolset"
      ? target.currentGroupId
      : target.mcpServer.toolVariationsGroupId;
  const isSaving =
    setToolsetGroup.isPending ||
    updateMcpServer.isPending ||
    createGroup.isPending;

  return (
    <div className={cn("space-y-4", className)}>
      <div>
        <Heading variant="h4">Enable Tool Filtering</Heading>
        <Type muted small className="mt-2 max-w-2xl">
          Enable tool filtering based on underlying tool tags. All tools are
          returned by default unless enabled and{" "}
          <code className="font-mono">tags</code> URL query parameter is
          provided.
        </Type>
      </div>

      {groups.length === 0 ? (
        <RequireScope scope="mcp:write" level="component">
          <Button
            variant="secondary"
            size="md"
            disabled={isSaving || groupsQuery.isLoading}
            onClick={() => createGroup.mutate({})}
          >
            {createGroup.isPending ? (
              <>
                <Button.LeftIcon>
                  <Loader2 className="size-4 animate-spin" />
                </Button.LeftIcon>
                <Button.Text>Enabling</Button.Text>
              </>
            ) : (
              <Button.Text>Enable</Button.Text>
            )}
          </Button>
        </RequireScope>
      ) : (
        <RequireScope scope="mcp:write" level="component">
          <Select
            value={currentGroupId ?? DISABLED_VALUE}
            disabled={isSaving}
            onValueChange={(value) =>
              applyGroup(value === DISABLED_VALUE ? null : value)
            }
          >
            <SelectTrigger className="w-72">
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
      )}
    </div>
  );
}
