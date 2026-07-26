import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from "@/components/ui/collapsible";
import { Dialog } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Link } from "@/components/ui/link";
import { TextArea } from "@/components/ui/textarea";
import { Type } from "@/components/ui/type";
import { useSdkClient } from "@/contexts/Sdk";
import { Button, Stack } from "@speakeasy-api/moonshine";
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
};

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
  const [testedFingerprint, setTestedFingerprint] = useState<string | null>(
    null,
  );

  const externalRef = useRef(external);
  externalRef.current = external;
  const issuerUrlRef = useRef(external.issuerUrl);
  issuerUrlRef.current = external.issuerUrl;
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
  const currentFingerprint = `${external.issuerUrl.trim()}\n${external.metadataJson}`;
  const configurationVerified =
    testedFingerprint !== null && testedFingerprint === currentFingerprint;

  const discoveryMutation = useMutation({
    mutationFn: async ({
      issuerUrl,
      purpose,
    }: {
      issuerUrl: string;
      purpose: DiscoveryPurpose;
    }) => {
      const draft = await client.remoteSessionIssuers.discover({
        discoverRemoteSessionIssuerRequestBody: { issuer: issuerUrl },
      });
      const metadataJson = JSON.stringify(
        metadataFromIssuerDraft(issuerUrl, draft),
        null,
        2,
      );
      const validation = validateExternalMetadataJson(metadataJson, issuerUrl);
      if (!validation.ok) {
        throw new Error(validation.reason);
      }
      return { issuerUrl, metadataJson, purpose };
    },
    onSuccess: ({ issuerUrl, metadataJson, purpose }) => {
      if (issuerUrlRef.current.trim() !== issuerUrl) return;

      if (purpose === "auto") {
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

      setTestedFingerprint(
        `${current.issuerUrl.trim()}\n${current.metadataJson}`,
      );
      setFeedback({
        kind: "verified",
        message:
          "Configuration verified. The issuer is reachable and advertises the required OAuth and dynamic registration endpoints.",
      });
    },
    onError: (error) => {
      setTestedFingerprint(null);
      setFeedback({
        kind: "error",
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
      discoverMetadata({ issuerUrl, purpose: "auto" });
    }, 500);

    return () => window.clearTimeout(timeout);
  }, [discoverMetadata, external.issuerUrl]);

  useEffect(() => {
    if (external.metadataJson && !metadataValidation.ok) {
      setAdvancedOpen(true);
    }
  }, [external.metadataJson, metadataValidation.ok]);

  const clearVerification = () => {
    setTestedFingerprint(null);
    setFeedback(null);
  };

  const handleIssuerChange = (value: string) => {
    clearVerification();
    send({ type: "FIELD_EXTERNAL", key: "issuerUrl", value });
  };

  const handleSlugChange = (value: string) => {
    clearVerification();
    send({ type: "FIELD_EXTERNAL", key: "slug", value });
  };

  const handleMetadataChange = (value: string) => {
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
          <div className="border-border bg-muted/50 mb-4 flex items-start justify-between gap-4 rounded-md border p-4">
            <div>
              <Type small className="font-medium">
                OAuth detected from {discovered.name}
              </Type>

              <Type muted small className="mt-1">
                We discovered OAuth {discovered.version} metadata from this
                server. You can use it to pre-fill the form below.
              </Type>
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
          <div className="border-border bg-muted/50 mb-4 rounded-md border p-4">
            <Type small className="font-medium">
              Pre-filled from detected OAuth metadata
            </Type>
            <Type muted small className="mt-1">
              This form has been pre-filled with information Speakeasy detected
              about this server's OAuth requirements. Please review carefully
              and refer to the MCP server or API's documentation to confirm
              these values are correct.
            </Type>
          </div>
        )}
        <div>
          <Type className="mb-2 font-medium">
            External OAuth Server Configuration
          </Type>
          <Type muted small className="mb-4">
            Configure your MCP server to use an external authorization server if
            your API fits the very specific MCP OAuth requirements.{" "}
            <Link
              external
              to="https://docs.getgram.ai/host-mcp/adding-oauth#authorization-code"
            >
              Docs
            </Link>
          </Type>

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
              <Type muted small>
                Metadata is fetched automatically from this authorization
                server.
              </Type>
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
              <Alert variant="destructive">
                <AlertTitle>Configuration could not be verified</AlertTitle>
                <AlertDescription>{feedback.message}</AlertDescription>
              </Alert>
            )}
            {feedback?.kind === "fetched" && (
              <Alert variant="info">
                <AlertDescription>{feedback.message}</AlertDescription>
              </Alert>
            )}
            {feedback?.kind === "verified" && (
              <div className="border-success-softest bg-success-softest text-success-foreground flex items-start gap-2 rounded-lg border p-3 text-sm">
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
                    <Type className="text-destructive! text-sm">
                      {metadataValidation.reason}
                    </Type>
                  )}
                  {external.jsonError && (
                    <Type className="text-destructive! text-sm">
                      {external.jsonError}
                    </Type>
                  )}
                </Stack>
              </CollapsibleContent>
            </Collapsible>

            <div className="flex items-center justify-between gap-3">
              <Type muted small>
                Test the issuer connection before saving.
              </Type>
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
              submitting || !external.slug.trim() || !configurationVerified
            }
          >
            {submitting ? "Configuring..." : "Configure External OAuth"}
          </Button>
        </div>
      </Dialog.Footer>
    </>
  );
}
