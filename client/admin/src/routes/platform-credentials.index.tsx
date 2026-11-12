import { createFileRoute } from "@tanstack/react-router";

import { PlatformCredentialList } from "@/pages/platform-credentials/PlatformCredentialList";

export const Route = createFileRoute("/platform-credentials/")({
  component: PlatformCredentialList,
});
