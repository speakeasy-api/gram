import type { GcpIamCredential } from "@gram/client/models/components/gcpiamcredential.js";

// The external-service providers a platform admin can create today. GCP is the
// only one with a platform-admin create endpoint; the switch-on-provider shape
// is in place so AWS drops in when its endpoint exists.
export const EXTERNAL_SERVICE_PROVIDERS = [
  { value: "gcp_iam", label: "Google Cloud Platform" },
] as const;

export type ExternalServiceProvider =
  (typeof EXTERNAL_SERVICE_PROVIDERS)[number]["value"];

// providerLabel maps a supertype `provider` discriminator to a display name.
export function providerLabel(provider: string): string {
  switch (provider) {
    case "gcp_iam":
      return "Google Cloud Platform";
    case "aws_iam":
      return "Amazon Web Services";
    default:
      return provider;
  }
}

// gcpAuthMode derives the authentication approach from which columns are set,
// matching the server-side inference (ambient / impersonation / WIF).
export function gcpAuthMode(credential: GcpIamCredential): string {
  if (credential.wifPoolId) {
    return "Workload Identity Federation";
  }
  if (credential.impersonateServiceAccount) {
    return "Service account impersonation";
  }
  return "Ambient attached identity";
}

// verifySourceLabel names which identity source answered a Verify probe so an
// operator can tell an in-cluster attached identity apart from local Application
// Default Credentials (the values differ between environments).
export function verifySourceLabel(source: string): string {
  switch (source) {
    case "metadata_server":
      return "the in-cluster attached identity (metadata server)";
    case "application_default_credentials":
      return "local Application Default Credentials";
    case "impersonation":
      return "service account impersonation";
    default:
      return "an unknown source";
  }
}
