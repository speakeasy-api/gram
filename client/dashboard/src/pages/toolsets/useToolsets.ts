import { useListToolsets } from "@gram/client/react-query/index.js";

export function useToolsets() {
  const { data: toolsets, refetch, isLoading } = useListToolsets();
  return Object.assign(toolsets?.toolsets || [], { refetch, isLoading });
}
