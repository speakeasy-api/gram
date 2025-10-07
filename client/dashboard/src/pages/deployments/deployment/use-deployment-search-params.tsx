import React from "react";
import { useSearchParams } from "react-router";

const defaultValue = {
  tab: "logs",
};

export type DeploymentPageSearchParams =
  | {
      tab: "logs";
      grouping?: "by_source";
    }
  | {
      tab: "assets";
    }
  | {
      tab: "tools";
    };

/** A hook to manage and consume the search params for the deployment page. */
export function useDeploymentSearchParams() {
  const [_searchParams, _setSearchParams] = useSearchParams(defaultValue);

  const setSearchParams = (
    updater:
      | DeploymentPageSearchParams
      | ((prev: DeploymentPageSearchParams) => DeploymentPageSearchParams),
  ) => {
    if (typeof updater === "function") {
      _setSearchParams((prev) => {
        const prevObj = Object.fromEntries(prev) as DeploymentPageSearchParams;
        return updater(prevObj);
      });
      return;
    }

    _setSearchParams(new URLSearchParams(updater));
  };

  const searchParams = React.useMemo(() => {
    return Object.fromEntries(_searchParams) as DeploymentPageSearchParams;
  }, [_searchParams]);

  return { searchParams, setSearchParams };
}
