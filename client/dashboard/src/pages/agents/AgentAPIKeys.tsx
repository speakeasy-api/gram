import { AgentGatewayInstall } from "./AgentGatewayInstall";
import { sessionAccountIdentity } from "@/components/sessions/session-account-identity";
import { useEffect, useRef, useState } from "react";
import { Check } from "lucide-react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useOrganization, useSession } from "@/contexts/Auth";
import { useSdkClient } from "@/contexts/Sdk";
import { Button } from "@/components/ui/Button";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/Select";
import { AgentKeyReview, type KeyReviewAccount } from "./AgentKeyReview";
import type { RemoteSession } from "@gram/client/models/components/remotesession.js";
import type { ListBindingsResponseBody } from "@gram/client/models/components/listbindingsresponsebody.js";
import { queryKeyRemoteSessionClients } from "@gram/client/react-query/remoteSessionClients.js";
import { queryKeyRemoteSessions } from "@gram/client/react-query/remoteSessions.js";
import { queryKeyRemoteSessionsListBindings } from "@gram/client/react-query/remoteSessionsListBindings.js";
import { Alert } from "@/components/ui/Alert";
import { Dialog } from "@/components/ui/Dialog";
import { Input } from "@/components/ui/Input";
import { Text } from "@/components/ui/Text";
import { agentIssuanceBlocked } from "./agent-issuance";
import {
  IdentityPanel,
  IdentityPanelEmpty,
  IdentityPanelRow,
} from "@/pages/identities/IdentityPanel";
import type { ManagedAgent } from "@gram/client/models/components/managedagent.js";
import type { Key } from "@gram/client/models/components/key.js";
import { GramError } from "@gram/client/models/errors/gramerror.js";
import { dateTimeFormatters } from "@/lib/dates";
import type { AgentPolicyGrantForm } from "@gram/client/models/components/agentpolicygrantform.js";
import {
  buildRequestedGrants,
  delegableGrantKey,
  validateAgentAPIKeyName,
} from "./agent-api-key-grants";
import { AgentGrantSelector, type GrantNarrowings } from "./AgentGrantSelector";
import { useCreateAPIKeyMutation } from "@gram/client/react-query/createAPIKey";
import { useKeysRotateMutation } from "@gram/client/react-query/keysRotate.js";
import { useRevokeAPIKeyMutation } from "@gram/client/react-query/revokeAPIKey";

import { AgentKeyServers, type KeyServer } from "./AgentKeyServers";

import { narrowGrantsToServers } from "./agent-key-server-grants";
import { discoverKeyGrants } from "./agent-key-discovery";
import { AgentKeyPermissions } from "./AgentKeyPermissions";
import { useAgentKeys, useAgentPolicyGrants } from "./useAgentKeyPermissions";
import {
  isAgentWidePermission,
  permissionsMissingFromKey,
  uniqueGrants,
} from "./agent-key-permissions";

const security = { sessionHeaderGramSession: "" };

export function AgentAPIKeys({
  agent,
  creation = false,
  replaceKeyId,
  federated = false,
  onCreate,
  onDone,
  onBusy,
}: {
  agent: ManagedAgent;
  creation?: boolean;
  /** In creation mode, the key the new one replaces once it is issued. */
  replaceKeyId?: string;
  /**
   * The agent signs in with a platform's tokens, so a key is optional.
   * "unknown" where the caller's own read of that has not settled: the panel
   * then claims neither, rather than telling a federated agent it cannot
   * authenticate.
   */
  federated?: boolean | "unknown";
  /** Opens issuance; with a key id, issuance replaces that key. */
  onCreate?: (replaceKeyId?: string) => void;
  onDone?: () => void;
  onBusy?: (busy: boolean) => void;
}): JSX.Element {
  const organization = useOrganization();
  // Delegable candidates are specific to the authorizer, and the signed-in
  // user can change without the organization changing.
  const { user } = useSession();
  // The mode is part of the key: both branches render the same component in
  // the same position now, so without it React keeps the listing's state when
  // the caller switches to issuance, and the wizard opens closed.
  if (creation)
    return (
      <AgentAPIKeysContent
        key={`creation:${organization.id}:${user.id}:${agent.id}:${agent.permissions.authorize}:${replaceKeyId ?? ""}`}
        agent={agent}
        organizationId={organization.id}
        userId={user.id}
        creation
        replaceKeyId={replaceKeyId}
        onDone={onDone}
        onBusy={onBusy}
      />
    );
  // The listing brings its own panel, the same one the federated platforms
  // beside it use, so the caller supplies only the section around them.
  return (
    <AgentAPIKeysContent
      key={`${organization.id}:${user.id}:${agent.id}:${agent.permissions.authorize}`}
      agent={agent}
      organizationId={organization.id}
      userId={user.id}
      federated={federated}
      onCreate={onCreate}
    />
  );
}

/**
 * One date shape for every credential column on this page: the day, with the
 * exact moment on hover. The page previously mixed a numeric locale string, a
 * truncated timestamp and a relative label in adjacent columns.
 */
function KeyDate({ date }: { date: Date }): JSX.Element {
  return (
    <time
      className="tabular-nums"
      title={dateTimeFormatters.full.format(date)}
      dateTime={date.toISOString()}
    >
      {dateTimeFormatters.day.format(date)}
    </time>
  );
}

function AgentAPIKeysContent({
  agent,
  organizationId,
  userId,
  creation = false,
  replaceKeyId,
  federated = false,
  onCreate,
  onDone,
  onBusy,
}: {
  agent: ManagedAgent;
  organizationId: string;
  userId: string;
  creation?: boolean;
  replaceKeyId?: string;
  federated?: boolean | "unknown";
  onCreate?: (replaceKeyId?: string) => void;
  onDone?: () => void;
  onBusy?: (busy: boolean) => void;
}) {
  const sdk = useSdkClient();
  const queryClient = useQueryClient();
  const mounted = useRef(true);
  const canManage = agent.permissions.authorize;
  const issuanceBlocked = agentIssuanceBlocked(agent, canManage);
  const canIssue = issuanceBlocked === null;
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
  const [error, setError] = useState<string | null>(null);
  const [revoke, setRevoke] = useState<Key | null>(null);
  const keys = useAgentKeys(agent);
  const policy = useAgentPolicyGrants(agent);
  const agentGrants = policy.data && !policy.isError ? policy.data : [];
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
      discoverKeyGrants(sdk.agents, agent.id, inventory, signal),
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
  const discovered = discoveryComplete ? delegable.data : [];
  const scopedGrants = uniqueGrants([
    ...narrowGrantsToServers(discovered, servers),
    ...discovered.filter((grant) => isAgentWidePermission(grant)),
  ]);
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
  const rotate = useKeysRotateMutation({ gcTime: 0, retry: false });
  // Read once the list has loaded: a key revoked or expired since the reissue
  // link was followed is no longer there to replace, and issuance then
  // creates a new key instead of failing at the last step.
  const replacing = replaceKeyId
    ? keys.data?.keys.find((key) => key.id === replaceKeyId)
    : undefined;
  const replacementGone =
    !!replaceKeyId && !!keys.data && !replacing && !issued;
  const issuance = replacing ? rotate : create;
  // Resetting mutation state after issuance must not unlock header navigation
  // while the post-issue inventory refresh is still in flight.
  const navigationBusy = issuance.isPending || (issued && keys.isFetching);
  useEffect(() => {
    onBusy?.(navigationBusy);
  }, [navigationBusy, onBusy]);
  // Issuance resolves after this component may have unmounted; the secret is
  // shown only while it is still on screen.
  useEffect(() => {
    mounted.current = true;
    return () => {
      mounted.current = false;
    };
  }, []);
  const [revokedIds, setRevokedIds] = useState<string[]>([]);
  const knownKeys = keys.data?.keys.filter(
    (key) => !revokedIds.includes(key.id),
  );
  const revocation = useRevokeAPIKeyMutation({ retry: false });
  const refresh = () => {
    if (!mounted.current) return;
    void keys.refetch();
    void queryClient.invalidateQueries({
      queryKey: ["@gram/client", "keys", "list"],
    });
  };
  const close = () => {
    setOpen(false);
    setSecret(null);
    setIssued(false);
    setName("");
    setNarrowings({});
    setExpiryDays("90");
    setCustomExpiry("");
    setError(null);
    issuance.reset();
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
  // One place decides whether the wizard can move on, and says why not. The
  // order runs from the conditions the user cannot fix (no authority, nothing
  // to grant) to the ones they can (pick a server, name the key).
  const blockedReason: string | null = !canIssue
    ? "You cannot issue keys for this agent."
    : replaceKeyId && keys.isPending
      ? "Loading the key you are replacing…"
      : !discoveryComplete
        ? "Loading what this agent can be delegated…"
        : serversBusy
          ? "Checking the servers you selected…"
          : !servers.length
            ? "Select at least one MCP server."
            : scopedGrants.length === 0
              ? "The agent's policy delegates nothing on these servers."
              : step >= 1 && !accountsReady
                ? "Connect an account for every server that needs one."
                : step >= 2 && !name.trim()
                  ? "Name this key."
                  : step >= 2 && expiryReason
                    ? expiryReason
                    : step >= 2 && !hasSelections
                      ? "Select at least one permission."
                      : null;
  // A name is required, so the wizard offers one rather than blocking on an
  // empty field. Names are unique per organization, so a name already in the
  // list is dated to keep the suggestion usable as typed.
  const takenNames = (knownKeys ?? []).map((key) => key.name).join("\u0000");
  const replacingName = replacing?.name;
  useEffect(() => {
    // Nothing is suggested until the list has been read once: a name chosen
    // against an empty list sticks, because the effect leaves a name the user
    // may have typed alone, and creation then fails on the uniqueness check.
    if (step !== 2 || keys.isPending) return;
    setName((current) => {
      if (current.trim()) return current;
      // The replacement takes over the name: the old key is revoked in the
      // same step, so the name is free by the time the new one is created.
      if (replacingName) return replacingName;
      const base = `${agent.name} key`;
      const taken = new Set(takenNames.split("\u0000"));
      if (!taken.has(base)) return base;
      const dated = `${base} ${new Date().toISOString().slice(0, 10)}`;
      return taken.has(dated) ? "" : dated;
    });
  }, [step, agent.name, takenNames, keys.isPending, replacingName]);
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
      issuance.isPending ||
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
    const callbacks = {
      onSuccess: (key: Key) => {
        if (mounted.current) {
          setIssued(true);
          setSecret(key.key ?? null);
        }
        issuance.reset();
        refresh();
      },
      onError: (failure: unknown) => {
        // A failed issuance may reflect a live policy change. Withdraw the
        // reviewed ceiling even when the error does not identify its cause.
        setNarrowings({});
        setReviewGrants([]);
        setReviewAccounts([]);
        setStep(2);
        if (mounted.current) void delegable.refetch();
        // A name already in use is the one failure the caller can fix from
        // this screen, and the server names it. Everything else keeps the
        // policy explanation, which is what the other causes have in common.
        const conflict =
          failure instanceof GramError &&
          failure.statusCode === 409 &&
          failure.message;
        setError(
          conflict ||
            `Could not ${replacing ? "reissue" : "create"} API key. Check that the requested grants are allowed by the agent policy, the owner's live permissions and your own, and that the agent is active with a valid owner.`,
        );
        issuance.reset();
      },
    };
    const form = {
      name: validatedName,
      expiresAt,
      delegatedGrantsVersion: 3,
      requestedGrants,
      scopes: [],
    };
    if (replacing)
      rotate.mutate(
        { security, request: { rotateKeyForm: { id: replacing.id, ...form } } },
        callbacks,
      );
    else
      create.mutate(
        {
          security,
          request: { createKeyForm: { agentId: agent.id, ...form } },
        },
        callbacks,
      );
  };
  if (!canManage)
    return (
      <Text muted>
        You do not have permission to view or manage this agent's API keys.
      </Text>
    );
  return (
    <>
      {!creation && (
        <IdentityPanel
          title="API keys"
          loading={keys.isPending}
          // A failed refetch keeps the keys it already has: they were really
          // returned, and blanking the list throws away the only true thing
          // the panel holds. The failure is said above them instead.
          error={keys.isError && !keys.data}
          refreshFailed={keys.isError && !!keys.data}
          onRetry={() => void keys.refetch()}
          loadingRows={1}
          footer={
            issuanceBlocked
              ? `${issuanceBlocked} Existing keys can still be revoked.`
              : "API keys are long-lived. Anyone holding one acts as this agent, so store them in a secret manager and revoke any that leak."
          }
          action={
            <Button
              size="sm"
              variant={knownKeys?.length ? "secondary" : "primary"}
              disabled={!canIssue}
              onClick={() => {
                onCreate?.();
              }}
            >
              Issue a key
            </Button>
          }
        >
          {knownKeys?.length ? (
            knownKeys.map((key) => {
              const missing = permissionsMissingFromKey(key, agentGrants);
              return (
                <IdentityPanelRow
                  key={key.id}
                  title={key.name}
                  accent={missing.length > 0 ? "warning" : undefined}
                  // Prefix, issue date and expiry together: which key a machine
                  // holds is remembered as the one issued then, and the expiry
                  // is the next thing anyone asks of it.
                  detail={
                    <span className="flex flex-wrap items-center gap-x-2">
                      <span className="font-mono">{key.keyPrefix}</span>
                      <span aria-hidden="true">·</span>
                      <span>
                        Issued{" "}
                        {key.createdAt ? <KeyDate date={key.createdAt} /> : "—"}
                      </span>
                      <span aria-hidden="true">·</span>
                      <span>
                        Expires{" "}
                        {key.expiresAt ? <KeyDate date={key.expiresAt} /> : "—"}
                      </span>
                    </span>
                  }
                  trailing={
                    <span className="flex items-center gap-2">
                      {missing.length > 0 && canIssue && (
                        <Button
                          size="sm"
                          variant="secondary"
                          onClick={() => onCreate?.(key.id)}
                          aria-label={`Reissue API key ${key.name}`}
                        >
                          Reissue
                        </Button>
                      )}
                      <Button
                        size="sm"
                        variant="destructive-secondary"
                        onClick={() => {
                          setError(null);
                          setRevoke(key);
                        }}
                        aria-label={`Revoke API key ${key.name}`}
                      >
                        Revoke
                      </Button>
                    </span>
                  }
                >
                  <AgentKeyPermissions apiKey={key} missing={missing} />
                </IdentityPanelRow>
              );
            })
          ) : (
            // The state that matters most on this page: an identity nothing
            // is holding yet, and the one step that changes that.
            <IdentityPanelEmpty>
              {federated === "unknown"
                ? // Whether a key is this agent's only way in is not settled
                  // yet, and the guess that it is tells a federated agent it
                  // cannot authenticate.
                  "No key issued."
                : federated
                  ? "No key issued. This agent signs in with its platform's own tokens, so it only needs a key for a runtime that cannot do that."
                  : "No key issued yet. The agent cannot authenticate until you issue one and install it where it runs."}
            </IdentityPanelEmpty>
          )}
        </IdentityPanel>
      )}
      {creation && open && (
        <div className="space-y-6">
          {/* Each step states its own purpose, so the shell speaks only for
              the one state that has none of its own: the issued key. */}
          {issued && (
            <Text muted>
              This key is shown only once. Copy it now and store it securely.
            </Text>
          )}
          {/* At the top of the step, not below the buttons: the old position
              put a failure off-screen on a long step, so a click that did
              nothing looked like a broken button. */}
          {error && (
            <p
              role="alert"
              className="border-destructive text-destructive border p-3 text-sm"
            >
              {error}
            </p>
          )}
          {replacing && !issued && (
            <Alert variant="warning" title={`Replacing ${replacing.name}`}>
              <div className="space-y-2">
                <AgentKeyPermissions
                  apiKey={replacing}
                  missing={permissionsMissingFromKey(replacing, agentGrants)}
                />
                <p>
                  Choose what the new key may do. Reissuing revokes{" "}
                  {replacing.name} at once, so whatever uses it stops signing in
                  until you install the new key.
                </p>
              </div>
            </Alert>
          )}
          {replacementGone && (
            <Text muted>
              The key you chose to reissue has been revoked or has expired, so
              this issues a new key instead.
            </Text>
          )}
          {!issued && (
            <ol
              aria-label="Creation steps"
              className="flex flex-wrap gap-4 text-sm"
            >
              {["Servers", "Accounts", "Access", "Review"].map(
                (label, index) => (
                  <li key={label} className="min-w-32 flex-1">
                    <button
                      type="button"
                      aria-current={step === index ? "step" : undefined}
                      disabled={index >= step || issuance.isPending}
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
              {secret ? null : (
                <Text role="alert">
                  The key was created but its secret was not returned. Revoke it
                  from the agent page before creating another.
                </Text>
              )}
              <h2 className="text-lg font-semibold">Connect your agent</h2>
              <AgentGatewayInstall agentID={agent.id} secret={secret} />
              <div className="flex justify-end border-t pt-5">
                <Button variant="secondary" onClick={close}>
                  Done
                </Button>
              </div>
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
                  <h2 className="text-lg font-semibold">Choose access</h2>
                  <Text small muted>
                    Pick what this key may do on the servers you chose. Nothing
                    is selected by default.
                  </Text>
                  <label className="block space-y-2 text-sm font-medium">
                    Key name
                    <Input required value={name} onChange={setName} />
                  </label>
                  <Text small muted>
                    Names are unique in the organization. Say where the key
                    lives — the laptop, the CI job, the container.
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
                      disabled={issuance.isPending}
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
                          disabled={issuance.isPending}
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
                    disabled={issuance.isPending}
                    onRetry={() => void delegable.refetch()}
                  />
                </>
              ) : (
                <AgentKeyReview
                  name={name}
                  servers={servers}
                  grants={reviewGrants.filter(
                    (grant) => !isAgentWidePermission(grant),
                  )}
                  otherGrants={reviewGrants.filter((grant) =>
                    isAgentWidePermission(grant),
                  )}
                  accounts={reviewAccounts}
                  onEditServers={() => returnToStep(0)}
                  onEditAccounts={() => returnToStep(1)}
                  onEditAccess={() => returnToStep(2)}
                  disabled={issuance.isPending}
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
                    disabled={issuance.isPending}
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
                  disabled={issuance.isPending}
                  onClick={close}
                >
                  Cancel
                </Button>
              </div>
              <div className="flex items-center gap-3">
                {/* A disabled primary with no stated reason is the wizard's
                    commonest dead end, so the gate says what it is waiting
                    for rather than leaving the button silently inert. */}
                {blockedReason && !issuance.isPending && (
                  <Text muted small>
                    {blockedReason}
                  </Text>
                )}
                <Button
                  title={blockedReason ?? undefined}
                  disabled={!!blockedReason || issuance.isPending}
                  onClick={() =>
                    step < 2 ? setStep(step + 1) : issue(step === 2)
                  }
                >
                  {issuance.isPending
                    ? replacing
                      ? "Reissuing…"
                      : "Creating…"
                    : step < 2
                      ? "Continue"
                      : step === 2
                        ? "Review key"
                        : replacing
                          ? "Reissue key"
                          : "Create key"}
                </Button>
              </div>
            </div>
          )}
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
