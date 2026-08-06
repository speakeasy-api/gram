import { RequireScope } from "@/components/require-scope";
import {
  Field,
  FieldDescription,
  FieldError,
  FieldLabel,
} from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { ImageUpload } from "@/components/upload";
import { mcpServerRouteParam } from "@/lib/sources";
import { useRoutes } from "@/routes";
import type { Asset } from "@gram/client/models/components/asset.js";
import type { McpServer } from "@gram/client/models/components/mcpserver.js";
import {
  invalidateAllGetMcpMetadata,
  useGetMcpMetadata,
} from "@gram/client/react-query/getMcpMetadata.js";
import { invalidateAllGetMcpServer } from "@gram/client/react-query/getMcpServer.js";
import { useMcpMetadataSetMutation } from "@gram/client/react-query/mcpMetadataSet.js";
import { invalidateAllMcpServers } from "@gram/client/react-query/mcpServers.js";
import { useUpdateMcpServerMutation } from "@gram/client/react-query/updateMcpServer.js";
import { useQueryClient } from "@tanstack/react-query";
import { useEffect, useState } from "react";
import { useNavigate } from "react-router";
import { toast } from "sonner";
import { FooterSaveButton, SettingsSection } from "../SettingsSection";

// The display name shares the mcp_servers.name column, whose CHECK caps length
// at 40 (see schema.sql / MCP_SERVER_NAME_MAX_LENGTH on the legacy page).
const NAME_MAX_LENGTH = 40;

export function BrandingSection({
  mcpServer,
}: {
  mcpServer: McpServer;
}): JSX.Element {
  const [nameDraft, setNameDraft] = useState(mcpServer.name ?? "");

  // Re-sync draft when the upstream record changes (e.g. another tab edited
  // it or a refetch landed). Without this a stale draft survives the refetch.
  useEffect(() => {
    setNameDraft(mcpServer.name ?? "");
  }, [mcpServer.id, mcpServer.name]);

  const queryClient = useQueryClient();
  const update = useUpdateMcpServerMutation();
  const navigate = useNavigate();
  const routes = useRoutes();

  // Logo lives on the server's MCP metadata (mcp_metadata.logo_id) — the same
  // record catalog installs persist the registry icon into.
  const { data: metadataData } = useGetMcpMetadata(
    { mcpServerId: mcpServer.id },
    undefined,
    {
      retry: false,
      throwOnError: false, // Expected 404 when no metadata exists
    },
  );
  const metadata = metadataData?.metadata;
  const setMetadata = useMcpMetadataSetMutation();

  // Staged logo asset id ("" = no logo). Picking a file uploads the asset (to
  // preview it) but the assignment only persists on Save, like the name.
  const persistedLogoId = metadata?.logoAssetId ?? "";
  const [logoDraft, setLogoDraft] = useState(persistedLogoId);
  useEffect(() => {
    setLogoDraft(persistedLogoId);
  }, [mcpServer.id, persistedLogoId]);

  const trimmedDraft = nameDraft.trim();
  const nameDirty = trimmedDraft !== (mcpServer.name ?? "").trim();
  const logoDirty = logoDraft !== persistedLogoId;
  const saving = update.isPending || setMetadata.isPending;
  const saveDisabled =
    (!nameDirty && !logoDirty) ||
    trimmedDraft === "" ||
    trimmedDraft.length > NAME_MAX_LENGTH ||
    saving;
  const characterCount = `${nameDraft.length} of ${NAME_MAX_LENGTH} characters used`;

  const handleSave = async () => {
    try {
      if (logoDirty) {
        // The metadata endpoint upserts every field, so carry the existing
        // values along to avoid wiping instructions or documentation links.
        // Omitting logoAssetId clears the logo.
        await setMetadata.mutateAsync({
          request: {
            setMcpMetadataRequestBody: {
              mcpServerId: mcpServer.id,
              logoAssetId: logoDraft || undefined,
              externalDocumentationUrl: metadata?.externalDocumentationUrl,
              externalDocumentationText: metadata?.externalDocumentationText,
              instructions: metadata?.instructions,
              installationOverrideUrl: metadata?.installationOverrideUrl,
            },
          },
        });
        await invalidateAllGetMcpMetadata(queryClient, { refetchType: "all" });
      }

      if (nameDirty) {
        const updated = await update.mutateAsync({
          request: {
            updateMcpServerForm: {
              id: mcpServer.id,
              name: trimmedDraft,
              remoteMcpServerId: mcpServer.remoteMcpServerId ?? undefined,
              tunneledMcpServerId: mcpServer.tunneledMcpServerId ?? undefined,
              toolsetId: mcpServer.toolsetId ?? undefined,
              environmentId: mcpServer.environmentId ?? undefined,
              toolVariationsGroupId:
                mcpServer.toolVariationsGroupId ?? undefined,
              visibility: mcpServer.visibility,
            },
          },
        });
        // The server recomputes slug on every update, so a name change
        // produces a new slug. Replace the route param with the new slug
        // *before* invalidating queries so the refetch uses the new lookup
        // args and the page-level not-found guard doesn't bounce the user
        // back to /mcp.
        const nextParam = mcpServerRouteParam(updated);
        void navigate(routes.mcp.x.settings.href(nextParam), { replace: true });
        await Promise.all([
          invalidateAllGetMcpServer(queryClient, { refetchType: "all" }),
          invalidateAllMcpServers(queryClient, { refetchType: "all" }),
        ]);
      }

      toast.success("MCP server updated");
    } catch (error) {
      const message =
        error instanceof Error ? error.message : "Failed to update MCP server";
      toast.error(message);
    }
  };

  return (
    <SettingsSection>
      <SettingsSection.Header>
        <SettingsSection.Title>Branding</SettingsSection.Title>
        <SettingsSection.Description>
          Used to identify your MCP server within the dashboard and on its
          installation page.
        </SettingsSection.Description>
      </SettingsSection.Header>
      <SettingsSection.Panel>
        <SettingsSection.Body>
          <Field
            data-invalid={update.isError ? true : undefined}
            className="max-w-md"
          >
            <FieldLabel htmlFor="mcp-server-display-name">
              Display Name
            </FieldLabel>
            <Input
              id="mcp-server-display-name"
              value={nameDraft}
              onChange={(value) => setNameDraft(value)}
              placeholder="My MCP server"
              maxLength={NAME_MAX_LENGTH}
              aria-invalid={update.isError}
            />
            {nameDirty && (
              <FieldDescription className="pl-1 text-xs">
                {characterCount}
              </FieldDescription>
            )}
            {update.isError && <FieldError>{update.error.message}</FieldError>}
          </Field>
          <Field className="max-w-md">
            <FieldLabel>Logo</FieldLabel>
            <FieldDescription>
              Shown next to this server in the dashboard and advertised to MCP
              clients. Catalog installs prefill it with the server's catalog
              icon.
            </FieldDescription>
            <ImageUpload
              key={persistedLogoId || "no-logo"}
              existingAssetId={metadata?.logoAssetId}
              onUpload={(asset: Asset) => setLogoDraft(asset.id)}
            />
          </Field>
        </SettingsSection.Body>
        <SettingsSection.Footer>
          <SettingsSection.FooterHint>
            {`Please use no more than ${NAME_MAX_LENGTH} characters.`}
          </SettingsSection.FooterHint>
          <SettingsSection.FooterActions>
            <RequireScope scope="mcp:write" level="component">
              <FooterSaveButton
                pending={saving}
                disabled={saveDisabled}
                onClick={() => void handleSave()}
              />
            </RequireScope>
          </SettingsSection.FooterActions>
        </SettingsSection.Footer>
      </SettingsSection.Panel>
    </SettingsSection>
  );
}
