import { SettingsSection } from "@/components/detail/settings-section";
import { Badge } from "@/components/ui/Badge";
import { Button } from "@/components/ui/Button";
import { ConfirmDialog } from "@/components/ui/ConfirmDialog";
import { FieldError } from "@/components/ui/Field";
import { SegmentedControl } from "@/components/ui/SegmentedControl";
import { Switch } from "@/components/ui/Switch";
import { Text } from "@/components/ui/Text";
import { useRBAC } from "@/hooks/useRBAC";
import {
  clientTypeLabel,
  otherServers,
  serverLabel,
  UNDECIDED_STATUS,
  UPSTREAM_AUTH_STATUS,
} from "@/lib/auth-clients";
import { enumPresentation } from "@/lib/enum";
import type { AuthClient } from "@gram/client/models/components/authclient.js";
import type { McpServer } from "@gram/client/models/components/mcpserver.js";
import { useMcpServerAuthentication } from "@gram/client/react-query/mcpServerAuthentication.js";
import { useSetMcpServerUpstreamAuthRequirementMutation } from "@gram/client/react-query/setMcpServerUpstreamAuthRequirement.js";
import { useUpdateAuthClientMutation } from "@gram/client/react-query/updateAuthClient.js";
import { useQueryClient } from "@tanstack/react-query";
import { ChevronRight, Globe, Plus } from "lucide-react";
import { useState } from "react";
import { toast } from "sonner";
import { AddCredentialSheet } from "./AddCredentialSheet";
import { AuthClientSheet } from "./AuthClientSheet";
import { invalidateAuthClientQueries } from "./authClientQueries";
import { AuthRow } from "./AuthRow";
import { HeadlessCallersSection } from "./HeadlessCallersSection";

export const UPSTREAM_CREDENTIALS_SECTION_ID = "upstream-credentials";

/**
 * The credentials Speakeasy uses to reach this server's upstream, and whether
 * the upstream needs any. Who may use each credential is decided on the
 * credential, so the list leads to it rather than repeating it.
 */
export function UpstreamCredentialsSection({
  mcpServer,
}: {
  mcpServer: McpServer;
}): JSX.Element {
  const { hasScope } = useRBAC();
  const canManage = hasScope("org:admin");
  const query = useMcpServerAuthentication({ mcpServerId: mcpServer.id });
  const [openClientId, setOpenClientId] = useState<string | null>(null);
  const [adding, setAdding] = useState(false);
  const openClient =
    query.data?.clients.find((client) => client.id === openClientId) ?? null;

  return (
    <SettingsSection id={UPSTREAM_CREDENTIALS_SECTION_ID}>
      <SettingsSection.Header>
        <SettingsSection.Title>Upstream credentials</SettingsSection.Title>
        <SettingsSection.Description>
          How Speakeasy signs in to the upstream on a caller&apos;s behalf.
          {canManage ? "" : " Only organization admins can change these."}
        </SettingsSection.Description>
      </SettingsSection.Header>
      <SettingsSection.Panel>
        <div className="divide-y">
          {query.isLoading ? (
            <AuthRow label="Credentials">
              <Text muted small>
                Loading…
              </Text>
            </AuthRow>
          ) : query.isError || !query.data ? (
            <AuthRow label="Credentials">
              <FieldError>
                The upstream credentials could not be loaded. Refresh the page
                to try again.
              </FieldError>
            </AuthRow>
          ) : (
            <>
              <RequirementRow
                mcpServerId={mcpServer.id}
                status={query.data.status}
                requiresUpstreamAuth={query.data.requiresUpstreamAuth}
                hasEnabledClient={query.data.clients.some((c) => c.enabled)}
                canManage={canManage}
              />
              {query.data.requiresUpstreamAuth === false &&
              query.data.clients.length === 0 ? null : (
                <AuthRow
                  label="Credentials"
                  hint="Turning one off or changing who can use it applies on every server that uses it."
                >
                  {query.data.clients.length === 0 ? (
                    <Text muted small className="block">
                      {canManage
                        ? "No credentials yet. Add one so callers can reach the upstream."
                        : "No credentials yet."}
                    </Text>
                  ) : (
                    <ul className="border-border divide-y border">
                      {query.data.clients.map((client) => (
                        <CredentialRow
                          key={client.id}
                          client={client}
                          mcpServerId={mcpServer.id}
                          canManage={canManage}
                          onOpen={() => setOpenClientId(client.id)}
                        />
                      ))}
                    </ul>
                  )}
                  {canManage && query.data.requiresUpstreamAuth !== false ? (
                    <div className="mt-3">
                      <Button
                        variant="secondary"
                        size="sm"
                        onClick={() => setAdding(true)}
                      >
                        <Button.LeftIcon>
                          <Plus className="h-4 w-4" />
                        </Button.LeftIcon>
                        <Button.Text>Add credential</Button.Text>
                      </Button>
                    </div>
                  ) : null}
                </AuthRow>
              )}
              {mcpServer.userSessionIssuerId ? (
                <HeadlessCallersSection
                  mcpServerId={mcpServer.id}
                  userSessionIssuerId={mcpServer.userSessionIssuerId}
                  credentials={query.data.clients}
                  canManage={canManage}
                />
              ) : null}
            </>
          )}
        </div>
      </SettingsSection.Panel>
      {adding ? (
        <AddCredentialSheet
          mcpServerId={mcpServer.id}
          projectId={mcpServer.projectId}
          tunneled={!!mcpServer.tunneledMcpServerId}
          onOpenChange={setAdding}
        />
      ) : null}
      {openClient ? (
        <AuthClientSheet
          client={openClient}
          mcpServerId={mcpServer.id}
          canManage={canManage}
          onOpenChange={(open) => {
            if (!open) setOpenClientId(null);
          }}
        />
      ) : null}
    </SettingsSection>
  );
}

function RequirementRow({
  mcpServerId,
  status,
  requiresUpstreamAuth,
  hasEnabledClient,
  canManage,
}: {
  mcpServerId: string;
  status: string;
  requiresUpstreamAuth: boolean | undefined;
  hasEnabledClient: boolean;
  canManage: boolean;
}): JSX.Element {
  const queryClient = useQueryClient();
  const presentation = enumPresentation(
    UPSTREAM_AUTH_STATUS,
    status,
    UNDECIDED_STATUS,
  );
  const setRequirement = useSetMcpServerUpstreamAuthRequirementMutation({
    onSuccess: () => {
      void invalidateAuthClientQueries(queryClient);
    },
    onError: (error) => {
      toast.error(
        error.message.includes("enabled auth clients")
          ? "Turn off this server's credentials before saying it needs none."
          : "The change could not be saved. Try again.",
      );
    },
  });

  const value =
    requiresUpstreamAuth === undefined
      ? null
      : requiresUpstreamAuth
        ? "required"
        : "none";

  return (
    <AuthRow label="Upstream sign-in" hint={presentation.hint}>
      <div className="flex flex-wrap items-center gap-3">
        <Badge
          variant={
            presentation.tone === "neutral" ? "neutral" : presentation.tone
          }
        >
          {presentation.label}
        </Badge>
      </div>
      {canManage ? (
        <SegmentedControl
          ariaLabel="Does the upstream need credentials?"
          value={value}
          busy={setRequirement.isPending}
          disabled={setRequirement.isPending}
          onChange={(next) =>
            setRequirement.mutate({
              request: {
                setMcpServerUpstreamAuthRequirementRequestBody: {
                  mcpServerId,
                  requiresUpstreamAuth: next === "required",
                },
              },
            })
          }
          options={[
            { value: "required", label: "Needs credentials" },
            {
              value: "none",
              label: "Needs none",
              disabled: hasEnabledClient && value !== "none",
              tooltip: hasEnabledClient
                ? "Turn off this server's credentials first."
                : undefined,
            },
          ]}
        />
      ) : null}
    </AuthRow>
  );
}

function CredentialRow({
  client,
  mcpServerId,
  canManage,
  onOpen,
}: {
  client: AuthClient;
  mcpServerId: string;
  canManage: boolean;
  onOpen: () => void;
}): JSX.Element {
  const queryClient = useQueryClient();
  const [confirmingOff, setConfirmingOff] = useState(false);
  const others = otherServers(client, mcpServerId);
  const isPublic = client.servers.some((server) => server.public);
  const update = useUpdateAuthClientMutation({
    onSuccess: (_data, variables) => {
      setConfirmingOff(false);
      toast.success(
        variables.request.updateAuthClientRequestBody.enabled
          ? `${client.name} turned on`
          : `${client.name} turned off`,
      );
      void invalidateAuthClientQueries(queryClient);
    },
    onError: () => toast.error("The change could not be saved. Try again."),
  });

  const setEnabled = (enabled: boolean) =>
    update.mutate({
      request: { updateAuthClientRequestBody: { id: client.id, enabled } },
    });

  return (
    <li className="flex items-center gap-4 px-4 py-3">
      <button
        type="button"
        onClick={onOpen}
        className="hover:bg-muted/30 -mx-2 flex min-w-0 flex-1 items-center gap-3 rounded px-2 py-1 text-left"
      >
        <div className="min-w-0 flex-1">
          <span className="block truncate font-medium" title={client.name}>
            {client.name}
          </span>
          <Text muted small className="block truncate">
            {clientTypeLabel(client.clientType)}
            {others.length > 0
              ? ` · Also used on ${others.length} other ${others.length === 1 ? "server" : "servers"}`
              : ""}
          </Text>
        </div>
        {isPublic ? (
          <Badge variant="warning">
            <Globe className="h-3 w-3" />
            Public server
          </Badge>
        ) : null}
        <ChevronRight className="text-muted-foreground h-4 w-4 shrink-0" />
      </button>
      {canManage ? (
        <Switch
          checked={client.enabled}
          disabled={update.isPending}
          aria-label={
            client.enabled
              ? `Turn off ${client.name}`
              : `Turn on ${client.name}`
          }
          onCheckedChange={(enabled) => {
            if (!enabled && others.length > 0) {
              setConfirmingOff(true);
              return;
            }
            setEnabled(enabled);
          }}
        />
      ) : (
        <Text muted small>
          {client.enabled ? "On" : "Off"}
        </Text>
      )}
      <ConfirmDialog
        open={confirmingOff}
        onOpenChange={setConfirmingOff}
        title={`Turn off ${client.name}?`}
        description="Nobody can use this credential while it is off, on any server that uses it."
        confirmLabel="Turn off"
        isPending={update.isPending}
        onConfirm={() => setEnabled(false)}
        impact={{
          summary: "It is also used on these servers:",
          mcpServerNames: others.map(serverLabel),
        }}
      />
    </li>
  );
}
