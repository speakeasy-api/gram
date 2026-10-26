import { isKnownEnumValue } from "@/lib/enum";
import { UpdateMetaMcpServerFormDiscoveryMode } from "@gram/client/models/components/updatemetamcpserverform.js";
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
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@/components/ui/Tooltip";
import { ReleaseStageBadge } from "@/components/release-stage-badge";
import { useRBAC } from "@/hooks/useRBAC";
import { useOrganization } from "@/contexts/Auth";
import { useProductFeatures } from "@gram/client/react-query/productFeatures.js";

export function GatewayDiscoverySection({
  metaMcpServer,
}: {
  metaMcpServer: MetaMcpServer;
}): JSX.Element {
  const { hasScope } = useRBAC();
  const canWrite = hasScope("mcp:write", metaMcpServer.projectId);
  const [mode, setMode] = useState(metaMcpServer.discoveryMode);
  const organization = useOrganization();
  const features = useProductFeatures(
    { organizationId: organization.id },
    undefined,
    { throwOnError: false },
  );
  // Code Mode is rolled out per organization by Speakeasy. Organizations
  // without it still see it, disabled, so they know to ask for it. Only a
  // successful read says the organization is not enabled; while the read is
  // loading or failed (members without org:read), the gateway's own
  // availability decides.
  const entitlementKnown = features.data !== undefined;
  const notEnabled = features.data?.codeModeEnabled === false;
  // Code Mode is never offered to anonymous callers.
  const requiresSignIn = !metaMcpServer.userSessionIssuerId;
  const codeModeSelectable =
    metaMcpServer.codeModeEnabled === true && !notEnabled;
  const supportsEditing = isKnownEnumValue(
    UpdateMetaMcpServerFormDiscoveryMode,
    metaMcpServer.discoveryMode,
  );
  const knownDraft = isKnownEnumValue(
    UpdateMetaMcpServerFormDiscoveryMode,
    mode,
  );
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
  return (
    <SettingsSection id="discovery">
      <SettingsSection.Header>
        <SettingsSection.Title>
          Tool discovery <ReleaseStageBadge stage="preview" />
        </SettingsSection.Title>
        <SettingsSection.Description>
          Progressive discovers tools as needed. Direct lists every available
          tool and works with clients that cannot discover tools progressively.
          Code Mode exposes one execute tool for discovering and calling tools
          in Python.
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
              value={supportsEditing && knownDraft ? mode : ""}
              onValueChange={(value) => {
                if (
                  canWrite &&
                  supportsEditing &&
                  !update.isPending &&
                  isKnownEnumValue(
                    UpdateMetaMcpServerFormDiscoveryMode,
                    value,
                  ) &&
                  (value !== "code_mode" || codeModeSelectable)
                )
                  setMode(value);
              }}
              disabled={!canWrite || !supportsEditing || update.isPending}
            >
              <SelectTrigger id="gateway-discovery-mode">
                <SelectValue placeholder="Unknown mode" />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="progressive">Progressive</SelectItem>
                <SelectItem value="direct">Direct</SelectItem>
                {notEnabled ? (
                  // A disabled item ignores the pointer, so the wrapper
                  // receives the hover that opens the tooltip.
                  <Tooltip>
                    <TooltipTrigger asChild>
                      <div>
                        <SelectItem value="code_mode" disabled>
                          Code Mode (not enabled)
                        </SelectItem>
                      </div>
                    </TooltipTrigger>
                    <TooltipContent side="right">
                      Code Mode is not enabled for your organization. Contact
                      Speakeasy to turn it on.
                    </TooltipContent>
                  </Tooltip>
                ) : (
                  <SelectItem value="code_mode" disabled={!codeModeSelectable}>
                    {requiresSignIn
                      ? "Code Mode (requires sign-in)"
                      : codeModeSelectable
                        ? "Code Mode"
                        : "Code Mode (unavailable)"}
                  </SelectItem>
                )}
              </SelectContent>
            </Select>
            {metaMcpServer.discoveryMode === "code_mode" &&
              !codeModeSelectable && (
                <p className="text-muted-foreground text-sm">
                  {notEnabled
                    ? "Code Mode is not enabled for your organization; contact Speakeasy to turn it on."
                    : requiresSignIn
                      ? "Code Mode requires sign-in, and this gateway allows anonymous callers."
                      : entitlementKnown
                        ? "The code runtime is unavailable."
                        : "Code Mode is unavailable for this gateway."}{" "}
                  Connections currently use Progressive discovery; the saved
                  default remains Code Mode.
                </p>
              )}
            {update.isError && <FieldError>{update.error.message}</FieldError>}
          </Field>
        </SettingsSection.Body>
        <SettingsSection.Footer>
          <SettingsSection.FooterHint>
            {supportsEditing
              ? "Applies to connections using the gateway default. Clients may need to reconnect to refresh their tool list."
              : "Refresh the page before changing this gateway’s unsupported discovery mode."}
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
                  !supportsEditing ||
                  !knownDraft ||
                  mode === metaMcpServer.discoveryMode ||
                  update.isPending ||
                  (mode === "code_mode" && !codeModeSelectable)
                }
                onClick={() => {
                  if (
                    !canWrite ||
                    !supportsEditing ||
                    update.isPending ||
                    !isKnownEnumValue(
                      UpdateMetaMcpServerFormDiscoveryMode,
                      mode,
                    ) ||
                    (mode === "code_mode" && !codeModeSelectable)
                  )
                    return;
                  update.mutate({
                    request: {
                      updateMetaMcpServerForm: {
                        id: metaMcpServer.id,
                        name: metaMcpServer.name,
                        discoveryMode: mode,
                      },
                    },
                  });
                }}
              />
            </RequireScope>
          </SettingsSection.FooterActions>
        </SettingsSection.Footer>
      </SettingsSection.Panel>
    </SettingsSection>
  );
}
