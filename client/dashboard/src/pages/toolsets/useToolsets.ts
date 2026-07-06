import { useProjectSlugForRequests } from "@/contexts/Sdk";
import { useListToolsets } from "@gram/client/react-query/index.js";

export function useToolsets(): NonNullable<
  ReturnType<typeof useListToolsets>["data"]
>["toolsets"] & {
  refetch: ReturnType<typeof useListToolsets>["refetch"];
  isLoading: ReturnType<typeof useListToolsets>["isLoading"];
  isFetching: ReturnType<typeof useListToolsets>["isFetching"];
  isError: ReturnType<typeof useListToolsets>["isError"];
} {
  const gramProject = useProjectSlugForRequests();
  const {
    data: toolsets,
    refetch,
    isLoading,
    isFetching,
    isError,
  } = useListToolsets({ gramProject }, undefined, {
    // toolsets.list is non-critical for the MCP screens — degrade to the last
    // good (or empty) list with an inline indicator instead of throwing to the
    // page error boundary. Key the query by project so a tolerated failure can
    // never leave another project's cached toolsets on screen after a switch.
    throwOnError: false,
  });
  return Object.assign(toolsets?.toolsets || [], {
    refetch,
    isLoading,
    isFetching,
    isError,
  });
}
