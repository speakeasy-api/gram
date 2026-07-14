import { CopyButton } from "@/components/ui/copy-button";
import { Label } from "@/components/ui/label";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Type } from "@/components/ui/type";
import { getServerURL } from "@/lib/utils";
import { CreateRemoteSessionClientFormTokenEndpointAuthMethod } from "@gram/client/models/components/createremotesessionclientform.js";
import { Stack } from "@/components/ui/stack";
import { Button } from "@/components/ui/button";
import { Alert } from "@/components/ui/alert";
import { Input } from "@/components/ui/input";
import {
  CLIENT_TYPE_LABELS,
  clientTypeHelp,
  type ClientType,
} from "./issuerFormUtils";

// remoteLoginCallbackURL is the single stable redirect_uri Gram uses for
// every upstream OAuth provider, regardless of MCP server or slug (see
// canonicalCallbackRouteBase in server/internal/remotesessions/challenge.go).
// Manual clients need it registered on the upstream's app out-of-band; DCR
// and CIMD clients send/publish it automatically, so this only surfaces
// where the operator has to do that registration by hand.
function remoteLoginCallbackURL(): string {
  return `${getServerURL()}/mcp/remote_login_callback`;
}

// RedirectURICallout shows the redirect_uri operators must register on the
// upstream provider's OAuth app before typed-in client credentials will
// work. Rendered inside ClientCredentialsFields so both the Attach sheet's
// Manual add path and the Modify sheet's existing-client edit path show it.
function RedirectURICallout(): JSX.Element {
  const redirectURI = remoteLoginCallbackURL();
  return (
    <Stack gap={2}>
      <Label className="text-muted-foreground text-xs">Redirect URI</Label>
      <div className="bg-muted/50 p-4 font-mono text-sm">
        <div className="flex items-center justify-between gap-2">
          <code className="break-all">{redirectURI}</code>
          <CopyButton
            size="inline"
            text={redirectURI}
            tooltip="Copy redirect URI"
          />
        </div>
      </div>
      <Type muted small>
        Register this as the callback / redirect URI on the upstream provider's
        OAuth app before pasting credentials below.
      </Type>
    </Stack>
  );
}

// Shared form-field components used by both AttachRemoteIdentityProviderSheet
// and ModifyRemoteIdentityProviderSheet. The state lives in the parent sheet;
// these are pure presentation components. Type aliases and small helpers
// live in issuerFormUtils.ts so this module exports components only (react
// fast refresh requirement).

// IssuerUrlField is the single Issuer URL input. Split out from the Endpoints
// section so callers can render a Slug field between them.
export function IssuerUrlField({
  issuerUrl,
  onIssuerUrlChange,
}: {
  issuerUrl: string;
  onIssuerUrlChange: (value: string) => void;
}): JSX.Element {
  return (
    <Stack gap={2}>
      <Label className="text-muted-foreground text-xs">Issuer URL</Label>
      <Input
        value={issuerUrl}
        onChange={(e) => onIssuerUrlChange(e.target.value)}
        placeholder="https://login.example.com"
      />
      <Type muted small>
        Issuer URL of the upstream authorization server.
      </Type>
    </Stack>
  );
}

// EndpointsFields renders the Endpoints section: Discover/Reset slot, the
// four RFC 8414 endpoint inputs, and any discovery warnings. Pure
// presentation — discovery state and handlers stay in the parent sheet.
export function EndpointsFields({
  issuerUrl,
  authorizationEndpoint,
  tokenEndpoint,
  registrationEndpoint,
  jwksUri,
  endpointWarnings,
  discoverPending,
  discoverError,
  showDiscoverControls,
  showResetControls,
  onAuthorizationEndpointChange,
  onTokenEndpointChange,
  onRegistrationEndpointChange,
  onJwksUriChange,
  onDiscover,
  onResetEndpoints,
}: {
  issuerUrl: string;
  authorizationEndpoint: string;
  tokenEndpoint: string;
  registrationEndpoint: string;
  jwksUri: string;
  endpointWarnings: string[];
  discoverPending: boolean;
  discoverError: string | null;
  showDiscoverControls: boolean;
  showResetControls: boolean;
  onAuthorizationEndpointChange: (value: string) => void;
  onTokenEndpointChange: (value: string) => void;
  onRegistrationEndpointChange: (value: string) => void;
  onJwksUriChange: (value: string) => void;
  onDiscover: () => void;
  onResetEndpoints: () => void;
}): JSX.Element {
  return (
    <Stack gap={3} className="border-t pt-6">
      <Stack gap={1}>
        <Label className="text-sm font-medium">Endpoints</Label>
        {showDiscoverControls && (
          <Type muted small>
            Discover fetches the issuer's RFC 8414 metadata and fills these
            fields in. Edit anything that needs to be overridden.
          </Type>
        )}
        {showResetControls && (
          <Type muted small>
            Restore discovered values for modified endpoints.
          </Type>
        )}
      </Stack>

      {showDiscoverControls && (
        <div>
          <Button
            variant="secondary"
            disabled={!issuerUrl.trim() || discoverPending}
            onClick={onDiscover}
          >
            <Button.Text>
              {discoverPending ? "Discovering…" : "Discover"}
            </Button.Text>
          </Button>
        </div>
      )}

      {showResetControls && (
        <div>
          <Button variant="secondary" onClick={onResetEndpoints}>
            <Button.Text>Reset to Discovered Values</Button.Text>
          </Button>
        </div>
      )}

      {showDiscoverControls && discoverError && (
        <Alert variant="error" dismissible={false}>
          {discoverError}
        </Alert>
      )}

      <Stack gap={2}>
        <Label className="text-muted-foreground text-xs">
          Authorization Endpoint
        </Label>
        <Input
          value={authorizationEndpoint}
          onChange={(e) => onAuthorizationEndpointChange(e.target.value)}
          placeholder="https://login.example.com/authorize"
        />
      </Stack>

      <Stack gap={2}>
        <Label className="text-muted-foreground text-xs">Token Endpoint</Label>
        <Input
          value={tokenEndpoint}
          onChange={(e) => onTokenEndpointChange(e.target.value)}
          placeholder="https://login.example.com/token"
        />
      </Stack>

      <Stack gap={2}>
        <Label className="text-muted-foreground text-xs">
          Registration Endpoint (optional)
        </Label>
        <Input
          value={registrationEndpoint}
          onChange={(e) => onRegistrationEndpointChange(e.target.value)}
          placeholder="https://login.example.com/register"
        />
      </Stack>

      <Stack gap={2}>
        <Label className="text-muted-foreground text-xs">
          JWKS URI (optional)
        </Label>
        <Input
          value={jwksUri}
          onChange={(e) => onJwksUriChange(e.target.value)}
          placeholder="https://login.example.com/.well-known/jwks.json"
        />
      </Stack>

      {endpointWarnings.length > 0 && (
        <Alert variant="warning" dismissible={false}>
          <Stack gap={1}>
            {endpointWarnings.map((warning) => (
              <span key={warning}>{warning}</span>
            ))}
          </Stack>
        </Alert>
      )}
    </Stack>
  );
}

// TokenEndpointAuthMethodField is the standalone Select for the upstream
// token endpoint auth method. It renders for the DCR client type (so operators
// can override the upstream-assigned default) and inside the manual
// ClientCredentialsFields. Extracted so the two paths stay in lockstep on the
// enum values and placeholder copy.
export function TokenEndpointAuthMethodField({
  value,
  onChange,
}: {
  value: CreateRemoteSessionClientFormTokenEndpointAuthMethod | "";
  onChange: (
    value: CreateRemoteSessionClientFormTokenEndpointAuthMethod | "",
  ) => void;
}): JSX.Element {
  return (
    <Stack gap={2}>
      <Label className="text-muted-foreground text-xs">
        Token Endpoint Auth Method
      </Label>
      <Select
        value={value}
        onValueChange={(next) =>
          onChange(next as CreateRemoteSessionClientFormTokenEndpointAuthMethod)
        }
      >
        <SelectTrigger>
          <SelectValue placeholder="client_secret_basic (default)" />
        </SelectTrigger>
        <SelectContent>
          <SelectItem
            value={
              CreateRemoteSessionClientFormTokenEndpointAuthMethod.ClientSecretBasic
            }
          >
            client_secret_basic
          </SelectItem>
          <SelectItem
            value={
              CreateRemoteSessionClientFormTokenEndpointAuthMethod.ClientSecretPost
            }
          >
            client_secret_post
          </SelectItem>
          <SelectItem
            value={CreateRemoteSessionClientFormTokenEndpointAuthMethod.None}
          >
            none
          </SelectItem>
        </SelectContent>
      </Select>
    </Stack>
  );
}

// ClientCredentialsFields is used by both Add (editable client_id) and
// Modify (client_id read-only — the API has no rotate path; create a fresh
// remote_session_client if you need a new one). clientSecret stays an input
// in both — empty means "leave existing in place" in Modify. Callers that
// render their own section header (e.g. above a client-type selector) pass
// showHeading={false} to suppress the built-in "OAuth Client Credentials" block.
export function ClientCredentialsFields({
  clientId,
  clientSecret,
  tokenEndpointAuthMethod,
  clientIdEditable = true,
  clientSecretLabel = "Client Secret (optional)",
  clientSecretPlaceholder = "••••••••",
  showHeading = true,
  onClientIdChange,
  onClientSecretChange,
  onTokenEndpointAuthMethodChange,
}: {
  clientId: string;
  clientSecret: string;
  tokenEndpointAuthMethod:
    | CreateRemoteSessionClientFormTokenEndpointAuthMethod
    | "";
  clientIdEditable?: boolean;
  clientSecretLabel?: string;
  clientSecretPlaceholder?: string;
  showHeading?: boolean;
  onClientIdChange: (value: string) => void;
  onClientSecretChange: (value: string) => void;
  onTokenEndpointAuthMethodChange: (
    value: CreateRemoteSessionClientFormTokenEndpointAuthMethod | "",
  ) => void;
}): JSX.Element {
  return (
    <Stack gap={4} className={showHeading ? "border-t pt-6" : undefined}>
      {showHeading && (
        <Stack gap={1}>
          <Label className="text-sm font-medium">
            OAuth Client Credentials
          </Label>
          <Type muted small>
            The platform acts as an OAuth client against the upstream issuer.
            Register a client with the issuer out-of-band and paste the
            credentials here.
          </Type>
        </Stack>
      )}

      <RedirectURICallout />

      <Stack gap={2}>
        <Label className="text-muted-foreground text-xs">Client ID</Label>
        {clientIdEditable ? (
          <Input
            value={clientId}
            onChange={(e) => onClientIdChange(e.target.value)}
            placeholder="client_abc123"
          />
        ) : (
          <Type small mono className="break-all">
            {clientId || "—"}
          </Type>
        )}
      </Stack>

      <Stack gap={2}>
        <Label className="text-muted-foreground text-xs">
          {clientSecretLabel}
        </Label>
        <Input
          type="password"
          value={clientSecret}
          onChange={(e) => onClientSecretChange(e.target.value)}
          placeholder={clientSecretPlaceholder}
        />
      </Stack>

      <TokenEndpointAuthMethodField
        value={tokenEndpointAuthMethod}
        onChange={onTokenEndpointAuthMethodChange}
      />
    </Stack>
  );
}

// OverridesFields renders the per-client OAuth dance overrides. Both fields
// are optional and apply in both DCR and manual modes — they control what
// Gram sends at authorize/token time, independent of how the client was
// registered.
export function OverridesFields({
  scopeOverride,
  audienceOverride,
  onScopeOverrideChange,
  onAudienceOverrideChange,
}: {
  scopeOverride: string;
  audienceOverride: string;
  onScopeOverrideChange: (value: string) => void;
  onAudienceOverrideChange: (value: string) => void;
}): JSX.Element {
  return (
    <Stack gap={4} className="border-t pt-6">
      <Stack gap={2}>
        <Label className="text-muted-foreground text-xs">
          Scope (override)
        </Label>
        <Input
          value={scopeOverride}
          onChange={(e) => onScopeOverrideChange(e.target.value)}
          placeholder="read, write, openid"
        />
        <Type muted small>
          Comma-separated. When provided, the platform requests these scopes
          during the OAuth dance; otherwise it falls back to the issuer's
          scopes_supported.
        </Type>
      </Stack>

      <Stack gap={2}>
        <Label className="text-muted-foreground text-xs">
          Audience (optional)
        </Label>
        <Input
          value={audienceOverride}
          onChange={(e) => onAudienceOverrideChange(e.target.value)}
          placeholder="https://api.example.com"
        />
        <Type muted small>
          When provided, the platform includes this audience in authorize and
          token requests (RFC 8707). Required by some providers (e.g. Auth0) to
          return JWT access tokens.
        </Type>
      </Stack>
    </Stack>
  );
}

// ClientTypeFields renders the Client Type selector (DCR / CIMD / Manual) and
// the credentials inputs the chosen type needs. Used by both the org-admin
// CreateRemoteSessionClientSheet and the Attach sheet so the two stay in
// lockstep on the available-types logic and per-type copy. The selector hides
// when only one type is available (nothing to choose). The owning sheet holds
// `clientType` so its submit handler can branch on it.
export function ClientTypeFields({
  availableTypes,
  clientType,
  onClientTypeChange,
  clientId,
  clientSecret,
  tokenEndpointAuthMethod,
  onClientIdChange,
  onClientSecretChange,
  onTokenEndpointAuthMethodChange,
}: {
  availableTypes: ClientType[];
  clientType: ClientType;
  onClientTypeChange: (value: ClientType) => void;
  clientId: string;
  clientSecret: string;
  tokenEndpointAuthMethod:
    | CreateRemoteSessionClientFormTokenEndpointAuthMethod
    | "";
  onClientIdChange: (value: string) => void;
  onClientSecretChange: (value: string) => void;
  onTokenEndpointAuthMethodChange: (
    value: CreateRemoteSessionClientFormTokenEndpointAuthMethod | "",
  ) => void;
}): JSX.Element {
  let credentials: JSX.Element | null;
  switch (clientType) {
    case "dcr":
      // DCR mints the client_id/client_secret at save time, so the only input
      // is the token endpoint auth method forwarded to the registration call.
      credentials = (
        <TokenEndpointAuthMethodField
          value={tokenEndpointAuthMethod}
          onChange={onTokenEndpointAuthMethodChange}
        />
      );
      break;
    case "cimd":
      // CIMD has no credentials to collect, and the selector help text above
      // already explains the hosted-document flow, so render nothing here.
      credentials = null;
      break;
    case "manual":
      credentials = (
        <ClientCredentialsFields
          showHeading={false}
          clientId={clientId}
          clientSecret={clientSecret}
          tokenEndpointAuthMethod={tokenEndpointAuthMethod}
          onClientIdChange={onClientIdChange}
          onClientSecretChange={onClientSecretChange}
          onTokenEndpointAuthMethodChange={onTokenEndpointAuthMethodChange}
        />
      );
  }

  return (
    <Stack gap={4}>
      <Stack gap={2}>
        {availableTypes.length > 1 && (
          <>
            <Label className="text-muted-foreground text-xs">Client Type</Label>
            <Select
              value={clientType}
              onValueChange={(value) => onClientTypeChange(value as ClientType)}
            >
              <SelectTrigger>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {availableTypes.map((type) => (
                  <SelectItem key={type} value={type}>
                    {CLIENT_TYPE_LABELS[type]}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </>
        )}
        <Type muted small>
          {clientTypeHelp(clientType, availableTypes)}
        </Type>
      </Stack>
      {credentials}
    </Stack>
  );
}
