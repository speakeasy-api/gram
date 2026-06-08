import { useListToolsets } from "@gram/client/react-query/index.js";

export function useToolsets(): NonNullable<
  ReturnType<typeof useListToolsets>["data"]
>["toolsets"] & {
  refetch: ReturnType<typeof useListToolsets>["refetch"];
  isLoading: ReturnType<typeof useListToolsets>["isLoading"];
} {
  const { data: toolsets, refetch, isLoading } = useListToolsets();
  return Object.assign(toolsets?.toolsets || [], { refetch, isLoading });
}
