import { useDeploymentLogs } from "@gram/client/react-query";

export function useDeploymentLogsSummary(deploymentId: string | undefined) {
  const { data: logs } = useDeploymentLogs(
    { deploymentId: deploymentId! },
    undefined,
    {
      staleTime: Infinity,
      refetchOnWindowFocus: false,
      refetchOnReconnect: false,
      enabled: !!deploymentId,
    },
  );

  return logs?.events.reduce(
    (acc, event) => {
      if (event.message.includes("skipped")) {
        acc.skipped++;
      }
      // Skipped are also errors
      if (event.event.includes("error")) {
        acc.errors++;
      }
      return acc;
    },
    { skipped: 0, errors: 0 },
  );
}
