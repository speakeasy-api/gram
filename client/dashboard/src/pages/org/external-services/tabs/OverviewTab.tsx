import {
  InfoField,
  InfoFieldGrid,
  InfoSection,
  InfoText,
} from "@/components/detail-fields";
import { RequireScope } from "@/components/require-scope";
import { CopyButton } from "@/components/ui/CopyButton";
import { Text } from "@/components/ui/Text";
import { HumanizeDateTime } from "@/lib/dates";
import type { GcpIamCredential } from "@gram/client/models/components/gcpiamcredential.js";
import type { VerifyCredentialResult } from "@gram/client/models/components/verifycredentialresult.js";
import { useVerifyGcpIamCredentialMutation } from "@gram/client/react-query/verifyGcpIamCredential";
import { Alert } from "@/components/ui/Alert";
import { Button } from "@/components/ui/Button";
import { useState } from "react";
import { GcpGrantInstructions } from "../GcpGrantInstructions";
import { providerLabel } from "../providers";

export function OverviewTab({
  credential,
}: {
  credential: GcpIamCredential;
}): JSX.Element {
  const [result, setResult] = useState<VerifyCredentialResult | null>(null);

  const verify = useVerifyGcpIamCredentialMutation({
    onSuccess: (data) => {
      setResult(data);
    },
    onError: (error) => {
      console.error("Verify external credential failed", error);
    },
  });

  const verifyError = verify.error
    ? verify.error instanceof Error && verify.error.message
      ? verify.error.message
      : "An unexpected error occurred. Please try again."
    : null;

  const handleVerify = () => {
    // Verify is a live probe; drop any prior result so a failed re-run never
    // shows a stale "Verified" panel alongside the new error.
    setResult(null);
    verify.mutate({
      security: { sessionHeaderGramSession: "" },
      request: { id: credential.id },
    });
  };

  return (
    <div className="flex max-w-4xl flex-col gap-8">
      <InfoSection title="Credential">
        <InfoFieldGrid columns={2}>
          <InfoField label="Name">
            <InfoText>{credential.name}</InfoText>
          </InfoField>
          <InfoField label="External Service">
            <InfoText>{providerLabel(credential.provider)}</InfoText>
          </InfoField>
        </InfoFieldGrid>
        {/* Full width: a service account address is long enough that a narrow
            column would wrap it into several lines of mono text. */}
        {credential.impersonateServiceAccount && (
          <InfoField label="Impersonated service account">
            <span className="flex items-center gap-1">
              <InfoText mono>{credential.impersonateServiceAccount}</InfoText>
              <CopyButton
                size="xs"
                text={credential.impersonateServiceAccount}
                tooltip="Copy service account"
              />
            </span>
          </InfoField>
        )}
        {/* Workload Identity Federation is no longer accepted here, but a
            credential created before that rule may still carry it, so it stays
            visible read-only rather than silently disappearing. */}
        {credential.wifPoolId && (
          <InfoFieldGrid>
            <InfoField label="Workload Identity Federation pool ID">
              <InfoText mono>{credential.wifPoolId}</InfoText>
            </InfoField>
            <InfoField label="Workload Identity Federation provider ID">
              <InfoText mono>{credential.wifProviderId}</InfoText>
            </InfoField>
            <InfoField label="Workload Identity Federation project number">
              <InfoText mono>{credential.wifProjectNumber}</InfoText>
            </InfoField>
          </InfoFieldGrid>
        )}
        <InfoFieldGrid>
          <InfoField label="Created">
            <InfoText>
              <HumanizeDateTime date={credential.createdAt} />
            </InfoText>
          </InfoField>
          <InfoField label="Updated">
            <InfoText>
              <HumanizeDateTime date={credential.updatedAt} />
            </InfoText>
          </InfoField>
        </InfoFieldGrid>
      </InfoSection>

      <InfoSection title="Access">
        <GcpGrantInstructions />
      </InfoSection>

      <InfoSection title="Verify">
        <Text small muted>
          Check that Speakeasy can still impersonate this service account.
          Nothing is stored; the check runs against your project each time.
        </Text>
        <RequireScope
          scope="org:admin"
          level="component"
          reason="Verifying a credential requires an organization admin."
        >
          <div>
            <Button onClick={handleVerify} disabled={verify.isPending}>
              <Button.Text>
                {verify.isPending ? "Verifying…" : "Verify access"}
              </Button.Text>
            </Button>
          </div>
        </RequireScope>
        {verifyError && (
          <Alert variant="error" dismissible={false}>
            {verifyError}
          </Alert>
        )}
        {result && <VerifyResult result={result} />}
      </InfoSection>
    </div>
  );
}

function verifyMessage(result: VerifyCredentialResult): string {
  if (result.verified) {
    return `Speakeasy can impersonate ${result.principal}.`;
  }
  return `Speakeasy could not impersonate ${result.principal}.`;
}

function VerifyResult({
  result,
}: {
  result: VerifyCredentialResult;
}): JSX.Element {
  return (
    <div className="border-border flex flex-col gap-1 border p-4">
      <Text small className="font-medium">
        {result.verified ? "Verified" : "Not verified"}
      </Text>
      {result.principal && <Text small>{verifyMessage(result)}</Text>}
      {result.detail && (
        <Text small muted>
          {result.detail}
        </Text>
      )}
    </div>
  );
}
