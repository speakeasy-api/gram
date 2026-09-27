import { sessionAccountIdentity } from "@/components/sessions/session-account-identity";
import { useEffect, useRef, useState } from "react";
import { Check } from "lucide-react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useOrganization, useSession } from "@/contexts/Auth";
import { useSdkClient } from "@/contexts/Sdk";
import { useFeatureFlag, type FeatureFlagResult } from "@/hooks/useFeatureFlag";
import { FEATURE_FLAGS } from "@/lib/featureFlags";
import { SettingsSection } from "@/components/page-templates";
import { Button } from "@/components/ui/Button";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/Select";
import { CopyButton } from "@/components/ui/CopyButton";
import { AgentKeyReview, type KeyReviewAccount } from "./AgentKeyReview";
import type { RemoteSession } from "@gram/client/models/components/remotesession.js";
import type { ListBindingsResponseBody } from "@gram/client/models/components/listbindingsresponsebody.js";
import { queryKeyRemoteSessionClients } from "@gram/client/react-query/remoteSessionClients.js";
import { queryKeyRemoteSessions } from "@gram/client/react-query/remoteSessions.js";
import { queryKeyRemoteSessionsListBindings } from "@gram/client/react-query/remoteSessionsListBindings.js";
import { Dialog } from "@/components/ui/Dialog";
import { Input } from "@/components/ui/Input";
import { Text } from "@/components/ui/Text";
import { Table, type Column } from "@/components/ui/Table";
import type { ManagedAgent } from "@gram/client/models/components/managedagent.js";
import type { Key } from "@gram/client/models/components/key.js";
import type { AgentPolicyGrantForm } from "@gram/client/models/components/agentpolicygrantform.js";
import {
  buildRequestedGrants,
  delegableGrantKey,
  validateAgentAPIKeyName,
} from "./agent-api-key-grants";
import { AgentGrantSelector, type GrantNarrowings } from "./AgentGrantSelector";
import { useListAPIKeys } from "@gram/client/react-query/listAPIKeys";
import { useCreateAPIKeyMutation } from "@gram/client/react-query/createAPIKey";
import { useRevokeAPIKeyMutation } from "@gram/client/react-query/revokeAPIKey";

import { AgentKeyServers, type KeyServer } from "./AgentKeyServers";

import { narrowGrantsToServers } from "./agent-key-server-grants";
import { discoverKeyServerGrants } from "./agent-key-discovery";

const security = { sessionHeaderGramSession: "" };

export function AgentAPIKeys({
  agent,
  creation = false,
  onCreate,
  onDone,
  onBusy,
}: {
  agent: ManagedAgent;
  creation?: boolean;
  onCreate?: () => void;
  onDone?: () => void;
  onBusy?: (busy: boolean) => void;
}): JSX.Element {
  const organization = useOrganization();
  // Delegable candidates are specific to the authorizer, and the signed-in
  // user can change without the organization changing.
  const { user } = useSession();
  const flag = useFeatureFlag(FEATURE_FLAGS.agentCredentials);
  if (creation)
    return (
      <AgentAPIKeysContent
        key={`${organization.id}:${user.id}:${agent.id}:${agent.permissions.authorize}`}
        agent={agent}
        organizationId={organization.id}
        userId={user.id}
        flag={flag}
        creation
        onDone={onDone}
        onBusy={onBusy}
      />
    );
  return (
    <SettingsSection>
      <SettingsSection.Header>
        <SettingsSection.Title>API keys</SettingsSection.Title>
        <SettingsSection.Description>
          Credentials delegated to this agent, limited by its policy, its
          owner's live permissions, and your own.
        </SettingsSection.Description>
      </SettingsSection.Header>
      <SettingsSection.Panel>
        <SettingsSection.Body>
          <AgentAPIKeysContent
            key={`${organization.id}:${user.id}:${agent.id}:${agent.permissions.authorize}`}
            agent={agent}
            organizationId={organization.id}
            userId={user.id}
            flag={flag}
            onCreate={onCreate}
          />
        </SettingsSection.Body>
      </SettingsSection.Panel>
    </SettingsSection>
  );
}

function AgentAPIKeysContent({
  agent,
  organizationId,
  userId,
  flag,
  creation = false,
  onCreate,
  onDone,
  onBusy,
}: {
  agent: ManagedAgent;
  organizationId: string;
  userId: string;
  flag: FeatureFlagResult;
  creation?: boolean;
  onCreate?: () => void;
  onDone?: () => void;
  onBusy?: (busy: boolean) => void;
}) {
  const sdk = useSdkClient();
  const queryClient = useQueryClient();
  const enabled = flag.status === "enabled";
  const rolloutEnabled = useRef(enabled);
  const canManage = agent.permissions.authorize;
  const canIssue =
    enabled &&
    canManage &&
    agent.lifecycle === "active" &&
    !agent.ownerReassignmentRequiredAt;
  const [open, setOpen] = useState(creation);
  const [step, setStep] = useState(0);
  const [inventory, setInventory] = useState<KeyServer[]>([]);
  const [servers, setServers] = useState<KeyServer[]>([]);
  const [accountsReady, setAccountsReady] = useState(false);
  const [serversBusy, setServersBusy] = useState(false);
  const [name, setName] = useState("");
  const [narrowings, setNarrowings] = useState<GrantNarrowings>({});
  const [expiryDays, setExpiryDays] = useState("90");
  const [customExpiry, setCustomExpiry] = useState("");
  const [issued, setIssued] = useState(false);
  const [reviewGrants, setReviewGrants] = useState<AgentPolicyGrantForm[]>([]);
  const [reviewAccounts, setReviewAccounts] = useState<KeyReviewAccount[]>([]);
  const [secret, setSecret] = useState<string | null>(null);
  const [copied, setCopied] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [revoke, setRevoke] = useState<Key | null>(null);
  const keys = useListAPIKeys({ agentId: agent.id }, security, {
    queryKeyHashFn: (key) => JSON.stringify([organizationId, key]),
    enabled: enabled && canManage,
    retry: false,
    throwOnError: false,
  });
  // Discovery is authorize-gated and already intersects the live agent, owner
  // and caller, so credential issuance never depends on reading agent policy.
  const resources = inventory.map((server) => ({
    projectId: server.projectId,
    toolsetId: server.resourceId,
  }));
  const delegable = useQuery({
    queryKey: [
      "agent-delegable-grants",
      organizationId,
      userId,
      agent.id,
      resources,
      agent.updatedAt,
      agent.ownerUserId,
    ],
    queryFn: ({ signal }) =>
      discoverKeyServerGrants(sdk.agents, agent.id, inventory, signal),
    enabled: open && canIssue && resources.length > 0,
    retry: false,
    throwOnError: false,
  });
  // A cached candidate set keeps `isSuccess` and its stale `data` while a
  // refetch is in flight, so reopening the dialog or a background refresh
  // would otherwise offer grants the current read has not confirmed. Editing
  // and issuance wait for the in-flight read to finish; failed
  // refetches can likewise retain stale data.
  const discoveryComplete =
    delegable.isSuccess &&
    !delegable.isFetching &&
    Array.isArray(delegable.data);
  const scopedGrants = narrowGrantsToServers(
    discoveryComplete ? delegable.data : [],
    servers,
  );
  let hasSelections = false;
  try {
    hasSelections =
      buildRequestedGrants(
        scopedGrants
          .filter((grant) => narrowings[delegableGrantKey(grant)] !== undefined)
          .map((grant) => ({
            grant,
            narrowing: narrowings[delegableGrantKey(grant)] ?? {},
          })),
      ).length > 0;
  } catch {
    // An empty tool selection is not permission to use every tool.
  }
  // Never carry a reviewed ceiling across a live read, even when the server
  // returns equivalent grants. Explicit reselection confirms the new policy.
  useEffect(() => {
    setNarrowings({});
    setReviewGrants([]);
    setReviewAccounts([]);
    setStep((current) => (current === 3 ? 2 : current));
  }, [discoveryComplete, delegable.dataUpdatedAt]);
  const create = useCreateAPIKeyMutation({ gcTime: 0, retry: false });
  // Resetting mutation state after issuance must not unlock header navigation
  // while the post-issue inventory refresh is still in flight.
  const navigationBusy = create.isPending || (issued && keys.isFetching);
  useEffect(() => {
    onBusy?.(navigationBusy);
  }, [navigationBusy, onBusy]);
  const resetCreation = create.reset;
  useEffect(() => {
    rolloutEnabled.current = enabled;
    if (!enabled) {
      setIssued(false);
      setOpen(false);
      setSecret(null);
      setCopied(false);
      resetCreation();
      if (creation && flag.status !== "loading") onDone?.();
    }
    return () => {
      rolloutEnabled.current = false;
    };
  }, [enabled, resetCreation, creation, flag.status, onDone]);
  const [revokedIds, setRevokedIds] = useState<string[]>([]);
  const knownKeys = keys.data?.keys.filter(
    (key) => !revokedIds.includes(key.id),
  );
  const revocation = useRevokeAPIKeyMutation({ retry: false });
  const unavailable =
    keys.isError && "statusCode" in keys.error && keys.error.statusCode === 404;
  const refresh = () => {
    if (!rolloutEnabled.current) return;
    void keys.refetch();
    void queryClient.invalidateQueries({
      queryKey: ["@gram/client", "keys", "list"],
    });
  };
  const close = () => {
    setOpen(false);
    setSecret(null);
    setCopied(false);
    setIssued(false);
    setName("");
    setNarrowings({});
    setExpiryDays("90");
    setCustomExpiry("");
    setError(null);
    create.reset();
    onDone?.();
  };
  const expiryValidation = (now: number) => {
    // Leave five minutes below the server limit for modest browser clock skew.
    const maxLifetime = 365 * 86_400_000 - 5 * 60_000;
    // Date-only selections expire at local midnight, not UTC midnight.
    const expiresAt =
      expiryDays === "custom"
        ? new Date(`${customExpiry}T00:00:00`)
        : new Date(
            now + Math.min(Number(expiryDays) * 86_400_000, maxLifetime),
          );
    let reason: string | undefined;
    if (!Number.isFinite(expiresAt.getTime()))
      reason = "Choose a valid expiration date.";
    else if (expiresAt.getTime() <= now)
      reason = "Expiration date must be in the future.";
    else if (expiresAt.getTime() > now + maxLifetime)
      reason =
        "Expiration date must be within 365 days minus a 5-minute clock-skew margin.";
    return { expiresAt, reason };
  };
  const expiryReason = expiryValidation(Date.now()).reason;
  const returnToStep = (next: number) => {
    setStep(next);
    setNarrowings({});
    setReviewGrants([]);
    setReviewAccounts([]);
    setError(null);
    void delegable.refetch();
  };
  const issue = (review = false) => {
    if (
      !canIssue ||
      create.isPending ||
      issued ||
      !accountsReady ||
      !servers.length ||
      (!review && step !== 3)
    )
      return;
    let validatedName: string;
    try {
      validatedName = validateAgentAPIKeyName(name);
    } catch (error) {
      setError(
        error instanceof Error ? error.message : "Enter a valid key name.",
      );
      return;
    }
    if (!discoveryComplete) {
      setError("Load available permissions before continuing.");
      return;
    }
    let requestedGrants;
    try {
      // Only candidates discovery actually returned can be requested, so a
      // failed read yields no permissions rather than a hand-written request.
      const selections = scopedGrants
        .map((grant) => ({ grant, key: delegableGrantKey(grant) }))
        .filter(({ key }) => narrowings[key] !== undefined)
        .map(({ grant, key }) => ({ grant, narrowing: narrowings[key] ?? {} }));
      requestedGrants = buildRequestedGrants(selections);
      if (!requestedGrants.length)
        throw new Error("Select at least one permission to continue.");
    } catch (error) {
      setError(
        error instanceof Error
          ? error.message
          : "Select at least one permission to continue.",
      );
      return;
    }
    const { expiresAt, reason } = expiryValidation(Date.now());
    if (reason) {
      setError(reason);
      return;
    }
    setError(null);
    if (review) {
      setReviewGrants(requestedGrants);
      // Account requirements were verified on the previous step. Reuse that
      // exact inventory, without fetching identities from the dashboard login.
      const required = [
        ...new Map(
          servers
            .filter((server) => server.issuerId)
            .map((server) => [
              `${server.projectId}:${server.issuerId}`,
              server,
            ]),
        ).values(),
      ];
      const ownerScope = { organizationId, userId };
      const accountRows = required.map((server) => {
        const request = {
          gramProject: server.projectSlug,
          principalId: agent.id,
          userSessionIssuerId: server.issuerId!,
        };
        return {
          server,
          clients: queryClient.getQueryData<unknown[]>([
            ...queryKeyRemoteSessionClients({
              gramProject: server.projectSlug,
              userSessionIssuerId: server.issuerId!,
            }),
            ownerScope,
            "all-pages",
          ]),
          candidates:
            queryClient.getQueryData<RemoteSession[]>([
              ...queryKeyRemoteSessions(request),
              ownerScope,
              "all-pages",
            ]) ?? [],
          bindings:
            queryClient.getQueryData<ListBindingsResponseBody>([
              ...queryKeyRemoteSessionsListBindings(request),
              ownerScope,
            ])?.items ?? [],
        };
      });
      setReviewAccounts(
        (accountRows ?? []).flatMap<KeyReviewAccount>((row) =>
          row.clients?.length === 0
            ? servers
                .filter(
                  (server) =>
                    server.projectId === row.server.projectId &&
                    server.issuerId === row.server.issuerId,
                )
                .map((server) => ({
                  resourceId: server.resourceId,
                  projectId: server.projectId,
                  status: "not-required" as const,
                }))
            : row.bindings
                .filter(
                  (binding) =>
                    binding.remoteSession !== undefined &&
                    row.candidates.some(
                      (candidate) => candidate.id === binding.remoteSessionId,
                    ),
                )
                .flatMap((binding) => {
                  // Several selected servers can share an issuer. Carry the same
                  // authorized identity into each server's review, not only the first.
                  return servers
                    .filter(
                      (server) =>
                        server.projectId === row.server.projectId &&
                        server.issuerId === row.server.issuerId,
                    )
                    .map((server) => ({
                      resourceId: server.resourceId,
                      projectId: server.projectId,
                      status: "connected" as const,
                      ...sessionAccountIdentity(binding.remoteSession),
                    }));
                }),
        ),
      );
      setStep(3);
      return;
    }
    if (JSON.stringify(requestedGrants) !== JSON.stringify(reviewGrants)) {
      setError(
        "Available permissions changed. Review your selections again before creating the key.",
      );
      setStep(2);
      return;
    }
    create.mutate(
      {
        security,
        request: {
          createKeyForm: {
            agentId: agent.id,
            name: validatedName,
            expiresAt,
            delegatedGrantsVersion: 2,
            requestedGrants,
            scopes: [],
          },
        },
      },
      {
        onSuccess: (key) => {
          if (rolloutEnabled.current) {
            setIssued(true);
            setSecret(key.key ?? null);
          }
          create.reset();
          refresh();
        },
        onError: () => {
          // A failed issuance may reflect a live policy change. Withdraw the
          // reviewed ceiling even when the error does not identify its cause.
          setNarrowings({});
          setReviewGrants([]);
          setReviewAccounts([]);
          setStep(2);
          if (rolloutEnabled.current) void delegable.refetch();
          setError(
            "Could not create API key. Check that the requested grants are allowed by the agent policy, the owner's live permissions and your own, and that the agent is active with a valid owner.",
          );
          create.reset();
        },
      },
    );
  };
  const columns: Column<Key>[] = [
    { key: "name", header: "Name", render: (key) => key.name },
    { key: "keyPrefix", header: "Prefix", render: (key) => key.keyPrefix },
    {
      key: "expiresAt",
      header: "Expires",
      // Absolute, like agent session expiry: a relative label reads a future
      // expiry as elapsed time, so a fresh 90-day key showed "3 months ago".
      render: (key) =>
        key.expiresAt ? (
          // Table cells clip their overflow, so a long localized date needs a
          // truncation and the full value on hover.
          <time
            className="min-w-0 truncate"
            title={key.expiresAt.toLocaleString()}
            dateTime={key.expiresAt.toISOString()}
          >
            {key.expiresAt.toLocaleString()}
          </time>
        ) : (
          "—"
        ),
    },
    {
      key: "id",
      header: "",
      render: (key) => (
        <Button
          size="sm"
          variant="destructive-secondary"
          onClick={() => {
            setError(null);
            setRevoke(key);
          }}
        >
          Revoke API key
        </Button>
      ),
    },
  ];
  if (!enabled) return <Text muted>Agent API keys are unavailable.</Text>;
  if (!canManage)
    return (
      <Text muted>
        You do not have permission to view or manage this agent's API keys.
      </Text>
    );
  return (
    <>
      {!creation && (
        <>
          {enabled && keys.isLoading ? (
            <Text muted>Loading API keys…</Text>
          ) : enabled && keys.isError ? (
            <div role="alert">
              <Text>
                {unavailable
                  ? "Agent API keys are unavailable. The credential feature may be disabled."
                  : "Could not load API keys."}
              </Text>
              <Button variant="secondary" onClick={() => void keys.refetch()}>
                Retry API keys
              </Button>
            </div>
          ) : null}
          {knownKeys?.length ? (
            <Table
              data={knownKeys}
              columns={columns}
              rowKey={(key) => key.id}
            />
          ) : keys.data ? (
            <Text muted>No API keys yet</Text>
          ) : null}
          {!canIssue && (
            <Text muted>
              Issuance requires an active agent with a valid owner and
              credential authorization. Existing keys can still be revoked.
            </Text>
          )}
          <Button
            disabled={!canIssue || unavailable}
            onClick={() => {
              onCreate?.();
            }}
          >
            Create API key
          </Button>
        </>
      )}
      {creation && open && enabled && (
        <div className="space-y-6">
          <Text muted>
            {issued
              ? "This key is shown only once. Copy it now and store it securely."
              : "Choose where this key can connect and what it can do. Choose an expiration of up to 365 days."}
          </Text>
          {!issued && (
            <ol
              aria-label="Creation steps"
              className="flex flex-wrap gap-4 text-sm"
            >
              {["MCP servers", "Accounts", "Permissions", "Review"].map(
                (label, index) => (
                  <li key={label} className="min-w-32 flex-1">
                    <button
                      type="button"
                      aria-current={step === index ? "step" : undefined}
                      disabled={index >= step || create.isPending}
                      onClick={() => {
                        returnToStep(index);
                        setError(null);
                      }}
                      className={`flex w-full items-center gap-3 border-b-2 px-1 py-3 text-left ${step === index ? "border-primary font-medium" : "border-border text-muted-foreground"}`}
                    >
                      <span
                        className={`flex h-6 w-6 shrink-0 items-center justify-center rounded-full border text-xs ${index < step ? "border-primary bg-primary text-primary-foreground" : ""}`}
                      >
                        {index < step ? (
                          <Check className="h-3 w-3" />
                        ) : (
                          index + 1
                        )}
                      </span>
                      {label}
                    </button>
                  </li>
                ),
              )}
            </ol>
          )}
          {!issued && step < 2 && (
            <AgentKeyServers
              agent={agent}
              grants={discoveryComplete ? delegable.data : []}
              discoveryComplete={discoveryComplete}
              discoveryError={delegable.isError}
              onRetryDiscovery={() => void delegable.refetch()}
              onInventory={setInventory}
              step={step}
              selected={servers}
              onChange={(next) => {
                setServers(next);
                setAccountsReady(false);
                setNarrowings({});
                setReviewGrants([]);
                setError(null);
              }}
              onReady={setAccountsReady}
              onBusy={setServersBusy}
            />
          )}
          {issued ? (
            <div className="space-y-4">
              <h2 className="text-lg font-semibold">Save your API key</h2>
              {secret ? (
                <code className="block break-all">{secret}</code>
              ) : (
                <Text role="alert">
                  The key was created but its secret was not returned. Revoke it
                  from the agent page before creating another.
                </Text>
              )}
              <ServerEndpoints servers={servers} />
              <Button
                disabled={!secret}
                onClick={() => {
                  if (!secret) return;
                  void navigator.clipboard.writeText(secret).then(
                    () => setCopied(true),
                    () => setError("Could not copy API key. Copy it manually."),
                  );
                }}
              >
                {copied ? "Copied" : "Copy API key"}
              </Button>
              <Button variant="secondary" onClick={close}>
                Done
              </Button>
            </div>
          ) : step >= 2 ? (
            <form
              className="space-y-4"
              onSubmit={(event) => {
                event.preventDefault();
                issue(step === 2);
              }}
            >
              {step === 2 ? (
                <>
                  <h2 className="text-lg font-semibold">Choose permissions</h2>
                  <label className="block space-y-2">
                    Key name
                    <Input required value={name} onChange={setName} />
                  </label>
                  <Text small muted>
                    Select access for your servers, then choose the tools this
                    key can use.
                  </Text>
                  <div className="space-y-2">
                    <label
                      htmlFor="agent-key-expiry"
                      className="text-sm font-medium"
                    >
                      Expiration
                    </label>
                    <Select
                      value={expiryDays}
                      onValueChange={setExpiryDays}
                      disabled={create.isPending}
                    >
                      <SelectTrigger id="agent-key-expiry">
                        <SelectValue />
                      </SelectTrigger>
                      <SelectContent>
                        {[7, 30, 90, 180, 365].map((days) => (
                          <SelectItem key={days} value={String(days)}>
                            {days} days
                          </SelectItem>
                        ))}
                        <SelectItem value="custom">Custom date</SelectItem>
                      </SelectContent>
                    </Select>
                    {expiryDays === "custom" && (
                      <label className="block space-y-2 text-sm font-medium">
                        Expiration date
                        <Input
                          type="date"
                          value={customExpiry}
                          onChange={setCustomExpiry}
                          disabled={create.isPending}
                        />
                      </label>
                    )}
                    <Text small muted>
                      Keys expire within 365 days. Custom dates use midnight in
                      your local time zone.
                    </Text>
                  </div>
                  <DelegableGrantSection
                    isFetching={delegable.isFetching}
                    isComplete={discoveryComplete}
                    grants={scopedGrants}
                    narrowings={narrowings}
                    onChangeNarrowings={setNarrowings}
                    disabled={create.isPending}
                    onRetry={() => void delegable.refetch()}
                  />
                </>
              ) : (
                <AgentKeyReview
                  name={name}
                  servers={servers}
                  grants={reviewGrants}
                  accounts={reviewAccounts}
                  onEditServers={() => returnToStep(0)}
                  onEditAccounts={() => returnToStep(1)}
                  onEditAccess={() => returnToStep(2)}
                  disabled={create.isPending}
                />
              )}
            </form>
          ) : null}
          {!issued && (
            <div className="flex items-center justify-between border-t pt-5">
              <div className="flex gap-2">
                {step > 0 && (
                  <Button
                    variant="secondary"
                    disabled={create.isPending}
                    onClick={() => {
                      returnToStep(step - 1);
                      setError(null);
                    }}
                  >
                    Back
                  </Button>
                )}
                <Button
                  variant="tertiary"
                  disabled={create.isPending}
                  onClick={close}
                >
                  Cancel
                </Button>
              </div>
              <Button
                title={expiryReason}
                disabled={
                  !canIssue ||
                  create.isPending ||
                  serversBusy ||
                  !discoveryComplete ||
                  !servers.length ||
                  scopedGrants.length === 0 ||
                  (step >= 1 && !accountsReady) ||
                  (step >= 2 &&
                    (!discoveryComplete ||
                      !hasSelections ||
                      !name.trim() ||
                      !!expiryReason))
                }
                onClick={() =>
                  step < 2 ? setStep(step + 1) : issue(step === 2)
                }
              >
                {create.isPending
                  ? "Creating…"
                  : step < 2
                    ? "Continue"
                    : step === 2
                      ? "Review key"
                      : "Create key"}
              </Button>
            </div>
          )}
          {error && <p role="alert">{error}</p>}
        </div>
      )}
      <Dialog
        open={revoke !== null}
        onOpenChange={(value) => {
          if (!value && !revocation.isPending) {
            setRevoke(null);
            setError(null);
          }
        }}
      >
        <Dialog.Content>
          <Dialog.Header>
            <Dialog.Title>Revoke API key?</Dialog.Title>
            <Dialog.Description>
              Revoking {revoke?.name} permanently prevents this credential from
              authenticating. This cannot be undone.
            </Dialog.Description>
          </Dialog.Header>
          {error && <p role="alert">{error}</p>}
          <Dialog.Footer>
            <Button
              variant="secondary"
              disabled={revocation.isPending}
              onClick={() => setRevoke(null)}
            >
              Cancel
            </Button>
            <Button
              variant="destructive-primary"
              disabled={revocation.isPending}
              onClick={() => {
                if (!revoke || !canManage) return;
                revocation.mutate(
                  { security, request: { id: revoke.id } },
                  {
                    onSuccess: () => {
                      setRevokedIds((ids) => [...ids, revoke.id]);
                      setRevoke(null);
                      setError(null);
                      refresh();
                    },
                    onError: () =>
                      setError("Could not revoke API key. Try again."),
                  },
                );
              }}
            >
              Confirm revoke
            </Button>
          </Dialog.Footer>
        </Dialog.Content>
      </Dialog>
    </>
  );
}

/**
 * The permissions a new credential requests. Candidates come from delegable
 * grant discovery and are narrowed rather than retyped, so a failed read
 * offers no permissions at all instead of a request built from scope names.
 */
function DelegableGrantSection({
  isFetching,
  isComplete,
  grants,
  narrowings,
  onChangeNarrowings,
  disabled,
  onRetry,
}: {
  isFetching: boolean;
  isComplete: boolean;
  grants: AgentPolicyGrantForm[];
  narrowings: GrantNarrowings;
  onChangeNarrowings: (next: GrantNarrowings) => void;
  disabled: boolean;
  onRetry: () => void;
}): JSX.Element {
  if (isFetching) return <Text>Loading delegable permissions…</Text>;
  if (isComplete)
    return (
      <AgentGrantSelector
        grants={grants}
        narrowings={narrowings}
        onChange={onChangeNarrowings}
        disabled={disabled}
      />
    );
  return (
    <div className="space-y-2">
      <Text muted>Permissions could not be loaded. Retry to continue.</Text>
      <Button type="button" variant="secondary" onClick={onRetry}>
        Retry permissions
      </Button>
    </div>
  );
}

function ServerEndpoints({ servers }: { servers: KeyServer[] }) {
  return (
    <div className="space-y-3">
      {servers.map((server) => (
        <div key={server.id}>
          <Text className="font-medium">{server.name}</Text>
          {server.endpoints?.length ? (
            server.endpoints.map((url) => (
              <div
                key={url}
                className="flex items-center gap-2 rounded-md border p-3"
              >
                <code className="min-w-0 flex-1 break-all text-sm">{url}</code>
                <CopyButton text={url} tooltip="Copy server URL" />
              </div>
            ))
          ) : (
            <Text small muted>
              {server.kind === "Unproxied"
                ? "Unproxied servers require their own upstream connection and do not accept this Gram key."
                : "No connection URL is available. Open this server’s settings to configure its endpoint."}
            </Text>
          )}
        </div>
      ))}
      <Text small muted>
        Use the key as a Bearer token only with Gram endpoints. Do not send it
        to an upstream server.
      </Text>
    </div>
  );
}
