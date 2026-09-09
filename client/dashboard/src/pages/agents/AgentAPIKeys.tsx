import { useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useOrganization } from "@/contexts/Auth";
import { useSdkClient } from "@/contexts/Sdk";
import { useFeatureFlag } from "@/hooks/useFeatureFlag";
import { FEATURE_FLAGS } from "@/lib/featureFlags";
import { SettingsSection } from "@/components/page-templates";
import { Button } from "@/components/ui/Button";
import { Dialog } from "@/components/ui/Dialog";
import { Input } from "@/components/ui/Input";
import { TextArea } from "@/components/ui/Textarea";
import { Text } from "@/components/ui/Text";
import { Table, type Column } from "@/components/ui/Table";
import { HumanizeDateTime } from "@/lib/dates";
import type { ManagedAgent } from "@gram/client/models/components/managedagent.js";
import type { Key } from "@gram/client/models/components/key.js";
import { parseDelegatedGrants } from "./agent-api-key-grants";
import { useListAPIKeys } from "@gram/client/react-query/listAPIKeys";
import { useCreateAPIKeyMutation } from "@gram/client/react-query/createAPIKey";
import { useRevokeAPIKeyMutation } from "@gram/client/react-query/revokeAPIKey";

const security = { sessionHeaderGramSession: "" };

export function AgentAPIKeys({ agent }: { agent: ManagedAgent }): JSX.Element {
  const organization = useOrganization();
  const flag = useFeatureFlag(FEATURE_FLAGS.agentCredentials);
  return (
    <SettingsSection>
      <SettingsSection.Header>
        <SettingsSection.Title>API keys</SettingsSection.Title>
        <SettingsSection.Description>
          Credentials delegated to this agent, limited by its policy and its
          owner's live permissions.
        </SettingsSection.Description>
      </SettingsSection.Header>
      <SettingsSection.Panel>
        <SettingsSection.Body>
          {flag.status === "enabled" ? (
            <AgentAPIKeysContent
              key={`${organization.id}:${agent.id}:${agent.permissions.authorize}`}
              agent={agent}
              organizationId={organization.id}
            />
          ) : (
            <Text muted>
              {flag.status === "loading"
                ? "Checking API key availability…"
                : flag.status === "disabled"
                  ? "Agent API keys are disabled for this organization."
                  : "Agent API key availability could not be determined."}
            </Text>
          )}
        </SettingsSection.Body>
      </SettingsSection.Panel>
    </SettingsSection>
  );
}

function AgentAPIKeysContent({
  agent,
  organizationId,
}: {
  agent: ManagedAgent;
  organizationId: string;
}) {
  const sdk = useSdkClient();
  const queryClient = useQueryClient();
  const canManage = agent.permissions.authorize;
  const canIssue =
    canManage &&
    agent.lifecycle === "active" &&
    !agent.ownerReassignmentRequiredAt;
  const [open, setOpen] = useState(false);
  const [name, setName] = useState("");
  const [policyJSON, setPolicyJSON] = useState("");
  const [selected, setSelected] = useState<string[]>([]);
  const [withoutPermissions, setWithoutPermissions] = useState(false);
  const [secret, setSecret] = useState<string | null>(null);
  const [copied, setCopied] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [revoke, setRevoke] = useState<Key | null>(null);
  const keys = useListAPIKeys({ agentId: agent.id }, security, {
    queryKeyHashFn: (key) => JSON.stringify([organizationId, key]),
    enabled: canManage,
    retry: false,
    throwOnError: false,
  });
  const policy = useQuery({
    queryKey: ["agent-api-key-policy", organizationId, agent.id],
    queryFn: ({ signal }) =>
      sdk.agents.listPolicyGrants({ agentId: agent.id }, undefined, { signal }),
    enabled: open && canIssue && agent.permissions.write,
    retry: false,
    throwOnError: false,
  });
  const create = useCreateAPIKeyMutation({ gcTime: 0, retry: false });
  const revocation = useRevokeAPIKeyMutation({ retry: false });
  const unavailable =
    keys.isError && "statusCode" in keys.error && keys.error.statusCode === 404;
  const refresh = () => {
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
    setSelected([]);
    setPolicyJSON("");
    setWithoutPermissions(false);
    setError(null);
    create.reset();
  };
  const issue = () => {
    if (!canIssue || create.isPending) return;
    try {
      const requestedGrants =
        policy.data && agent.permissions.write
          ? policy.data
              .filter((grant) => selected.includes(grant.id))
              .map(({ effect, scope, selector }) => ({
                effect,
                scope,
                selector,
              }))
          : parseDelegatedGrants(
              policyJSON || (withoutPermissions ? "[]" : ""),
            );
      if (!requestedGrants.length && !withoutPermissions)
        throw new Error(
          "Select permissions or explicitly confirm Create without permissions.",
        );
      setError(null);
      create.mutate(
        {
          security,
          request: {
            createKeyForm: {
              agentId: agent.id,
              name: name.trim(),
              delegatedGrantsVersion: 1,
              requestedGrants,
              scopes: [],
            },
          },
        },
        {
          onSuccess: (key) => {
            setSecret(key.key ?? null);
            create.reset();
            refresh();
          },
          onError: () => {
            setError(
              "Could not create API key. Check that the requested grants are allowed by the agent policy and owner's live permissions, and that the agent is active with a valid owner.",
            );
            create.reset();
          },
        },
      );
    } catch {
      setError(
        "Enter a valid JSON array of allow grants, select existing permissions, or explicitly confirm Create without permissions.",
      );
    }
  };
  const columns: Column<Key>[] = [
    { key: "name", header: "Name", render: (key) => key.name },
    { key: "keyPrefix", header: "Prefix", render: (key) => key.keyPrefix },
    {
      key: "expiresAt",
      header: "Expires",
      render: (key) =>
        key.expiresAt ? <HumanizeDateTime date={key.expiresAt} /> : "—",
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
      {keys.isLoading ? (
        <Text muted>Loading API keys…</Text>
      ) : keys.isError ? (
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
      ) : keys.data?.keys.length ? (
        <Table
          data={keys.data.keys}
          columns={columns}
          rowKey={(key) => key.id}
        />
      ) : (
        <Text muted>No API keys yet</Text>
      )}
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
        open={open}
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
                : "Keys expire after 90 days. Effective access remains limited by the agent policy and owner's live permissions; validation can reject grants outside that ceiling."}
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
              {agent.permissions.write && policy.isLoading ? (
                <Text>Loading agent policy…</Text>
              ) : agent.permissions.write && policy.data ? (
                <fieldset className="space-y-2">
                  <legend>Delegate selected agent permissions</legend>
                  {policy.data.length ? (
                    policy.data.map((grant) => (
                      <label key={grant.id} className="flex items-start gap-2">
                        <input
                          type="checkbox"
                          checked={selected.includes(grant.id)}
                          onChange={(event) =>
                            setSelected(
                              event.target.checked
                                ? [...selected, grant.id]
                                : selected.filter((id) => id !== grant.id),
                            )
                          }
                        />
                        <span>
                          {grant.scope}
                          <code className="block text-xs break-all">
                            {JSON.stringify(grant.selector)}
                          </code>
                        </span>
                      </label>
                    ))
                  ) : (
                    <Text muted>The agent policy has no permissions.</Text>
                  )}
                </fieldset>
              ) : (
                <div className="space-y-2">
                  <Text muted>
                    {agent.permissions.write
                      ? "Agent policy could not be loaded. Retry or enter explicit grants below."
                      : "You can authorize credentials but cannot read agent policy. Ask a policy administrator for the exact grants to delegate."}
                  </Text>
                  {agent.permissions.write && (
                    <Button
                      type="button"
                      variant="secondary"
                      onClick={() => void policy.refetch()}
                    >
                      Retry policy
                    </Button>
                  )}
                  <label className="block space-y-2">
                    Requested grants (JSON)
                    <TextArea
                      value={policyJSON}
                      onChange={setPolicyJSON}
                      placeholder={
                        '[{"effect":"allow","scope":"…","selector":{"resource_kind":"…","resource_id":"…"}}]'
                      }
                    />
                  </label>
                  <Text small muted>
                    Use API field names such as resource_kind and resource_id.
                    No permissions are selected by default.
                  </Text>
                </div>
              )}
              <label className="flex items-start gap-2">
                <input
                  type="checkbox"
                  checked={withoutPermissions}
                  onChange={(event) =>
                    setWithoutPermissions(event.target.checked)
                  }
                />
                <span>
                  Create without permissions
                  <br />
                  <Text as="span" small muted>
                    If no grants are selected, this key will not authorize any
                    actions.
                  </Text>
                </span>
              </label>
              <Button
                type="submit"
                disabled={
                  !canIssue ||
                  create.isPending ||
                  !name.trim() ||
                  (agent.permissions.write && policy.isLoading)
                }
              >
                {create.isPending ? "Creating…" : "Create key"}
              </Button>
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
