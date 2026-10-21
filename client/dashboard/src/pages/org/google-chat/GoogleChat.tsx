import { useState } from "react";
import { useSearchParams } from "react-router";
import { useQueryClient } from "@tanstack/react-query";
import { useOrganization } from "@/contexts/Auth";
import { RequireScope } from "@/components/require-scope";
import { Heading } from "@/components/ui/Heading";
import { Text } from "@/components/ui/Text";
import { Button } from "@/components/ui/Button";
import { Alert } from "@/components/ui/Alert";
import { Input } from "@/components/ui/Input";
import { GoogleChatPicker as Combobox } from "./GoogleChatPicker";
import { ConfirmDialog } from "@/components/ui/ConfirmDialog";
import { ApiErrorAlert } from "@/components/api-error-alert";
import { DEMO_ORG_SLUG } from "@/lib/demo";
import {
  useGoogleChatConnections,
  invalidateAllGoogleChatConnections,
} from "@gram/client/react-query/googleChatConnections.js";
import { useBeginGoogleChatConnectionMutation } from "@gram/client/react-query/beginGoogleChatConnection.js";
import { useDisconnectGoogleChatConnectionMutation } from "@gram/client/react-query/disconnectGoogleChatConnection.js";
import { useGoogleChatSpaces } from "@gram/client/react-query/googleChatSpaces.js";
import { useBindGoogleChatSpaceMutation } from "@gram/client/react-query/bindGoogleChatSpace.js";
import { useListProjects } from "@gram/client/react-query/listProjects.js";
import { useAssistantsList } from "@gram/client/react-query/assistantsList.js";
import type { GoogleChatConnection } from "@gram/client/models/components/googlechatconnection.js";
import { SESSION_SECURITY } from "../identity-provider/identityProviderQueries";
import { GoogleChatBindingPanel } from "./GoogleChatBindingPanel";

export function GoogleChat(): JSX.Element {
  return (
    <RequireScope scope="org:admin" level="page">
      <GoogleChatContent />
    </RequireScope>
  );
}
function GoogleChatContent(): JSX.Element {
  const org = useOrganization();
  const cache = useQueryClient();
  const [params] = useSearchParams();
  const query = useGoogleChatConnections(undefined, SESSION_SECURITY, {
    retry: false,
    throwOnError: false,
  });
  const begin = useBeginGoogleChatConnectionMutation();
  const disconnect = useDisconnectGoogleChatConnectionMutation();
  const [selected, setSelected] = useState("");
  const [remove, setRemove] = useState<GoogleChatConnection | null>(null);
  const [error, setError] = useState<string | null>(null);
  const refresh = () => invalidateAllGoogleChatConnections(cache);
  const connections =
    query.data?.connections.filter((c) => c.status !== "disconnected") ?? [];
  const connection =
    connections.find((c) => c.id === selected) ?? connections[0];
  const start = async () => {
    setError(null);
    try {
      const result = await begin.mutateAsync({
        request: {},
        security: SESSION_SECURITY,
      });
      const url = new URL(result.authorizationUrl);
      if (
        url.origin !== "https://accounts.google.com" ||
        url.pathname !== "/o/oauth2/v2/auth"
      )
        throw new Error("Invalid Google authorization link");
      window.location.assign(url.href);
    } catch (e) {
      setError(e instanceof Error ? e.message : "Could not connect Google");
    }
  };
  return (
    <section className="space-y-6" aria-label="Google Chat">
      <div className="flex flex-wrap justify-between gap-4">
        <div className="max-w-xl space-y-2">
          <Heading variant="h3">Google Chat</Heading>
          <Text muted>
            Connect once, then add assistants to shared spaces. Each participant
            acts with their own Speakeasy permissions. Your Google Workspace
            administrator must approve the Chat app and its message access.
          </Text>
        </div>
        <Button
          onClick={() => void start()}
          disabled={
            !query.data?.authorizationConfigured ||
            begin.isPending ||
            org.slug === DEMO_ORG_SLUG
          }
        >
          <Button.Text>
            {begin.isPending ? "Opening Google…" : "Add Google account"}
          </Button.Text>
        </Button>
      </div>
      {params.get("google_chat") === "connected" && (
        <Alert variant="success">
          Google connected. Choose a space and assistant below.
        </Alert>
      )}
      {params.get("google_chat") === "authorization_failed" && (
        <Alert variant="error">
          Google authorization did not complete. Connect again and approve the
          requested permissions.
        </Alert>
      )}
      {query.data && !query.data.authorizationConfigured && (
        <Alert variant="info">
          Google Chat is not enabled on this deployment yet. Your administrator
          must configure the hosted Google app.
        </Alert>
      )}
      <ApiErrorAlert error={query.error ?? begin.error ?? disconnect.error} />
      {error && <Alert variant="error">{error}</Alert>}
      {connections.length === 0 && !query.isPending && (
        <Text muted>
          Add a Google account to select or create a space. Credentials stay on
          the server.
        </Text>
      )}
      {connection && (
        <>
          <div className="flex flex-wrap items-center gap-3">
            <Combobox
              id="google-connection"
              label="Google account"
              selected={connection.id}
              items={connections.map((c) => ({ value: c.id, label: c.email }))}
              onSelectionChange={(item) => setSelected(item.value)}
            />
            <Button variant="secondary" onClick={() => setRemove(connection)}>
              <Button.Text>Disconnect</Button.Text>
            </Button>
          </div>
          {connection.status === "connected" &&
          query.data?.authorizationConfigured ? (
            <AddGoogleChatSpace connection={connection} onSaved={refresh} />
          ) : connection.status !== "connected" ? (
            <Text muted small>
              This account needs to reconnect before you can add or resume
              spaces.
            </Text>
          ) : null}
        </>
      )}
      <div className="space-y-4">
        {query.data?.bindings.map((b) => (
          <GoogleChatBindingPanel
            key={b.id}
            binding={b}
            onChanged={refresh}
            canConnect={
              !!query.data?.authorizationConfigured &&
              connections.some(
                (c) => c.id === b.connectionId && c.status === "connected",
              )
            }
          />
        ))}
      </div>
      <ConfirmDialog
        open={!!remove}
        onOpenChange={(open) => {
          if (!open) setRemove(null);
        }}
        title="Disconnect Google?"
        description="This stops connected assistants, removes their event subscriptions, and deletes the stored Google credentials. It does not delete your Google spaces or messages."
        confirmLabel="Disconnect"
        onConfirm={async () => {
          if (!remove) return;
          await disconnect.mutateAsync({
            request: {
              disconnectGoogleChatRequestBody: {
                id: remove.id,
                generation: remove.generation,
              },
            },
            security: SESSION_SECURITY,
          });
          setRemove(null);
          await refresh();
        }}
      />
    </section>
  );
}
function AddGoogleChatSpace({
  connection,
  onSaved,
}: {
  connection: GoogleChatConnection;
  onSaved: () => Promise<unknown>;
}): JSX.Element {
  const org = useOrganization();
  const [projectId, setProjectId] = useState("");
  const [assistant, setAssistant] = useState("");
  const [space, setSpace] = useState("new");
  const [name, setName] = useState("");
  const [requestId, setRequestId] = useState(() => crypto.randomUUID());
  const [prefix, setPrefix] = useState("assistant:");
  const [emails, setEmails] = useState("");
  const [confirmed, setConfirmed] = useState(false);
  const projects = useListProjects({ organizationId: org.id });
  const project = projects.data?.projects.find((p) => p.id === projectId);
  const assistants = useAssistantsList(
    { gramProject: project?.slug ?? "" },
    undefined,
    { enabled: !!project, retry: false, throwOnError: false },
  );
  const spaces = useGoogleChatSpaces(
    { connectionId: connection.id },
    SESSION_SECURITY,
    { retry: false, throwOnError: false },
  );
  const bind = useBindGoogleChatSpaceMutation();
  const save = async () => {
    await bind.mutateAsync({
      request: {
        bindGoogleChatRequestBody: {
          requestId,
          connectionId: connection.id,
          projectId,
          assistantId: assistant,
          spaceName: space === "new" ? undefined : space,
          displayName:
            space === "new"
              ? name
              : (spaces.data?.spaces.find((s) => s.name === space)
                  ?.displayName ?? space),
          prefix,
          inviteEmails: emails.split(/[\s,;]+/).filter(Boolean),
        },
      },
      security: SESSION_SECURITY,
    });
    setRequestId(crypto.randomUUID());
    await onSaved();
  };
  return (
    <div className="border border-border p-5 space-y-4">
      <Heading variant="h4">Add an assistant to a space</Heading>
      <div className="grid gap-4 md:grid-cols-2">
        <Combobox
          id="google-project"
          label="Project"
          selected={projectId}
          items={
            projects.data?.projects.map((p) => ({
              value: p.id,
              label: p.name,
            })) ?? []
          }
          onSelectionChange={(item) => {
            setProjectId(item.value);
            setAssistant("");
          }}
        />
        <Combobox
          id="google-assistant"
          label="Assistant"
          selected={assistant}
          items={
            assistants.data?.assistants.map((a) => ({
              value: a.id,
              label: a.name,
            })) ?? []
          }
          onSelectionChange={(item) => setAssistant(item.value)}
        />
        <Combobox
          id="google-space"
          label="Space"
          selected={space}
          items={[
            { value: "new", label: "Create a new shared space" },
            ...(spaces.data?.spaces.map((s) => ({
              value: s.name,
              label: s.displayName || s.name,
            })) ?? []),
          ]}
          onSelectionChange={(item) => setSpace(item.value)}
        />
        {space === "new" && (
          <label className="space-y-2">
            Space name
            <Input value={name} onChange={setName} />
          </label>
        )}
        <label className="space-y-2">
          Command prefix
          <Input value={prefix} onChange={setPrefix} />
          <Text muted small>
            Needed only to start a conversation. Replies in active threads do
            not need it.
          </Text>
        </label>
        <label className="space-y-2">
          Invite people (optional)
          <Input
            value={emails}
            onChange={setEmails}
            placeholder="Email addresses, separated by commas"
          />
        </label>
      </div>
      <label className="flex gap-2 text-sm">
        <input
          type="checkbox"
          checked={confirmed}
          onChange={(e) => setConfirmed(e.target.checked)}
        />
        This is a shared space. Replies and scheduled digests are visible to its
        members. I will give this assistant only data and tools appropriate for
        that audience.
      </label>
      <Text muted small>
        We add the Chat tools and an assistant identity. Map participants below
        before they can invoke it. Use the assistant’s Triggers tab to schedule
        a daily digest.
      </Text>
      <ApiErrorAlert error={bind.error ?? spaces.error ?? assistants.error} />
      <Button
        onClick={() => void save()}
        disabled={
          !assistant ||
          !projectId ||
          !prefix.trim() ||
          (!name.trim() && space === "new") ||
          !confirmed ||
          bind.isPending ||
          org.slug === DEMO_ORG_SLUG
        }
      >
        <Button.Text>
          {bind.isPending ? "Connecting space…" : "Connect assistant"}
        </Button.Text>
      </Button>
    </div>
  );
}
