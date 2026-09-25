import { RequireScope } from "@/components/require-scope";
import { useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import type { MetaMcpServer } from "@gram/client/models/components/metamcpserver.js";
import { invalidateAllGetMetaMcpServer } from "@gram/client/react-query/getMetaMcpServer.js";
import { invalidateAllMetaMcpServers } from "@gram/client/react-query/metaMcpServers.js";
import { useUpdateMetaMcpServerMutation } from "@gram/client/react-query/updateMetaMcpServer.js";
import {
  SettingsSection,
  FooterSaveButton,
} from "@/components/detail/settings-section";
import { Field, FieldError, FieldLabel } from "@/components/ui/Field";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/Select";
import { ReleaseStageBadge } from "@/components/release-stage-badge";
import { useRBAC } from "@/hooks/useRBAC";

export function GatewayDiscoverySection({
  metaMcpServer,
}: {
  metaMcpServer: MetaMcpServer;
}): JSX.Element | null {
  const { hasScope } = useRBAC();
  const canWrite = hasScope("mcp:write", metaMcpServer.projectId);
  const [mode, setMode] = useState(metaMcpServer.discoveryMode);
  const queryClient = useQueryClient();
  const update = useUpdateMetaMcpServerMutation({
    onSuccess: async () => {
      await Promise.all([
        invalidateAllGetMetaMcpServer(queryClient, { refetchType: "all" }),
        invalidateAllMetaMcpServers(queryClient, { refetchType: "all" }),
        queryClient.invalidateQueries({ queryKey: ["gatewayInspection"] }),
      ]);
      toast.success("Gateway discovery updated");
    },
  });
  if (!metaMcpServer.discoveryModesEnabled) return null;
  return (
    <SettingsSection id="discovery">
      <SettingsSection.Header>
        <SettingsSection.Title>
          Tool discovery <ReleaseStageBadge stage="preview" />
        </SettingsSection.Title>
        <SettingsSection.Description>
          Progressive discovers tools as needed. Direct lists every available
          tool and works with clients that cannot discover tools progressively.
        </SettingsSection.Description>
      </SettingsSection.Header>
      <SettingsSection.Panel>
        <SettingsSection.Body>
          <Field
            data-invalid={update.isError || undefined}
            className="max-w-md"
          >
            <FieldLabel htmlFor="gateway-discovery-mode">
              Default mode
            </FieldLabel>
            <Select
              value={mode}
              onValueChange={(value) => {
                if (value === "direct" || value === "progressive")
                  setMode(value);
              }}
              disabled={!canWrite || update.isPending}
            >
              <SelectTrigger id="gateway-discovery-mode">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="progressive">Progressive</SelectItem>
                <SelectItem value="direct">Direct</SelectItem>
              </SelectContent>
            </Select>
            {update.isError && <FieldError>{update.error.message}</FieldError>}
          </Field>
        </SettingsSection.Body>
        <SettingsSection.Footer>
          <SettingsSection.FooterHint>
            Applies to connections using the gateway default. Clients may need
            to reconnect to refresh their tool list.
          </SettingsSection.FooterHint>
          <SettingsSection.FooterActions>
            <RequireScope
              scope="mcp:write"
              resourceId={metaMcpServer.projectId}
              level="component"
            >
              <FooterSaveButton
                pending={update.isPending}
                disabled={
                  !canWrite ||
                  mode === metaMcpServer.discoveryMode ||
                  update.isPending
                }
                onClick={() =>
                  update.mutate({
                    request: {
                      updateMetaMcpServerForm: {
                        id: metaMcpServer.id,
                        name: metaMcpServer.name,
                        discoveryMode: mode,
                      },
                    },
                  })
                }
              />
            </RequireScope>
          </SettingsSection.FooterActions>
        </SettingsSection.Footer>
      </SettingsSection.Panel>
    </SettingsSection>
  );
}
