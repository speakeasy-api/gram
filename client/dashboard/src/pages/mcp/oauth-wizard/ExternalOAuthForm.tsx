import { Alert, AlertDescription, AlertTitle } from "@/components/ui/Alert";
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from "@/components/ui/Collapsible";
import { Dialog } from "@/components/ui/Dialog";
import { Input } from "@/components/ui/Input";
import { Label } from "@/components/ui/Label";
import { Link } from "@/components/ui/Link";
import { TextArea } from "@/components/ui/Textarea";
import { Text } from "@/components/ui/Text";
import { useSdkClient } from "@/contexts/Sdk";
import { Button } from "@/components/ui/Button";
import { Stack } from "@/components/ui/Stack";
import { useMutation } from "@tanstack/react-query";
import { CheckCircle2, ChevronDown } from "lucide-react";
import { useEffect, useMemo, useRef, useState } from "react";

import { WizardContext } from "./machine";
import {
  metadataFromIssuerDraft,
  validateExternalMetadataJson,
  validateIssuerUrl,
} from "./externalOAuthMetadata";

const EXTERNAL_METADATA_PLACEHOLDER = `{
  "issuer": "https://your-oauth-server.com",
  "authorization_endpoint": "https://your-oauth-server.com/oauth/authorize",
  "registration_endpoint": "https://your-oauth-server.com/oauth/register",
  "token_endpoint": "https://your-oauth-server.com/oauth/token",
  "scopes_supported": ["read", "write"],
  "response_types_supported": ["code"],
  "grant_types_supported": ["authorization_code"],
  "token_endpoint_auth_methods_supported": [
    "client_secret_post"
  ],
  "code_challenge_methods_supported": [
    "S256"
  ]
}`;

type DiscoveryPurpose = "auto" | "test";

type DiscoveryFeedback = {
  kind: "error" | "fetched" | "verified";
  message: string;
  retryable?: boolean;
};

function issuerUrlsMatch(left: string, right: string): boolean {
  return left.trim().replace(/\/$/, "") === right.trim().replace(/\/$/, "");
}

export function ExternalOAuthForm({
  hasMultipleOAuth2AuthCode,
  oauth2SecurityCount,
  onCancel,
}: {
  hasMultipleOAuth2AuthCode: boolean;
  oauth2SecurityCount: number;
  onCancel: () => void;
}): JSX.Element {
  const client = useSdkClient();
  const actorRef = WizardContext.useActorRef();
  const send = actorRef.send.bind(actorRef);
  const external = WizardContext.useSelector((s) => s.context.external);
  const directEntry = WizardContext.useSelector(
    (s) => s.context.initialPath === "external",
  );
  const discovered = WizardContext.useSelector((s) => {
    const d = s.context.discovered;
    return d?.version === "2.1" ? d : null;
  });
  const submitting = WizardContext.useSelector((s) =>
    s.matches({ external: "submitting" }),
  );
  const [advancedOpen, setAdvancedOpen] = useState(false);
  const [feedback, setFeedback] = useState<DiscoveryFeedback | null>(null);

  const externalRef = useRef(external);
  externalRef.current = external;
  const issuerUrlRef = useRef(external.issuerUrl);
  issuerUrlRef.current = external.issuerUrl;
  const manualMetadataDirtyRef = useRef(false);
  const lastAutoDiscoveryRef = useRef(
    external.metadataJson ? external.issuerUrl.trim() : "",
  );

  const issuerError = validateIssuerUrl(external.issuerUrl);
  const metadataValidation = useMemo(
    () =>
      validateExternalMetadataJson(
        external.metadataJson,
        external.issuerUrl.trim(),
      ),
    [external.issuerUrl, external.metadataJson],
  );
  const discoveryMutation = useMutation({
    mutationFn: async ({
      issuerUrl,
      purpose,
      metadataJsonAtStart,
    }: {
      issuerUrl: string;
      purpose: DiscoveryPurpose;
      metadataJsonAtStart: string;
    }) => {
      const draft = await client.remoteSessionIssuers.fetchMetadata({
        fetchIssuerMetadataRequestBody: { issuer: issuerUrl },
      });
      const metadataJson = JSON.stringify(
        metadataFromIssuerDraft(draft),
        null,
        2,
      );
      return {
        issuerUrl,
        metadataJson,
        metadataJsonAtStart,
        purpose,
        discoveredIssuer: draft.issuer,
      };
    },
    onSuccess: ({
      issuerUrl,
      metadataJson,
      metadataJsonAtStart,
      purpose,
      discoveredIssuer,
    }) => {
      if (issuerUrlRef.current.trim() !== issuerUrl) return;
      if (!issuerUrlsMatch(discoveredIssuer, issuerUrl)) {
        setAdvancedOpen(true);
        setFeedback({
          kind: "error",
          message:
            "The discovered metadata issuer does not match the Issuer URL.",
        });
        return;
      }

      if (purpose === "auto") {
        if (
          manualMetadataDirtyRef.current ||
          externalRef.current.metadataJson !== metadataJsonAtStart
        )
          return;
        send({
          type: "FIELD_EXTERNAL",
          key: "metadataJson",
          value: metadataJson,
        });
        setFeedback({
          kind: "fetched",
          message:
            "OAuth metadata fetched automatically. Review it, then test the configuration before saving.",
        });
        return;
      }

      const current = externalRef.current;
      const validation = validateExternalMetadataJson(
        current.metadataJson,
        current.issuerUrl.trim(),
      );
      if (!validation.ok) {
        setAdvancedOpen(true);
        setFeedback({ kind: "error", message: validation.reason });
        return;
      }

      setFeedback({
        kind: "verified",
        message:
          "Issuer discovery succeeded. Configured endpoint URLs are syntactically valid; endpoint reachability was not tested.",
      });
    },
    onError: (error, { issuerUrl, purpose }) => {
      if (issuerUrlRef.current.trim() !== issuerUrl) return;
      if (purpose === "auto") lastAutoDiscoveryRef.current = "";
      setFeedback({
        kind: "error",
        retryable: purpose === "auto",
        message:
          error instanceof Error
            ? error.message
            : "Could not discover OAuth metadata from this issuer.",
      });
    },
  });

  const { mutate: discoverMetadata } = discoveryMutation;
  useEffect(() => {
    const issuerUrl = external.issuerUrl.trim();
    if (
      validateIssuerUrl(issuerUrl) ||
      issuerUrl === lastAutoDiscoveryRef.current
    )
      return;

    const timeout = window.setTimeout(() => {
      lastAutoDiscoveryRef.current = issuerUrl;
      discoverMetadata({
        issuerUrl,
        purpose: "auto",
        metadataJsonAtStart: externalRef.current.metadataJson,
      });
    }, 500);

    return () => window.clearTimeout(timeout);
  }, [discoverMetadata, external.issuerUrl]);

  useEffect(() => {
    if (external.metadataJson && !metadataValidation.ok) {
      setAdvancedOpen(true);
    }
  }, [external.metadataJson, metadataValidation.ok]);

  const clearVerification = () => {
    setFeedback(null);
  };

  const handleIssuerChange = (value: string) => {
    if (!issuerUrlsMatch(value, external.issuerUrl)) {
      manualMetadataDirtyRef.current = false;
    }
    clearVerification();
    send({ type: "FIELD_EXTERNAL", key: "issuerUrl", value });
  };

  const handleSlugChange = (value: string) => {
    send({ type: "FIELD_EXTERNAL", key: "slug", value });
  };

  const handleMetadataChange = (value: string) => {
    manualMetadataDirtyRef.current = true;
    clearVerification();
    send({ type: "FIELD_EXTERNAL", key: "metadataJson", value });
  };

  const handleTest = () => {
    if (issuerError) {
      setAdvancedOpen(true);
      setFeedback({
        kind: "error",
        message: issuerError,
      });
      return;
    }
    if (!metadataValidation.ok) {
      setAdvancedOpen(true);
      setFeedback({
        kind: "error",
        message: metadataValidation.reason,
      });
      return;
    }

    setFeedback(null);
    discoverMetadata({
      issuerUrl: external.issuerUrl.trim(),
      purpose: "test",
      metadataJsonAtStart: external.metadataJson,
    });
  };

  const handleRetryDiscovery = () => {
    const issuerUrl = external.issuerUrl.trim();
    if (validateIssuerUrl(issuerUrl)) return;

    setFeedback(null);
    lastAutoDiscoveryRef.current = issuerUrl;
    discoverMetadata({
      issuerUrl,
      purpose: "auto",
      metadataJsonAtStart: external.metadataJson,
    });
  };

  return (
    <>
      <div className="max-h-[60vh] space-y-4 overflow-auto">
        {hasMultipleOAuth2AuthCode && (
          <Alert variant="warning">
            <AlertTitle>Multiple OAuth2 security schemes detected</AlertTitle>
            <AlertDescription>
              This MCP server has {oauth2SecurityCount} OAuth2 security schemes.
              The applicable scheme can't be determined automatically.
              Double-check that the configuration below matches the scheme you
              intend to use before continuing.
            </AlertDescription>
          </Alert>
        )}
        {discovered && !external.prefilled && (
          <div className="border-border bg-muted/50 mb-4 flex items-start justify-between gap-4 border p-4">
            <div>
              <Text small className="font-medium">
                OAuth detected from {discovered.name}
              </Text>

              <Text muted small className="mt-1">
                We discovered OAuth {discovered.version} metadata from this
                server. You can use it to pre-fill the form below.
              </Text>
            </div>
            <Button
              size="sm"
              variant="secondary"
              onClick={() => send({ type: "APPLY_DISCOVERED" })}
            >
              Apply
            </Button>
          </div>
        )}
        {external.prefilled && (
          <div className="border-border bg-muted/50 mb-4 border p-4">
            <Text small className="font-medium">
              Pre-filled from detected OAuth metadata
            </Text>
            <Text muted small className="mt-1">
              This form has been pre-filled with information Speakeasy detected
              about this server's OAuth requirements. Please review carefully
              and refer to the MCP server or API's documentation to confirm
              these values are correct.
            </Text>
          </div>
        )}
        <div>
          <Text className="mb-2 font-medium">
            External OAuth Server Configuration
          </Text>
          <Text muted small className="mb-4">
            Configure your MCP server to use an external authorization server if
            your API fits the very specific MCP OAuth requirements.{" "}
            <Link
              href="https://docs.getgram.ai/host-mcp/adding-oauth#authorization-code"
              target="_blank"
            >
              Docs
            </Link>
          </Text>

          <Stack gap={4}>
            <Stack gap={2}>
              <Label htmlFor="external-oauth-issuer">Issuer URL</Label>
              <Input
                id="external-oauth-issuer"
                placeholder="https://login.example.com"
                value={external.issuerUrl}
                onChange={handleIssuerChange}
                validate={(value) => validateIssuerUrl(value) ?? true}
                autoFocus
              />
              <Text muted small>
                Metadata is fetched automatically from this authorization
                server.
              </Text>
            </Stack>

            <Stack gap={2}>
              <Label htmlFor="external-oauth-slug">OAuth Server Slug</Label>
              <Input
                id="external-oauth-slug"
                placeholder="my-oauth-server"
                value={external.slug}
                onChange={handleSlugChange}
                maxLength={40}
              />
            </Stack>

            {feedback?.kind === "error" && (
              <Alert variant="error">
                <AlertTitle>Configuration could not be verified</AlertTitle>
                <AlertDescription>
                  <Stack gap={2}>
                    <span>{feedback.message}</span>
                    {feedback.retryable && (
                      <div>
                        <Button
                          variant="secondary"
                          size="sm"
                          onClick={handleRetryDiscovery}
                          disabled={discoveryMutation.isPending}
                        >
                          <Button.Text>Retry Metadata Fetch</Button.Text>
                        </Button>
                      </div>
                    )}
                  </Stack>
                </AlertDescription>
              </Alert>
            )}
            {feedback?.kind === "fetched" && (
              <Alert variant="info">
                <AlertDescription>{feedback.message}</AlertDescription>
              </Alert>
            )}
            {feedback?.kind === "verified" && (
              <div className="border-success-softest bg-success-softest text-success-foreground flex items-start gap-2 border p-3 text-sm">
                <CheckCircle2 className="mt-0.5 h-4 w-4 shrink-0" />
                <span>{feedback.message}</span>
              </div>
            )}

            <Collapsible open={advancedOpen} onOpenChange={setAdvancedOpen}>
              <CollapsibleTrigger asChild>
                <Button variant="tertiary" size="sm">
                  <Button.Text>Advanced metadata</Button.Text>
                  <Button.RightIcon>
                    <ChevronDown
                      className={`h-4 w-4 transition-transform ${
                        advancedOpen ? "rotate-180" : ""
                      }`}
                    />
                  </Button.RightIcon>
                </Button>
              </CollapsibleTrigger>
              <CollapsibleContent className="pt-3">
                <Stack gap={2}>
                  <Label htmlFor="external-oauth-metadata">
                    OAuth Authorization Server Metadata
                  </Label>
                  <TextArea
                    id="external-oauth-metadata"
                    placeholder={EXTERNAL_METADATA_PLACEHOLDER}
                    value={external.metadataJson}
                    onChange={handleMetadataChange}
                    rows={12}
                    className="font-mono text-sm"
                  />
                  {!metadataValidation.ok && external.metadataJson && (
                    <Text className="text-destructive! text-sm">
                      {metadataValidation.reason}
                    </Text>
                  )}
                  {external.jsonError && (
                    <Text className="text-destructive! text-sm">
                      {external.jsonError}
                    </Text>
                  )}
                </Stack>
              </CollapsibleContent>
            </Collapsible>

            <div className="flex items-center justify-between gap-3">
              <Text muted small>
                Checks issuer discovery. Endpoint URL format is validated
                locally; endpoint reachability is not tested.
              </Text>
              <Button
                variant="secondary"
                onClick={handleTest}
                disabled={
                  discoveryMutation.isPending ||
                  !!issuerError ||
                  !metadataValidation.ok
                }
              >
                <Button.Text>
                  {discoveryMutation.isPending
                    ? "Testing…"
                    : "Test Configuration"}
                </Button.Text>
              </Button>
            </div>
          </Stack>
        </div>
      </div>

      <Dialog.Footer className="flex justify-between">
        <Button
          variant="secondary"
          onClick={() => (directEntry ? onCancel() : send({ type: "BACK" }))}
        >
          {directEntry ? "Cancel" : "Back"}
        </Button>
        <div className="ml-auto">
          <Button
            onClick={() => send({ type: "SUBMIT" })}
            disabled={
              submitting ||
              !external.slug.trim() ||
              !!issuerError ||
              !metadataValidation.ok
            }
          >
            {submitting ? "Configuring..." : "Configure External OAuth"}
          </Button>
        </div>
      </Dialog.Footer>
    </>
  );
}
