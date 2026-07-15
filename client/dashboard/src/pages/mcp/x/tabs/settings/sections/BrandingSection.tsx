import { RequireScope } from "@/components/require-scope";
import {
  Field,
  FieldDescription,
  FieldError,
  FieldLabel,
} from "@/components/ui/field";
import { mcpServerRouteParam } from "@/lib/sources";
import { toastError } from "@/lib/toast-error";
import { useRoutes } from "@/routes";
import type { McpServer } from "@gram/client/models/components/mcpserver.js";
import { invalidateAllGetMcpServer } from "@gram/client/react-query/getMcpServer.js";
import { invalidateAllMcpServers } from "@gram/client/react-query/mcpServers.js";
import { useUpdateMcpServerMutation } from "@gram/client/react-query/updateMcpServer.js";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { useQueryClient } from "@tanstack/react-query";
import { useEffect, useState } from "react";
import { useNavigate } from "react-router";
import { toast } from "sonner";
import { FooterSaveButtonContent, SettingsSection } from "../SettingsSection";

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

  const trimmedDraft = nameDraft.trim();
  const dirty = trimmedDraft !== (mcpServer.name ?? "").trim();
  const saveDisabled =
    !dirty ||
    trimmedDraft === "" ||
    trimmedDraft.length > NAME_MAX_LENGTH ||
    update.isPending;
  const characterCount = `${nameDraft.length} of ${NAME_MAX_LENGTH} characters used`;

  const handleSave = async () => {
    try {
      const updated = await update.mutateAsync({
        request: {
          updateMcpServerForm: {
            id: mcpServer.id,
            name: trimmedDraft,
            remoteMcpServerId: mcpServer.remoteMcpServerId ?? undefined,
            tunneledMcpServerId: mcpServer.tunneledMcpServerId ?? undefined,
            toolsetId: mcpServer.toolsetId ?? undefined,
            environmentId: mcpServer.environmentId ?? undefined,
            toolVariationsGroupId: mcpServer.toolVariationsGroupId ?? undefined,
            visibility: mcpServer.visibility,
          },
        },
      });
      // The server recomputes slug on every update, so a name change produces
      // a new slug. Replace the route param with the new slug *before*
      // invalidating queries so the refetch uses the new lookup args and the
      // page-level not-found guard doesn't bounce the user back to /mcp.
      const nextParam = mcpServerRouteParam(updated);
      void navigate(routes.mcp.x.settings.href(nextParam), { replace: true });
      await Promise.all([
        invalidateAllGetMcpServer(queryClient, { refetchType: "all" }),
        invalidateAllMcpServers(queryClient, { refetchType: "all" }),
      ]);
      toast.success("MCP server updated");
    } catch (error) {
      toastError(error, "Failed to update MCP server");
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
              onChange={(e) => setNameDraft(e.target.value)}
              placeholder="My MCP server"
              maxLength={NAME_MAX_LENGTH}
              aria-invalid={update.isError}
            />
            {dirty && (
              <FieldDescription className="pl-1 text-xs">
                {characterCount}
              </FieldDescription>
            )}
            {update.isError && <FieldError>{update.error.message}</FieldError>}
          </Field>
        </SettingsSection.Body>
        <SettingsSection.Footer>
          <SettingsSection.FooterHint>
            {`Please use no more than ${NAME_MAX_LENGTH} characters.`}
          </SettingsSection.FooterHint>
          <SettingsSection.FooterActions>
            <RequireScope scope="mcp:write" level="component">
              <Button
                variant="primary"
                size="md"
                disabled={saveDisabled}
                onClick={() => void handleSave()}
              >
                <FooterSaveButtonContent pending={update.isPending} />
              </Button>
            </RequireScope>
          </SettingsSection.FooterActions>
        </SettingsSection.Footer>
      </SettingsSection.Panel>
    </SettingsSection>
  );
}
