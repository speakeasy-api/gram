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
      toast.success(`${server.name} now signs in through ${target.label}`);
      await Promise.all([
        invalidateAllListMcpServersForOrg(queryClient, { refetchType: "all" }),
        invalidateAllGetMcpServer(queryClient, { refetchType: "all" }),
        invalidateAllMcpServers(queryClient, { refetchType: "all" }),
      ]);
    },
  });

  const issuerUrl = impact.data?.candidateIssuer?.issuerUrl;
  const summary = impact.isError
    ? "Couldn't check where clients will sign in. You can still switch."
    : issuerUrl
      ? `Clients will be sent to ${target.label}'s shared authorization server, ${issuerUrl}. Update any identity provider or platform set up with ${server.name}'s current one.`
      : `Clients will be sent to ${target.label}'s shared authorization server.`;

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
        <Button.Text>Switch to {target.label}</Button.Text>
      </Button>
      <ConfirmDialog
        open={open}
        onOpenChange={setOpen}
        title={`Switch ${server.name} to ${target.label}?`}
        description={`${server.name} will sign people and workloads in through ${target.label}. Everyone connected to ${server.name} will need to sign in again, because their sessions belong to its current user session issuer.`}
        confirmLabel="Switch sign-in"
        pendingLabel="Switching…"
        confirmVariant="primary"
        onConfirm={() => move.mutate()}
        isPending={move.isPending}
        impact={{ summary, isLoading: impact.isFetching }}
        error={move.error?.message}
      />
    </>
  );
}
