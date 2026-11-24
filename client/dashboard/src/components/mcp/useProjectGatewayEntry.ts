import { useProject } from "@/contexts/Auth";
import { useProjectSlugForRequests } from "@/contexts/Sdk";
import { useRBAC } from "@/hooks/useRBAC";
import { useProjectGateway } from "@gram/client/react-query/projectGateway.js";

export type ProjectGatewayEntryStatus =
  | "loading"
  | "available"
  | "off"
  | "unavailable";

/**
 * The project's gateway entry for the MCP Servers list. Always shown to
 * project readers regardless of whether the gateway is enabled or served, so
 * availability changes appear as a disabled/unavailable state rather than
 * silently hiding the feature.
 */
export function useProjectGatewayEntry(): {
  show: boolean;
  status: ProjectGatewayEntryStatus;
  url: string | undefined;
} {
  const project = useProject();
  const { hasScope, isLoading: permissionsLoading } = useRBAC();
  const gramProject = useProjectSlugForRequests();
  const canReadProject =
    !permissionsLoading && hasScope("project:read", project.id);
  // Keyed by project so switching projects never shows the last one's URL.
  const { data, isLoading, isError } = useProjectGateway(
    { gramProject },
    undefined,
    {
      enabled: canReadProject,
      throwOnError: false,
    },
  );

  let status: ProjectGatewayEntryStatus;
  if (isLoading) {
    status = "loading";
  } else if (isError) {
    status = "unavailable";
  } else if (data?.available) {
    status = "available";
  } else if (data?.unavailableReason === "turned_off") {
    status = "off";
  } else {
    status = "unavailable";
  }

  return {
    show: canReadProject,
    status,
    url: !isError && data?.available ? data.url : undefined,
  };
}
