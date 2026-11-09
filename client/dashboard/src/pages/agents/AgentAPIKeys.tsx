import { useEffect, useRef, useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { useOrganization, useSession } from "@/contexts/Auth";
import { Button } from "@/components/ui/Button";
import { Dialog } from "@/components/ui/Dialog";
import { Text } from "@/components/ui/Text";
import { agentIssuanceBlocked } from "./agent-issuance";
import {
  IdentityPanel,
  IdentityPanelEmpty,
  IdentityPanelRow,
} from "@/pages/identities/IdentityPanel";
import type { ManagedAgent } from "@gram/client/models/components/managedagent.js";
import type { Key } from "@gram/client/models/components/key.js";
import { dateTimeFormatters } from "@/lib/dates";
import { useListAPIKeys } from "@gram/client/react-query/listAPIKeys";
import { useRevokeAPIKeyMutation } from "@gram/client/react-query/revokeAPIKey";

const security = { sessionHeaderGramSession: "" };

export function AgentAPIKeys({
  agent,
  federated = false,
  onCreate,
}: {
  agent: ManagedAgent;
  /**
   * The agent signs in with a platform's tokens, so a key is optional.
   * "unknown" where the caller's own read of that has not settled: the panel
   * then claims neither, rather than telling a federated agent it cannot
   * authenticate.
   */
  federated?: boolean | "unknown";
  onCreate?: () => void;
}): JSX.Element {
  const organization = useOrganization();
  // Revocation state belongs to one authorizer, and the signed-in user can
  // change without the organization changing.
  const { user } = useSession();
  // The listing brings its own panel, the same one the federated platforms
  // beside it use, so the caller supplies only the section around them.
  return (
    <AgentAPIKeysContent
      key={`${organization.id}:${user.id}:${agent.id}:${agent.permissions.authorize}`}
      agent={agent}
      organizationId={organization.id}
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
  federated = false,
  onCreate,
}: {
  agent: ManagedAgent;
  organizationId: string;
  federated?: boolean | "unknown";
  onCreate?: () => void;
}) {
  const queryClient = useQueryClient();
  const mounted = useRef(true);
  const canManage = agent.permissions.authorize;
  const issuanceBlocked = agentIssuanceBlocked(agent, canManage);
  const canIssue = issuanceBlocked === null;
  const [error, setError] = useState<string | null>(null);
  const [revoke, setRevoke] = useState<Key | null>(null);
  const keys = useListAPIKeys({ agentId: agent.id }, security, {
    queryKeyHashFn: (key) => JSON.stringify([organizationId, key]),
    enabled: canManage,
    retry: false,
    throwOnError: false,
  });
  // Revocation resolves after this component may have unmounted.
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
  if (!canManage)
    return (
      <Text muted>
        You do not have permission to view or manage this agent's API keys.
      </Text>
    );
  return (
    <>
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
          knownKeys.map((key) => (
            <IdentityPanelRow
              key={key.id}
              title={key.name}
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
              }
            />
          ))
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
