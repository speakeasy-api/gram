import { SettingsSection } from "@/components/detail/settings-section";
import { useBuiltinMcpServers } from "@/components/mcp/useBuiltinMcpServers";
import { Button } from "@/components/ui/Button";
import { Field, FieldLabel } from "@/components/ui/Field";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/Select";
import { Text } from "@/components/ui/Text";
import { useProjectSlugForRequests } from "@/contexts/Sdk";
import type { BuiltinMcpServer } from "@gram/client/models/components/builtinmcpserver.js";
import type { MetaMcpMember } from "@gram/client/models/components/metamcpmember.js";
import { useListEnvironments } from "@gram/client/react-query/listEnvironments.js";
import { invalidateAllMetaMcpMembers } from "@gram/client/react-query/metaMcpMembers.js";
import { invalidateAllMetaMcpServers } from "@gram/client/react-query/metaMcpServers.js";
import { useSetBuiltinMetaMcpMemberMutation } from "@gram/client/react-query/setBuiltinMetaMcpMember.js";
import { useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { toast } from "sonner";

export function BuiltinGatewayMembersSection({
  gatewayId,
  members,
}: {
  gatewayId: string;
  members: MetaMcpMember[];
}) {
  const { servers, isAdmin } = useBuiltinMcpServers();
  if (!isAdmin || servers.length === 0) return null;
  return (
    <SettingsSection>
      <SettingsSection.Header>
        <SettingsSection.Title>Built-in servers</SettingsSection.Title>
        <SettingsSection.Description>
          Add code-owned tools to this gateway. Membership does not grant
          connection access; human callers cannot connect to these servers.
        </SettingsSection.Description>
      </SettingsSection.Header>
      <SettingsSection.Panel>
        <SettingsSection.Body>
          {servers.map((server) => {
            const member = members.find(
              (entry) => entry.builtinKey === server.key,
            );
            return (
              <BuiltinGatewayMemberForm
                key={`${gatewayId}:${server.id}:${member?.id ?? ""}:${member?.environmentId ?? ""}`}
                server={server}
                gatewayId={gatewayId}
                member={member}
                sortOrder={
                  Math.max(-1, ...members.map((entry) => entry.sortOrder)) + 1
                }
              />
            );
          })}
        </SettingsSection.Body>
      </SettingsSection.Panel>
    </SettingsSection>
  );
}

function BuiltinGatewayMemberForm({
  server,
  gatewayId,
  member,
  sortOrder,
}: {
  server: BuiltinMcpServer;
  gatewayId: string;
  member?: MetaMcpMember;
  sortOrder: number;
}) {
  const gramProject = useProjectSlugForRequests();
  const queryClient = useQueryClient();
  const { data, isLoading, isError } = useListEnvironments(
    { gramProject },
    undefined,
    { throwOnError: false },
  );
  const [environmentId, setEnvironmentId] = useState(
    member?.environmentId ?? "default",
  );
  const update = useSetBuiltinMetaMcpMemberMutation({
    onSuccess: async () => {
      await Promise.all([
        invalidateAllMetaMcpMembers(queryClient),
        invalidateAllMetaMcpServers(queryClient),
      ]);
      toast.success(
        member
          ? "Built-in gateway settings saved"
          : "Built-in server added to gateway",
      );
    },
    onError: (error) =>
      toast.error(
        `Could not update built-in gateway membership: ${error.message}`,
      ),
  });
  return (
    <div className="space-y-3">
      <Text className="font-medium">{server.name}</Text>
      <div className="flex items-end gap-3">
        <Field className="max-w-md">
          <FieldLabel htmlFor={`gateway-builtin-${server.key}`}>
            Environment override
          </FieldLabel>
          <Select
            value={environmentId}
            onValueChange={setEnvironmentId}
            disabled={isLoading || isError || update.isPending}
          >
            <SelectTrigger id={`gateway-builtin-${server.key}`}>
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="default">
                Use built-in default environment
              </SelectItem>
              {(data?.environments ?? []).map((environment) => (
                <SelectItem key={environment.id} value={environment.id}>
                  {environment.name}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </Field>
        <Button
          variant="secondary"
          disabled={
            update.isPending ||
            isError ||
            isLoading ||
            (!!member && environmentId === (member.environmentId ?? "default"))
          }
          onClick={() =>
            update.mutate({
              request: {
                gramProject,
                setBuiltinMetaMcpMemberRequestBody: {
                  metaMcpServerId: gatewayId,
                  builtinKey: server.key,
                  environmentId:
                    environmentId === "default" ? undefined : environmentId,
                  sortOrder: member?.sortOrder ?? sortOrder,
                },
              },
            })
          }
        >
          {update.isPending
            ? "Saving…"
            : member
              ? "Save override"
              : "Add to gateway"}
        </Button>
      </div>
      {isError && (
        <Text small className="text-destructive">
          Could not load project environments.
        </Text>
      )}
      <Text small muted>
        Only environments in this project can supply credentials. Remove this
        membership from the server list above.
      </Text>
    </div>
  );
}
