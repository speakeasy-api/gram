import { RequireScope } from "@/components/require-scope";
import { useRBAC } from "@/hooks/useRBAC";
import {
  DangerSettingsSection,
  FooterSaveButton,
  SettingsSection,
} from "@/components/detail/settings-section";
import { Button } from "@/components/ui/Button";
import { Dialog } from "@/components/ui/Dialog";
import { Field, FieldError, FieldLabel } from "@/components/ui/Field";
import { Input } from "@/components/ui/Input";
import { Textarea } from "@/components/moon/textarea";
import { RadioGroup, RadioGroupItem } from "@/components/ui/RadioGroup";
import { cn } from "@/lib/utils";
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

// Mirrors the normalized rune limit in the metaMcp update handler.
const INSTRUCTIONS_MAX_LENGTH = 10000;

type InstructionsMode = "append" | "replace";

const INSTRUCTIONS_MODES: {
  value: InstructionsMode;
  title: string;
  hint: string;
}[] = [
  {
    value: "append",
    title: "Add to the built-in instructions",
    hint: "Clients receive Gram's drill-down guidance first, then your text.",
  },
  {
    value: "replace",
    title: "Replace the built-in instructions",
    hint: "Clients receive only your text. Describe the four tools yourself.",
  },
];

export const GATEWAY_AUTHENTICATION_SECTION_ID = "authentication";
export const GATEWAY_INSTRUCTIONS_SECTION_ID = "instructions";

function useScrollToSettingsHash() {
  const location = useLocation();

  useEffect(() => {
    const targetId = location.hash.replace("#", "");
    if (
      targetId !== MCP_SERVER_URL_SECTION_ID &&
      targetId !== GATEWAY_AUTHENTICATION_SECTION_ID &&
      targetId !== GATEWAY_INSTRUCTIONS_SECTION_ID
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
      <GatewayInstructionsSection metaMcpServer={metaMcpServer} />
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

export function GatewayInstructionsSection({
  metaMcpServer,
}: {
  metaMcpServer: MetaMcpServer;
}): JSX.Element {
  const { hasScope } = useRBAC();
  const canWrite = hasScope("mcp:write", metaMcpServer.projectId);
  const stored = metaMcpServer.instructions ?? "";
  const storedMode: InstructionsMode =
    metaMcpServer.instructionsMode ?? "append";
  const [draft, setDraft] = useState(stored);
  const [mode, setMode] = useState<InstructionsMode>(storedMode);

  useEffect(() => {
    setDraft(stored);
    setMode(storedMode);
  }, [metaMcpServer.id, stored, storedMode]);

  const queryClient = useQueryClient();
  const update = useUpdateMetaMcpServerMutation({
    onSuccess: async () => {
      await Promise.all([
        invalidateAllGetMetaMcpServer(queryClient, { refetchType: "all" }),
        invalidateAllMetaMcpServers(queryClient, { refetchType: "all" }),
        queryClient.invalidateQueries({ queryKey: ["gatewayInspection"] }),
      ]);
      toast.success("Gateway instructions updated");
    },
  });

  const trimmedDraft = draft.trim();
  const dirty = trimmedDraft !== stored.trim() || mode !== storedMode;
  // Mirror the server's normalization (NUL strip + trim) so the counter and
  // the limit agree with what will be stored.
  const characterCount = Array.from(trimmedDraft.replaceAll("\0", "")).length;
  const overLimit = characterCount > INSTRUCTIONS_MAX_LENGTH;
  const saveDisabled = !canWrite || !dirty || overLimit || update.isPending;

  return (
    <SettingsSection id={GATEWAY_INSTRUCTIONS_SECTION_ID}>
      <SettingsSection.Header>
        <SettingsSection.Title>Instructions</SettingsSection.Title>
        <SettingsSection.Description>
          Sent to every client on connect. Gram&apos;s built-in instructions
          teach agents to list servers and describe tools before executing; your
          text can extend or replace them. Leave it blank to send only the
          built-in text. Anyone who can connect to this gateway can read the
          text, and clients already connected keep the old text until they
          reconnect.
        </SettingsSection.Description>
      </SettingsSection.Header>
      <SettingsSection.Panel>
        <SettingsSection.Body>
          <Field data-invalid={update.isError || overLimit ? true : undefined}>
            <FieldLabel htmlFor="gateway-instructions">
              Custom instructions
            </FieldLabel>
            <Textarea
              id="gateway-instructions"
              value={draft}
              onChange={(event) => setDraft(event.target.value)}
              placeholder={`Which member to use for which task, required workflows,\nand any constraints.\n\nKeep it concise — members are already listed by list_servers.`}
              className="min-h-[160px]"
              aria-invalid={update.isError || overLimit}
              disabled={!canWrite || update.isPending}
            />
            {overLimit && (
              <FieldError>
                {`Instructions must be ${INSTRUCTIONS_MAX_LENGTH.toLocaleString()} characters or fewer.`}
              </FieldError>
            )}
            {update.isError && <FieldError>{update.error.message}</FieldError>}
          </Field>
          <RadioGroup
            value={mode}
            onValueChange={(value) => {
              setMode(value as InstructionsMode);
            }}
            disabled={!canWrite || update.isPending}
            aria-label="How custom instructions combine with the built-in text"
            className="gap-2"
          >
            {INSTRUCTIONS_MODES.map((option) => (
              <label
                key={option.value}
                htmlFor={`gateway-instructions-mode-${option.value}`}
                className="hover:bg-muted/40 flex cursor-pointer items-start gap-3 border px-3 py-2.5"
              >
                <RadioGroupItem
                  id={`gateway-instructions-mode-${option.value}`}
                  value={option.value}
                  className="mt-0.5"
                />
                <div className="min-w-0">
                  <div className="text-sm">{option.title}</div>
                  <div className="text-muted-foreground text-xs">
                    {option.hint}
                  </div>
                </div>
              </label>
            ))}
          </RadioGroup>
        </SettingsSection.Body>
        <SettingsSection.Footer>
          <SettingsSection.FooterHint
            className={cn(overLimit && "text-destructive")}
          >
            {`${characterCount.toLocaleString()} / ${INSTRUCTIONS_MAX_LENGTH.toLocaleString()} characters.`}
          </SettingsSection.FooterHint>
          <SettingsSection.FooterActions>
            <RequireScope
              scope="mcp:write"
              resourceId={metaMcpServer.projectId}
              level="component"
            >
              <FooterSaveButton
                pending={update.isPending}
                disabled={saveDisabled}
                onClick={() =>
                  update.mutate({
                    request: {
                      updateMetaMcpServerForm: {
                        id: metaMcpServer.id,
                        name: metaMcpServer.name,
                        userSessionIssuerId:
                          metaMcpServer.userSessionIssuerId ?? undefined,
                        // An empty string clears back to the built-in text.
                        instructions: trimmedDraft,
                        instructionsMode: mode,
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
          Who may connect to this gateway and how they sign in. Changes take
          effect on new connections. Without an identity provider it serves
          anonymously.
        </SettingsSection.Description>
      </SettingsSection.Header>
      <AuthenticationSectionBody target={target} />
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
