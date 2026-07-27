import { InfoField, InfoSection, InfoText } from "@/components/detail-fields";
import { Type } from "@/components/ui/type";
import { HumanizeDateTime } from "@/lib/dates";
import type { GcpIamCredential } from "@gram/client/models/components/gcpiamcredential.js";
import type { VerifyPlatformCredentialResult } from "@gram/client/models/components/verifyplatformcredentialresult.js";
import { useVerifyGcpIamPlatformCredentialMutation } from "@gram/client/react-query/verifyGcpIamPlatformCredential";
import { Alert, Button } from "@speakeasy-api/moonshine";
import { useState } from "react";
import { gcpAuthMode, providerLabel, verifySourceLabel } from "../providers";

export function OverviewTab({
  credential,
}: {
  credential: GcpIamCredential;
}): JSX.Element {
  const [result, setResult] = useState<VerifyPlatformCredentialResult | null>(
    null,
  );

  const verify = useVerifyGcpIamPlatformCredentialMutation({
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
    <div className="flex max-w-2xl flex-col gap-8">
      <InfoSection title="Credential">
        <InfoField label="Name">
          <InfoText>{credential.name}</InfoText>
        </InfoField>
        <InfoField label="External Service">
          <InfoText>{providerLabel(credential.provider)}</InfoText>
        </InfoField>
        <InfoField label="Authentication">
          <InfoText>{gcpAuthMode(credential)}</InfoText>
        </InfoField>
        {credential.impersonateServiceAccount && (
          <InfoField label="Impersonated service account">
            <InfoText mono>{credential.impersonateServiceAccount}</InfoText>
          </InfoField>
        )}
        {credential.wifPoolId && (
          <>
            <InfoField label="Workload Identity Federation pool ID">
              <InfoText mono>{credential.wifPoolId}</InfoText>
            </InfoField>
            <InfoField label="Workload Identity Federation provider ID">
              <InfoText mono>{credential.wifProviderId}</InfoText>
            </InfoField>
            <InfoField label="Workload Identity Federation project number">
              <InfoText mono>{credential.wifProjectNumber}</InfoText>
            </InfoField>
          </>
        )}
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
      </InfoSection>

      <InfoSection title="Verify">
        <Type small muted>
          Run a credential identity probe and report the effective principal.
        </Type>
        <div>
          <Button onClick={handleVerify} disabled={verify.isPending}>
            <Button.Text>
              {verify.isPending ? "Verifying…" : "Verify identity"}
            </Button.Text>
          </Button>
        </div>
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

function VerifyResult({
  result,
}: {
  result: VerifyPlatformCredentialResult;
}): JSX.Element {
  return (
    <div className="border-border flex flex-col gap-1 rounded-md border p-4">
      <Type small className="font-medium">
        {result.verified ? "Verified" : "Not verified"}
      </Type>
      {result.principal && <Type small>Principal: {result.principal}</Type>}
      {result.identitySource && (
        <Type small muted>
          Resolved via {verifySourceLabel(result.identitySource)}.
        </Type>
      )}
      {result.detail && (
        <Type small muted>
          {result.detail}
        </Type>
      )}
    </div>
  );
}
