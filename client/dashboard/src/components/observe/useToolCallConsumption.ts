import {
  buildConsumptionFilters,
  hasConsumptionActivity,
} from "@/components/observe/toolCallConsumption";
import { useProject } from "@/contexts/Auth";
import { telemetryQuery } from "@gram/client/funcs/telemetryQuery";
import { useGramContext } from "@gram/client/react-query/_context.js";
import { unwrapAsync } from "@gram/client/types/fp";
import { keepPreviousData, useQuery } from "@tanstack/react-query";
import { useMemo } from "react";

export function useToolCallConsumptionTotals(args: {
  from: Date;
  to: Date;
  hookSources: string[];
  accountType: string;
  enabled?: boolean;
}): {
  pending: boolean;
  error: boolean;
  hasActivity: boolean;
} {
  const project = useProject();
  const client = useGramContext();
  const filters = useMemo(
    () =>
      buildConsumptionFilters(project.id, args.hookSources, args.accountType),
    [project.id, args.hookSources, args.accountType],
  );

  const query = useQuery({
    queryKey: [
      "tool-call-consumption-totals",
      project.id,
      args.from.toISOString(),
      args.to.toISOString(),
      args.hookSources,
      args.accountType,
    ],
    queryFn: () =>
      unwrapAsync(
        telemetryQuery(client, {
          queryPayload: {
            from: args.from,
            to: args.to,
            filters,
            sortBy: "total_tokens",
            topN: 1,
          },
        }),
      ),
    enabled: args.enabled !== false && !!project.id,
    placeholderData: keepPreviousData,
    throwOnError: false,
  });

  return {
    pending: query.isPending,
    error: query.isError,
    hasActivity: hasConsumptionActivity(query.data?.table ?? []),
  };
}
