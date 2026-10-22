import { Alert } from "@/components/ui/Alert";
import { Button } from "@/components/ui/Button";
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from "@/components/ui/Collapsible";
import { Input } from "@/components/ui/Input";
import { Label } from "@/components/ui/Label";
import { RadioCard, RadioCardGroup } from "@/components/ui/RadioCard";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/Select";
import { Text } from "@/components/ui/Text";
import { cn } from "@/lib/utils";
import { ChevronDown, KeyRound, Settings } from "lucide-react";
import { Link } from "react-router";
import { toast } from "sonner";
import type { AgentCredentialFields } from "../drafts/useCredentialDraft";
import type { ServiceAccountClientDraft } from "../drafts/useServiceAccountClientDraft";
import {
  isOAuthFormat,
  keySetChoiceReady,
  oauthFormatOf,
  type AssertionAudience,
  type SecretMethod,
  type ServiceAccountFormat,
  type ServiceAccountOAuthFormat,
} from "../model/serviceAccount";
import { AutoHeight } from "./AutoHeight";
import { AvailabilityBadge } from "./AvailabilityBadge";
import { ClientKeyRegistration } from "./ClientKeyRegistration";
import { StaticCredentialFields } from "./CredentialFields";
import { IssuerLogo } from "./IssuerLogo";
import { KeySetPicker } from "./KeySetPicker";
import { ScopeMultiSelect } from "./ScopeMultiSelect";
import { SigningKeySourceStep } from "./SigningKeySourceStep";
import { StepMarker } from "./StepMarker";

/**
 * The Service Account choice: a static credential sent as the Authorization
 * header, or an OAuth client that gets its own token from the identity
 * provider and shares it with every caller.
 */
export function ServiceAccountPanel({
  format,
  onFormatChange,
  agentDraft,
  clientDraft,
  upstreamName,
  disabled,
  customerManagedKeys,
  providersHref,
  clientHref,
}: {
  format: ServiceAccountFormat;
  onFormatChange: (format: ServiceAccountFormat) => void;
  agentDraft: AgentCredentialFields;
  clientDraft: ServiceAccountClientDraft;
  upstreamName: string;
  disabled: boolean;
  /** The organization may sign with its own KMS keys. */
  customerManagedKeys: boolean;
  providersHref: string;
  clientHref: (issuerId: string, clientId: string) => string;
}): JSX.Element {
  const choose = (next: string) => {
    const picked = next as ServiceAccountFormat;
    if (!isOAuthFormat(picked)) agentDraft.setFormat(picked);
    onFormatChange(picked);
  };
  // A saved client is the answer, so the choice that led to it steps aside
  // until the operator clears it to pick again.
  const showingSaved =
    isOAuthFormat(format) && clientDraft.showingBound(format);

  return (
    <div className="flex flex-wrap items-start gap-x-8 gap-y-6">
      {showingSaved ? null : (
        <div className="flex max-w-[280px] min-w-[240px] flex-1 flex-col gap-4">
          <FormatGroup
            label="Static"
            value={format}
            onChange={choose}
            disabled={disabled}
            options={[
              {
                value: "bearer",
                title: "Bearer",
                description: "A token sent after the Bearer prefix.",
              },
              {
                value: "basic",
                title: "Basic",
                description: "A username and password.",
              },
              {
                value: "manual",
                title: "Manual",
                description: "Any header value you type.",
              },
            ]}
          />
          <FormatGroup
            label="OAuth"
            value={format}
            onChange={choose}
            disabled={disabled}
            options={[
              {
                value: "client-secret",
                title: "Client Secret",
                description:
                  "Paste a client ID and secret from the authorization server.",
              },
              {
                value: "signing-key",
                title: "Signing Client",
                description:
                  "Speakeasy signs its token requests with a key in your KMS.",
                unavailable: customerManagedKeys ? undefined : "Requires KMS",
              },
            ]}
          />
        </div>
      )}

      <div className="flex min-w-0 flex-[999_1_480px] flex-col gap-4">
        {isOAuthFormat(format) ? (
          <OAuthClientFields
            format={format}
            draft={clientDraft}
            disabled={disabled}
            providersHref={providersHref}
            clientHref={clientHref}
          />
        ) : (
          <StaticCredentialFields
            draft={agentDraft}
            disabled={disabled}
            upstreamName={upstreamName}
          />
        )}
      </div>
    </div>
  );
}

function FormatGroup({
  label,
  value,
  onChange,
  disabled,
  options,
}: {
  label: string;
  value: ServiceAccountFormat;
  onChange: (value: string) => void;
  disabled: boolean;
  options: {
    value: ServiceAccountFormat;
    title: string;
    description: string;
    unavailable?: string;
  }[];
}): JSX.Element {
  // Both groups are one choice, so each shows nothing selected while the
  // other holds it.
  const inGroup = options.some((option) => option.value === value);
  return (
    <div className="flex flex-col gap-2">
      <span className="text-eyebrow">{label}</span>
      <RadioCardGroup
        aria-label={`${label} credential`}
        size="sm"
        showIndicator={false}
        value={inGroup ? value : null}
        onValueChange={onChange}
        disabled={disabled}
      >
        {options.map((option) => (
          <RadioCard
            key={option.value}
            value={option.value}
            disabled={!!option.unavailable}
            title={
              <span className="flex flex-wrap items-center gap-2">
                {option.title}
                {option.unavailable ? (
                  <AvailabilityBadge>{option.unavailable}</AvailabilityBadge>
                ) : null}
              </span>
            }
          >
            {option.description}
          </RadioCard>
        ))}
      </RadioCardGroup>
    </div>
  );
}

function OAuthClientFields({
  format,
  draft,
  disabled,
  providersHref,
  clientHref,
}: {
  format: ServiceAccountOAuthFormat;
  draft: ServiceAccountClientDraft;
  disabled: boolean;
  providersHref: string;
  clientHref: (issuerId: string, clientId: string) => string;
}): JSX.Element {
  const bound = draft.showingBound(format) ? draft.boundClient : undefined;
  const providerName = draft.issuer?.name ?? "the authorization server";
  // The client being created reads the provider and these fields, so they
  // hold still until the request settles.
  const locked =
    disabled ||
    draft.saving ||
    draft.creating ||
    (format === "signing-key" && draft.created !== null);
  // Everything after the provider depends on it (its name, token endpoint and
  // scopes), so nothing else shows until one that can issue a token is picked.
  const providerChosen = !!bound || draft.issuerUsable;

  return (
    <>
      {bound?.upstreamRejectedAt ? (
        <Alert variant="error" dismissible={false}>
          {providerName} stopped accepting this client&apos;s credentials on{" "}
          {bound.upstreamRejectedAt.toLocaleString()}. Callers get an error that
          names you as the server&apos;s administrator until you enter a new
          secret or key. If the app requires DPoP, turn that off in{" "}
          {providerName}.
        </Alert>
      ) : null}

      <ProviderRow
        draft={draft}
        disabled={locked || !!bound}
        providersHref={providersHref}
        settingsHref={
          bound ? clientHref(bound.remoteSessionIssuerId, bound.id) : undefined
        }
        onClear={bound && !disabled ? draft.replace : undefined}
      />

      <AutoHeight className="flex flex-col gap-4">
        {providerChosen ? (
          <>
            {bound ? (
              <BoundClientSummary draft={draft} />
            ) : format === "client-secret" ? (
              <ClientSecretFields
                draft={draft}
                disabled={disabled || draft.saving}
              />
            ) : (
              <SigningKeySteps
                draft={draft}
                disabled={disabled}
                providerName={providerName}
              />
            )}

            {bound ? null : (
              <AdvancedFields format={format} draft={draft} disabled={locked} />
            )}
          </>
        ) : null}
      </AutoHeight>
    </>
  );
}

function ProviderRow({
  draft,
  disabled,
  providersHref,
  settingsHref,
  onClear,
}: {
  draft: ServiceAccountClientDraft;
  disabled: boolean;
  providersHref: string;
  settingsHref: string | undefined;
  /** Starts over with a new client; the saved one stays until Save. */
  onClear?: () => void;
}): JSX.Element {
  const issuer = draft.issuer;
  return (
    <div className="flex flex-col gap-1.5">
      <div className="flex flex-wrap items-center justify-between gap-4">
        <div className="flex min-w-0 items-center gap-3">
          <span className="bg-card flex size-10 shrink-0 items-center justify-center border">
            {issuer ? (
              <IssuerLogo
                logoAssetId={issuer.logoAssetId}
                name={issuer.name ?? issuer.issuer}
                size="md"
              />
            ) : (
              <KeyRound aria-hidden="true" className="size-4" />
            )}
          </span>
          <div className="flex min-w-0 flex-col gap-0.5">
            <Select
              value={issuer?.id ?? ""}
              onValueChange={draft.pickIssuer}
              disabled={disabled || draft.issuersLoading}
            >
              <SelectTrigger
                aria-label="Authorization server"
                className="h-auto w-auto gap-1.5 border-0 p-0 text-base font-medium shadow-none"
              >
                <SelectValue
                  placeholder={
                    draft.issuersLoading
                      ? "Loading authorization servers…"
                      : "Choose an authorization server"
                  }
                />
              </SelectTrigger>
              <SelectContent>
                {draft.issuers.map((candidate) => (
                  <SelectItem
                    key={candidate.id}
                    value={candidate.id}
                    disabled={!candidate.tokenEndpoint}
                  >
                    {candidate.name}
                    {candidate.tokenEndpoint ? "" : " · no token endpoint"}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
            {issuer ? (
              <span className="text-muted-foreground truncate font-mono text-xs">
                {issuer.issuer}
              </span>
            ) : null}
          </div>
        </div>
        {settingsHref || onClear ? (
          <div className="flex items-center gap-1">
            {settingsHref ? (
              <Button variant="tertiary" size="sm" asChild>
                <Link to={settingsHref}>
                  <Button.LeftIcon>
                    <Settings />
                  </Button.LeftIcon>
                  <Button.Text>Advanced</Button.Text>
                </Link>
              </Button>
            ) : null}
            {onClear ? (
              <Button variant="tertiary" size="sm" onClick={onClear}>
                <Button.Text>Clear</Button.Text>
              </Button>
            ) : null}
          </div>
        ) : null}
      </div>
      {!issuer && !draft.issuersLoading ? (
        <Text muted small>
          A service account needs the authorization server&apos;s token
          endpoint. Missing one?{" "}
          <Link
            to={providersHref}
            className="hover:text-foreground underline underline-offset-2"
          >
            Add an authorization server
          </Link>
          .
        </Text>
      ) : null}
    </div>
  );
}

function BoundClientSummary({
  draft,
}: {
  draft: ServiceAccountClientDraft;
}): JSX.Element | null {
  const bound = draft.boundClient;
  if (!bound) return null;
  const signing = oauthFormatOf(bound) === "signing-key";
  return (
    <div className="flex flex-col gap-4">
      <div className="flex flex-col gap-1">
        <Text muted small>
          Client ID
        </Text>
        <span className="font-mono text-sm break-all">{bound.clientId}</span>
      </div>
      {signing && bound.jsonWebKeySetId ? (
        <ClientKeyRegistration
          clientRowId={bound.id}
          keySetId={bound.jsonWebKeySetId}
        />
      ) : null}
    </div>
  );
}

function ClientSecretFields({
  draft,
  disabled,
}: {
  draft: ServiceAccountClientDraft;
  disabled: boolean;
}): JSX.Element {
  const providerName = draft.issuer?.name ?? "the authorization server";
  return (
    <div className="flex flex-col gap-4">
      <div className="flex flex-col gap-1.5">
        <Label htmlFor="sa-client-id">Client ID</Label>
        <Input
          id="sa-client-id"
          value={draft.clientId}
          onChange={draft.setClientId}
          placeholder={`Paste the client ID from ${providerName}`}
          className="font-mono"
          disabled={disabled}
          noAutofill
        />
      </div>
      <div className="flex flex-col gap-1.5">
        <Label htmlFor="sa-client-secret">Client secret</Label>
        <Input
          id="sa-client-secret"
          value={draft.clientSecret}
          onChange={draft.setClientSecret}
          reveal
          placeholder="Paste the client secret"
          disabled={disabled}
          noAutofill
        />
        <Text muted small>
          Encrypted at rest and never shown again.
        </Text>
      </div>
    </div>
  );
}

function SigningKeySteps({
  draft,
  disabled,
  providerName,
}: {
  draft: ServiceAccountClientDraft;
  disabled: boolean;
  providerName: string;
}): JSX.Element {
  const created = draft.created;
  const create = () => {
    draft.createSigningClient().catch((error: unknown) => {
      toast.error(
        error instanceof Error ? error.message : "Couldn't create the client",
      );
    });
  };

  return (
    <div className="flex flex-col gap-3">
      <ol aria-label="Signing key setup" className="bg-card border">
        <li className="flex gap-3 border-b px-4 py-3.5">
          <StepMarker index={1} done={false} />
          <div className="flex min-w-0 flex-1 flex-col gap-2">
            <Label htmlFor="sa-key-client-id">
              Create an app in {providerName} and paste its client ID
            </Label>
            <Input
              id="sa-key-client-id"
              value={draft.clientId}
              onChange={draft.setClientId}
              placeholder={`Paste the client ID from ${providerName}`}
              className="font-mono"
              disabled={disabled || draft.creating || created !== null}
              noAutofill
            />
          </div>
        </li>
        <li className="border-b px-4 py-3.5">
          <SigningKeySourceStep
            title="Choose the signing key"
            marker={<StepMarker index={2} done={created !== null} />}
            value={draft.keySource}
            onChange={draft.setKeySource}
            disabled={disabled || created !== null}
          >
            {created ? (
              <span className="text-success-foreground flex items-center gap-2 text-[13px]">
                <span className="bg-success size-2 rounded-full" />
                Client created. Its signing key is published.
              </span>
            ) : (
              <>
                <KeySetPicker
                  value={draft.keySet}
                  onChange={draft.setKeySet}
                  suggestedSetId={draft.suggestedKeySetId}
                  suggestionLoading={draft.suggestionLoading}
                  newSetPlaceholder={`${providerName} signing key`}
                  disabled={disabled || draft.creating}
                />
                {/* Only once there is a key to create it with, so an empty key
                    list never shows a second, dead button beside its own. */}
                {keySetChoiceReady(draft.keySet) ? (
                  <div className="flex flex-wrap items-center gap-3">
                    <Button
                      onClick={create}
                      disabled={
                        disabled ||
                        draft.creating ||
                        !draft.issuerUsable ||
                        draft.clientId.trim() === ""
                      }
                    >
                      <Button.Text>
                        {draft.creating ? "Creating…" : "Create client"}
                      </Button.Text>
                    </Button>
                    {draft.clientId.trim() === "" ? (
                      <Text muted small>
                        Paste the client ID in step 1 first.
                      </Text>
                    ) : !draft.issuerUsable ? (
                      <Text muted small>
                        Choose an authorization server with a token endpoint
                        first.
                      </Text>
                    ) : null}
                  </div>
                ) : null}
              </>
            )}
          </SigningKeySourceStep>
        </li>
        <li
          className={cn(
            "flex gap-3 px-4 py-3.5",
            created === null && "opacity-50",
          )}
        >
          <StepMarker index={3} done={false} />
          <div className="flex min-w-0 flex-1 flex-col gap-2">
            <Text small className="font-medium">
              Add the key to {providerName}
            </Text>
            {created?.jsonWebKeySetId ? (
              <ClientKeyRegistration
                clientRowId={created.id}
                keySetId={created.jsonWebKeySetId}
              />
            ) : (
              <Text muted small>
                The key set URL appears here once the client exists.
              </Text>
            )}
          </div>
        </li>
      </ol>
      {created ? (
        <Text muted small>
          Save to use this client for the server.
        </Text>
      ) : null}
    </div>
  );
}

function AdvancedFields({
  format,
  draft,
  disabled,
}: {
  format: ServiceAccountOAuthFormat;
  draft: ServiceAccountClientDraft;
  disabled: boolean;
}): JSX.Element {
  return (
    <Collapsible className="border">
      <CollapsibleTrigger className="group hover:bg-muted/40 flex w-full items-center gap-2 px-3 py-2.5 text-left">
        <ChevronDown
          aria-hidden="true"
          className="size-3.5 transition-transform group-data-[state=open]:rotate-180"
        />
        <Text small>Advanced</Text>
        <Text muted small>
          Scopes, audience and how credentials are sent
        </Text>
      </CollapsibleTrigger>
      <CollapsibleContent className="flex flex-col gap-3.5 border-t px-3 pt-3 pb-3.5">
        <div className="flex flex-col gap-1.5">
          <Label id="sa-scopes-label" htmlFor="sa-scopes">
            Scopes
          </Label>
          <ScopeMultiSelect
            id="sa-scopes"
            labelId="sa-scopes-label"
            options={draft.issuer?.scopesSupported ?? []}
            value={draft.scopes}
            onValueChange={draft.setScopes}
            placeholder="Add a scope…"
            disabled={disabled}
          />
          <Text muted small>
            Requested each time the client asks for a token.
          </Text>
        </div>
        <div className="flex flex-col gap-1.5">
          <Label htmlFor="sa-audience">Audience (optional)</Label>
          <Input
            id="sa-audience"
            value={draft.audience}
            onChange={draft.setAudience}
            placeholder="https://api.example.com"
            disabled={disabled}
          />
        </div>
        {format === "client-secret" ? (
          <div className="flex flex-col gap-1.5">
            <Label>Send the secret in</Label>
            <Select
              value={draft.secretMethod}
              onValueChange={(next) =>
                draft.setSecretMethod(next as SecretMethod)
              }
              disabled={disabled}
            >
              <SelectTrigger aria-label="Send the secret in">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="client_secret_basic">
                  Authorization header (default)
                </SelectItem>
                <SelectItem value="client_secret_post">Request body</SelectItem>
              </SelectContent>
            </Select>
          </div>
        ) : (
          <div className="flex flex-col gap-1.5">
            <Label>Client assertion audience</Label>
            <Select
              value={draft.assertionAudience}
              onValueChange={(next) =>
                draft.setAssertionAudience(next as AssertionAudience)
              }
              disabled={disabled}
            >
              <SelectTrigger aria-label="Client assertion audience">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="issuer">
                  Authorization server URL (default)
                </SelectItem>
                <SelectItem value="token_endpoint">
                  Token endpoint URL
                </SelectItem>
              </SelectContent>
            </Select>
          </div>
        )}
      </CollapsibleContent>
    </Collapsible>
  );
}
