import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetFooter,
  SheetHeader,
  SheetTitle,
} from "@/components/ui/Sheet";

import { Button } from "@/components/ui/Button";
import { Label } from "@/components/ui/Label";
import { NetworkIngressCredentialsFields } from "./NetworkIngressCredentialsFields";
import { Switch } from "@/components/ui/Switch";
import { Text } from "@/components/ui/Text";
import { handleAPIError } from "@/lib/errors";
import { invalidateAllNetworkIngress } from "@gram/client/react-query/networkIngress.js";
import { toast } from "sonner";
import { useCreateNetworkIngressMutation } from "@gram/client/react-query/createNetworkIngress.js";
import { useQueryClient } from "@tanstack/react-query";
import { useState } from "react";

const TAILSCALE_POLICY_SNIPPET = `"tagOwners": {
  "tag:speakeasy": ["autogroup:admin"]
},
"autoApprovers": {
  "services": {
    "svc:speakeasy": ["tag:speakeasy"]
  }
},
"grants": [
  {
    "src": ["autogroup:member"],
    "dst": ["svc:speakeasy"],
    "ip": ["tcp:443"]
  }
]`;

export function PrivateNetworkSetupSheet({
  open,
  onOpenChange,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
}): JSX.Element {
  const queryClient = useQueryClient();
  const [clientId, setClientId] = useState("");
  const [clientSecret, setClientSecret] = useState("");
  const [identityRequired, setIdentityRequired] = useState(false);

  const resetCredentials = () => {
    setClientId("");
    setClientSecret("");
  };

  const create = useCreateNetworkIngressMutation({
    gcTime: 0,
    onSuccess: async () => {
      resetCredentials();
      setIdentityRequired(false);
      await invalidateAllNetworkIngress(queryClient);
      onOpenChange(false);
      toast.success("Private network setup started");
    },
    onError: (error) => {
      resetCredentials();
      handleAPIError(error, "Failed to start private network setup");
    },
    onSettled: () => {
      create.reset();
    },
  });

  const close = () => {
    if (create.isPending) return;
    resetCredentials();
    create.reset();
    onOpenChange(false);
  };

  const canSubmit =
    clientId.trim() !== "" && clientSecret !== "" && !create.isPending;

  return (
    <Sheet
      open={open}
      onOpenChange={(nextOpen) => {
        if (!nextOpen) close();
        else onOpenChange(true);
      }}
    >
      <SheetContent side="right" className="w-full overflow-y-auto sm:max-w-xl">
        <SheetHeader>
          <SheetTitle>Connect Tailscale</SheetTitle>
          <SheetDescription>
            Connect this organization to one Tailscale tailnet. Credentials are
            encrypted on submission and never returned.
          </SheetDescription>
        </SheetHeader>
        <div className="flex-1 space-y-6 overflow-y-auto px-4">
          <div className="space-y-2">
            <Text variant="subheading">1. Check the tailnet prerequisites</Text>
            <ul className="text-muted-foreground list-disc space-y-1 pl-5 text-sm">
              <li>MagicDNS is enabled.</li>
              <li>HTTPS certificates are enabled.</li>
              <li>
                Devices that will connect run Tailscale 1.86 or later, which
                Tailscale Services require.
              </li>
              <li>
                The policy owns a <code>tag:speakeasy</code> tag, auto-approves
                the <code>svc:speakeasy</code> service for it, and grants your
                users access to that service.
              </li>
            </ul>
            <pre className="overflow-x-auto border bg-muted/30 p-3 text-xs">
              <code>{TAILSCALE_POLICY_SNIPPET}</code>
            </pre>
            <Text small muted>
              Keep the tag dedicated to Speakeasy. Replace{" "}
              <code>autogroup:member</code> with the group that should reach
              your private MCP servers.
            </Text>
          </div>

          <div className="space-y-2">
            <Text variant="subheading">2. Create a Tailscale OAuth client</Text>
            <Text small muted>
              Grant Auth Keys, Devices Core, and Services write scopes, then
              apply the <code>tag:speakeasy</code> tag. Speakeasy uses it only
              to register its gateway nodes and define the service; it never
              leaves our control plane.
            </Text>
          </div>

          <NetworkIngressCredentialsFields
            idPrefix="tailscale"
            clientId={clientId}
            clientSecret={clientSecret}
            onClientIdChange={setClientId}
            onClientSecretChange={setClientSecret}
          />
          <div className="space-y-2">
            <Label>Private hostname</Label>
            <Text small>
              <code>speakeasy.&lt;your tailnet&gt;.ts.net</code>
            </Text>
            <Text small muted>
              One hostname serves the whole organization. MCP servers opt in
              separately and keep their own endpoint paths on the shared
              hostname.
            </Text>
          </div>
          <div className="flex items-start justify-between gap-6 border p-4">
            <div className="space-y-1">
              <Label id="identity-required-label">Require user identity</Label>
              <Text small muted>
                Denies tagged devices and service nodes because Tailscale does
                not attach a user identity to them.
              </Text>
            </div>
            <Switch
              aria-labelledby="identity-required-label"
              checked={identityRequired}
              onCheckedChange={setIdentityRequired}
            />
          </div>
        </div>
        <SheetFooter className="flex-row justify-end gap-2">
          <Button
            variant="secondary"
            onClick={close}
            disabled={create.isPending}
          >
            Cancel
          </Button>
          <Button
            disabled={!canSubmit}
            onClick={() =>
              create.mutate({
                security: { sessionHeaderGramSession: "" },
                request: {
                  createIngressRequestBody: {
                    provider: "tailscale",
                    oauthClientId: clientId.trim(),
                    oauthClientSecret: clientSecret,
                    identityRequired,
                  },
                },
              })
            }
          >
            {create.isPending ? "Connecting..." : "Connect Tailscale"}
          </Button>
        </SheetFooter>
      </SheetContent>
    </Sheet>
  );
}
