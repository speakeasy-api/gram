import { useListDeployments } from "@gram/client/react-query/listDeployments.js";
import { useMemo } from "react";

type ListDeploymentsResult = ReturnType<typeof useListDeployments>;
type DeploymentItem = NonNullable<
  ListDeploymentsResult["data"]
>["items"][number];

export const useActiveDeployment = (): Omit<ListDeploymentsResult, "data"> & {
  data: DeploymentItem | undefined;
} => {
  const { data, ...rest } = useListDeployments({}, {});

  const activeDeployment = useMemo(() => {
    return data?.items.find((item) => item.status === "completed");
  }, [data?.items]);

  return {
    data: activeDeployment,
    ...rest,
  };
};
