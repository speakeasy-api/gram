import { useState } from "react";
import { SettingsSection } from "@/components/page-templates";
import { InlineEmptyState } from "@/components/inline-empty-state";
import { Badge } from "@/components/ui/Badge";
import { Button } from "@/components/ui/Button";
import { Dialog } from "@/components/ui/Dialog";
import { SkeletonTable } from "@/components/ui/Skeleton";
import { Table, type Column } from "@/components/ui/Table";
import { Text } from "@/components/ui/Text";
import { HumanizeDateTime } from "@/lib/dates";

/** Presentation model, populated only by the agent-scoped sessions endpoint. */
export type AgentSessionRow = {
  id: string;
  clientName?: string;
  issuerSlug?: string;
  refreshExpiresAt?: Date;
  createdAt: Date;
  expiresAt: Date;
  lastUsedAt?: Date;
  revokedAt?: Date;
};

export type AgentSessionsSectionProps = {
  sessions: AgentSessionRow[];
  isLoading: boolean;
  isError: boolean;
  /** Use the agent's server-calculated credential permission, not an RBAC scope. */
  canRevoke: boolean;
  canRead?: boolean;
  onRetry: () => void;
  onRevoke: (session: AgentSessionRow) => Promise<void>;
  hasMore?: boolean;
  isLoadingMore?: boolean;
  loadMoreError?: boolean;
  onLoadMore?: () => void;
};

export function AgentSessionsSection({
  sessions,
  isLoading,
  isError,
  canRevoke,
  canRead = true,
  onRetry,
  onRevoke,
  hasMore = false,
  isLoadingMore = false,
  loadMoreError = false,
  onLoadMore,
}: AgentSessionsSectionProps): JSX.Element {
  const [selected, setSelected] = useState<AgentSessionRow | null>(null);
  const [pending, setPending] = useState(false);
  const [revokeError, setRevokeError] = useState(false);

  async function confirmRevoke() {
    if (!selected || !canRevoke || pending) return;
    setPending(true);
    setRevokeError(false);
    try {
      await onRevoke(selected);
      setSelected(null);
    } catch {
      setRevokeError(true);
    } finally {
      setPending(false);
    }
  }

  const columns: Column<AgentSessionRow>[] = [
    {
      key: "clientName",
      header: "Client name (unverified)",
      render: (session) => session.clientName || "Unknown client",
    },
    {
      key: "issuerSlug",
      header: "Issuer",
      render: (session) => session.issuerSlug || "Unknown issuer",
    },
    {
      key: "createdAt",
      header: "Created",
      render: (session) => <HumanizeDateTime date={session.createdAt} />,
    },
    {
      key: "lastUsedAt",
      header: "Last used",
      render: (session) =>
        session.lastUsedAt ? (
          <HumanizeDateTime date={session.lastUsedAt} />
        ) : (
          "Unknown"
        ),
    },
    {
      key: "expiresAt",
      header: "Expires",
      render: (session) => <HumanizeDateTime date={session.expiresAt} />,
    },
    {
      key: "status",
      header: "Status",
      render: (session) => (
        <Badge
          size="sm"
          variant={
            session.revokedAt
              ? "destructive"
              : sessionIsExpired(session)
                ? "neutral"
                : "success"
          }
        >
          {session.revokedAt
            ? "Revoked"
            : sessionIsExpired(session)
              ? "Expired"
              : "Active"}
        </Badge>
      ),
    },
    {
      key: "actions",
      header: "",
      render: (session) =>
        !session.revokedAt && canRevoke ? (
          <Button
            size="sm"
            variant="destructive-secondary"
            onClick={() => {
              setRevokeError(false);
              setSelected(session);
            }}
          >
            Revoke session
          </Button>
        ) : null,
    },
  ];

  return (
    <SettingsSection>
      <SettingsSection.Header>
        <SettingsSection.Title>Sessions</SettingsSection.Title>
        <SettingsSection.Description>
          Sessions authorized to act as this agent. Revocation prevents further
          use of a session.
        </SettingsSection.Description>
      </SettingsSection.Header>
      <SettingsSection.Panel>
        <SettingsSection.Body>
          {!canRead ? (
            <Text>
              You do not have permission to view this agent's sessions.
            </Text>
          ) : isLoading ? (
            <div role="status" aria-label="Loading agent sessions">
              <SkeletonTable />
            </div>
          ) : isError ? (
            <div role="alert" className="space-y-3">
              <Text>Unable to load agent sessions.</Text>
              <Button variant="secondary" onClick={onRetry}>
                Try again
              </Button>
            </div>
          ) : sessions.length === 0 ? (
            <InlineEmptyState
              icon="key-round"
              heading="No sessions yet"
              description="Choose this agent when authorizing an MCP connection to create a session."
            />
          ) : (
            <Table
              columns={columns}
              data={sessions}
              rowKey={(session) => session.id}
            />
          )}
          {canRead && loadMoreError && (
            <Text role="alert">Unable to load more sessions. Try again.</Text>
          )}
          {canRead && hasMore && onLoadMore && (
            <Button
              variant="secondary"
              disabled={isLoadingMore}
              onClick={onLoadMore}
            >
              {isLoadingMore ? "Loading more…" : "Load more sessions"}
            </Button>
          )}
          {canRead && !canRevoke && (
            <Text small muted>
              You do not have permission to revoke this agent's sessions.
            </Text>
          )}
        </SettingsSection.Body>
      </SettingsSection.Panel>
      <Dialog
        open={selected !== null}
        onOpenChange={(open) => {
          if (!open && !pending) setSelected(null);
        }}
      >
        <Dialog.Content>
          <Dialog.Header>
            <Dialog.Title>Revoke session?</Dialog.Title>
            <Dialog.Description>
              This permanently revokes the selected session. The client must
              authorize a new session to reconnect.
            </Dialog.Description>
          </Dialog.Header>
          {selected && (
            <Text small>
              Client name (unverified):{" "}
              {selected.clientName || "Unknown client"}. Created{" "}
              <HumanizeDateTime date={selected.createdAt} />.
            </Text>
          )}
          {revokeError && (
            <Text role="alert">Unable to revoke the session. Try again.</Text>
          )}
          <Dialog.Footer>
            <Button
              variant="secondary"
              disabled={pending}
              onClick={() => setSelected(null)}
            >
              Cancel
            </Button>
            <Button
              variant="destructive-primary"
              disabled={pending || !canRevoke}
              onClick={() => void confirmRevoke()}
            >
              {pending ? "Revoking…" : "Confirm revoke"}
            </Button>
          </Dialog.Footer>
        </Dialog.Content>
      </Dialog>
    </SettingsSection>
  );
}

function sessionIsExpired(session: AgentSessionRow): boolean {
  return (
    Math.min(
      session.expiresAt.getTime(),
      session.refreshExpiresAt?.getTime() ?? Infinity,
    ) <= Date.now()
  );
}
