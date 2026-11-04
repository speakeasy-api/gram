import { useOrganization, useProject } from "@/contexts/Auth";
import { useProjectSlugForRequests, useSdkClient } from "@/contexts/Sdk";
import { useRBAC } from "@/hooks/useRBAC";
import { buildBuiltinMcpServersQuery } from "@gram/client/react-query/builtinMcpServers.js";
import type { BuiltinMcpServer } from "@gram/client/models/components/builtinmcpserver.js";
import type { BuiltinMcpServersQueryData } from "@gram/client/react-query/builtinMcpServers.js";
import type { UseQueryResult } from "@tanstack/react-query";
import { useQuery } from "@tanstack/react-query";

/** The API owns authorization; this gate avoids forbidden discovery requests. */
export function useBuiltinMcpServers(
  enabled = true,
): UseQueryResult<BuiltinMcpServersQueryData> & {
  servers: BuiltinMcpServer[];
  isAdmin: boolean;
} {
  const organization = useOrganization();
  const project = useProject();
  const gramProject = useProjectSlugForRequests();
  const client = useSdkClient();
  const { hasScope } = useRBAC();
  const isAdmin = hasScope("org:admin", organization.id);
  const built = buildBuiltinMcpServersQuery(client, { gramProject });
  const query = useQuery({
    ...built,
    // Project slugs may repeat across organizations; keep preferences isolated.
    queryKey: [...built.queryKey, organization.id, project.id],
    enabled: enabled && isAdmin,
    throwOnError: false,
    refetchOnWindowFocus: false,
  });
  return {
    ...query,
    servers: isAdmin && !query.isError ? (query.data?.servers ?? []) : [],
    isAdmin,
  };
}
