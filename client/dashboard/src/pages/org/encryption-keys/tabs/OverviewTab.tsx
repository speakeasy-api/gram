import { InfoField, InfoSection, InfoText } from "@/components/detail-fields";
import { RequireScope } from "@/components/require-scope";
import { Alert } from "@/components/ui/Alert";
import { Button } from "@/components/ui/Button";
import { CopyButton } from "@/components/ui/CopyButton";
import { Text } from "@/components/ui/Text";
import { HumanizeDateTime } from "@/lib/dates";
import { useOrgRoutes } from "@/routes";
import type { GcpKmsKey } from "@gram/client/models/components/gcpkmskey.js";
import type { VerifyKmsKeyResult } from "@gram/client/models/components/verifykmskeyresult.js";
import { useGetGcpIamCredential } from "@gram/client/react-query/getGcpIamCredential";
import { useVerifyGcpKmsKeyMutation } from "@gram/client/react-query/verifyGcpKmsKey";
import { useState } from "react";
import { Link } from "react-router";
import { providerLabel } from "../providers";

export function OverviewTab({
  externalKey,
}: {
  externalKey: GcpKmsKey;
}): JSX.Element {
  const [result, setResult] = useState<VerifyKmsKeyResult | null>(null);

  const verify = useVerifyGcpKmsKeyMutation({
    onSuccess: (data) => {
      setResult(data);
    },
    onError: (error) => {
      console.error("Verify external key failed", error);
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
      request: { id: externalKey.id },
    });
  };

  return (
    <div className="flex max-w-2xl flex-col gap-8">
      <InfoSection title="Key">
        <InfoField label="Name">
          <InfoText>{externalKey.name}</InfoText>
        </InfoField>
        <InfoField label="Provider">
          <InfoText>{providerLabel(externalKey.provider)}</InfoText>
        </InfoField>
        <InfoField label="Algorithm">
          <InfoText>{externalKey.algorithm}</InfoText>
        </InfoField>
        <InfoField label="Resource name">
          <span className="flex items-center gap-1">
            <InfoText mono>{externalKey.resourceName}</InfoText>
            <CopyButton
              size="xs"
              text={externalKey.resourceName}
              tooltip="Copy resource name"
            />
          </span>
        </InfoField>
        {externalKey.customerGrantReference && (
          <InfoField label="Granted identity">
            <InfoText mono>{externalKey.customerGrantReference}</InfoText>
          </InfoField>
        )}
        <InfoField label="Created">
          <InfoText>
            <HumanizeDateTime date={externalKey.createdAt} />
          </InfoText>
        </InfoField>
        <InfoField label="Updated">
          <InfoText>
            <HumanizeDateTime date={externalKey.updatedAt} />
          </InfoText>
        </InfoField>
      </InfoSection>

      <InfoSection title="Access">
        <Text small muted>
          Gram reaches this key by impersonating the service account named by
          the credential below. That service account needs the
          roles/cloudkms.signerVerifier role on the key.
        </Text>
        <BackingCredential credentialId={externalKey.externalCredentialId} />
      </InfoSection>

      <InfoSection title="Verify">
        <Text small muted>
          Check that Gram can reach this key and sign with it. Nothing is
          stored, and the check performs a real signing operation against your
          KMS each time.
        </Text>
        <RequireScope
          scope="org:admin"
          level="component"
          reason="Verifying a key requires an organization admin."
        >
          <div>
            <Button onClick={handleVerify} disabled={verify.isPending}>
              <Button.Text>
                {verify.isPending ? "Verifying…" : "Verify signing"}
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

// BackingCredential links to the credential that reaches this key. It resolves
// the name rather than showing the id, and stays useful when the credential has
// been deleted out from under the key — a state older rows can still be in, and
// the one thing that most needs explaining when verify starts failing.
function BackingCredential({
  credentialId,
}: {
  credentialId: string;
}): JSX.Element {
  const orgRoutes = useOrgRoutes();
  const {
    data: credential,
    isLoading,
    isError,
  } = useGetGcpIamCredential({ id: credentialId }, undefined, {
    throwOnError: false,
  });

  if (isLoading) {
    return (
      <InfoField label="External credential">
        <InfoText>Loading…</InfoText>
      </InfoField>
    );
  }

  // A credential the key names but that no longer reads back is the deleted
  // case, which verify reports as credential_deleted. Say so here rather than
  // rendering a link to a page that will bounce.
  if (isError || !credential) {
    return (
      <InfoField label="External credential">
        <InfoText>
          No longer available. This key cannot sign until it points at a live
          credential.
        </InfoText>
      </InfoField>
    );
  }

  return (
    <InfoField label="External credential">
      <Link
        to={orgRoutes.externalServices.credentialDetail.href(
          "gcp",
          credentialId,
        )}
        className="underline underline-offset-2"
      >
        <InfoText>{credential.name}</InfoText>
      </Link>
    </InfoField>
  );
}

// verifyHeadline turns the machine-readable outcome into the one line a reader
// needs. The server's detail carries the specifics underneath; this says what
// kind of problem it is, which is what decides whether the reader edits the key,
// fixes a grant in their cloud console, or simply tries again.
function verifyHeadline(result: VerifyKmsKeyResult): string {
  switch (result.probeOutcome) {
    case "verified":
      return "Gram signed with this key and verified the signature.";
    case "credential_deleted":
      return "The credential that reached this key has been deleted.";
    case "credential_unusable":
      return "Gram could not authenticate as the backing credential.";
    case "permission_denied":
      return "Gram reached your KMS but is not allowed to use this key.";
    case "key_not_found":
      return "No key version exists at this resource name.";
    case "key_unusable":
      return "The key version exists but cannot sign right now.";
    case "algorithm_mismatch":
      return "The key signs with a different algorithm than the one recorded.";
    case "invalid_resource_name":
      return "The stored resource name is not a valid key version.";
    case "unsupported_algorithm":
      return "This key's algorithm is not one Gram can publish.";
    case "signature_invalid":
      return "The key produced a signature that did not verify.";
    case "unavailable":
      return "The check could not complete. This one is worth retrying.";
    case "unexpected":
      return "The check failed for an unexpected reason.";
    default:
      // A server that has grown an outcome this build's SDK does not know
      // about. The detail below still explains it.
      return "The check did not succeed.";
  }
}

function VerifyResult({ result }: { result: VerifyKmsKeyResult }): JSX.Element {
  return (
    <div className="border-border flex flex-col gap-1 border p-4">
      <Text small className="font-medium">
        {result.verified ? "Verified" : "Not verified"}
      </Text>
      <Text small>{verifyHeadline(result)}</Text>
      {result.detail && (
        <Text small muted>
          {result.detail}
        </Text>
      )}
    </div>
  );
}
