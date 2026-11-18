import { createFileRoute } from "@tanstack/react-router";

import { PlatformCredentialDetail } from "@/pages/platform-credentials/PlatformCredentialDetail";
import { gcpIamPlatformCredentialQuery } from "@/pages/platform-credentials/platformCredentialsClient";

export const Route = createFileRoute("/platform-credentials/$credentialId")({
  component: PlatformCredentialDetail,
  staticData: {
    crumb: ({ credentialId }) =>
      credentialId
        ? { queryKey: gcpIamPlatformCredentialQuery(credentialId).queryKey }
        : undefined,
  },
});
