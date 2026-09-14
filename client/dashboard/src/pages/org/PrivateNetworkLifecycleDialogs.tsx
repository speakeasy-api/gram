import { Alert } from "@/components/ui/Alert";
import { Button } from "@/components/ui/Button";
import { Dialog } from "@/components/ui/Dialog";
import type { NetworkIngress } from "@gram/client/models/components/networkingress.js";
import { NetworkIngressCredentialsFields } from "./NetworkIngressCredentialsFields";
import { Text } from "@/components/ui/Text";
import { handleAPIError } from "@/lib/errors";
import { invalidateAllNetworkIngress } from "@gram/client/react-query/networkIngress.js";
import { toast } from "sonner";
import { useNetworkIngressDeleteIngressMutation } from "@gram/client/react-query/networkIngressDeleteIngress.js";
import { useNetworkIngressGetDeleteImpact } from "@gram/client/react-query/networkIngressGetDeleteImpact.js";
import { useQueryClient } from "@tanstack/react-query";
import { useRotateNetworkIngressCredentialsMutation } from "@gram/client/react-query/rotateNetworkIngressCredentials.js";
import { useState } from "react";

function totalImpactedServers(impact: {
  mcpServersDual: number;
  mcpServersPrivateOnly: number;
  metaMcpServersDual: number;
  metaMcpServersPrivateOnly: number;
}): number {
  return (
    impact.mcpServersDual +
    impact.mcpServersPrivateOnly +
    impact.metaMcpServersDual +
    impact.metaMcpServersPrivateOnly
  );
}

export function RotateNetworkIngressCredentialsDialog({
  open,
  onOpenChange,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
}): JSX.Element {
  const queryClient = useQueryClient();
  const [clientId, setClientId] = useState("");
  const [clientSecret, setClientSecret] = useState("");
  const clear = () => {
    setClientId("");
    setClientSecret("");
  };
  const rotate = useRotateNetworkIngressCredentialsMutation({
    gcTime: 0,
    onSuccess: async () => {
      clear();
      await invalidateAllNetworkIngress(queryClient);
      onOpenChange(false);
      toast.success("Tailscale credentials rotated");
    },
    onError: (error) => {
      clear();
      handleAPIError(error, "Failed to rotate Tailscale credentials");
    },
    onSettled: () => {
      rotate.reset();
    },
  });

  const close = () => {
    if (rotate.isPending) return;
    clear();
    rotate.reset();
    onOpenChange(false);
  };

  return (
    <Dialog
      open={open}
      onOpenChange={(nextOpen) => {
        if (!nextOpen) close();
        else onOpenChange(true);
      }}
    >
      <Dialog.Content>
        <Dialog.Header>
          <Dialog.Title>Rotate Tailscale credentials</Dialog.Title>
          <Dialog.Description>
            The new OAuth client replaces the stored credentials. Values are
            never shown again after submission.
          </Dialog.Description>
        </Dialog.Header>
        <div className="space-y-4 py-4">
          <NetworkIngressCredentialsFields
            idPrefix="rotate-tailscale"
            clientId={clientId}
            clientSecret={clientSecret}
            onClientIdChange={setClientId}
            onClientSecretChange={setClientSecret}
          />
        </div>
        <Dialog.Footer>
          <Button
            variant="secondary"
            onClick={close}
            disabled={rotate.isPending}
          >
            Cancel
          </Button>
          <Button
            disabled={
              clientId.trim() === "" || clientSecret === "" || rotate.isPending
            }
            onClick={() =>
              rotate.mutate({
                security: { sessionHeaderGramSession: "" },
                request: {
                  rotateCredentialsRequestBody: {
                    oauthClientId: clientId.trim(),
                    oauthClientSecret: clientSecret,
                  },
                },
              })
            }
          >
            {rotate.isPending ? "Rotating..." : "Rotate credentials"}
          </Button>
        </Dialog.Footer>
      </Dialog.Content>
    </Dialog>
  );
}

export function DeleteNetworkIngressDialog({
  open,
  onOpenChange,
  ingress,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  ingress: NetworkIngress;
}): JSX.Element {
  const queryClient = useQueryClient();
  const impact = useNetworkIngressGetDeleteImpact(undefined, undefined, {
    enabled: open,
    throwOnError: false,
  });
  const remove = useNetworkIngressDeleteIngressMutation({
    onSuccess: async () => {
      onOpenChange(false);
      await invalidateAllNetworkIngress(queryClient);
      toast.success("Private network removal started");
    },
    onError: (error) =>
      handleAPIError(error, "Failed to remove private network"),
  });
  const affected = impact.data ? totalImpactedServers(impact.data) : 0;

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <Dialog.Content>
        <Dialog.Header>
          <Dialog.Title>Remove private network</Dialog.Title>
          <Dialog.Description>
            This revokes the private route and starts provider cleanup. It never
            reopens private-only servers on public routes.
          </Dialog.Description>
        </Dialog.Header>
        <div className="space-y-4 py-4">
          {impact.isLoading && (
            <Text small muted>
              Checking affected MCP servers...
            </Text>
          )}
          {impact.isError ? (
            <Alert variant="error" dismissible={false}>
              <div className="space-y-3">
                <Text small>
                  Affected MCP servers could not be checked. Removal is blocked
                  until this check succeeds.
                </Text>
                <Button
                  variant="secondary"
                  size="sm"
                  disabled={impact.isFetching}
                  onClick={() => void impact.refetch()}
                >
                  {impact.isFetching ? "Retrying..." : "Retry"}
                </Button>
              </div>
            </Alert>
          ) : (
            impact.data && (
              <Alert
                variant={affected > 0 ? "warning" : "info"}
                dismissible={false}
              >
                {affected > 0
                  ? `${affected} hosted MCP server${affected === 1 ? "" : "s"} will retain a dual or private-only mode. Private-only servers will remain unavailable until an admin explicitly restores public-only access.`
                  : "No hosted MCP servers currently use a non-public network mode."}
              </Alert>
            )
          )}
          {ingress.endpointNamespaceKind === "custom_domain" && (
            <Text small muted>
              This ingress is pinned to the current custom-domain endpoint
              namespace. Removing it does not change the custom domain.
            </Text>
          )}
        </div>
        <Dialog.Footer>
          <Button
            variant="secondary"
            onClick={() => onOpenChange(false)}
            disabled={remove.isPending}
          >
            Cancel
          </Button>
          <Button
            variant="destructive-primary"
            disabled={remove.isPending || impact.isFetching || impact.isError}
            onClick={() =>
              remove.mutate({ security: { sessionHeaderGramSession: "" } })
            }
          >
            {remove.isPending ? "Removing..." : "Remove private network"}
          </Button>
        </Dialog.Footer>
      </Dialog.Content>
    </Dialog>
  );
}
