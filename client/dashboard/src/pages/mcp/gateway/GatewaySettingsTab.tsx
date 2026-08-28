import { RequireScope } from "@/components/require-scope";
import {
  DangerSettingsSection,
  FooterSaveButton,
  SettingsSection,
} from "@/components/detail/settings-section";
import { Button } from "@/components/ui/Button";
import { Dialog } from "@/components/ui/Dialog";
import { Field, FieldError, FieldLabel } from "@/components/ui/Field";
import { Input } from "@/components/ui/Input";
import { Text } from "@/components/ui/Text";
import { useRoutes } from "@/routes";
import type { McpEndpoint } from "@gram/client/models/components/mcpendpoint.js";
import type { MetaMcpServer } from "@gram/client/models/components/metamcpserver.js";
import { invalidateAllGetMetaMcpServer } from "@gram/client/react-query/getMetaMcpServer.js";
import { invalidateAllMcpEndpoints } from "@gram/client/react-query/mcpEndpoints.js";
import { invalidateAllMetaMcpServers } from "@gram/client/react-query/metaMcpServers.js";
import { useDeleteMetaMcpServerMutation } from "@gram/client/react-query/deleteMetaMcpServer.js";
import { useUpdateMetaMcpServerMutation } from "@gram/client/react-query/updateMetaMcpServer.js";
import { useQueryClient } from "@tanstack/react-query";
import { Loader2 } from "lucide-react";
import { useEffect, useState } from "react";
import { useLocation, useNavigate } from "react-router";
import { toast } from "sonner";
import { AuthenticationSectionBody } from "@/pages/mcp/x/tabs/settings/sections/authentication/AuthenticationSection";
import { useMetaMcpAuthTarget } from "@/pages/mcp/x/tabs/settings/sections/authentication/authTarget";
import {
  MCP_SERVER_URL_SECTION_ID,
  ServerUrlSection,
} from "@/pages/mcp/x/tabs/settings/sections/ServerUrlSection";

// Shares mcp_servers' 40-char display-name convention.
const NAME_MAX_LENGTH = 40;

export const GATEWAY_AUTHENTICATION_SECTION_ID = "authentication";

function useScrollToSettingsHash() {
  const location = useLocation();

  useEffect(() => {
    const targetId = location.hash.replace("#", "");
    if (
      targetId !== MCP_SERVER_URL_SECTION_ID &&
      targetId !== GATEWAY_AUTHENTICATION_SECTION_ID
    ) {
      return;
    }

    const animationFrame = window.requestAnimationFrame(() => {
      document
        .getElementById(targetId)
        ?.scrollIntoView({ behavior: "smooth", block: "start" });
    });

    return () => window.cancelAnimationFrame(animationFrame);
  }, [location.hash]);
}

export function GatewaySettingsTab({
  metaMcpServer,
  endpoints,
  isLoadingEndpoints,
}: {
  metaMcpServer: MetaMcpServer;
  endpoints: McpEndpoint[];
  isLoadingEndpoints: boolean;
}): JSX.Element {
  useScrollToSettingsHash();

  return (
    <div className="mx-auto w-full max-w-[1270px] space-y-10 px-8 py-8">
      <GatewayNameSection metaMcpServer={metaMcpServer} />
      <ServerUrlSection
        backend={{ metaMcpServerId: metaMcpServer.id }}
        endpoints={endpoints}
        isLoadingEndpoints={isLoadingEndpoints}
        subject="gateway"
      />
      <GatewayAuthenticationSection
        metaMcpServer={metaMcpServer}
        endpoints={endpoints}
      />
      <GatewayDangerZoneSection
        metaMcpServer={metaMcpServer}
        endpoints={endpoints}
      />
    </div>
  );
}

function GatewayNameSection({
  metaMcpServer,
}: {
  metaMcpServer: MetaMcpServer;
}): JSX.Element {
  const [nameDraft, setNameDraft] = useState(metaMcpServer.name);

  useEffect(() => {
    setNameDraft(metaMcpServer.name);
  }, [metaMcpServer.id, metaMcpServer.name]);

  const queryClient = useQueryClient();
  const update = useUpdateMetaMcpServerMutation({
    onSuccess: async () => {
      await Promise.all([
        invalidateAllGetMetaMcpServer(queryClient, { refetchType: "all" }),
        invalidateAllMetaMcpServers(queryClient, { refetchType: "all" }),
      ]);
      toast.success("Gateway updated");
    },
  });

  const trimmedDraft = nameDraft.trim();
  const dirty = trimmedDraft !== metaMcpServer.name.trim();
  const saveDisabled =
    !dirty ||
    trimmedDraft === "" ||
    trimmedDraft.length > NAME_MAX_LENGTH ||
    update.isPending;

  return (
    <SettingsSection>
      <SettingsSection.Header>
        <SettingsSection.Title>Name</SettingsSection.Title>
        <SettingsSection.Description>
          Identifies this gateway within the dashboard.
        </SettingsSection.Description>
      </SettingsSection.Header>
      <SettingsSection.Panel>
        <SettingsSection.Body>
          <Field
            data-invalid={update.isError ? true : undefined}
            className="max-w-md"
          >
            <FieldLabel htmlFor="gateway-name">Display Name</FieldLabel>
            <Input
              id="gateway-name"
              value={nameDraft}
              onChange={(value) => setNameDraft(value)}
              placeholder="My Gateway"
              maxLength={NAME_MAX_LENGTH}
              aria-invalid={update.isError}
            />
            {update.isError && <FieldError>{update.error.message}</FieldError>}
          </Field>
        </SettingsSection.Body>
        <SettingsSection.Footer>
          <SettingsSection.FooterHint>
            {`Please use no more than ${NAME_MAX_LENGTH} characters.`}
          </SettingsSection.FooterHint>
          <SettingsSection.FooterActions>
            <RequireScope scope="mcp:write" level="component">
              <FooterSaveButton
                pending={update.isPending}
                disabled={saveDisabled}
                onClick={() =>
                  update.mutate({
                    request: {
                      updateMetaMcpServerForm: {
                        id: metaMcpServer.id,
                        name: trimmedDraft,
                        // Full-record replace: keep the issuer link intact.
                        userSessionIssuerId:
                          metaMcpServer.userSessionIssuerId ?? undefined,
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

function GatewayAuthenticationSection({
  metaMcpServer,
  endpoints,
}: {
  metaMcpServer: MetaMcpServer;
  endpoints: McpEndpoint[];
}): JSX.Element {
  // Seeds auto-derived issuer slugs; the platform endpoint slug is the
  // gateway's closest thing to a slug of its own.
  const slugSeed =
    endpoints.find((e) => !e.customDomainId)?.slug ??
    endpoints[0]?.slug ??
    "gateway";
  const target = useMetaMcpAuthTarget(metaMcpServer, slugSeed);

  return (
    <SettingsSection id={GATEWAY_AUTHENTICATION_SECTION_ID}>
      <SettingsSection.Header>
        <SettingsSection.Title>Authentication</SettingsSection.Title>
        <SettingsSection.Description>
          Configure user sessions for clients connecting to this gateway.
          Without an issuer the gateway serves anonymously.
        </SettingsSection.Description>
      </SettingsSection.Header>
      <SettingsSection.Panel>
        <SettingsSection.Body>
          <AuthenticationSectionBody target={target} />
        </SettingsSection.Body>
        <SettingsSection.Footer>
          <SettingsSection.FooterHint>
            Authentication changes apply to new client connections.
          </SettingsSection.FooterHint>
        </SettingsSection.Footer>
      </SettingsSection.Panel>
    </SettingsSection>
  );
}

function GatewayDangerZoneSection({
  metaMcpServer,
  endpoints,
}: {
  metaMcpServer: MetaMcpServer;
  endpoints: McpEndpoint[];
}): JSX.Element {
  const [confirmOpen, setConfirmOpen] = useState(false);
  const queryClient = useQueryClient();
  const navigate = useNavigate();
  const routes = useRoutes();

  const remove = useDeleteMetaMcpServerMutation({
    onSuccess: async () => {
      await Promise.all([
        invalidateAllMetaMcpServers(queryClient, { refetchType: "all" }),
        invalidateAllMcpEndpoints(queryClient, { refetchType: "all" }),
      ]);
      toast.success("Gateway deleted");
      void navigate(routes.mcp.href());
    },
    onError: (error) => {
      toast.error(
        error instanceof Error ? error.message : "Failed to delete gateway",
      );
    },
  });

  return (
    <DangerSettingsSection>
      <SettingsSection.Header>
        <SettingsSection.Title>Danger Zone</SettingsSection.Title>
        <SettingsSection.Description>
          Deleting a gateway removes its endpoints and memberships. Member MCP
          servers are untouched.
        </SettingsSection.Description>
      </SettingsSection.Header>
      <SettingsSection.Panel>
        <SettingsSection.Body>
          <div className="flex items-center justify-between gap-4">
            <Text muted small>
              {endpoints.length > 0
                ? `Removes ${endpoints.length} ${endpoints.length === 1 ? "address" : "addresses"}: ${endpoints
                    .map((e) => e.slug)
                    .join(", ")}`
                : "This gateway has no addresses."}
            </Text>
            <RequireScope scope="mcp:write" level="component">
              <Button
                variant="destructive-primary"
                disabled={remove.isPending}
                onClick={() => setConfirmOpen(true)}
              >
                <Button.Text>Delete gateway</Button.Text>
              </Button>
            </RequireScope>
          </div>
        </SettingsSection.Body>
      </SettingsSection.Panel>

      <Dialog open={confirmOpen} onOpenChange={setConfirmOpen}>
        <Dialog.Content className="max-w-md">
          <Dialog.Header>
            <Dialog.Title>Delete this gateway?</Dialog.Title>
            <Dialog.Description>
              {`Clients can no longer connect to ${
                metaMcpServer.name
              }. Its addresses${
                endpoints.length > 0
                  ? ` (${endpoints.map((e) => e.slug).join(", ")})`
                  : ""
              } and member list are removed. Member MCP servers keep serving their own endpoints.`}
            </Dialog.Description>
          </Dialog.Header>
          <Dialog.Footer>
            <Button
              variant="secondary"
              disabled={remove.isPending}
              onClick={() => setConfirmOpen(false)}
            >
              <Button.Text>Cancel</Button.Text>
            </Button>
            <Button
              variant="destructive-primary"
              disabled={remove.isPending}
              onClick={() =>
                remove.mutate({ request: { id: metaMcpServer.id } })
              }
            >
              {remove.isPending && (
                <Button.LeftIcon>
                  <Loader2 aria-hidden="true" className="size-4 animate-spin" />
                </Button.LeftIcon>
              )}
              <Button.Text>Delete gateway</Button.Text>
            </Button>
          </Dialog.Footer>
        </Dialog.Content>
      </Dialog>
    </DangerSettingsSection>
  );
}
