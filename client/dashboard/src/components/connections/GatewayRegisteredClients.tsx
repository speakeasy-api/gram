import { useSearchParams } from "react-router";
import { useState } from "react";
import type { UserSessionClient } from "@gram/client/models/components/usersessionclient.js";
import { useUserSessionClientsInfinite } from "@gram/client/react-query/userSessionClients.js";
import { RevokeClientDialog } from "@/components/sessions/RevokeClientDialog";
import { Table, type Column } from "@/components/ui/Table";
import { SkeletonTable } from "@/components/ui/Skeleton";
import { MoreActions } from "@/components/ui/MoreActions";
import { Button } from "@/components/ui/Button";
import { Text } from "@/components/ui/Text";
import { useRBAC } from "@/hooks/useRBAC";

/** Registrations remain manageable even when they have no active sessions. */
export function GatewayRegisteredClients({
  issuerId,
  projectId,
  onRevoked,
}: {
  issuerId: string;
  projectId: string;
  onRevoked: () => void;
}): JSX.Element {
  const [open, setOpen] = useState(false);
  const [, setParams] = useSearchParams();
  const openRegistration = (id: string) =>
    setParams(
      (previous) => {
        const next = new URLSearchParams(previous);
        next.set("registration", id);
        return next;
      },
      { replace: true },
    );
  const [revoking, setRevoking] = useState<UserSessionClient | null>(null);
  const clients = useUserSessionClientsInfinite(
    { userSessionIssuerId: issuerId, limit: 50 },
    undefined,
    { enabled: open },
  );
  const { hasScope } = useRBAC();
  const canRevoke = hasScope("project:write", projectId);
  const columns: Column<UserSessionClient>[] = [
    {
      key: "clientName",
      header: "Client",
      render: (client) => client.clientName,
    },
    { key: "activeSessionCount", header: "Active sessions" },
    {
      key: "createdAt",
      header: "Registered",
      render: (client) => client.createdAt.toLocaleString(),
    },
    {
      key: "actions",
      header: "",
      width: "48px",
      render: (client) => (
        <MoreActions
          actions={[
            {
              label: "View registration",
              onClick: () => openRegistration(client.id),
            },
            ...(canRevoke
              ? [
                  {
                    label: "Revoke client",
                    destructive: true,
                    onClick: () => setRevoking(client),
                  },
                ]
              : []),
          ]}
        />
      ),
    },
  ];
  return (
    <section className="space-y-4">
      <Button
        variant="tertiary"
        onClick={() => setOpen(!open)}
        aria-expanded={open}
      >
        {open ? "Hide registered clients" : "Show registered clients"}
      </Button>
      {open && (
        <>
          <Text small muted>
            Client registrations belong to the gateway’s issuer and can be
            shared across endpoints.
          </Text>
          {clients.isPending ? (
            <SkeletonTable />
          ) : (
            <Table
              columns={columns}
              data={
                clients.data?.pages.flatMap((page) => page.result.items) ?? []
              }
              rowKey={(client) => client.id}
            />
          )}
          {clients.isError && (
            <Text small>Couldn’t load registered clients.</Text>
          )}
          {(clients.hasNextPage || clients.isError) && (
            <Button
              variant="tertiary"
              size="sm"
              disabled={clients.isFetching}
              onClick={() =>
                void (clients.isError && !clients.isFetchNextPageError
                  ? clients.refetch()
                  : clients.fetchNextPage())
              }
            >
              {clients.isFetching
                ? "Loading…"
                : clients.isError
                  ? "Retry"
                  : "Load more clients"}
            </Button>
          )}
        </>
      )}
      {revoking && (
        <RevokeClientDialog
          client={revoking}
          open
          onOpenChange={(value) => {
            if (!value) setRevoking(null);
          }}
          onRevoked={() => {
            onRevoked();
          }}
        />
      )}
    </section>
  );
}
