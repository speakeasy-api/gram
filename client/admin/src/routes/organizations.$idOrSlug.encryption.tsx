import { createFileRoute } from "@tanstack/react-router";

import { EncryptionRoute } from "@/pages/organization/Encryption";

export const Route = createFileRoute("/organizations/$idOrSlug/encryption")({
  component: EncryptionRoute,
  staticData: { crumb: "Encryption" },
});
