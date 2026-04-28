import { Dialog } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Type } from "@/components/ui/type";
import { useTelemetry } from "@/contexts/Telemetry";
import { Toolset } from "@/lib/toolTypes";
import {
  invalidateAllToolset,
  useUpdateOAuthProxyServerMutation,
} from "@gram/client/react-query";
import { Button, Stack } from "@speakeasy-api/moonshine";
import { useQueryClient } from "@tanstack/react-query";
import { useEffect, useState } from "react";

import { parseScopes } from "./machine-types";

type ProxyServer = NonNullable<Toolset["oauthProxyServer"]>;

export function EditOAuthProxyModal({
  isOpen,
  onClose,
  toolsetSlug,
  proxyServer,
}: {
  isOpen: boolean;
  onClose: () => void;
  toolsetSlug: string;
  proxyServer: ProxyServer;
}) {
  // Parent keeps this modal mounted whenever a proxy exists, so without a
  // resetKey-based remount, useState initializers would only run on the very
  // first open and dirty form values would survive cancel-and-reopen cycles.
  const [resetKey, setResetKey] = useState(0);
  useEffect(() => {
    if (isOpen) return;
    const id = setTimeout(() => setResetKey((k) => k + 1), 200);
    return () => clearTimeout(id);
  }, [isOpen]);

  return (
    <Dialog open={isOpen} onOpenChange={onClose}>
      <Dialog.Content className="max-h-[90vh] max-w-6xl overflow-hidden">
        <EditOAuthProxyForm
          key={resetKey}
          onClose={onClose}
          toolsetSlug={toolsetSlug}
          proxyServer={proxyServer}
        />
      </Dialog.Content>
    </Dialog>
  );
}

function EditOAuthProxyForm({
  onClose,
  toolsetSlug,
  proxyServer,
}: {
  onClose: () => void;
  toolsetSlug: string;
  proxyServer: ProxyServer;
}) {
  const queryClient = useQueryClient();
  const telemetry = useTelemetry();
  const provider = proxyServer.oauthProxyProviders?.[0];
  const initialAudience = proxyServer.audience ?? "";

  const [authorizationEndpoint, setAuthorizationEndpoint] = useState(
    provider?.authorizationEndpoint ?? "",
  );
  const [tokenEndpoint, setTokenEndpoint] = useState(
    provider?.tokenEndpoint ?? "",
  );
  const [scopes, setScopes] = useState(
    (provider?.scopesSupported ?? []).join(", "),
  );
  const [audience, setAudience] = useState(initialAudience);
  const [tokenAuthMethod, setTokenAuthMethod] = useState(
    provider?.tokenEndpointAuthMethodsSupported?.[0] ?? "client_secret_post",
  );
  const [environmentSlug, setEnvironmentSlug] = useState(
    provider?.environmentSlug ?? "",
  );
  const [error, setError] = useState<string | null>(null);

  const updateMutation = useUpdateOAuthProxyServerMutation({
    onSuccess: () => {
      invalidateAllToolset(queryClient);
      telemetry.capture("mcp_event", {
        action: "oauth_proxy_updated",
        slug: toolsetSlug,
      });
      onClose();
    },
    onError: (err) => {
      setError(
        err instanceof Error ? err.message : "Failed to update OAuth proxy",
      );
    },
  });

  const handleSubmit = () => {
    if (!authorizationEndpoint.trim()) {
      setError("Authorization endpoint is required");
      return;
    }
    if (!tokenEndpoint.trim()) {
      setError("Token endpoint is required");
      return;
    }
    const scopesArray = parseScopes(scopes);
    if (scopesArray.length === 0) {
      setError("At least one scope is required");
      return;
    }
    setError(null);

    const audienceChanged = audience !== initialAudience;

    updateMutation.mutate({
      request: {
        slug: toolsetSlug,
        updateOAuthProxyServerRequestBody: {
          oauthProxyServer: {
            audience: audienceChanged ? audience : undefined,
            authorizationEndpoint,
            tokenEndpoint,
            scopesSupported: scopesArray,
            tokenEndpointAuthMethodsSupported: [tokenAuthMethod],
            environmentSlug: environmentSlug || undefined,
          },
        },
      },
    });
  };

  const submitDisabled =
    updateMutation.isPending ||
    !authorizationEndpoint.trim() ||
    !tokenEndpoint.trim();

  return (
    <>
      <Dialog.Header>
        <Dialog.Title>Edit OAuth Proxy</Dialog.Title>
      </Dialog.Header>

      <div className="max-h-[60vh] space-y-4 overflow-auto">
        {error && <Type className="mb-4 text-sm text-red-500!">{error}</Type>}

        <Stack gap={4}>
          <div>
            <Type className="mb-2 font-medium">OAuth Proxy Server Slug</Type>
            <Input value={proxyServer.slug ?? ""} disabled />
          </div>

          <div>
            <Type className="mb-2 font-medium">Authorization Endpoint</Type>
            <Input
              placeholder="https://provider.com/oauth/authorize"
              value={authorizationEndpoint}
              onChange={(v: string) => setAuthorizationEndpoint(v)}
            />
          </div>

          <div>
            <Type className="mb-2 font-medium">Token Endpoint</Type>
            <Input
              placeholder="https://provider.com/oauth/token"
              value={tokenEndpoint}
              onChange={(v: string) => setTokenEndpoint(v)}
            />
          </div>

          <div>
            <Type className="mb-2 font-medium">Scopes (comma-separated)</Type>
            <Input
              placeholder="read, write, openid"
              value={scopes}
              onChange={(v: string) => setScopes(v)}
            />
          </div>

          <div>
            <Type className="mb-2 font-medium">Audience (optional)</Type>
            <Input
              placeholder="https://api.example.com"
              value={audience}
              onChange={(v: string) => setAudience(v)}
            />
            <Type muted small className="mt-1">
              The audience parameter sent to the upstream OAuth provider.
              Required by some providers (e.g. Auth0) to return JWT access
              tokens.
            </Type>
          </div>

          <div>
            <Type className="mb-2 font-medium">Token Endpoint Auth Method</Type>
            <select
              className="bg-background w-full rounded border px-3 py-2"
              value={tokenAuthMethod}
              onChange={(e) => setTokenAuthMethod(e.target.value)}
            >
              <option value="client_secret_post">client_secret_post</option>
              <option value="client_secret_basic">client_secret_basic</option>
              <option value="none">none</option>
            </select>
          </div>

          <div>
            <Type className="mb-2 font-medium">Environment Slug</Type>
            <Input
              value={environmentSlug}
              onChange={(v: string) => setEnvironmentSlug(v)}
            />
          </div>
        </Stack>
      </div>

      <Dialog.Footer className="flex justify-between">
        <Button variant="secondary" onClick={onClose}>
          Cancel
        </Button>
        <div className="ml-auto">
          <Button onClick={handleSubmit} disabled={submitDisabled}>
            {updateMutation.isPending ? "Saving..." : "Save changes"}
          </Button>
        </div>
      </Dialog.Footer>
    </>
  );
}
