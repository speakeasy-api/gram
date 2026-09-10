import { useEffect, useRef, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useOrganization, useSession } from "@/contexts/Auth";
import { useSdkClient } from "@/contexts/Sdk";
import { useFeatureFlag, type FeatureFlagResult } from "@/hooks/useFeatureFlag";
import { FEATURE_FLAGS } from "@/lib/featureFlags";
import { SettingsSection } from "@/components/page-templates";
import { Button } from "@/components/ui/Button";
import { SimpleTooltip, TooltipProvider } from "@/components/ui/Tooltip";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/Select";
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

const security = { sessionHeaderGramSession: "" };

export function AgentAPIKeys({ agent }: { agent: ManagedAgent }): JSX.Element {
  const organization = useOrganization();
  // Delegable candidates are specific to the authorizer, and the signed-in
  // user can change without the organization changing.
  const { user } = useSession();
  const flag = useFeatureFlag(FEATURE_FLAGS.agentCredentials);
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
}: {
  agent: ManagedAgent;
  organizationId: string;
  userId: string;
  flag: FeatureFlagResult;
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
  const [open, setOpen] = useState(false);
  const [name, setName] = useState("");
  const [narrowings, setNarrowings] = useState<GrantNarrowings>({});
  const [expiryDays, setExpiryDays] = useState("90");
  const [customExpiry, setCustomExpiry] = useState("");
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
  const delegable = useQuery({
    queryKey: ["agent-delegable-grants", organizationId, userId, agent.id],
    queryFn: ({ signal }) =>
      sdk.agents.listDelegableGrants({ agentId: agent.id }, undefined, {
        signal,
      }),
    enabled: open && canIssue,
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
  const create = useCreateAPIKeyMutation({ gcTime: 0, retry: false });
  const resetCreation = create.reset;
  useEffect(() => {
    rolloutEnabled.current = enabled;
    if (!enabled) {
      setOpen(false);
      setSecret(null);
      setCopied(false);
      resetCreation();
    }
  }, [enabled, resetCreation]);
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
    setName("");
    setNarrowings({});
    setExpiryDays("90");
    setCustomExpiry("");
    setError(null);
    create.reset();
  };
  const disablingReasons: string[] = [];
  if (!canIssue)
    disablingReasons.push(
      "Issuance requires an active agent with a valid owner and credential authorization.",
    );
  if (create.isPending) disablingReasons.push("An API key is being created.");
  if (delegable.isFetching)
    disablingReasons.push("Delegable permissions are still loading.");
  else if (!discoveryComplete)
    disablingReasons.push(
      "Delegable permissions could not be loaded. Retry permissions.",
    );
  let validatedName = "";
  try {
    validatedName = validateAgentAPIKeyName(name);
  } catch (error) {
    disablingReasons.push(
      error instanceof Error ? error.message : "Enter a valid key name.",
    );
  }
  let requestedGrants: AgentPolicyGrantForm[] = [];
  try {
    const selections = (discoveryComplete ? (delegable.data ?? []) : [])
      .map((grant) => ({ grant, key: delegableGrantKey(grant) }))
      .filter(({ key }) => narrowings[key] !== undefined)
      .map(({ grant, key }) => ({ grant, narrowing: narrowings[key] ?? {} }));
    requestedGrants = buildRequestedGrants(selections);
  } catch (error) {
    disablingReasons.push(
      error instanceof Error ? error.message : "Select valid permissions.",
    );
  }
  if (!requestedGrants.length)
    disablingReasons.push("Select at least one valid permission.");
  const expiryValidation = (now: number) => {
    // Date-only selections expire at local midnight, not UTC midnight.
    const expiresAt =
      expiryDays === "custom"
        ? new Date(`${customExpiry}T00:00:00`)
        : new Date(now + Number(expiryDays) * 86_400_000);
    let reason: string | undefined;
    if (!Number.isFinite(expiresAt.getTime()))
      reason = "Choose a valid expiration date.";
    else if (expiresAt.getTime() <= now)
      reason = "Expiration date must be in the future.";
    else if (expiresAt.getTime() > now + 365 * 86_400_000)
      reason = "Expiration date must be within 365 days.";
    return { expiresAt, reason };
  };
  const expiryReason = expiryValidation(Date.now()).reason;
  if (expiryReason) disablingReasons.push(expiryReason);
  const issue = () => {
    if (disablingReasons.length) return;
    // Recheck at submission in case the form stayed open past midnight.
    const { expiresAt, reason } = expiryValidation(Date.now());
    if (reason) {
      setError(reason);
      return;
    }
    setError(null);
    create.mutate(
      {
        security,
        request: {
          createKeyForm: {
            agentId: agent.id,
            name: validatedName,
            expiresAt,
            delegatedGrantsVersion: 1,
            requestedGrants,
            scopes: [],
          },
        },
      },
      {
        onSuccess: (key) => {
          if (rolloutEnabled.current) setSecret(key.key ?? null);
          create.reset();
          refresh();
        },
        onError: () => {
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
  if (!canManage)
    return (
      <Text muted>
        You do not have permission to view or manage this agent's API keys.
      </Text>
    );
  return (
    <>
      {!enabled && (
        <Text muted>
          {flag.status === "loading"
            ? "Checking API key availability…"
            : flag.status === "disabled"
              ? "Agent API keys are disabled for this organization."
              : "Agent API key availability could not be determined."}
        </Text>
      )}
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
        <Table data={knownKeys} columns={columns} rowKey={(key) => key.id} />
      ) : keys.data ? (
        <Text muted>No API keys yet</Text>
      ) : null}
      {!canIssue && (
        <Text muted>
          Issuance requires an active agent with a valid owner and credential
          authorization. Existing keys can still be revoked.
        </Text>
      )}
      <Button
        disabled={!canIssue || unavailable}
        onClick={() => {
          setError(null);
          setOpen(true);
        }}
      >
        Create API key
      </Button>
      <Dialog
        open={open && enabled}
        onOpenChange={(value) => {
          if (!value && !create.isPending) close();
        }}
      >
        <Dialog.Content>
          <Dialog.Header>
            <Dialog.Title>
              {secret ? "Save your API key" : "Create agent API key"}
            </Dialog.Title>
            <Dialog.Description>
              {secret
                ? "This key is shown only once. Copy it now and store it securely."
                : "Choose an expiry for this key. Effective access remains limited by the agent policy, the owner's live permissions and your own; validation can reject grants outside that ceiling."}
            </Dialog.Description>
          </Dialog.Header>
          {secret ? (
            <div className="space-y-4">
              <code className="block break-all">{secret}</code>
              <Button
                onClick={() => {
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
          ) : (
            <form
              className="space-y-4"
              onSubmit={(event) => {
                event.preventDefault();
                issue();
              }}
            >
              <label className="block space-y-2">
                Key name
                <Input required value={name} onChange={setName} />
              </label>
              <DelegableGrantSection
                isFetching={delegable.isFetching}
                isComplete={discoveryComplete}
                grants={delegable.data ?? []}
                narrowings={narrowings}
                onChangeNarrowings={setNarrowings}
                disabled={create.isPending}
                onRetry={() => void delegable.refetch()}
              />
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
                  Keys expire within 365 days. Custom dates use midnight in your
                  local time zone.
                </Text>
              </div>
              <TooltipProvider>
                <SimpleTooltip
                  tooltip={
                    disablingReasons.length ? (
                      <ul className="list-disc pl-4">
                        {disablingReasons.map((reason) => (
                          <li key={reason}>{reason}</li>
                        ))}
                      </ul>
                    ) : (
                      "Create this API key"
                    )
                  }
                >
                  <span
                    className="inline-flex"
                    tabIndex={disablingReasons.length ? 0 : undefined}
                    aria-label={
                      disablingReasons.length
                        ? disablingReasons.join(" ")
                        : undefined
                    }
                  >
                    <Button
                      type="submit"
                      disabled={disablingReasons.length > 0}
                    >
                      {create.isPending ? "Creating…" : "Create key"}
                    </Button>
                  </span>
                </SimpleTooltip>
              </TooltipProvider>
            </form>
          )}
          {error && <p role="alert">{error}</p>}
        </Dialog.Content>
      </Dialog>
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
      <Text muted>
        Delegable permissions could not be loaded, so none can be delegated.
        Retry to select permissions before creating a key.
      </Text>
      <Button type="button" variant="secondary" onClick={onRetry}>
        Retry permissions
      </Button>
    </div>
  );
}
