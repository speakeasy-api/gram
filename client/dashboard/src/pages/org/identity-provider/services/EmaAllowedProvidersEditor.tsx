import { useMemo, useState, type ReactNode } from "react";
import { Check } from "lucide-react";
import { useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";

import { ApiErrorAlert } from "@/components/api-error-alert";
import { SettingsSection } from "@/components/detail/settings-section";
import { Alert } from "@/components/ui/Alert";
import { Button } from "@/components/ui/Button";
import { Checkbox } from "@/components/ui/Checkbox";
import { Label } from "@/components/ui/Label";
import { Text } from "@/components/ui/Text";
import type { EMAProviderInput } from "@gram/client/models/components/emaproviderinput.js";
import { GramError } from "@gram/client/models/errors/gramerror.js";
import { invalidateAllOrganizationRemoteSessionIssuerEMAServices } from "@gram/client/react-query/organizationRemoteSessionIssuerEMAServices.js";
import { useSetOrganizationRemoteSessionIssuerEMAProvidersMutation } from "@gram/client/react-query/setOrganizationRemoteSessionIssuerEMAProviders.js";

import {
  joinNames,
  serverDisplayName,
  strandedNotes,
  strandedServers,
  type ProviderView,
  type ServerSignIn,
} from "./emaServiceView";

/** The API's limit on how many providers a service can be saved with. */
const MAX_ACCEPTED = 10;

/** One identity provider an admin can choose for a service to accept. */
export type ProviderCandidate = {
  key: string;
  kind: "external" | "speakeasy";
  name: string;
  /** The external provider's issuer record. */
  remoteSessionIssuerId?: string;
};

type Entry = ProviderCandidate;

/**
 * Which identity providers a service accepts. Each server is vouched for only
 * by the provider its sign-in goes through, so the saved list works as a set:
 * it's stored in order, but the order never changes who vouches. Key it by
 * service so a draft never carries over to another one.
 */
export function EmaAllowedProvidersEditor({
  serviceName,
  remoteSessionIssuerId,
  current,
  isDefault,
  candidates,
  candidatesReady,
  candidatesError,
  canEdit,
  canRestore,
  readOnlyReason,
  servers,
}: {
  serviceName: string;
  /** Any issuer record naming the service; the API keys the list by it. */
  remoteSessionIssuerId: string | undefined;
  current: ProviderView[];
  isDefault: boolean;
  candidates: ProviderCandidate[];
  /** Whether every candidate has loaded, so a saved entry missing from them is really gone. */
  candidatesReady: boolean;
  /** Why the candidates couldn't load, with a retry; editing waits for them. */
  candidatesError?: { retry: () => void };
  /** Choose and save the accepted providers. */
  canEdit: boolean;
  /** Go back to the default. */
  canRestore: boolean;
  /** Why the person can't edit, when they can't. */
  readOnlyReason?: string;
  /** The servers that reach the service, by the sign-in that decides who can vouch for them. */
  servers: (ServerSignIn & { name: string; slug: string })[];
}): JSX.Element {
  const queryClient = useQueryClient();
  // The keys of the providers checked in the draft.
  const [draft, setDraft] = useState<string[] | null>(null);
  // The saved list the draft started from, so a change underneath it stops
  // Save instead of silently restoring what another admin removed.
  const [base, setBase] = useState<Entry[] | null>(null);
  const [conflict, setConflict] = useState(false);
  // A refused save waits for the current list before the editor can rebase
  // onto it, or the next save would conflict again.
  const [refreshing, setRefreshing] = useState(false);
  const saved: Entry[] = isDefault
    ? []
    : current.map((provider) => {
        const candidate = candidates.find((c) => c.key === provider.key);
        return {
          key: provider.key,
          kind: provider.kind,
          name: candidate?.name ?? provider.name,
          remoteSessionIssuerId: provider.remoteSessionIssuerId,
        };
      });
  // Every candidate in its stable order, then any saved entry that is no
  // longer one, so it can be unchecked.
  const rows: Entry[] = [
    ...candidates,
    ...saved.filter((entry) => !candidates.some((c) => c.key === entry.key)),
  ];
  // Judged on every render, so a draft started before the candidates loaded
  // is checked against them once they have. An entry that is only paused or
  // disabled stays valid and gets a readiness note instead.
  const available = (entry: Entry): boolean =>
    !candidatesReady ||
    (candidates.some((c) => c.key === entry.key) &&
      current.find((p) => p.key === entry.key)?.unavailableReason !==
        "deleted");
  const readiness = (key: string): string | undefined => {
    const provider = current.find((p) => p.key === key);
    return provider && !provider.usable && !provider.placeholder
      ? provider.unavailable
      : undefined;
  };
  const startEditing = (keys: string[]) => {
    setBase(saved);
    setConflict(false);
    setDraft(keys);
  };
  const stopEditing = () => {
    setDraft(null);
    setBase(null);
    setConflict(false);
  };
  const editing = draft !== null;
  const custom = editing || !isDefault;
  const checkedKeys = draft ?? saved.map((entry) => entry.key);
  const checked = rows.filter((row) => checkedKeys.includes(row.key));
  const changedUnderneath =
    editing && (conflict || (base !== null && !sameOrder(base, saved)));
  const dirty =
    editing &&
    (isDefault ||
      !sameSet(
        checked.map((entry) => entry.key),
        saved.map((entry) => entry.key),
      ));
  const invalidChecked = checked.filter((entry) => !available(entry));
  const strandingKeys = custom ? checked.map((entry) => entry.key) : null;
  const strandingKey = strandingKeys?.join(",");
  const stranded = useMemo(
    () =>
      strandedNotes(
        strandedServers(
          strandingKeys === null
            ? null
            : rows.filter((row) => strandingKeys.includes(row.key)),
          servers,
        ).map((server) => ({ ...server, name: serverDisplayName(server) })),
        { draft: editing },
      ),
    // eslint-disable-next-line react-hooks/exhaustive-deps -- rows and strandingKeys are rebuilt each render; strandingKey stands for both
    [strandingKey, servers, editing],
  );

  const save = useSetOrganizationRemoteSessionIssuerEMAProvidersMutation({
    onSuccess: async (result) => {
      // Refresh before dropping the draft, so the saved list never flickers.
      await Promise.all([
        invalidateAllOrganizationRemoteSessionIssuerEMAServices(queryClient),
        queryClient.invalidateQueries({
          queryKey: ["identityChaining", "readEMA"],
        }),
      ]);
      stopEditing();
      toast.success(
        result.isDefault
          ? `${serviceName} accepts every identity provider`
          : `Saved the identity providers ${serviceName} accepts`,
      );
    },
    onError: (error) => {
      if (error instanceof GramError && error.statusCode === 409) {
        setConflict(true);
        setRefreshing(true);
        void invalidateAllOrganizationRemoteSessionIssuerEMAServices(
          queryClient,
        ).finally(() => setRefreshing(false));
      }
    },
  });
  const submit = (entries: Entry[]) => {
    if (!remoteSessionIssuerId) return;
    save.mutate({
      request: {
        setEMAProvidersRequestBody: {
          remoteSessionIssuerId,
          providers: entries.map(providerInput),
          expectedProviders: (base ?? saved).map(providerInput),
        },
      },
    });
  };
  const conflictError =
    save.error instanceof GramError && save.error.statusCode === 409;
  const toggle = (key: string, on: boolean) =>
    setDraft((keys) =>
      on
        ? [...(keys ?? []), key]
        : (keys ?? []).filter((existing) => existing !== key),
    );
  // Editing the default starts from what it does today: every candidate.
  const edit = () =>
    startEditing(
      isDefault
        ? candidates.map((c) => c.key)
        : saved.map((entry) => entry.key),
    );
  // In the default every provider is accepted. Before the candidates load,
  // or for someone who can't load them, show what the service lists today.
  const defaultRows: { key: string; name: string }[] =
    candidatesReady && candidates.length > 0 ? candidates : current;

  return (
    <SettingsSection id="accepted-providers">
      <SettingsSection.Header>
        <SettingsSection.Title>
          Identity providers {serviceName} accepts
        </SettingsSection.Title>
        <SettingsSection.Description>
          Each server is vouched for by the identity provider its sign-in goes
          through, and only if {serviceName} accepts it here. By default every
          identity provider is accepted.
        </SettingsSection.Description>
      </SettingsSection.Header>
      <SettingsSection.Panel>
        <SettingsSection.Body>
          {!custom ? (
            <div className="space-y-3">
              <Text small>Default: accepts every identity provider</Text>
              <ul
                aria-label="Accepted identity providers"
                className="space-y-1"
              >
                {defaultRows.map((row) => (
                  <AcceptedRow key={row.key} name={row.name} accepted />
                ))}
              </ul>
            </div>
          ) : (
            <>
              {changedUnderneath && (
                <Alert variant="warning" alignTop>
                  <div className="flex flex-col items-start gap-2">
                    <Text small className="block">
                      What {serviceName} accepts changed since you started
                      editing.{" "}
                      {refreshing
                        ? "Loading the current choice…"
                        : saved.length > 0
                          ? `It now accepts ${joinNames(saved.map((entry) => entry.name))}.`
                          : "It accepts every identity provider now."}
                    </Text>
                    <div className="flex flex-wrap gap-2">
                      <Button
                        variant="secondary"
                        size="sm"
                        onClick={stopEditing}
                      >
                        Discard my changes
                      </Button>
                      <Button
                        variant="tertiary"
                        size="sm"
                        disabled={refreshing}
                        onClick={() => {
                          setBase(saved);
                          setConflict(false);
                          save.reset();
                        }}
                      >
                        Review and keep editing
                      </Button>
                    </div>
                  </div>
                </Alert>
              )}
              {editing && canEdit ? (
                <ul
                  aria-label="Identity providers to accept"
                  className="space-y-2"
                >
                  {rows.map((row) => {
                    const id = `accept-${row.key}`;
                    const on = checkedKeys.includes(row.key);
                    return (
                      <li key={row.key} className="flex items-start gap-3">
                        <Checkbox
                          id={id}
                          className="mt-0.5"
                          checked={on}
                          disabled={
                            save.isPending ||
                            (!on && checked.length >= MAX_ACCEPTED)
                          }
                          onCheckedChange={(value) =>
                            toggle(row.key, value === true)
                          }
                        />
                        <span className="flex min-w-0 flex-1 flex-wrap items-baseline gap-x-2">
                          <Label
                            htmlFor={id}
                            className="truncate text-sm font-medium"
                          >
                            {row.name}
                          </Label>
                          <EntryNote
                            available={available(row)}
                            readiness={on ? readiness(row.key) : undefined}
                          />
                        </span>
                      </li>
                    );
                  })}
                </ul>
              ) : (
                <ul
                  aria-label="Accepted identity providers"
                  className="space-y-1"
                >
                  {rows.map((row) => {
                    const accepted = checkedKeys.includes(row.key);
                    return (
                      <AcceptedRow
                        key={row.key}
                        name={row.name}
                        accepted={accepted}
                      >
                        {accepted && (
                          <EntryNote
                            available={available(row)}
                            readiness={readiness(row.key)}
                          />
                        )}
                      </AcceptedRow>
                    );
                  })}
                </ul>
              )}
              {editing && checked.length === 0 && (
                <Text muted small>
                  Accept at least one identity provider.
                </Text>
              )}
              {editing && checked.length > MAX_ACCEPTED && (
                <Text muted small>
                  A service can accept at most {MAX_ACCEPTED} identity
                  providers.
                </Text>
              )}
              {stranded.map((note) => (
                <Text key={note} small className="block">
                  {note}
                </Text>
              ))}
            </>
          )}
          {(canEdit || canRestore) && !remoteSessionIssuerId && (
            <Text muted small>
              This can be changed once a server reaches {serviceName}.
            </Text>
          )}
          {readOnlyReason && (
            <Text muted small>
              {readOnlyReason}
            </Text>
          )}
          {canEdit && candidatesError ? (
            <Alert variant="error" alignTop>
              <div className="flex flex-col items-start gap-2">
                <Text small className="block">
                  Couldn’t load the identity providers you can choose from, so
                  this can’t be changed right now.
                </Text>
                <Button
                  variant="secondary"
                  size="sm"
                  onClick={candidatesError.retry}
                >
                  Retry
                </Button>
              </div>
            </Alert>
          ) : (
            canEdit &&
            !candidatesReady && (
              <Text muted small role="status">
                Checking which identity providers you can choose from…
              </Text>
            )
          )}
          <ApiErrorAlert error={conflictError ? null : save.error} />
        </SettingsSection.Body>
        {remoteSessionIssuerId && (canEdit || (canRestore && !isDefault)) && (
          <SettingsSection.Footer>
            {(editing || custom) && (
              <SettingsSection.FooterHint>
                {editing
                  ? "Takes effect for the next person who connects."
                  : "Go back to the default to accept every identity provider."}
              </SettingsSection.FooterHint>
            )}
            <SettingsSection.FooterActions>
              {canRestore && !isDefault && (
                <Button
                  variant="secondary"
                  size="sm"
                  disabled={save.isPending}
                  onClick={() => submit([])}
                >
                  Use default
                </Button>
              )}
              {canEdit && !editing && (
                <Button
                  variant="secondary"
                  size="sm"
                  disabled={save.isPending || !candidatesReady}
                  onClick={edit}
                >
                  Edit
                </Button>
              )}
              {canEdit && editing && (
                <>
                  <Button
                    variant="secondary"
                    size="sm"
                    disabled={save.isPending}
                    onClick={stopEditing}
                  >
                    Cancel
                  </Button>
                  <Button
                    variant="primary"
                    size="sm"
                    disabled={
                      save.isPending ||
                      changedUnderneath ||
                      !dirty ||
                      checked.length === 0 ||
                      checked.length > MAX_ACCEPTED ||
                      invalidChecked.length > 0
                    }
                    onClick={() => submit(checked)}
                  >
                    {save.isPending ? "Saving…" : "Save"}
                  </Button>
                </>
              )}
            </SettingsSection.FooterActions>
          </SettingsSection.Footer>
        )}
      </SettingsSection.Panel>
    </SettingsSection>
  );
}

/** One provider and whether the service accepts it, outside the editor. */
function AcceptedRow({
  name,
  accepted,
  children,
}: {
  name: string;
  accepted: boolean;
  children?: ReactNode;
}): JSX.Element {
  return (
    <li className="flex items-start gap-3">
      <span className="flex size-4 shrink-0 translate-y-0.5 items-center justify-center">
        {accepted && <Check aria-hidden="true" className="size-3.5" />}
      </span>
      <span className="flex min-w-0 flex-1 flex-wrap items-baseline gap-x-2">
        <Text
          as="span"
          small
          className={accepted ? "font-medium" : "text-muted-foreground"}
        >
          {name}
        </Text>
        <Text as="span" muted small>
          {accepted ? "Accepted" : "Not accepted"}
        </Text>
        {children}
      </span>
    </li>
  );
}

/** Why a checked provider can't be saved, or can't vouch right now. */
function EntryNote({
  available,
  readiness,
}: {
  available: boolean;
  readiness: string | undefined;
}): JSX.Element | null {
  if (!available)
    return (
      <Text as="span" small className="text-default-warning">
        No longer available. Uncheck it to save.
      </Text>
    );
  if (!readiness) return null;
  return (
    <Text as="span" muted small>
      {readiness}. It’s skipped until it can vouch again.
    </Text>
  );
}

function providerInput(entry: ProviderCandidate): EMAProviderInput {
  return entry.kind === "speakeasy" || !entry.remoteSessionIssuerId
    ? { kind: "speakeasy" }
    : { kind: "external", remoteSessionIssuerId: entry.remoteSessionIssuerId };
}

function sameOrder(a: Entry[], b: Entry[]): boolean {
  return (
    a.length === b.length && a.every((entry, i) => entry.key === b[i]?.key)
  );
}

function sameSet(a: string[], b: string[]): boolean {
  return a.length === b.length && a.every((key) => b.includes(key));
}
