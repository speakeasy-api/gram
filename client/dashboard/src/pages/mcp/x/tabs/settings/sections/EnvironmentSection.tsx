import { RequireScope } from "@/components/require-scope";
import { Field, FieldLabel } from "@/components/ui/field";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import type { McpServer } from "@gram/client/models/components/mcpserver.js";
import { invalidateAllGetMcpServer } from "@gram/client/react-query/getMcpServer.js";
import { useListEnvironments } from "@gram/client/react-query/listEnvironments.js";
import { invalidateAllMcpServers } from "@gram/client/react-query/mcpServers.js";
import { useUpdateMcpServerMutation } from "@gram/client/react-query/updateMcpServer.js";
import { Button } from "@speakeasy-api/moonshine";
import { useQueryClient } from "@tanstack/react-query";
import { useEffect, useState } from "react";
import { toast } from "sonner";
import { FooterSaveButtonContent, SettingsSection } from "../SettingsSection";

const NONE_VALUE = "__none__";

function buildUpdateForm(
  mcpServer: McpServer,
  environmentId: string | undefined,
) {
  return {
    id: mcpServer.id,
    name: mcpServer.name ?? undefined,
    remoteMcpServerId: mcpServer.remoteMcpServerId ?? undefined,
    tunneledMcpServerId: mcpServer.tunneledMcpServerId ?? undefined,
    toolsetId: mcpServer.toolsetId ?? undefined,
    environmentId,
    userSessionIssuerId: mcpServer.userSessionIssuerId ?? undefined,
    toolVariationsGroupId: mcpServer.toolVariationsGroupId ?? undefined,
    visibility: mcpServer.visibility,
  };
}

export function EnvironmentSection({
  mcpServer,
}: {
  mcpServer: McpServer;
}): JSX.Element | null {
  if (!mcpServer.remoteMcpServerId) {
    return null;
  }

  return <EnvironmentSectionContent mcpServer={mcpServer} />;
}

function EnvironmentSectionContent({
  mcpServer,
}: {
  mcpServer: McpServer;
}): JSX.Element {
  const queryClient = useQueryClient();
  const environmentsQuery = useListEnvironments();
  const environments = environmentsQuery.data?.environments ?? [];

  const currentValue = mcpServer.environmentId ?? NONE_VALUE;
  const [draft, setDraft] = useState(currentValue);

  useEffect(() => {
    setDraft(currentValue);
  }, [currentValue]);

  const updateMcpServer = useUpdateMcpServerMutation({
    onSuccess: async () => {
      await Promise.all([
        invalidateAllGetMcpServer(queryClient, { refetchType: "all" }),
        invalidateAllMcpServers(queryClient, { refetchType: "all" }),
      ]);
      toast.success("Environment settings updated");
    },
    onError: (error) =>
      toast.error(
        error instanceof Error
          ? error.message
          : "Failed to update environment settings",
      ),
  });

  const applyEnvironment = (environmentId: string | undefined) => {
    updateMcpServer.mutate({
      request: {
        updateMcpServerForm: buildUpdateForm(mcpServer, environmentId),
      },
    });
  };

  const dirty = draft !== currentValue;
  const isSaving = updateMcpServer.isPending;

  return (
    <SettingsSection>
      <SettingsSection.Header>
        <SettingsSection.Title>Environment</SettingsSection.Title>
        <SettingsSection.Description>
          Attach a project environment to supply upstream credentials for remote
          MCP headers configured to source values from the environment.
        </SettingsSection.Description>
      </SettingsSection.Header>
      <SettingsSection.Panel>
        <SettingsSection.Body>
          <Field>
            <FieldLabel htmlFor="mcp-server-environment" className="sr-only">
              Attached environment
            </FieldLabel>
            <RequireScope scope="mcp:write" level="component">
              <Select
                value={draft}
                disabled={isSaving || environmentsQuery.isLoading}
                onValueChange={(value) => setDraft(value)}
              >
                <SelectTrigger id="mcp-server-environment" className="w-72">
                  <SelectValue placeholder="Select an environment" />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value={NONE_VALUE}>None</SelectItem>
                  {environments.map((environment) => (
                    <SelectItem key={environment.id} value={environment.id}>
                      {environment.name}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </RequireScope>
          </Field>
        </SettingsSection.Body>
        <SettingsSection.Footer>
          <SettingsSection.FooterHint>
            Only configured remote headers with empty values are filled from the
            attached environment.
          </SettingsSection.FooterHint>
          <SettingsSection.FooterActions>
            <RequireScope scope="mcp:write" level="component">
              <Button
                variant="primary"
                size="md"
                disabled={!dirty || isSaving}
                onClick={() =>
                  applyEnvironment(draft === NONE_VALUE ? undefined : draft)
                }
              >
                <FooterSaveButtonContent pending={isSaving} />
              </Button>
            </RequireScope>
          </SettingsSection.FooterActions>
        </SettingsSection.Footer>
      </SettingsSection.Panel>
    </SettingsSection>
  );
}
