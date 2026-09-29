import { useMcpMetadataMetadataForm } from "@/components/mcp_install_page/useMcpMetadataForm";
import { RequireScope } from "@/components/require-scope";
import {
  Field,
  FieldDescription,
  FieldError,
  FieldLabel,
} from "@/components/ui/Field";
import { Input } from "@/components/ui/Input";
import { Text } from "@/components/ui/Text";
import { cn } from "@/lib/utils";
import { mcpServerRouteParam } from "@/lib/sources";
import { useRoutes } from "@/routes";
import type { McpServer } from "@gram/client/models/components/mcpserver.js";
import { GramError } from "@gram/client/models/errors/gramerror.js";
import { useGetMcpMetadata } from "@gram/client/react-query/getMcpMetadata.js";
import { invalidateAllGetMcpServer } from "@gram/client/react-query/getMcpServer.js";
import { invalidateAllMcpServers } from "@gram/client/react-query/mcpServers.js";
import { useUpdateMcpServerMutation } from "@gram/client/react-query/updateMcpServer.js";
import { useQueryClient } from "@tanstack/react-query";
import { Network, Pencil } from "lucide-react";
import { useEffect, useRef, useState } from "react";
import { useNavigate } from "react-router";
import { toast } from "sonner";
import { VerifyRemoteMcpUrlButton } from "@/pages/sources/remote-mcp/VerifyRemoteMcpUrlButton";
import type { RemoteMcpServer } from "@gram/client/models/components/remotemcpserver.js";
import { UpstreamUrlField } from "./UpstreamUrlField";
import {
  type UpstreamUrlDraft,
  useUpstreamUrlDraft,
} from "./useUpstreamUrlDraft";
import {
  FooterSaveButton,
  SettingsSection,
} from "@/components/detail/settings-section";

// The display name shares the mcp_servers.name column, whose CHECK caps length
// at 40 (see schema.sql / MCP_SERVER_NAME_MAX_LENGTH on the legacy page).
const NAME_MAX_LENGTH = 40;

/**
 * The server's basic facts: its icon and name, plus the upstream URL when a
 * remote source backs it. One Save commits whatever changed.
 */
export function GeneralSection({
  mcpServer,
  remoteMcpServer,
}: {
  mcpServer: McpServer;
  remoteMcpServer?: RemoteMcpServer;
}): JSX.Element {
  if (remoteMcpServer) {
    return (
      <RemoteGeneralSection
        key={remoteMcpServer.id}
        mcpServer={mcpServer}
        remoteMcpServer={remoteMcpServer}
      />
    );
  }
  return <GeneralSectionContent mcpServer={mcpServer} upstream={null} />;
}

function RemoteGeneralSection({
  mcpServer,
  remoteMcpServer,
}: {
  mcpServer: McpServer;
  remoteMcpServer: RemoteMcpServer;
}): JSX.Element {
  const upstream = useUpstreamUrlDraft(remoteMcpServer);
  return <GeneralSectionContent mcpServer={mcpServer} upstream={upstream} />;
}

function GeneralSectionContent({
  mcpServer,
  upstream,
}: {
  mcpServer: McpServer;
  upstream: UpstreamUrlDraft | null;
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
  const fileInputRef = useRef<HTMLInputElement>(null);

  const metadataResult = useGetMcpMetadata(
    { mcpServerId: mcpServer.id },
    undefined,
    {
      retry: (failureCount, err) => {
        if (err instanceof GramError && err.statusCode === 404) {
          return false;
        }
        return failureCount < 3;
      },
      throwOnError: false,
    },
  );
  const metadataIs404 =
    metadataResult.error instanceof GramError &&
    metadataResult.error.statusCode === 404;
  // Anything other than a confirmed "no metadata yet" 404 means the form's
  // local draft (seeded from this) can't be trusted as a complete picture of
  // the current record. Saving before this resolves would spread undefined
  // branding/install-page fields into a full-record upsert and wipe out real
  // values that just hadn't loaded yet.
  const metadataUnresolved =
    metadataResult.isLoading ||
    (metadataResult.isError && !metadataIs404) ||
    metadataResult.isRefetchError;
  const metadataForm = useMcpMetadataMetadataForm(
    { kind: "mcp_server", mcpServerId: mcpServer.id },
    metadataResult.data?.metadata,
  );

  const trimmedDraft = nameDraft.trim();
  const nameDirty = trimmedDraft !== (mcpServer.name ?? "").trim();
  const upstreamDirty = !!upstream?.dirty;
  const dirty = nameDirty || metadataForm.brandingDirty || upstreamDirty;
  const saving =
    update.isPending || metadataForm.isLoading || !!upstream?.pending;
  const saveDisabled =
    !dirty ||
    trimmedDraft === "" ||
    trimmedDraft.length > NAME_MAX_LENGTH ||
    saving ||
    (metadataUnresolved && metadataForm.brandingDirty) ||
    (!!upstream?.dirty && upstream.invalid);
  const nameTooLong = trimmedDraft.length > NAME_MAX_LENGTH;

  const handleSave = async () => {
    try {
      if (metadataForm.brandingDirty) {
        await metadataForm.saveAsync();
      }

      if (upstream?.dirty) {
        await upstream.save();
      }

      // Last: a name change moves the route to the new slug.

      if (nameDirty) {
        const updated = await update.mutateAsync({
          request: {
            updateMcpServerForm: {
              id: mcpServer.id,
              name: trimmedDraft,
              remoteMcpServerId: mcpServer.remoteMcpServerId ?? undefined,
              tunneledMcpServerId: mcpServer.tunneledMcpServerId ?? undefined,
              toolsetId: mcpServer.toolsetId ?? undefined,
              unproxiedMcpServerId: mcpServer.unproxiedMcpServerId ?? undefined,
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
        void navigate(routes.mcp.x.settings.href(nextParam), {
          replace: true,
        });
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
        <SettingsSection.Title>General</SettingsSection.Title>
      </SettingsSection.Header>
      <SettingsSection.Panel>
        <SettingsSection.Body>
          <div className="flex items-start gap-5">
            <input
              ref={fileInputRef}
              type="file"
              accept="image/png,image/jpeg,image/gif,image/webp"
              className="hidden"
              onChange={(e) => {
                const file = e.target.files?.[0];
                e.target.value = "";
                if (file) {
                  metadataForm.logoUploadHandlers
                    .onUpload(file)
                    .catch((error: unknown) => {
                      toast.error(
                        error instanceof Error
                          ? error.message
                          : "Failed to upload icon",
                      );
                    });
                }
              }}
            />
            {/* The icon is its own control: the pencil badge marks it as
                editable without spending a row on an "Upload icon" button. */}
            <button
              type="button"
              aria-label="Change icon"
              disabled={metadataUnresolved}
              onClick={() => fileInputRef.current?.click()}
              className="bg-muted text-foreground relative flex size-16 shrink-0 items-center justify-center disabled:cursor-not-allowed disabled:opacity-50"
            >
              {metadataForm.logoUploadHandlers.renderFilePreview() ?? (
                <Network aria-hidden="true" className="size-7" />
              )}
              <span className="border-input bg-background text-foreground absolute -right-1.5 -bottom-1.5 flex size-[22px] items-center justify-center border">
                <Pencil aria-hidden="true" className="size-3" />
              </span>
            </button>
            {/* Both fields share one column so their left edges line up
                beside the icon rather than one of them wrapping under it. */}
            <div className="flex max-w-xl min-w-0 flex-1 flex-col gap-4">
              <Field
                data-invalid={update.isError ? true : undefined}
                className="max-w-md"
              >
                <FieldLabel htmlFor="mcp-server-display-name">
                  Display Name
                </FieldLabel>
                <div className="relative">
                  <Input
                    id="mcp-server-display-name"
                    value={nameDraft}
                    onChange={(value) => setNameDraft(value)}
                    placeholder="My MCP server"
                    aria-invalid={update.isError || nameTooLong}
                    className={cn(
                      "pr-10",
                      nameTooLong && "border-warning-default",
                    )}
                  />
                  <Pencil
                    aria-hidden="true"
                    className="text-muted-foreground pointer-events-none absolute top-1/2 right-4 size-4 -translate-y-1/2"
                  />
                </div>
                {nameTooLong ? (
                  <Text small warning className="block">
                    Display names can be up to {NAME_MAX_LENGTH} characters.
                  </Text>
                ) : null}
                {update.isError && (
                  <FieldError>{update.error.message}</FieldError>
                )}
              </Field>
              {upstream ? <UpstreamUrlField upstream={upstream} /> : null}
            </div>
          </div>
          {metadataUnresolved && !metadataResult.isLoading && (
            <FieldDescription className="text-destructive text-xs">
              Couldn't load current branding settings. Refresh the page before
              making changes.
            </FieldDescription>
          )}
        </SettingsSection.Body>
        <SettingsSection.Footer>
          <SettingsSection.FooterHint>
            {upstream?.dirty
              ? "Verify before saving to confirm the upstream URL answers as an MCP server."
              : null}
          </SettingsSection.FooterHint>
          <SettingsSection.FooterActions>
            <RequireScope scope="mcp:write" level="component">
              {upstream ? (
                <VerifyRemoteMcpUrlButton
                  state={upstream.verify}
                  url={upstream.draft}
                  disabled={upstream.pending || upstream.invalid}
                />
              ) : null}
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
