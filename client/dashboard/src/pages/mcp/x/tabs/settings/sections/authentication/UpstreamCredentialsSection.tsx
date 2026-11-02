import { SettingsSection } from "@/components/detail/settings-section";
import { Badge } from "@/components/ui/Badge";
import { Button } from "@/components/ui/Button";
import { ConfirmDialog } from "@/components/ui/ConfirmDialog";
import { FieldError } from "@/components/ui/Field";
import { Switch } from "@/components/ui/Switch";
import { Text } from "@/components/ui/Text";
import { useRBAC } from "@/hooks/useRBAC";
import {
  clientTypeLabel,
  otherServers,
  serverLabel,
  SIGN_IN_DETECTED_STATUS,
  UNKNOWN_STATUS,
  UPSTREAM_AUTH_STATUS,
} from "@/lib/auth-clients";
import { enumPresentation } from "@/lib/enum";
import { useUpstreamProbe } from "@/lib/remote-identity";
import type { AuthClient } from "@gram/client/models/components/authclient.js";
import type { McpServer } from "@gram/client/models/components/mcpserver.js";
import { useMcpServerAuthentication } from "@gram/client/react-query/mcpServerAuthentication.js";
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
 * the upstream needs any, which follows from the credentials turned on. Who may use each credential is decided on the
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
                remoteMcpServerId={mcpServer.remoteMcpServerId}
                status={query.data.status}
                canManage={canManage}
              />
              <AuthRow
                label="Credentials"
                hint="Turning one off or changing who can use it applies on every server that uses it."
              >
                {query.data.clients.length === 0 ? (
                  <Text muted small className="block">
                    {canManage
                      ? "No credentials. Add one if the upstream needs sign-in."
                      : "No credentials."}
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
                {canManage ? (
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
              {mcpServer.userSessionIssuerId &&
              query.data.requiresUpstreamAuth ? (
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

/** What probing the upstream found, phrased for the requirement row. */
const DETECTED_REQUIREMENT: Partial<Record<string, string>> = {
  "authentication-required":
    "Speakeasy checked the upstream: it asks for sign-in.",
  available:
    "Speakeasy checked the upstream: it answers without sign-in. It can still need a header credential for its tools.",
};

/**
 * Whether the upstream needs sign-in, as Speakeasy derives it from the
 * credentials the server offers. Nothing here changes it: adding, turning on,
 * turning off or removing a credential does. For admins, a probe of the
 * upstream also flags one that asks for sign-in while no credential is set up.
 */
function RequirementRow({
  remoteMcpServerId,
  status,
  canManage,
}: {
  remoteMcpServerId: string | undefined;
  status: string;
  canManage: boolean;
}): JSX.Element {
  const probe = useUpstreamProbe(
    remoteMcpServerId ?? "",
    canManage && !!remoteMcpServerId,
  );
  const detected = DETECTED_REQUIREMENT[probe];
  const presentation =
    status === "no_auth_required" && probe === "authentication-required"
      ? SIGN_IN_DETECTED_STATUS
      : enumPresentation(UPSTREAM_AUTH_STATUS, status, UNKNOWN_STATUS);

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
      {detected ? (
        <Text muted small className="mt-2 block">
          {detected}
        </Text>
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
