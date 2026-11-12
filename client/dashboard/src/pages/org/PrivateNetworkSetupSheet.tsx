import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetFooter,
  SheetHeader,
  SheetTitle,
} from "@/components/ui/Sheet";

import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from "@/components/ui/Collapsible";
import { Button } from "@/components/ui/Button";
import { Icon } from "@/components/ui/Icon";
import { Input } from "@/components/ui/Input";
import { Label } from "@/components/ui/Label";
import {
  NetworkIngressCredentialsFields,
  TailscaleTrustCredentialsLink,
} from "./NetworkIngressCredentialsFields";
import { Switch } from "@/components/ui/Switch";
import { Text } from "@/components/ui/Text";
import { cn } from "@/lib/utils";
import { handleAPIError } from "@/lib/errors";
import { invalidateAllNetworkIngress } from "@gram/client/react-query/networkIngress.js";
import { toast } from "sonner";
import { useCreateNetworkIngressMutation } from "@gram/client/react-query/createNetworkIngress.js";
import { useQueryClient } from "@tanstack/react-query";
import { useState } from "react";

const DEFAULT_HOSTNAME = "speakeasy";

// Mirrors the server's rule: a lowercase DNS label of at most 63 characters.
const HOSTNAME_PATTERN = /^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$/;

// ACCESS_GROUP is the example group the restricted snippet introduces.
const ACCESS_GROUP = "group:speakeasy-users";

function tailscalePolicySnippet(
  hostname: string,
  accessGroup?: string,
): string {
  const groups = accessGroup
    ? `"groups": {
  "${accessGroup}": ["alice@example.com"]
},
`
    : "";
  const source = accessGroup ?? "autogroup:member";
  return `${groups}"tagOwners": {
  "tag:speakeasy": ["autogroup:admin"]
},
"autoApprovers": {
  "services": {
    "svc:${hostname}": ["tag:speakeasy"]
  }
},
"grants": [
  {
    "src": ["${source}"],
    "dst": ["svc:${hostname}"],
    "ip": ["tcp:443"]
  }
]`;
}

export function PrivateNetworkSetupSheet({
  open,
  onOpenChange,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
}): JSX.Element {
  const queryClient = useQueryClient();
  const [hostname, setHostname] = useState(DEFAULT_HOSTNAME);
  const [clientId, setClientId] = useState("");
  const [clientSecret, setClientSecret] = useState("");
  const [identityRequired, setIdentityRequired] = useState(false);
  const [groupSnippetOpen, setGroupSnippetOpen] = useState(false);

  const resetCredentials = () => {
    setClientId("");
    setClientSecret("");
  };

  const create = useCreateNetworkIngressMutation({
    gcTime: 0,
    onSuccess: async () => {
      resetCredentials();
      setHostname(DEFAULT_HOSTNAME);
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

  const hostnameValid = HOSTNAME_PATTERN.test(hostname);
  // The snippet always names a valid service, even mid-edit.
  const serviceLabel = hostnameValid ? hostname : DEFAULT_HOSTNAME;

  const canSubmit =
    hostnameValid &&
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
            <Text variant="subheading">1. Choose a private hostname</Text>
            <Input
              id="private-hostname"
              aria-label="Private hostname"
              value={hostname}
              onChange={(value) => setHostname(value.trim().toLowerCase())}
              placeholder={DEFAULT_HOSTNAME}
              validate={(value) =>
                HOSTNAME_PATTERN.test(value.trim().toLowerCase()) ||
                "Use lowercase letters, numbers, and hyphens, up to 63 characters."
              }
              autoComplete="off"
            />
            <Text small muted>
              Your private MCP servers will be at{" "}
              <code>{serviceLabel}.&lt;your tailnet&gt;.ts.net</code>. One
              hostname serves the whole organization, and each MCP server keeps
              its own path on it. Pick a durable name that no device or service
              on your tailnet already uses.
            </Text>
          </div>

          <div className="space-y-2">
            <Text variant="subheading">2. Check the tailnet prerequisites</Text>
            <ul className="text-muted-foreground list-disc space-y-1 pl-5 text-sm">
              <li>MagicDNS is enabled.</li>
              <li>HTTPS certificates are enabled.</li>
              <li>
                Devices that will connect run Tailscale 1.86 or later, which
                Tailscale Services require.
              </li>
              <li>
                The policy owns a <code>tag:speakeasy</code> tag, auto-approves
                the <code>svc:{serviceLabel}</code> service for it, and grants
                your users access to that service.
              </li>
            </ul>
            <pre className="overflow-x-auto border bg-muted/30 p-3 text-xs">
              <code>{tailscalePolicySnippet(serviceLabel)}</code>
            </pre>
            <Text small muted>
              Keep the tag dedicated to Speakeasy. This grant lets every member
              of your tailnet reach your private MCP servers.
            </Text>
            <Collapsible
              open={groupSnippetOpen}
              onOpenChange={setGroupSnippetOpen}
            >
              <CollapsibleTrigger className="text-muted-foreground hover:text-default flex items-center gap-1 text-sm">
                <Icon
                  name="chevron-right"
                  className={cn(
                    "h-4 w-4 transition-transform",
                    groupSnippetOpen && "rotate-90",
                  )}
                />
                Limit access to a group
              </CollapsibleTrigger>
              <CollapsibleContent className="space-y-2 pt-2">
                <Text small muted>
                  Use this policy instead to let only the people in{" "}
                  <code>{ACCESS_GROUP}</code> reach the service. Replace the
                  example address with their logins.
                </Text>
                <pre className="overflow-x-auto border bg-muted/30 p-3 text-xs">
                  <code>
                    {tailscalePolicySnippet(serviceLabel, ACCESS_GROUP)}
                  </code>
                </pre>
              </CollapsibleContent>
            </Collapsible>
          </div>

          <div className="space-y-2">
            <Text variant="subheading">3. Create a Tailscale OAuth client</Text>
            <Text small muted>
              In the Tailscale admin console, open{" "}
              <TailscaleTrustCredentialsLink /> and create an OAuth client.
              Grant Auth Keys, Devices Core, and Services write scopes, then
              apply the <code>tag:speakeasy</code> tag. Speakeasy uses the OAuth
              client only to register its gateway nodes and define the service.
              The client never leaves Speakeasy&apos;s control plane.
            </Text>
          </div>

          <NetworkIngressCredentialsFields
            idPrefix="tailscale"
            clientId={clientId}
            clientSecret={clientSecret}
            onClientIdChange={setClientId}
            onClientSecretChange={setClientSecret}
          />
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
                    hostname,
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
