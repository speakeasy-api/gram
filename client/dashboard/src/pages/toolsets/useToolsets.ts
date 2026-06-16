import { useListToolsets } from "@gram/client/react-query/index.js";

export function useToolsets(): NonNullable<
  ReturnType<typeof useListToolsets>["data"]
>["toolsets"] & {
  refetch: ReturnType<typeof useListToolsets>["refetch"];
  isLoading: ReturnType<typeof useListToolsets>["isLoading"];
  isError: ReturnType<typeof useListToolsets>["isError"];
} {
  const {
    data: toolsets,
    refetch,
    isLoading,
    isError,
  } = useListToolsets(undefined, undefined, {
    // toolsets.list is non-critical for the MCP screens — degrade to the last
    // good (or empty) list with an inline indicator instead of throwing to the
    // page error boundary.
    throwOnError: false,
  });
  return Object.assign(toolsets?.toolsets || [], {
    refetch,
    isLoading,
    isError,
  });
}
