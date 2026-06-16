import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Type } from "@/components/ui/type";
import { TokenEndpointAuthMethod } from "@gram/client/models/components";
import { Alert, Button, Stack } from "@speakeasy-api/moonshine";

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
        onChange={onIssuerUrlChange}
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
          onChange={onAuthorizationEndpointChange}
          placeholder="https://login.example.com/authorize"
        />
      </Stack>

      <Stack gap={2}>
        <Label className="text-muted-foreground text-xs">Token Endpoint</Label>
        <Input
          value={tokenEndpoint}
          onChange={onTokenEndpointChange}
          placeholder="https://login.example.com/token"
        />
      </Stack>

      <Stack gap={2}>
        <Label className="text-muted-foreground text-xs">
          Registration Endpoint (optional)
        </Label>
        <Input
          value={registrationEndpoint}
          onChange={onRegistrationEndpointChange}
          placeholder="https://login.example.com/register"
        />
      </Stack>

      <Stack gap={2}>
        <Label className="text-muted-foreground text-xs">
          JWKS URI (optional)
        </Label>
        <Input
          value={jwksUri}
          onChange={onJwksUriChange}
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

// DcrNotice replaces the OAuth Client Credentials form when the issuer
// advertises a registration_endpoint. In Add we still call proxy-register to
// obtain credentials; in Modify the existing client_id is kept and only
// scope/audience can drift. The token_endpoint_auth_method field stays
// editable in both DCR and manual modes — see TokenEndpointAuthMethodField.
export function DcrNotice({
  description,
}: {
  description?: string;
}): JSX.Element {
  return (
    <Stack gap={2} className="border-t pt-6">
      <Label className="text-sm font-medium">OAuth Client Credentials</Label>
      <Type muted small>
        {description ??
          "The issuer advertises a registration endpoint (RFC 7591), so the platform will automatically register a client on save."}
      </Type>
    </Stack>
  );
}

// TokenEndpointAuthMethodField is the standalone Select for the upstream
// token endpoint auth method. It renders in both DCR mode (alongside
// DcrNotice so operators can override the upstream-assigned default) and in
// the manual ClientCredentialsFields. Extracted so the two paths stay in
// lockstep on the enum values and placeholder copy.
export function TokenEndpointAuthMethodField({
  value,
  onChange,
}: {
  value: TokenEndpointAuthMethod | "";
  onChange: (value: TokenEndpointAuthMethod | "") => void;
}): JSX.Element {
  return (
    <Stack gap={2}>
      <Label className="text-muted-foreground text-xs">
        Token Endpoint Auth Method
      </Label>
      <Select
        value={value}
        onValueChange={(next) => onChange(next as TokenEndpointAuthMethod)}
      >
        <SelectTrigger>
          <SelectValue placeholder="client_secret_basic (default)" />
        </SelectTrigger>
        <SelectContent>
          <SelectItem value={TokenEndpointAuthMethod.ClientSecretBasic}>
            client_secret_basic
          </SelectItem>
          <SelectItem value={TokenEndpointAuthMethod.ClientSecretPost}>
            client_secret_post
          </SelectItem>
          <SelectItem value={TokenEndpointAuthMethod.None}>none</SelectItem>
        </SelectContent>
      </Select>
    </Stack>
  );
}

// ClientCredentialsFields is used by both Add (editable client_id) and
// Modify (client_id read-only — the API has no rotate path; create a fresh
// remote_session_client if you need a new one). clientSecret stays an input
// in both — empty means "leave existing in place" in Modify.
export function ClientCredentialsFields({
  clientId,
  clientSecret,
  tokenEndpointAuthMethod,
  clientIdEditable = true,
  clientSecretLabel = "Client Secret (optional)",
  clientSecretPlaceholder = "••••••••",
  onClientIdChange,
  onClientSecretChange,
  onTokenEndpointAuthMethodChange,
}: {
  clientId: string;
  clientSecret: string;
  tokenEndpointAuthMethod: TokenEndpointAuthMethod | "";
  clientIdEditable?: boolean;
  clientSecretLabel?: string;
  clientSecretPlaceholder?: string;
  onClientIdChange: (value: string) => void;
  onClientSecretChange: (value: string) => void;
  onTokenEndpointAuthMethodChange: (
    value: TokenEndpointAuthMethod | "",
  ) => void;
}): JSX.Element {
  return (
    <Stack gap={4} className="border-t pt-6">
      <Stack gap={1}>
        <Label className="text-sm font-medium">OAuth Client Credentials</Label>
        <Type muted small>
          The platform acts as an OAuth client against the upstream issuer.
          Register a client with the issuer out-of-band and paste the
          credentials here.
        </Type>
      </Stack>

      <Stack gap={2}>
        <Label className="text-muted-foreground text-xs">Client ID</Label>
        {clientIdEditable ? (
          <Input
            value={clientId}
            onChange={onClientIdChange}
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
          onChange={onClientSecretChange}
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
          onChange={onScopeOverrideChange}
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
          onChange={onAudienceOverrideChange}
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
