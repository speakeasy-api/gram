import { Alert, AlertDescription, AlertTitle } from "@/components/ui/Alert";
import { Badge } from "@/components/ui/Badge";
import { Button } from "@/components/ui/Button";
import { Dialog } from "@/components/ui/Dialog";
import { Input } from "@/components/ui/Input";
import { Label } from "@/components/ui/Label";
import { Stack } from "@/components/ui/Stack";
import { TextArea } from "@/components/ui/Textarea";
import { Text } from "@/components/ui/Text";
import { ServerIcon, WaypointsIcon } from "lucide-react";
import type { ReactElement } from "react";

import { WizardContext } from "./machine";
import { validateProviderIssuerUrl } from "./externalOAuthMetadata";

const EXTERNAL_METADATA_PLACEHOLDER = `{
  "issuer": "https://your-oauth-server.com",
  "authorization_endpoint": "https://your-oauth-server.com/oauth/authorize",
  "registration_endpoint": "https://your-oauth-server.com/oauth/register",
  "token_endpoint": "https://your-oauth-server.com/oauth/token"
}`;

export function ExternalOAuthForm({
  hasMultipleOAuth2AuthCode,
  oauth2SecurityCount,
  onCancel,
}: {
  hasMultipleOAuth2AuthCode: boolean;
  oauth2SecurityCount: number;
  onCancel: () => void;
}): ReactElement {
  const state = WizardContext.useSelector((snapshot) => snapshot);
  const actorRef = WizardContext.useActorRef();
  const { external, error, initialPath } = state.context;
  const directEntry = initialPath === "external";
  const submitting = state.matches({ external: "submitting" });

  const back = () => {
    if (state.matches({ external: "source" }) && directEntry) onCancel();
    else actorRef.send({ type: "BACK" });
  };

  return (
    <>
      <div className="max-h-[65vh] overflow-y-auto px-6 py-4">
        {hasMultipleOAuth2AuthCode && (
          <Alert variant="warning" className="mb-4">
            <AlertTitle>Multiple OAuth schemes detected</AlertTitle>
            <AlertDescription>
              This toolset has {oauth2SecurityCount} OAuth schemes. External
              OAuth applies one authorization server to the toolset.
            </AlertDescription>
          </Alert>
        )}

        {state.matches({ external: "source" }) && (
          <Stack gap={4}>
            <Text muted small>
              Choose where MCP clients should discover authorization server
              metadata.
            </Text>
            <SourceCard
              title="Provider-hosted metadata"
              description="Clients discover metadata directly from your authorization server."
              onClick={() => actorRef.send({ type: "SELECT_PROVIDER_ISSUER" })}
              icon={ServerIcon}
              recommended
            />
            <SourceCard
              title="Gram-hosted metadata"
              description="Gram hosts metadata you provide for compatibility with existing deployments."
              onClick={() => actorRef.send({ type: "SELECT_GRAM_HOSTED" })}
              icon={WaypointsIcon}
            />
          </Stack>
        )}

        {(state.matches({ external: "providerIssuer" }) ||
          state.matches({ external: "verifying" })) && (
          <Stack gap={4}>
            <Stack gap={2}>
              <Label htmlFor="external-oauth-issuer">Issuer URL</Label>
              <Input
                id="external-oauth-issuer"
                placeholder="https://login.example.com"
                value={external.issuerUrl}
                onChange={(value) =>
                  actorRef.send({
                    type: "FIELD_EXTERNAL",
                    key: "issuerUrl",
                    value,
                  })
                }
                validate={(value) => validateProviderIssuerUrl(value) ?? true}
                autoFocus
              />
              <Text muted small>
                Enter the exact HTTPS issuer advertised by your provider.
              </Text>
            </Stack>
            {error && (
              <Alert variant="error">
                <AlertDescription>{error}</AlertDescription>
              </Alert>
            )}
            {state.matches({ external: "verifying" }) && (
              <Text muted>Verifying provider metadata...</Text>
            )}
          </Stack>
        )}

        {state.matches({ external: "review" }) && external.verifiedMetadata && (
          <Stack gap={4}>
            <Text muted small>
              Review the metadata discovered from your provider.
            </Text>
            <ReviewRow
              label="Issuer"
              value={external.verifiedMetadata.issuer}
            />
            <ReviewRow
              label="Authorization endpoint"
              value={
                external.verifiedMetadata.authorizationEndpoint ??
                "Not advertised"
              }
            />
            <ReviewRow
              label="Token endpoint"
              value={
                external.verifiedMetadata.tokenEndpoint ?? "Not advertised"
              }
            />
            <ReviewRow
              label="RFC 9207 support"
              value={
                external.verifiedMetadata
                  .authorizationResponseIssParameterSupported
                  ? "Supported"
                  : "Not advertised"
              }
            />
            {error && (
              <Alert variant="error">
                <AlertDescription>{error}</AlertDescription>
              </Alert>
            )}
          </Stack>
        )}

        {(state.matches({ external: "gramHosted" }) ||
          (submitting && !external.verifiedMetadata)) && (
          <Stack gap={4}>
            <Alert variant="warning">
              <AlertTitle>Compatibility mode</AlertTitle>
              <AlertDescription>
                Gram hosts authorization-server metadata in this mode. Modern
                clients may reject multi-origin OAuth configurations without
                issuer-bound responses.
              </AlertDescription>
            </Alert>
            <Stack gap={2}>
              <Label htmlFor="external-oauth-metadata">
                OAuth Metadata JSON
              </Label>
              <TextArea
                id="external-oauth-metadata"
                placeholder={EXTERNAL_METADATA_PLACEHOLDER}
                value={external.metadataJson}
                onChange={(value) =>
                  actorRef.send({
                    type: "FIELD_EXTERNAL",
                    key: "metadataJson",
                    value,
                  })
                }
                rows={12}
                className="font-mono text-sm"
              />
              {external.jsonError && (
                <Text className="text-destructive! text-sm">
                  {external.jsonError}
                </Text>
              )}
            </Stack>
          </Stack>
        )}
      </div>

      <Dialog.Footer className="flex justify-between">
        <Button variant="secondary" onClick={back}>
          Back
        </Button>
        {state.matches({ external: "providerIssuer" }) && (
          <Button
            onClick={() => actorRef.send({ type: "NEXT" })}
            disabled={!!validateProviderIssuerUrl(external.issuerUrl)}
          >
            Verify metadata
          </Button>
        )}
        {(state.matches({ external: "review" }) ||
          state.matches({ external: "gramHosted" })) && (
          <Button onClick={() => actorRef.send({ type: "SUBMIT" })}>
            Configure External OAuth
          </Button>
        )}
        {submitting && <Button disabled>Configuring...</Button>}
      </Dialog.Footer>
    </>
  );
}

function SourceCard({
  title,
  description,
  onClick,
  icon: Icon,
  recommended = false,
}: {
  title: string;
  description: string;
  onClick: () => void;
  icon: typeof ServerIcon;
  recommended?: boolean;
}) {
  return (
    <button
      type="button"
      className="border-border hover:border-primary hover:bg-muted/50 flex flex-col items-start gap-2 border p-6 text-left transition-colors"
      onClick={onClick}
    >
      <div className="flex items-center gap-2">
        <Icon className="text-muted-foreground w-5" />
        <Text className="font-medium">{title}</Text>
        {recommended && <Badge variant="information">Recommended</Badge>}
      </div>
      <Text muted small>
        {description}
      </Text>
    </button>
  );
}

function ReviewRow({ label, value }: { label: string; value: string }) {
  return (
    <div>
      <Text small className="font-medium">
        {label}
      </Text>
      <Text muted small>
        {value}
      </Text>
    </div>
  );
}
