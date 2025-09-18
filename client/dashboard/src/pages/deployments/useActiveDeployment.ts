import {
  useDeploymentSuspense,
  useListDeploymentsSuspense,
} from "@gram/client/react-query";
import { useMemo } from "react";

export const useActiveDeployment = () => {
  const { data, ...rest } = useListDeploymentsSuspense({}, {});

  const activeDeployment = useMemo(() => {
    return data?.items.find((item) => item.status === "completed");
  }, [data.items]);

  return {
    data: activeDeployment,
    ...rest,
  };
};
