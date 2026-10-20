import type { QueryClient } from "@tanstack/react-query";
import { invalidateAllAudienceOptions } from "@gram/client/react-query/audienceOptions.js";
import { invalidateAllAudiences } from "@gram/client/react-query/audiences.js";
import { invalidateAllDirectoryRoleMappings } from "@gram/client/react-query/directoryRoleMappings.js";
import { invalidateAllExplainResourceAccess } from "@gram/client/react-query/explainResourceAccess.js";
import { invalidateAllIdentityAccess } from "@gram/client/react-query/identityAccess.js";
import { invalidateAllMembers } from "@gram/client/react-query/members.js";
import { invalidateAllResourceAudience } from "@gram/client/react-query/resourceAudience.js";
import { invalidateAllRole } from "@gram/client/react-query/role.js";
import { invalidateAllRoles } from "@gram/client/react-query/roles.js";

export function invalidateDirectoryMappingAccess(
  queryClient: QueryClient,
): Promise<void[]> {
  return Promise.all([
    invalidateAllDirectoryRoleMappings(queryClient),
    invalidateAllMembers(queryClient),
    invalidateAllRoles(queryClient),
    invalidateAllRole(queryClient),
    invalidateAllAudienceOptions(queryClient),
    invalidateAllAudiences(queryClient),
    invalidateAllResourceAudience(queryClient),
    invalidateAllExplainResourceAccess(queryClient),
    invalidateAllIdentityAccess(queryClient),
  ]);
}
