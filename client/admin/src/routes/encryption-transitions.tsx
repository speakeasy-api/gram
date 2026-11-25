import { createFileRoute } from "@tanstack/react-router";

import { EncryptionTransitions } from "@/pages/encryption-transitions/EncryptionTransitions";
import { encryptionTransitionsSearch } from "@/pages/encryption-transitions/encryptionTransitionsSearch";

export const Route = createFileRoute("/encryption-transitions")({
  component: EncryptionTransitions,
  validateSearch: encryptionTransitionsSearch,
  staticData: { crumb: "Encryption transitions" },
});
