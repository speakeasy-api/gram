import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetFooter,
  SheetHeader,
  SheetTitle,
} from "@/components/ui/Sheet";

import { Button } from "@/components/ui/Button";
import { Input } from "@/components/ui/Input";
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

const HOSTNAME_PATTERN = /^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$/;

const TAILSCALE_POLICY_SNIPPET = `"tagOwners": {
  "tag:k8s-operator": ["autogroup:admin"],
  "tag:gram-proxy": ["tag:k8s-operator"],
  "tag:gram-service": ["tag:k8s-operator"]
},
"autoApprovers": {
  "services": {
    "tag:gram-service": ["tag:gram-proxy"]
  }
}`;

export function PrivateNetworkSetupSheet({
  open,
  onOpenChange,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
}): JSX.Element {
  const queryClient = useQueryClient();
  const [hostname, setHostname] = useState("");
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
      create.reset();
      setHostname("");
      setIdentityRequired(false);
      onOpenChange(false);
      await invalidateAllNetworkIngress(queryClient);
      toast.success("Private network setup started");
    },
    onError: (error) => {
      handleAPIError(error, "Failed to start private network setup");
    },
    onSettled: () => {
      resetCredentials();
      create.reset();
    },
  });

  const close = () => {
    resetCredentials();
    create.reset();
    onOpenChange(false);
  };

  const canSubmit =
    HOSTNAME_PATTERN.test(hostname.toLowerCase()) &&
    clientId.trim() !== "" &&
    clientSecret !== "" &&
    !create.isPending;

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
                The policy has isolated operator, proxy, and service tags plus
                service-advertisement auto-approval.
              </li>
            </ul>
            <pre className="overflow-x-auto border bg-muted/30 p-3 text-xs">
              <code>{TAILSCALE_POLICY_SNIPPET}</code>
            </pre>
            <Text small muted>
              Keep these tags dedicated to Gram. Your tailnet policy must also
              allow the intended users to reach the generated service tag.
            </Text>
          </div>

          <div className="space-y-2">
            <Text variant="subheading">2. Create a Tailscale OAuth client</Text>
            <Text small muted>
              Grant Devices Core, Auth Keys, and Services write scopes, then
              apply the <code>tag:k8s-operator</code> tag. Do not reuse a
              broadly privileged client.
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
            <Label htmlFor="private-hostname">Private hostname label</Label>
            <Input
              id="private-hostname"
              value={hostname}
              onChange={(value) => setHostname(value.toLowerCase())}
              placeholder="acme-mcp"
              validate={(value) =>
                HOSTNAME_PATTERN.test(value.toLowerCase()) ||
                "Use a lowercase DNS label containing letters, numbers, or hyphens."
              }
              autoComplete="off"
            />
            <Text small muted>
              This label is immutable after setup. Tailscale supplies the
              complete <code>.ts.net</code> name.
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
                    hostname: hostname.toLowerCase(),
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
