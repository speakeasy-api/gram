import { z } from "zod";

import { TRANSITION_STATES } from "@/lib/organizationEncryption";

const searchSchema = z.object({
  // The use case listed. Absent lists the first one the server reports.
  useCase: z.string().min(1).optional().catch(undefined),
  // Only organizations in this transition state; absent lists every state.
  state: z.enum(TRANSITION_STATES).optional().catch(undefined),
  page: z.coerce.number().int().min(0).optional().catch(undefined),
});

export type EncryptionTransitionsSearch = z.infer<typeof searchSchema>;

export function encryptionTransitionsSearch(
  search: Record<string, unknown>,
): EncryptionTransitionsSearch {
  return searchSchema.parse(search);
}
