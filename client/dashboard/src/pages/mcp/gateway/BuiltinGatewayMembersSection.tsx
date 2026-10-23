import { useBuiltinMcpServers } from "@/components/mcp/useBuiltinMcpServers";
import { Button } from "@/components/ui/Button";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/Select";
import { Text } from "@/components/ui/Text";
import { useProjectSlugForRequests } from "@/contexts/Sdk";
import { useOrganization } from "@/contexts/Auth";
import { useRBAC } from "@/hooks/useRBAC";
import { useRoutes } from "@/routes";
import type { Environment } from "@gram/client/models/components/environment.js";
import type { MetaMcpMember } from "@gram/client/models/components/metamcpmember.js";
import { useListEnvironments } from "@gram/client/react-query/listEnvironments.js";
import { invalidateAllMetaMcpMembers } from "@gram/client/react-query/metaMcpMembers.js";
import { invalidateAllMetaMcpServers } from "@gram/client/react-query/metaMcpServers.js";
import { useSetBuiltinMetaMcpMemberMutation } from "@gram/client/react-query/setBuiltinMetaMcpMember.js";
import { useQueryClient } from "@tanstack/react-query";
import { Plus } from "lucide-react";
import { Link } from "react-router";
import { toast } from "sonner";

/**
 * What a viewer who cannot change the override sees. An override whose name
 * has not been resolved is not the same as having no override, so it never
 * reads as the server's default.
 */
function readOnlyEnvironmentLabel(
  environmentId: string | undefined,
  state: {
    environments: Environment[] | undefined;
    isLoading: boolean;
    isError: boolean;
  },
): string {
  if (!environmentId) return "Server default";
  const name = state.environments?.find(
    (entry) => entry.id === environmentId,
  )?.name;
  if (name) return name;
  if (state.isLoading) return "Loading…";
  return "Unavailable";
}

/** Unset means the member follows whatever the built-in server is set to. */
const SERVER_DEFAULT_VALUE = "server-default";

/**
 * Writes one built-in membership. Everything the gateway reads about its
 * members goes stale on success, including the inspection and describe caches
 * the Overview and Inspect tabs render from.
 */
function useSetBuiltinGatewayMember(gatewayId: string) {
  const gramProject = useProjectSlugForRequests();
  const queryClient = useQueryClient();
  const mutation = useSetBuiltinMetaMcpMemberMutation({
    onSuccess: async () => {
      await Promise.all([
        invalidateAllMetaMcpMembers(queryClient),
        invalidateAllMetaMcpServers(queryClient),
        queryClient.invalidateQueries({ queryKey: ["gatewayInspection"] }),
        queryClient.invalidateQueries({ queryKey: ["gatewayDescribeServer"] }),
      ]);
    },
    onError: (error) => {
      toast.error(`Couldn't update this server: ${error.message}`);
    },
  });
  return {
    isPending: mutation.isPending,
    save: (input: {
      builtinKey: string;
      environmentId?: string;
      sortOrder: number;
      onSuccess?: () => void;
    }): void => {
      mutation.mutate(
        {
          request: {
            gramProject,
            setBuiltinMetaMcpMemberRequestBody: {
              metaMcpServerId: gatewayId,
              builtinKey: input.builtinKey,
              environmentId: input.environmentId,
              sortOrder: input.sortOrder,
            },
          },
        },
        { onSuccess: input.onSuccess },
      );
    },
  };
}

/**
 * The built-in servers this gateway could still serve. Members themselves are
 * rows in the gateway's own member table — listing them again here would say
 * the same thing twice — so this section exists only while something is left
 * to add, and disappears once everything has been.
 */
export function BuiltinGatewayAddSection({
  gatewayId,
  members,
}: {
  gatewayId: string;
  members: MetaMcpMember[];
}): JSX.Element | null {
  const organization = useOrganization();
  const routes = useRoutes();
  const { hasScope, isLoading: permissionsLoading } = useRBAC();
  const { servers, isAdmin, isLoading } = useBuiltinMcpServers();
  const update = useSetBuiltinGatewayMember(gatewayId);
  const canWrite = !permissionsLoading && hasScope("mcp:write", gatewayId);
  const memberKeys = new Set(
    members.flatMap((member) => (member.builtinKey ? [member.builtinKey] : [])),
  );
  const available = servers.filter((server) => !memberKeys.has(server.key));

  // Only organization admins can attach a built-in, so for everyone else there
  // is nothing to offer and no point explaining what they cannot do.
  if (
    !isAdmin ||
    !hasScope("org:admin", organization.id) ||
    !canWrite ||
    isLoading ||
    available.length === 0
  ) {
    return null;
  }

  const nextSortOrder =
    Math.max(-1, ...members.map((member) => member.sortOrder)) + 1;

  return (
    <div className="border-border flex flex-col gap-3 border border-dashed px-6 py-5">
      <div>
        <Text className="font-medium">Add a built-in server</Text>
        <Text small muted>
          Speakeasy maintains these, so there is nothing to build or deploy.
          They serve through this gateway like any other member.
        </Text>
      </div>
      <ul className="flex flex-col gap-2">
        {available.map((server, index) => (
          <li
            key={server.id}
            className="flex items-center justify-between gap-4"
          >
            <div className="min-w-0">
              <Link
                to={routes.mcp.builtin.overview.href(server.key)}
                className="truncate font-medium hover:underline"
              >
                {server.name}
              </Link>
              <Text small muted className="truncate">
                {server.tools.length} tools
              </Text>
            </div>
            <Button
              variant="secondary"
              size="sm"
              disabled={update.isPending}
              onClick={() =>
                update.save({
                  builtinKey: server.key,
                  sortOrder: nextSortOrder + index,
                  onSuccess: () => {
                    toast.success(`${server.name} added to this gateway`);
                  },
                })
              }
            >
              <Button.LeftIcon>
                <Plus className="size-4" />
              </Button.LeftIcon>
              <Button.Text>Add</Button.Text>
            </Button>
          </li>
        ))}
      </ul>
    </div>
  );
}

/**
 * A built-in member's credential source, shown in the member table beside the
 * member it belongs to. Built-in servers carry a project-wide default of their
 * own; a gateway that needs a different workspace overrides it here.
 */
export function BuiltinMemberEnvironmentCell({
  gatewayId,
  member,
  canWrite,
}: {
  gatewayId: string;
  member: MetaMcpMember;
  canWrite: boolean;
}): JSX.Element {
  const gramProject = useProjectSlugForRequests();
  const { data, isLoading, isError } = useListEnvironments(
    { gramProject },
    undefined,
    { throwOnError: false },
  );
  const update = useSetBuiltinGatewayMember(gatewayId);
  const value = member.environmentId ?? SERVER_DEFAULT_VALUE;

  if (!canWrite) {
    return (
      <Text small muted className="truncate">
        {readOnlyEnvironmentLabel(member.environmentId, {
          environments: data?.environments,
          isLoading,
          isError,
        })}
      </Text>
    );
  }

  return (
    <Select
      value={value}
      disabled={isLoading || isError || update.isPending}
      onValueChange={(next) =>
        update.save({
          builtinKey: member.builtinKey ?? "",
          environmentId: next === SERVER_DEFAULT_VALUE ? undefined : next,
          sortOrder: member.sortOrder,
          onSuccess: () => {
            toast.success("Environment updated");
          },
        })
      }
    >
      <SelectTrigger
        aria-label={`Environment for ${member.mcpServerName ?? member.builtinKey}`}
        className="h-8 max-w-[200px]"
      >
        <SelectValue />
      </SelectTrigger>
      <SelectContent>
        <SelectItem value={SERVER_DEFAULT_VALUE}>Server default</SelectItem>
        {(data?.environments ?? []).map((environment) => (
          <SelectItem key={environment.id} value={environment.id}>
            {environment.name}
          </SelectItem>
        ))}
      </SelectContent>
    </Select>
  );
}
