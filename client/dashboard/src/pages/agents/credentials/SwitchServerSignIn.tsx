import { Button } from "@/components/ui/Button";
import { ConfirmDialog } from "@/components/ui/ConfirmDialog";
import { useSdkClient } from "@/contexts/Sdk";
import { linkMcpServerUserSessionIssuer } from "@/lib/linkUserSessionIssuer";
import { invalidateAllGetMcpServer } from "@gram/client/react-query/getMcpServer.js";
import { invalidateAllListMcpServersForOrg } from "@gram/client/react-query/listMcpServersForOrg.js";
import { invalidateAllMcpServers } from "@gram/client/react-query/mcpServers.js";
import { useUserSessionIssuerAttachImpact } from "@gram/client/react-query/userSessionIssuerAttachImpact.js";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useState, type JSX } from "react";
import { toast } from "sonner";
import type { SetupServer } from "./setupInstructions";

/**
 * Moves a server onto a shared issuer, after a confirmation that previews what
 * the move changes. Everyone connected to the server signs in again, because
 * their sessions belong to its current issuer.
 */
export function SwitchServerSignIn({
  server,
  target,
}: {
  server: SetupServer;
  target: { id: string; label: string };
}): JSX.Element {
  const sdk = useSdkClient();
  const queryClient = useQueryClient();
  const [open, setOpen] = useState(false);

  const impact = useUserSessionIssuerAttachImpact(
    {
      gramProject: server.projectSlug,
      mcpServerId: server.id,
      userSessionIssuerId: target.id,
    },
    undefined,
    { enabled: open, throwOnError: false, retry: false },
  );

  const move = useMutation({
    mutationFn: () =>
      linkMcpServerUserSessionIssuer(
        sdk,
        server.id,
        target.id,
        undefined,
        server.projectSlug,
      ),
    onSuccess: async () => {
      setOpen(false);
      toast.success(`${target.label} is now assigned to ${server.name}`);
      await Promise.all([
        invalidateAllListMcpServersForOrg(queryClient, { refetchType: "all" }),
        invalidateAllGetMcpServer(queryClient, { refetchType: "all" }),
        invalidateAllMcpServers(queryClient, { refetchType: "all" }),
      ]);
    },
  });

  const issuerUrl = impact.data?.candidateIssuer?.issuerUrl;
  const summary = impact.isError
    ? "Couldn't check where clients will sign in. You can still assign it."
    : issuerUrl
      ? `Clients will sign in at ${target.label}, ${issuerUrl}. Update any identity provider or platform set up with ${server.name}'s current MCP authentication provider.`
      : `Clients will sign in at ${target.label}.`;

  return (
    <>
      <Button
        variant="secondary"
        size="sm"
        onClick={() => {
          move.reset();
          setOpen(true);
        }}
      >
        <Button.Text>
          Assign {target.label} to {server.name}
        </Button.Text>
      </Button>
      <ConfirmDialog
        open={open}
        onOpenChange={setOpen}
        title={`Assign ${target.label} to ${server.name}?`}
        description={`${server.name} will use ${target.label} as its MCP authentication provider, so this agent's token works there too. Everyone connected to ${server.name} will need to sign in again.`}
        confirmLabel="Assign"
        pendingLabel="Assigning…"
        confirmVariant="primary"
        onConfirm={() => move.mutate()}
        isPending={move.isPending}
        impact={{ summary, isLoading: impact.isFetching }}
        error={move.error?.message}
      />
    </>
  );
}
