import { Alert } from "@/components/ui/Alert";
import { Badge } from "@/components/ui/Badge";
import { Button } from "@/components/ui/Button";
import { ConfirmDialog } from "@/components/ui/ConfirmDialog";
import { FieldError } from "@/components/ui/Field";
import { SegmentedControl } from "@/components/ui/SegmentedControl";
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetFooter,
  SheetHeader,
  SheetTitle,
} from "@/components/ui/Sheet";
import { Text } from "@/components/ui/Text";
import { useOrganization } from "@/contexts/Auth";
import {
  clientTypeHint,
  clientTypeLabel,
  otherServers,
  serverLabel,
} from "@/lib/auth-clients";
import { AddAudienceDialog } from "@/pages/mcp/access/AddAudienceDialog";
import { PrincipalBadge } from "@/pages/mcp/access/PrincipalBadge";
import type { AuthClient } from "@gram/client/models/components/authclient.js";
import type { AuthClientAudienceEntry } from "@gram/client/models/components/authclientaudienceentry.js";
import type { AudienceOption } from "@gram/client/models/components/audienceoption.js";
import { AuthClientAudienceRuleLevel } from "@gram/client/models/components/authclientaudiencerule.js";
import { useAudienceOptions } from "@gram/client/react-query/audienceOptions.js";
import {
  invalidateAllAuthClientAudience,
  useAuthClientAudience,
} from "@gram/client/react-query/authClientAudience.js";
import { useDetachAuthClientFromMcpServerMutation } from "@gram/client/react-query/detachAuthClientFromMcpServer.js";
import { useSetAuthClientAudienceMutation } from "@gram/client/react-query/setAuthClientAudience.js";
import { useSetAuthClientHeadersMutation } from "@gram/client/react-query/setAuthClientHeaders.js";
import { useQueryClient } from "@tanstack/react-query";
import { Plus, Trash2 } from "lucide-react";
import { useMemo, useState } from "react";
import { toast } from "sonner";
import { invalidateAuthClientQueries } from "./authClientQueries";
import { CredentialHeadersEditor } from "./CredentialHeadersEditor";
import {
  generatesAuthorization,
  headerDraftProblem,
  headerDraftsFrom,
  headerInputs,
  type HeaderDraft,
} from "./credentialHeaders";

type Level = "use" | "blocked";

type DraftRule = { principalUrn: string; level: Level };

/** An edit in progress, with the audience version it started from. */
type Draft = { rules: DraftRule[]; version: string };

const KNOWN_LEVELS = new Set<string>(["use", "blocked"]);
const KNOWN_APPLIES_TO = new Set<string>([
  "client",
  "client_type",
  "all_clients",
]);

const LEVEL_LABEL: Record<Level, string> = {
  use: "Can use",
  blocked: "Blocked",
};

const EVERYONE_URN = "user:all";

/**
 * One credential: who may use it, and the servers that use it. Both apply
 * wherever the credential is used, so the sheet says so once, at the top of
 * the rules, instead of on every row.
 */
export function AuthClientSheet({
  client,
  mcpServerId,
  canManage,
  onOpenChange,
}: {
  client: AuthClient;
  mcpServerId: string;
  canManage: boolean;
  onOpenChange: (open: boolean) => void;
}): JSX.Element {
  const others = otherServers(client, mcpServerId);
  const [confirmingDetach, setConfirmingDetach] = useState(false);
  const queryClient = useQueryClient();
  const detach = useDetachAuthClientFromMcpServerMutation({
    onSuccess: () => {
      toast.success(`${client.name} removed from this server`);
      setConfirmingDetach(false);
      onOpenChange(false);
      void invalidateAuthClientQueries(queryClient);
    },
    onError: () =>
      toast.error("The credential could not be removed. Try again."),
  });

  return (
    <Sheet open onOpenChange={onOpenChange}>
      <SheetContent
        side="right"
        className="flex w-[560px] flex-col sm:max-w-[560px]"
      >
        <SheetHeader>
          <SheetTitle>{client.name}</SheetTitle>
          <SheetDescription>
            {clientTypeLabel(client.clientType)}.{" "}
            {clientTypeHint(client.clientType)}
          </SheetDescription>
        </SheetHeader>

        <div className="min-h-0 flex-1 space-y-8 overflow-y-auto px-4 pb-4">
          {!client.enabled ? (
            <Alert variant="warning" inline>
              This credential is turned off. Nobody can use it until it is
              turned back on.
            </Alert>
          ) : null}

          <AudienceEditor client={client} canManage={canManage} />

          <HeadersSection
            client={client}
            mcpServerId={mcpServerId}
            canManage={canManage}
          />

          <section className="space-y-3">
            <div className="space-y-1">
              <h3 className="text-sm font-medium">Used on</h3>
              <Text muted small className="block">
                Changes to this credential apply on every server listed here.
              </Text>
            </div>
            <ul className="border-border divide-y border">
              {client.servers.map((server) => (
                <li
                  key={server.id}
                  className="flex items-center justify-between gap-3 px-3 py-2"
                >
                  <span className="truncate text-sm">
                    {serverLabel(server)}
                    {server.id === mcpServerId ? (
                      <Text muted small className="ml-2">
                        This server
                      </Text>
                    ) : null}
                  </span>
                  {server.public ? (
                    <Badge variant="warning">Public</Badge>
                  ) : null}
                </li>
              ))}
            </ul>
          </section>
        </div>

        {canManage ? (
          <SheetFooter className="border-t">
            <Button
              variant="secondary"
              onClick={() => setConfirmingDetach(true)}
              disabled={detach.isPending}
            >
              <Button.LeftIcon>
                <Trash2 className="h-4 w-4" />
              </Button.LeftIcon>
              <Button.Text>Remove from this server</Button.Text>
            </Button>
          </SheetFooter>
        ) : null}

        <ConfirmDialog
          open={confirmingDetach}
          onOpenChange={setConfirmingDetach}
          title={`Remove ${client.name} from this server?`}
          description={
            others.length > 0
              ? "This server stops offering the credential. It stays on the other servers that use it."
              : "This server stops offering the credential. Callers who chose it reconnect to pick another."
          }
          confirmLabel="Remove"
          isPending={detach.isPending}
          onConfirm={() =>
            detach.mutate({
              request: {
                detachAuthClientFromMcpServerRequestBody: {
                  id: client.id,
                  mcpServerId,
                },
              },
            })
          }
          impact={
            others.length > 0
              ? {
                  summary: "It stays on these servers:",
                  mcpServerNames: others.map(serverLabel),
                }
              : undefined
          }
        />
      </SheetContent>
    </Sheet>
  );
}

function AudienceEditor({
  client,
  canManage,
}: {
  client: AuthClient;
  canManage: boolean;
}): JSX.Element {
  const queryClient = useQueryClient();
  const audience = useAuthClientAudience({ id: client.id });
  const options = useAudienceOptions();
  const [draft, setDraft] = useState<Draft | null>(null);
  const [adding, setAdding] = useState(false);

  const names = useMemo(() => {
    const byUrn = new Map<string, AudienceOption>();
    for (const option of options.data?.options ?? []) {
      byUrn.set(option.principalUrn, option);
    }
    return byUrn;
  }, [options.data?.options]);

  const entries = useMemo(
    () => audience.data?.entries ?? [],
    [audience.data?.entries],
  );
  // A rule this page does not understand is never shown as something it is
  // not, and is never rewritten by a save, so editing waits until it does.
  const hasUnsupported = entries.some(
    (entry) =>
      !KNOWN_LEVELS.has(entry.level) || !KNOWN_APPLIES_TO.has(entry.appliesTo),
  );
  const editable = canManage && !hasUnsupported;
  const saved = useMemo<DraftRule[]>(
    () =>
      entries
        .filter(
          (entry) =>
            entry.appliesTo === "client" && KNOWN_LEVELS.has(entry.level),
        )
        .map((entry) => ({
          principalUrn: entry.principalUrn,
          level: entry.level as Level,
        })),
    [entries],
  );
  const rules = draft?.rules ?? saved;
  const inherited = entries.filter(
    (entry) =>
      entry.appliesTo !== "client" &&
      KNOWN_APPLIES_TO.has(entry.appliesTo) &&
      KNOWN_LEVELS.has(entry.level),
  );
  // A broader block outranks a rule added here, so the picker withholds
  // whoever it reaches. Only a block on every server counts; one confined
  // to a project leaves the rule useful elsewhere.
  const blockedEverywhere = new Set(
    inherited
      .filter((entry) => entry.level === "blocked" && !entry.projectId)
      .map((entry) => entry.principalUrn),
  );
  const blockedReason = (principalUrn: string) =>
    blockedEverywhere.has(EVERYONE_URN) || blockedEverywhere.has(principalUrn)
      ? "Blocked by a broader rule"
      : undefined;
  const notes = useMemo(() => {
    const byUrn = new Map<string, string>();
    for (const entry of entries) {
      if (entry.appliesTo === "client" && entry.connectionNote) {
        byUrn.set(entry.principalUrn, entry.connectionNote);
      }
    }
    return byUrn;
  }, [entries]);

  const save = useSetAuthClientAudienceMutation({
    onSuccess: () => {
      setDraft(null);
      toast.success("Saved who can use this credential");
      void invalidateAllAuthClientAudience(queryClient);
    },
    onError: (error) => {
      if (error.message.includes("changed while you were editing")) {
        toast.error(
          "Someone else changed who can use this credential. Their changes are shown now; make yours again.",
        );
        setDraft(null);
        void invalidateAllAuthClientAudience(queryClient);
        return;
      }
      toast.error("The change could not be saved. Try again.");
    },
  });

  const update = (next: DraftRule[]) =>
    setDraft({
      rules: next,
      version: draft?.version ?? audience.data?.version ?? "",
    });

  if (audience.isLoading) {
    return (
      <Text muted small>
        Loading who can use this credential…
      </Text>
    );
  }
  if (audience.isError || !audience.data) {
    return (
      <FieldError>
        Who can use this credential could not be loaded. Refresh the page to try
        again.
      </FieldError>
    );
  }

  const describe = (principalUrn: string) => {
    if (principalUrn === EVERYONE_URN) {
      return { name: "Everyone in the organization", kind: "everyone" };
    }
    const option = names.get(principalUrn);
    return {
      name: option?.displayName ?? "Former member",
      kind: option?.kind ?? principalUrn.split(":")[0] ?? "unknown",
    };
  };

  return (
    <section className="space-y-3">
      <div className="flex items-start justify-between gap-4">
        <div className="space-y-1">
          <h3 className="text-sm font-medium">Who can use this</h3>
          <Text muted small className="block">
            Applies wherever this credential is used. People still need access
            to each server.
          </Text>
        </div>
        {editable ? (
          <Button variant="secondary" size="sm" onClick={() => setAdding(true)}>
            <Button.LeftIcon>
              <Plus className="h-4 w-4" />
            </Button.LeftIcon>
            <Button.Text>Add</Button.Text>
          </Button>
        ) : null}
      </div>

      {canManage && hasUnsupported ? (
        <Alert variant="warning" inline>
          This credential has a rule this page cannot show. Update the dashboard
          before changing who can use it.
        </Alert>
      ) : null}

      {audience.data.publicServers.length > 0 ? (
        <Alert variant="warning" inline>
          A public server uses this credential. Anonymous callers there never
          get these rules; only callers who sign in do.
        </Alert>
      ) : null}

      {rules.length === 0 && inherited.length === 0 ? (
        <Text muted small className="block">
          Nobody can use this credential yet.
        </Text>
      ) : (
        <ul className="border-border divide-y border">
          {rules.map((rule) => {
            const who = describe(rule.principalUrn);
            const note =
              rule.level === "use" ? notes.get(rule.principalUrn) : undefined;
            return (
              <li
                key={rule.principalUrn}
                className="flex items-center gap-3 px-3 py-2"
              >
                <PrincipalBadge kind={who.kind} />
                <div className="min-w-0 flex-1">
                  <span className="block truncate text-sm font-medium">
                    {who.name}
                  </span>
                  {note ? (
                    <Text muted small className="block">
                      {note}
                    </Text>
                  ) : null}
                </div>
                {editable ? (
                  <>
                    <SegmentedControl<Level>
                      ariaLabel={`Access for ${who.name}`}
                      value={rule.level}
                      onChange={(level) =>
                        update(
                          rules.map((r) =>
                            r.principalUrn === rule.principalUrn
                              ? { ...r, level }
                              : r,
                          ),
                        )
                      }
                      options={[
                        { value: "use", label: LEVEL_LABEL.use },
                        { value: "blocked", label: LEVEL_LABEL.blocked },
                      ]}
                    />
                    <Button
                      variant="tertiary"
                      size="sm"
                      aria-label={`Remove ${who.name}`}
                      onClick={() =>
                        update(
                          rules.filter(
                            (r) => r.principalUrn !== rule.principalUrn,
                          ),
                        )
                      }
                    >
                      <Trash2 className="h-4 w-4" />
                    </Button>
                  </>
                ) : (
                  <Text small>{LEVEL_LABEL[rule.level]}</Text>
                )}
              </li>
            );
          })}
          {inherited.map((entry) => (
            <InheritedRule
              key={`${entry.principalUrn}:${entry.level}:${entry.appliesTo}:${entry.projectId ?? ""}`}
              entry={entry}
              who={describe(entry.principalUrn)}
              clientType={client.clientType}
            />
          ))}
        </ul>
      )}

      {editable && draft !== null ? (
        <div className="flex justify-end gap-2">
          <Button
            variant="secondary"
            size="sm"
            onClick={() => setDraft(null)}
            disabled={save.isPending}
          >
            <Button.Text>Discard</Button.Text>
          </Button>
          <Button
            variant="primary"
            size="sm"
            disabled={save.isPending}
            onClick={() =>
              save.mutate({
                request: {
                  setAuthClientAudienceRequestBody: {
                    id: client.id,
                    expectedVersion: draft.version,
                    entries: rules.map((rule) => ({
                      principalUrn: rule.principalUrn,
                      level:
                        rule.level === "blocked"
                          ? AuthClientAudienceRuleLevel.Blocked
                          : AuthClientAudienceRuleLevel.Use,
                    })),
                  },
                },
              })
            }
          >
            <Button.Text>{save.isPending ? "Saving…" : "Save"}</Button.Text>
          </Button>
        </div>
      ) : null}

      {adding ? (
        <AddAudienceDialog
          title="Who else can use this credential?"
          description="They can use it on every server that uses it, as long as they can reach the server."
          kinds={["everyone", "user", "role", "agent"]}
          alreadyAdded={rules.map((rule) => rule.principalUrn)}
          blockedReason={blockedReason}
          pending={false}
          onAdd={(principalUrns) => {
            update([
              ...rules,
              ...principalUrns.map((principalUrn) => ({
                principalUrn,
                level: "use" as const,
              })),
            ]);
            setAdding(false);
          }}
          onClose={() => setAdding(false)}
        />
      ) : null}
    </section>
  );
}

function InheritedRule({
  entry,
  who,
  clientType,
}: {
  entry: AuthClientAudienceEntry;
  who: { name: string; kind: string };
  clientType: string;
}): JSX.Element {
  const organization = useOrganization();
  const project = entry.projectId
    ? organization.projects.find((p) => p.id === entry.projectId)
    : undefined;
  const where = entry.projectId
    ? ` on servers in ${project ? `the ${project.name} project` : "one project"}`
    : "";
  const source =
    entry.appliesTo === "client_type"
      ? `From a rule covering all “${clientTypeLabel(clientType)}” credentials${where}`
      : `From a rule covering every credential${where}`;
  return (
    <li className="bg-muted/15 flex items-center gap-3 px-3 py-2">
      <PrincipalBadge kind={who.kind} />
      <div className="min-w-0 flex-1">
        <span className="block truncate text-sm font-medium">{who.name}</span>
        <Text muted small className="block">
          {source}
          {entry.connectionNote ? `. ${entry.connectionNote}` : ""}
        </Text>
      </div>
      <Text small>{entry.level === "blocked" ? "Blocked" : "Can use"}</Text>
    </li>
  );
}

/**
 * The headers this credential sends. An administrator-created credential
 * owns them, so they are edited and rotated here; a credential copied from
 * this server's identity settings follows those settings.
 */
function HeadersSection({
  client,
  mcpServerId,
  canManage,
}: {
  client: AuthClient;
  mcpServerId: string;
  canManage: boolean;
}): JSX.Element | null {
  const queryClient = useQueryClient();
  const others = otherServers(client, mcpServerId);
  const editable = canManage && client.authClientSource === "admin";
  const [drafts, setDrafts] = useState<HeaderDraft[] | null>(null);
  const [confirming, setConfirming] = useState(false);
  const save = useSetAuthClientHeadersMutation({
    onSuccess: () => {
      setDrafts(null);
      setConfirming(false);
      toast.success(`Saved the headers ${client.name} sends`);
      void invalidateAuthClientQueries(queryClient);
    },
    onError: (error) =>
      toast.error(
        error.message || "The headers could not be saved. Try again.",
      ),
  });

  if (
    client.headers.length === 0 &&
    !editable &&
    !generatesAuthorization(client.clientType)
  ) {
    return null;
  }

  const shown = drafts ?? headerDraftsFrom(client.headers);
  const problem =
    drafts === null ? null : headerDraftProblem(client.clientType, drafts);
  const commit = () =>
    save.mutate({
      request: {
        setAuthClientHeadersRequestBody: {
          id: client.id,
          headers: headerInputs(drafts ?? []),
        },
      },
    });

  return (
    <section className="space-y-3">
      <div className="space-y-1">
        <h3 className="text-sm font-medium">Headers sent upstream</h3>
        <Text muted small className="block">
          {client.authClientSource === "admin"
            ? "Secret values are stored encrypted and never shown. Leave a stored secret empty to keep it."
            : "This credential follows this server's identity settings above. Change its headers there."}
        </Text>
      </div>
      <CredentialHeadersEditor
        clientType={client.clientType}
        drafts={shown}
        onChange={setDrafts}
        readOnly={!editable}
      />
      {editable && drafts !== null ? (
        <div className="flex items-center justify-end gap-2">
          {problem ? (
            <Text muted small className="mr-auto">
              {problem}
            </Text>
          ) : null}
          <Button
            variant="secondary"
            size="sm"
            onClick={() => setDrafts(null)}
            disabled={save.isPending}
          >
            <Button.Text>Discard</Button.Text>
          </Button>
          <Button
            variant="primary"
            size="sm"
            disabled={save.isPending || !!problem}
            onClick={() => (others.length > 0 ? setConfirming(true) : commit())}
          >
            <Button.Text>{save.isPending ? "Saving…" : "Save"}</Button.Text>
          </Button>
        </div>
      ) : null}
      <ConfirmDialog
        open={confirming}
        onOpenChange={setConfirming}
        title={`Change what ${client.name} sends on every server?`}
        description="These headers are part of the credential, so the change applies wherever it is used."
        confirmLabel="Save on every server"
        isPending={save.isPending}
        onConfirm={commit}
        impact={{
          summary: "It is also used on these servers:",
          mcpServerNames: others.map(serverLabel),
        }}
      />
    </section>
  );
}
