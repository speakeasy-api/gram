import { Alert } from "@/components/ui/Alert";
import { Button } from "@/components/ui/Button";
import { Input } from "@/components/ui/Input";
import { Label } from "@/components/ui/Label";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/Select";
import {
  Sheet,
  SheetContent,
  SheetFooter,
  SheetHeader,
  SheetTitle,
} from "@/components/ui/Sheet";
import { Stack } from "@/components/ui/Stack";
import { Text } from "@/components/ui/Text";
import { useSdkClient } from "@/contexts/Sdk";
import { useRBAC } from "@/hooks/useRBAC";
import { remoteSessionClientDisplayName } from "@/pages/remote-identity-providers/clientDisplay";
import { useOrgRoutes } from "@/routes";
import type { CreateRemoteSessionClientFormTokenEndpointAuthMethod } from "@gram/client/models/components/createremotesessionclientform.js";
import type { RemoteSessionIssuer } from "@gram/client/models/components/remotesessionissuer.js";
import { invalidateAllRemoteSessionClients } from "@gram/client/react-query/remoteSessionClients.js";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useEffect, useMemo, useState } from "react";
import { Link } from "react-router";
import { toast } from "sonner";
import type { AuthTarget } from "./authTarget";
import { availableClientTypes, parseScopes } from "./issuerFormUtils";
import { useAllRemoteSessionClients } from "./useAllRemoteSessionClients";

type RegistrationMode = "auto" | "manual" | `existing:${string}`;

export function ConfigureRemoteMcpUserIdentitySheet({
  open,
  onOpenChange,
  target,
  issuers,
  initialProviderId,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  target: AuthTarget;
  issuers: RemoteSessionIssuer[];
  initialProviderId?: string;
}): JSX.Element {
  const client = useSdkClient();
  const queryClient = useQueryClient();
  const orgRoutes = useOrgRoutes();
  const { hasScope, isLoading: rbacLoading } = useRBAC();
  const canWriteTarget =
    !rbacLoading && hasScope("mcp:write", target.resourceId);
  const canWriteProject =
    !rbacLoading && hasScope("project:write", target.projectId);
  const [providerId, setProviderId] = useState(initialProviderId ?? "");
  const [registrationMode, setRegistrationMode] =
    useState<RegistrationMode>("manual");
  const [clientId, setClientId] = useState("");
  const [clientSecret, setClientSecret] = useState("");
  const [tokenEndpointAuthMethod, setTokenEndpointAuthMethod] = useState<
    CreateRemoteSessionClientFormTokenEndpointAuthMethod | ""
  >("");
  const [scope, setScope] = useState("");
  const [audience, setAudience] = useState("");

  const provider = issuers.find((issuer) => issuer.id === providerId);
  const defaultProviderId = initialProviderId ?? issuers[0]?.id ?? "";
  const clientTypes = useMemo(
    () =>
      availableClientTypes({
        cimdAvailable: provider?.clientIdMetadataDocumentSupported ?? false,
        dcrAvailable: !!provider?.registrationEndpoint?.trim(),
      }),
    [provider],
  );
  const automaticType = clientTypes.find((type) => type !== "manual");
  const { items: providerClients, isLoading: clientsLoading } =
    useAllRemoteSessionClients(
      { remoteSessionIssuerId: providerId },
      { enabled: open && providerId !== "" },
    );
  const attachableClients = providerClients.filter(
    (candidate) =>
      !target.userSessionIssuerId ||
      !candidate.userSessionIssuerIds.includes(target.userSessionIssuerId),
  );

  useEffect(() => {
    if (!open) return;
    setProviderId(defaultProviderId);
    setClientId("");
    setClientSecret("");
    setTokenEndpointAuthMethod("");
    setScope("");
    setAudience("");
  }, [defaultProviderId, open]);

  useEffect(() => {
    setRegistrationMode(automaticType ? "auto" : "manual");
  }, [automaticType, providerId]);

  const configure = useMutation({
    mutationFn: async () => {
      const existingClientId = registrationMode.startsWith("existing:")
        ? registrationMode.slice("existing:".length)
        : undefined;
      let clientMode: "auto" | "existing" | "manual" =
        registrationMode === "auto" ? "auto" : "manual";
      if (existingClientId) clientMode = "existing";
      const scopes = parseScopes(scope);
      return await client.remoteSessions.commitServerUserIdentityConfiguration({
        commitServerUserIdentityConfigurationForm: {
          mcpServerId: target.resourceId,
          providerId,
          clientMode,
          existingClientId,
          clientConfiguration: existingClientId
            ? undefined
            : {
                clientId:
                  registrationMode === "manual" ? clientId.trim() : undefined,
                clientSecret:
                  registrationMode === "manual"
                    ? clientSecret.trim() || undefined
                    : undefined,
                tokenEndpointAuthMethod: tokenEndpointAuthMethod || undefined,
                scope: scopes.length > 0 ? scopes : undefined,
                audience: audience.trim() || undefined,
              },
        },
      });
    },
    onSuccess: async (result) => {
      if (result.manualSetupRequired) {
        setRegistrationMode("manual");
        return;
      }
      if (!result.status) return;
      await Promise.all([
        invalidateAllRemoteSessionClients(queryClient, { refetchType: "all" }),
        target.invalidate(queryClient),
      ]);
      toast.success("User Identity configured");
      onOpenChange(false);
    },
    onError: (error) => {
      toast.error(
        error instanceof Error
          ? error.message
          : "Failed to configure User Identity",
      );
    },
  });

  const createsClient = !registrationMode.startsWith("existing:");
  const submittable =
    canWriteTarget &&
    providerId !== "" &&
    (!createsClient || canWriteProject) &&
    (registrationMode !== "manual" || clientId.trim() !== "");

  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <SheetContent
        side="right"
        className="flex w-full flex-col sm:max-w-[560px]"
      >
        <SheetHeader className="px-6 pt-6 pb-0">
          <SheetTitle>Configure User Identity</SheetTitle>
        </SheetHeader>
        <div className="flex-1 space-y-6 overflow-y-auto px-6 py-6">
          <Stack gap={2}>
            <Label>Remote Identity Provider</Label>
            {issuers.length > 0 ? (
              <Select value={providerId} onValueChange={setProviderId}>
                <SelectTrigger
                  className="w-full"
                  aria-label="Remote Identity Provider"
                >
                  <SelectValue placeholder="Choose an identity provider…" />
                </SelectTrigger>
                <SelectContent>
                  {issuers.map((issuer) => (
                    <SelectItem
                      key={issuer.id}
                      value={issuer.id}
                      description={issuer.issuer}
                    >
                      {issuer.name?.trim() || issuer.slug}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            ) : (
              <Alert variant="warning" dismissible={false}>
                Create a provider in{" "}
                <Link
                  className="font-medium underline underline-offset-2"
                  to={orgRoutes.remoteIdentityProviders.href()}
                >
                  Remote Identity Providers
                </Link>{" "}
                before returning here.
              </Alert>
            )}
          </Stack>

          {provider ? (
            <Stack gap={4}>
              <Stack gap={2}>
                <Label>OAuth client</Label>
                <Select
                  value={registrationMode}
                  onValueChange={(value) =>
                    setRegistrationMode(value as RegistrationMode)
                  }
                >
                  <SelectTrigger className="w-full" aria-label="OAuth client">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    {automaticType ? (
                      <SelectItem value="auto" disabled={!canWriteProject}>
                        Automatic ({automaticType.toUpperCase()})
                      </SelectItem>
                    ) : null}
                    {attachableClients.map((candidate) => (
                      <SelectItem
                        key={candidate.id}
                        value={`existing:${candidate.id}`}
                        description={`${candidate.userSessionIssuerIds.length} connection${candidate.userSessionIssuerIds.length === 1 ? "" : "s"}`}
                      >
                        {remoteSessionClientDisplayName(candidate)}
                      </SelectItem>
                    ))}
                    <SelectItem value="manual" disabled={!canWriteProject}>
                      Manual credentials
                    </SelectItem>
                  </SelectContent>
                </Select>
                {clientsLoading ? (
                  <Text muted small>
                    Loading existing clients…
                  </Text>
                ) : null}
                {automaticType ? (
                  <Text muted small>
                    Automatic setup prefers CIMD, then falls back to DCR when
                    CIMD is unavailable.
                  </Text>
                ) : null}
              </Stack>

              {registrationMode === "manual" ? (
                <Stack gap={3}>
                  <Stack gap={1}>
                    <Label htmlFor="remote-mcp-client-id">Client ID</Label>
                    <Input
                      id="remote-mcp-client-id"
                      value={clientId}
                      onChange={setClientId}
                    />
                  </Stack>
                  <Stack gap={1}>
                    <Label htmlFor="remote-mcp-client-secret">
                      Client secret (optional)
                    </Label>
                    <Input
                      id="remote-mcp-client-secret"
                      type="password"
                      value={clientSecret}
                      onChange={setClientSecret}
                    />
                  </Stack>
                </Stack>
              ) : null}

              {!registrationMode.startsWith("existing:") ? (
                <Stack gap={3}>
                  <Stack gap={1}>
                    <Label htmlFor="remote-mcp-client-auth-method">
                      Token endpoint auth method (optional)
                    </Label>
                    <Select
                      value={tokenEndpointAuthMethod}
                      onValueChange={(value) =>
                        setTokenEndpointAuthMethod(
                          value as CreateRemoteSessionClientFormTokenEndpointAuthMethod,
                        )
                      }
                    >
                      <SelectTrigger
                        id="remote-mcp-client-auth-method"
                        aria-label="Token endpoint auth method"
                      >
                        <SelectValue placeholder="Use provider default" />
                      </SelectTrigger>
                      <SelectContent>
                        <SelectItem value="client_secret_basic">
                          client_secret_basic
                        </SelectItem>
                        <SelectItem value="client_secret_post">
                          client_secret_post
                        </SelectItem>
                        <SelectItem value="none">none</SelectItem>
                      </SelectContent>
                    </Select>
                  </Stack>
                  <Stack gap={1}>
                    <Label htmlFor="remote-mcp-client-scopes">
                      Scopes (comma-separated)
                    </Label>
                    <Input
                      id="remote-mcp-client-scopes"
                      value={scope}
                      onChange={setScope}
                    />
                  </Stack>
                  <Stack gap={1}>
                    <Label htmlFor="remote-mcp-client-audience">Audience</Label>
                    <Input
                      id="remote-mcp-client-audience"
                      value={audience}
                      onChange={setAudience}
                    />
                  </Stack>
                </Stack>
              ) : null}
            </Stack>
          ) : null}

          {configure.data?.failure ? (
            <Alert variant="error" dismissible={false}>
              {configure.data.failure.providerMessage ??
                `Automatic registration failed (${configure.data.failure.reason}).`}
            </Alert>
          ) : null}
          {configure.data?.manualSetupRequired ? (
            <Alert variant="warning" dismissible={false}>
              Automatic registration is unavailable. Enter OAuth client
              credentials registered with this provider.
            </Alert>
          ) : null}
          {!rbacLoading && !canWriteTarget ? (
            <Alert variant="warning" dismissible={false}>
              You need mcp:write on this MCP server to configure User Identity.
            </Alert>
          ) : null}
          {!rbacLoading && createsClient && !canWriteProject ? (
            <Alert variant="warning" dismissible={false}>
              Creating an OAuth client requires project:write. Choose an
              existing client or ask a project administrator.
            </Alert>
          ) : null}
        </div>
        <SheetFooter className="flex-row items-center justify-end gap-2 border-t px-6 py-4">
          <Button
            variant="secondary"
            disabled={configure.isPending}
            onClick={() => onOpenChange(false)}
          >
            <Button.Text>Cancel</Button.Text>
          </Button>
          <Button
            variant="primary"
            disabled={!submittable || configure.isPending}
            onClick={() => configure.mutate()}
          >
            <Button.Text>
              {configure.isPending ? "Configuring…" : "Configure User Identity"}
            </Button.Text>
          </Button>
        </SheetFooter>
      </SheetContent>
    </Sheet>
  );
}
