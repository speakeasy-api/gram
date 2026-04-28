import { useListEnvironmentsSuspense } from "@gram/client/react-query/index.js";

export function useEnvironments() {
  const { data: environments, refetch: refetchEnvironments } =
    useListEnvironmentsSuspense(undefined, undefined, {
      refetchOnWindowFocus: false,
    });

  return Object.assign(environments?.environments || [], {
    refetch: refetchEnvironments,
  });
}
