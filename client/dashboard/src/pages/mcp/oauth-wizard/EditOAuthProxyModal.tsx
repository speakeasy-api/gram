import { Dialog } from "@/components/ui/dialog";
import { Field, FieldDescription, FieldLabel } from "@/components/ui/field";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { useTelemetry } from "@/contexts/Telemetry";
import { Toolset } from "@/lib/toolTypes";
import { invalidateAllToolset } from "@gram/client/react-query/toolset.js";
import { useUpdateOAuthProxyServerMutation } from "@gram/client/react-query/updateOAuthProxyServer.js";
import { Alert, Button, Input, Stack } from "@/components/ui/moonshine";
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
}): JSX.Element {
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
      void invalidateAllToolset(queryClient);
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
        {error && (
          <Alert variant="error" dismissible={false}>
            {error}
          </Alert>
        )}

        <Stack gap={4}>
          <Field>
            <FieldLabel>OAuth Proxy Server Slug</FieldLabel>
            <Input value={proxyServer.slug ?? ""} disabled />
          </Field>

          <Field>
            <FieldLabel>Authorization Endpoint</FieldLabel>
            <Input
              placeholder="https://provider.com/oauth/authorize"
              value={authorizationEndpoint}
              onChange={(e) => setAuthorizationEndpoint(e.target.value)}
            />
          </Field>

          <Field>
            <FieldLabel>Token Endpoint</FieldLabel>
            <Input
              placeholder="https://provider.com/oauth/token"
              value={tokenEndpoint}
              onChange={(e) => setTokenEndpoint(e.target.value)}
            />
          </Field>

          <Field>
            <FieldLabel>Scopes (comma-separated)</FieldLabel>
            <Input
              placeholder="read, write, openid"
              value={scopes}
              onChange={(e) => setScopes(e.target.value)}
            />
          </Field>

          <Field>
            <FieldLabel optional>Audience</FieldLabel>
            <Input
              placeholder="https://api.example.com"
              value={audience}
              onChange={(e) => setAudience(e.target.value)}
            />
            <FieldDescription>
              The audience parameter sent to the upstream OAuth provider.
              Required by some providers (e.g. Auth0) to return JWT access
              tokens.
            </FieldDescription>
          </Field>

          <Field>
            <FieldLabel>Token Endpoint Auth Method</FieldLabel>
            <Select value={tokenAuthMethod} onValueChange={setTokenAuthMethod}>
              <SelectTrigger className="w-full">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="client_secret_post">
                  client_secret_post
                </SelectItem>
                <SelectItem value="client_secret_basic">
                  client_secret_basic
                </SelectItem>
                <SelectItem value="none">none</SelectItem>
              </SelectContent>
            </Select>
          </Field>

          <Field>
            <FieldLabel>Environment Slug</FieldLabel>
            <Input
              value={environmentSlug}
              onChange={(e) => setEnvironmentSlug(e.target.value)}
            />
          </Field>
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
