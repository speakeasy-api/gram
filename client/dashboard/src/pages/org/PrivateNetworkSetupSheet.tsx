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
import { Alert } from "@/components/ui/Alert";
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
import { useFeatureFlag } from "@/hooks/useFeatureFlag";
import { FEATURE_FLAGS } from "@/lib/featureFlags";
import { invalidateAllNetworkIngress } from "@gram/client/react-query/networkIngress.js";
import type { NetworkIngress } from "@gram/client/models/components/networkingress.js";
import { toast } from "sonner";
import { useCreateNetworkIngressMutation } from "@gram/client/react-query/createNetworkIngress.js";
import { useQueryClient } from "@tanstack/react-query";
import { useState } from "react";

const DEFAULT_HOSTNAME = "speakeasy";

// Mirrors the server's rule: a lowercase DNS label of at most 63 characters.
const HOSTNAME_PATTERN = /^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$/;

// Production reserves this prefix for local development stacks and rejects it
// on create. A local stack accepts its own local-<id>- names, so development
// builds allow it.
const RESERVED_HOSTNAME_PREFIX = "local-";

// hostnameError explains why the API would reject hostname, or returns null.
function hostnameError(hostname: string): string | null {
  if (!HOSTNAME_PATTERN.test(hostname)) {
    return "Use lowercase letters, numbers, and hyphens, up to 63 characters.";
  }
  if (hostname.startsWith(RESERVED_HOSTNAME_PREFIX) && !import.meta.env.DEV) {
    return `Hostnames starting with "${RESERVED_HOSTNAME_PREFIX}" are reserved. Choose another name.`;
  }
  return null;
}

// ACCESS_GROUP is the example group the restricted snippet introduces.
const ACCESS_GROUP = "group:speakeasy-users";

// The policy an operator-hosted connection needs: the Kubernetes operator owns
// the proxy and service tags and auto-approves the service advertisements.
const OPERATOR_POLICY_SNIPPET = `"tagOwners": {
  "tag:k8s-operator": ["autogroup:admin"],
  "tag:gram-proxy": ["tag:k8s-operator"],
  "tag:gram-service": ["tag:k8s-operator"]
},
"autoApprovers": {
  "services": {
    "tag:gram-service": ["tag:gram-proxy"]
  }
}`;

function tailscalePolicySnippet(
  hostname: string,
  gatewayTag: string,
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
  "${gatewayTag}": ["autogroup:admin"]
},
"autoApprovers": {
  "services": {
    "svc:${hostname}": ["${gatewayTag}"]
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
  ingress,
  gatewayTag,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  // The organization's existing connection. With one, the sheet shows the
  // steps for how it is hosted instead of the form that creates one.
  ingress?: NetworkIngress;
  // The Tailscale tag this environment's gateway carries, which the policy
  // and OAuth client must use.
  gatewayTag: string;
}): JSX.Element {
  const queryClient = useQueryClient();
  const [hostname, setHostname] = useState(DEFAULT_HOSTNAME);
  const [clientId, setClientId] = useState("");
  const [clientSecret, setClientSecret] = useState("");
  const [identityRequired, setIdentityRequired] = useState(false);
  const [groupSnippetOpen, setGroupSnippetOpen] = useState(false);
  // The connection this sheet just created, when its steps differ from the
  // ones shown before it existed.
  const [created, setCreated] = useState<NetworkIngress | null>(null);
  const connection = ingress ?? created;
  // The server decides how a connection is hosted when it creates it. Before
  // one exists, the sheet predicts that from the same flag the server checks:
  // the tailnet gateway when it is on, the Tailscale operator otherwise,
  // including when the flag cannot be read.
  const gatewayFlag = useFeatureFlag(FEATURE_FLAGS.privateNetworkGateway);
  const flagLoading = !connection && gatewayFlag.status === "loading";
  const predictsGateway = gatewayFlag.status === "enabled";
  const usesGateway = connection
    ? connection.runtime === "gateway"
    : predictsGateway;

  const resetCredentials = () => {
    setClientId("");
    setClientSecret("");
  };

  const create = useCreateNetworkIngressMutation({
    gcTime: 0,
    onSuccess: async (result) => {
      resetCredentials();
      setHostname(DEFAULT_HOSTNAME);
      setIdentityRequired(false);
      await invalidateAllNetworkIngress(queryClient);
      toast.success("Private network setup started");
      if ((result.runtime === "gateway") === predictsGateway) {
        onOpenChange(false);
        return;
      }
      // The admin followed steps for the other runtime: keep the sheet open on
      // the steps this connection needs.
      setCreated(result);
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
    setCreated(null);
    onOpenChange(false);
  };

  const hostnameValid = hostnameError(hostname) === null;
  // The snippet always names a valid service, even mid-edit, and an existing
  // connection's real one.
  const serviceLabel =
    connection?.hostname ?? (hostnameValid ? hostname : DEFAULT_HOSTNAME);

  const canSubmit =
    hostnameValid &&
    clientId.trim() !== "" &&
    clientSecret !== "" &&
    !flagLoading &&
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
          <SheetTitle>
            {connection ? "Tailscale setup steps" : "Connect Tailscale"}
          </SheetTitle>
          <SheetDescription>
            {connection
              ? "Complete these steps in Tailscale so devices on your tailnet can reach your private MCP servers."
              : "Connect this organization to one Tailscale tailnet. Credentials are encrypted on submission and never returned."}
          </SheetDescription>
        </SheetHeader>
        <div className="flex-1 space-y-6 overflow-y-auto px-4">
          {created && (
            <Alert variant="warning" dismissible={false}>
              Your connection needs different Tailscale steps from the ones
              shown before it was created. Follow the steps below instead.
            </Alert>
          )}
          {connection ? (
            <div className="space-y-2">
              <Text variant="subheading">1. Your private hostname</Text>
              <Text small muted>
                Your private MCP servers are at{" "}
                <code>{serviceLabel}.&lt;your tailnet&gt;.ts.net</code>, each on
                its own path.
              </Text>
            </div>
          ) : (
            <div className="space-y-2">
              <Text variant="subheading">1. Choose a private hostname</Text>
              <Input
                id="private-hostname"
                aria-label="Private hostname"
                value={hostname}
                onChange={(value) => setHostname(value.trim().toLowerCase())}
                placeholder={DEFAULT_HOSTNAME}
                validate={(value) =>
                  hostnameError(value.trim().toLowerCase()) ?? true
                }
                autoComplete="off"
              />
              <Text small muted>
                Your private MCP servers will be at{" "}
                <code>{serviceLabel}.&lt;your tailnet&gt;.ts.net</code>. One
                hostname serves the whole organization, and each MCP server
                keeps its own path on it. Pick a durable name that no device or
                service on your tailnet already uses. Changing it later moves
                every MCP server to a new address, so their clients would need
                to be reconfigured.
              </Text>
            </div>
          )}

          {flagLoading ? (
            <Text small muted>
              Loading the setup steps for your organization...
            </Text>
          ) : usesGateway ? (
            <>
              <div className="space-y-2">
                <Text variant="subheading">
                  2. Check the tailnet prerequisites
                </Text>
                <ul className="text-muted-foreground list-disc space-y-1 pl-5 text-sm">
                  <li>MagicDNS is enabled.</li>
                  <li>HTTPS certificates are enabled.</li>
                  <li>
                    Devices that will connect run Tailscale 1.86 or later, which
                    Tailscale Services require.
                  </li>
                  <li>
                    The policy owns a <code>{gatewayTag}</code> tag,
                    auto-approves the <code>svc:{serviceLabel}</code> service
                    for it, and grants your users access to that service.
                  </li>
                </ul>
                <pre className="overflow-x-auto border bg-muted/30 p-3 text-xs">
                  <code>
                    {tailscalePolicySnippet(serviceLabel, gatewayTag)}
                  </code>
                </pre>
                <Text small muted>
                  Keep the tag dedicated to Speakeasy. This grant lets every
                  member of your tailnet reach your private MCP servers.
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
                        {tailscalePolicySnippet(
                          serviceLabel,
                          gatewayTag,
                          ACCESS_GROUP,
                        )}
                      </code>
                    </pre>
                  </CollapsibleContent>
                </Collapsible>
              </div>

              <div className="space-y-2">
                <Text variant="subheading">
                  3. Create a Tailscale OAuth client
                </Text>
                <Text small muted>
                  In the Tailscale admin console, open{" "}
                  <TailscaleTrustCredentialsLink /> and create an OAuth client.
                  Grant Auth Keys, Devices Core, and Services write scopes, then
                  apply the <code>{gatewayTag}</code> tag. Speakeasy uses the
                  OAuth client only to register its gateway nodes and define the
                  service. The client never leaves Speakeasy&apos;s control
                  plane.
                </Text>
              </div>
            </>
          ) : (
            <>
              <div className="space-y-2">
                <Text variant="subheading">
                  2. Check the tailnet prerequisites
                </Text>
                <ul className="text-muted-foreground list-disc space-y-1 pl-5 text-sm">
                  <li>MagicDNS is enabled.</li>
                  <li>HTTPS certificates are enabled.</li>
                  <li>
                    The policy has isolated operator, proxy, and service tags
                    plus service-advertisement auto-approval.
                  </li>
                </ul>
                <pre className="overflow-x-auto border bg-muted/30 p-3 text-xs">
                  <code>{OPERATOR_POLICY_SNIPPET}</code>
                </pre>
                <Text small muted>
                  Keep these tags dedicated to Speakeasy. Your tailnet policy
                  must also allow the intended users to reach the generated
                  service tag.
                </Text>
              </div>

              <div className="space-y-2">
                <Text variant="subheading">
                  3. Create a Tailscale OAuth client
                </Text>
                <Text small muted>
                  In the Tailscale admin console, open{" "}
                  <TailscaleTrustCredentialsLink /> and create an OAuth client.
                  Grant Devices Core, Auth Keys, and Services write scopes, then
                  apply the <code>tag:k8s-operator</code> tag. Do not reuse a
                  broadly privileged client.
                </Text>
              </div>
            </>
          )}

          {!connection && (
            <>
              <NetworkIngressCredentialsFields
                idPrefix="tailscale"
                clientId={clientId}
                clientSecret={clientSecret}
                onClientIdChange={setClientId}
                onClientSecretChange={setClientSecret}
              />
              <div className="flex items-start justify-between gap-6 border p-4">
                <div className="space-y-1">
                  <Label id="identity-required-label">
                    Require user identity
                  </Label>
                  <Text small muted>
                    Denies tagged devices and service nodes because Tailscale
                    does not attach a user identity to them.
                  </Text>
                </div>
                <Switch
                  aria-labelledby="identity-required-label"
                  checked={identityRequired}
                  onCheckedChange={setIdentityRequired}
                />
              </div>
            </>
          )}
        </div>
        <SheetFooter className="flex-row justify-end gap-2">
          {connection ? (
            <Button onClick={close}>Done</Button>
          ) : (
            <>
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
            </>
          )}
        </SheetFooter>
      </SheetContent>
    </Sheet>
  );
}
