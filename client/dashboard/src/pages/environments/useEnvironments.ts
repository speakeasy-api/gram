import { useListEnvironmentsSuspense } from "@gram/client/react-query/index.js";

export function useEnvironments(): NonNullable<
  ReturnType<typeof useListEnvironmentsSuspense>["data"]
>["environments"] & {
  refetch: ReturnType<typeof useListEnvironmentsSuspense>["refetch"];
} {
  const { data: environments, refetch: refetchEnvironments } =
    useListEnvironmentsSuspense(undefined, undefined, {
      refetchOnWindowFocus: false,
    });

  return Object.assign(environments?.environments || [], {
    refetch: refetchEnvironments,
  });
}
