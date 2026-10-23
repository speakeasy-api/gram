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
import {
  EMAProviderInputKind,
  type EMAProviderInput,
} from "@gram/client/models/components/emaproviderinput.js";
import { isKnownEnumValue, requireKnownEnumValue } from "@/lib/enum";
import { GramError } from "@gram/client/models/errors/gramerror.js";
import { invalidateAllOrganizationRemoteSessionIssuerEMAServices } from "@gram/client/react-query/organizationRemoteSessionIssuerEMAServices.js";
import { useSetOrganizationRemoteSessionIssuerEMAProvidersMutation } from "@gram/client/react-query/setOrganizationRemoteSessionIssuerEMAProviders.js";

import {
  joinNames,
  serverDisplayName,
  strandedNotes,
  strandedServers,
  supportsProvider,
  type ProviderView,
  type ServerSignIn,
} from "./emaServiceView";

/** The API's limit on how many providers a service can be saved with. */
const MAX_ACCEPTED = 10;

/** One identity provider an admin can choose for a service to accept. */
export type ProviderCandidate = {
  key: string;
  kind: ProviderView["kind"];
  name: string;
  /** The external provider's issuer record. */
  remoteSessionIssuerId?: string;
};

/**
 * Which identity providers are enabled for a service. Each server is vouched for only
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
  refreshCandidates,
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
  /** Reload the candidates, so a provider another admin just added can be kept. */
  refreshCandidates?: () => Promise<unknown>;
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
  const [base, setBase] = useState<ProviderCandidate[] | null>(null);
  const [conflict, setConflict] = useState(false);
  // A refused save waits for the current list before the editor can rebase
  // onto it, or the next save would conflict again.
  const [refreshing, setRefreshing] = useState(false);
  const supportsEditing =
    current.every(supportsProvider) &&
    (isDefault || current.every(supportsProviderInput)) &&
    candidates.every(supportsProviderInput);
  const saved: ProviderCandidate[] = isDefault
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
  const rows: ProviderCandidate[] = [
    ...candidates,
    ...saved.filter((entry) => !candidates.some((c) => c.key === entry.key)),
  ];
  // A saved Speakeasy whose issuer was deleted and set up again; saving
  // points it at the current issuer.
  const speakeasyRecreated =
    candidates.some((c) => c.kind === "speakeasy") &&
    current.some(
      (p) => p.kind === "speakeasy" && p.unavailableReason === "deleted",
    );
  // Judged on every render, so a draft started before the candidates loaded
  // is checked against them once they have. An entry that is only paused or
  // disabled stays valid and gets a readiness note instead.
  const available = (entry: ProviderCandidate): boolean | undefined => {
    const provider = current.find((p) => p.key === entry.key);
    if (provider && !supportsProvider(provider)) return undefined;
    return (
      !candidatesReady ||
      (candidates.some((c) => c.key === entry.key) &&
        (entry.kind === "speakeasy" ||
          provider?.unavailableReason !== "deleted"))
    );
  };
  const readiness = (key: string): string | undefined => {
    if (key === "speakeasy" && speakeasyRecreated)
      return canEdit
        ? "Speakeasy was set up again. Save this list again to use it."
        : "Speakeasy was set up again. An organization admin needs to save this list again to use it.";
    const provider = current.find((p) => p.key === key);
    if (provider && !supportsProvider(provider)) return provider.unavailable;
    return provider && !provider.usable && !provider.placeholder
      ? `${provider.unavailable}. It’s skipped until it works again.`
      : undefined;
  };
  const startEditing = (keys: string[]) => {
    if (!supportsEditing || !canEdit || !candidatesReady) return;
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
  // A refused Use default reports the conflict outside the editor too.
  const changedUnderneath =
    conflict || (editing && base !== null && !sameOrder(base, saved));
  const dirty =
    editing &&
    (isDefault ||
      (speakeasyRecreated && checkedKeys.includes("speakeasy")) ||
      !sameSet(
        checked.map((entry) => entry.key),
        saved.map((entry) => entry.key),
      ));
  const invalidChecked = checked.filter((entry) => !available(entry));
  const strandingKeys =
    supportsEditing && custom ? checked.map((entry) => entry.key) : null;
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
          ? `All identity providers are enabled for ${serviceName}`
          : `Saved enabled identity providers for ${serviceName}`,
      );
    },
    onError: (error) => {
      if (error instanceof GramError && error.statusCode === 409) {
        setConflict(true);
        setRefreshing(true);
        void Promise.all([
          invalidateAllOrganizationRemoteSessionIssuerEMAServices(queryClient),
          refreshCandidates?.(),
        ]).finally(() => setRefreshing(false));
      }
    },
  });
  const submit = (entries: ProviderCandidate[]) => {
    if (
      !remoteSessionIssuerId ||
      !supportsEditing ||
      save.isPending ||
      refreshing ||
      changedUnderneath ||
      !entries.every(supportsProviderInput) ||
      !(base ?? saved).every(supportsProviderInput)
    )
      return;
    if (
      entries.length === 0
        ? !canRestore
        : !canEdit ||
          !candidatesReady ||
          !dirty ||
          entries.length > MAX_ACCEPTED ||
          invalidChecked.length > 0
    )
      return;
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
  const toggle = (key: string, on: boolean) => {
    if (!supportsEditing || !canEdit) return;
    setDraft((keys) =>
      on
        ? [...(keys ?? []), key]
        : (keys ?? []).filter((existing) => existing !== key),
    );
  };
  // Editing the default starts from what it does today: every candidate.
  const edit = () =>
    startEditing(
      isDefault
        ? candidates.map((c) => c.key)
        : saved.map((entry) => entry.key),
    );
  // In the default every provider is enabled. Before the candidates load,
  // or for someone who can't load them, show what the service lists today.
  const defaultRows: { key: string; name: string }[] =
    supportsEditing && candidatesReady && candidates.length > 0
      ? candidates
      : current;

  return (
    <SettingsSection id="accepted-providers">
      <SettingsSection.Header>
        <SettingsSection.Title>
          Enabled identity providers for {serviceName}
        </SettingsSection.Title>
        <SettingsSection.Description>
          When someone uses a server that connects to {serviceName}, Speakeasy
          vouches for them using their existing login, such as Okta or
          Speakeasy’s own sign-in. By default, all identity providers are
          enabled. Uncheck an identity provider to require an authentication
          method outside Enterprise Managed Authorization for people who log in
          with it, such as signing in to their own {serviceName} account.
        </SettingsSection.Description>
      </SettingsSection.Header>
      <SettingsSection.Panel>
        <SettingsSection.Body>
          {!custom ? (
            <div className="space-y-3">
              <Text small>Default: all identity providers enabled</Text>
              <ul aria-label="Enabled identity providers" className="space-y-1">
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
                      {editing
                        ? `The enabled identity providers for ${serviceName} changed since you started editing.`
                        : `Another admin changed the enabled identity providers for ${serviceName}, so it wasn’t reset to the default.`}{" "}
                      {refreshing
                        ? "Loading the current choice…"
                        : saved.length > 0
                          ? `Now enabled: ${joinNames(saved.map((entry) => entry.name))}.`
                          : "All identity providers are enabled now."}
                    </Text>
                    <div className="flex flex-wrap gap-2">
                      {editing && (
                        <Button
                          variant="secondary"
                          size="sm"
                          onClick={stopEditing}
                        >
                          Discard my changes
                        </Button>
                      )}
                      <Button
                        variant="tertiary"
                        size="sm"
                        disabled={refreshing || !supportsEditing}
                        onClick={() => {
                          if (!supportsEditing) return;
                          if (editing) setBase(saved);
                          setConflict(false);
                          save.reset();
                        }}
                      >
                        {editing ? "Review and keep editing" : "Review"}
                      </Button>
                    </div>
                  </div>
                </Alert>
              )}
              {editing && canEdit && supportsEditing ? (
                <ul
                  aria-label="Identity providers to enable"
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
                            canEdit
                          />
                        </span>
                      </li>
                    );
                  })}
                </ul>
              ) : (
                <ul
                  aria-label="Enabled identity providers"
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
                            canEdit={canEdit && supportsEditing}
                          />
                        )}
                      </AcceptedRow>
                    );
                  })}
                </ul>
              )}
              {editing && checked.length === 0 && (
                <Text muted small>
                  Enable at least one identity provider.
                </Text>
              )}
              {editing && checked.length > MAX_ACCEPTED && (
                <Text muted small>
                  You can enable up to {MAX_ACCEPTED} identity providers.
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
              You can change this once a server connects to {serviceName}.
            </Text>
          )}
          {readOnlyReason && (
            <Text muted small>
              {readOnlyReason}
            </Text>
          )}
          {!supportsEditing && (
            <Text muted small>
              Refresh the page before changing this unsupported identity
              provider configuration.
            </Text>
          )}
          {canEdit && candidatesError ? (
            <Alert variant="error" alignTop>
              <div className="flex flex-col items-start gap-2">
                <Text small className="block">
                  Couldn’t load the identity providers you can choose from. You
                  can’t change this right now.
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
                  ? `Applies to each person’s next request. People who log in with a provider you remove will need to sign in to their own ${serviceName} account.`
                  : "Use default to enable every identity provider again."}
              </SettingsSection.FooterHint>
            )}
            <SettingsSection.FooterActions>
              {canRestore && !isDefault && (
                <Button
                  variant="secondary"
                  size="sm"
                  disabled={
                    save.isPending ||
                    !supportsEditing ||
                    changedUnderneath ||
                    refreshing
                  }
                  onClick={() => submit([])}
                >
                  Use default
                </Button>
              )}
              {canEdit && !editing && (
                <Button
                  variant="secondary"
                  size="sm"
                  disabled={
                    save.isPending || !candidatesReady || !supportsEditing
                  }
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
                      !supportsEditing ||
                      !candidatesReady ||
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

/** One provider and whether it's enabled for the service, outside the editor. */
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
          {accepted ? "Enabled" : "Not enabled"}
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
  canEdit,
}: {
  available: boolean | undefined;
  readiness: string | undefined;
  canEdit: boolean;
}): JSX.Element | null {
  if (available === false)
    return (
      <Text as="span" small className="text-default-warning">
        No longer available.{canEdit ? " Uncheck it to save." : ""}
      </Text>
    );
  if (!readiness) return null;
  return (
    <Text as="span" muted small>
      {readiness}
    </Text>
  );
}

function providerInput(entry: ProviderCandidate): EMAProviderInput {
  const kind = requireKnownEnumValue(EMAProviderInputKind, entry.kind);
  if (kind === "external" && !entry.remoteSessionIssuerId)
    throw new Error(
      "An external identity provider needs an issuer before it can be saved.",
    );
  return kind === "speakeasy"
    ? { kind }
    : { kind, remoteSessionIssuerId: entry.remoteSessionIssuerId };
}

function supportsProviderInput(
  entry: Pick<ProviderCandidate, "kind" | "remoteSessionIssuerId">,
): boolean {
  return (
    isKnownEnumValue(EMAProviderInputKind, entry.kind) &&
    (entry.kind !== "external" || Boolean(entry.remoteSessionIssuerId))
  );
}

function sameOrder(a: ProviderCandidate[], b: ProviderCandidate[]): boolean {
  return (
    a.length === b.length && a.every((entry, i) => entry.key === b[i]?.key)
  );
}

function sameSet(a: string[], b: string[]): boolean {
  return a.length === b.length && a.every((key) => b.includes(key));
}
