import {
  QueryClient,
  QueryFunctionContext,
  QueryKey,
} from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { GetProjectOverviewResult } from "../models/components/getprojectoverviewresult.js";
import {
  GetProjectOverviewRequest,
  GetProjectOverviewSecurity,
} from "../models/operations/getprojectoverview.js";
export type GetProjectOverviewQueryData = GetProjectOverviewResult;
export declare function prefetchGetProjectOverview(
  queryClient: QueryClient,
  client$: GramCore,
  request: GetProjectOverviewRequest,
  security?: GetProjectOverviewSecurity | undefined,
  options?: RequestOptions,
): Promise<void>;
export declare function buildGetProjectOverviewQuery(
  client$: GramCore,
  request: GetProjectOverviewRequest,
  security?: GetProjectOverviewSecurity | undefined,
  options?: RequestOptions,
): {
  queryKey: QueryKey;
  queryFn: (
    context: QueryFunctionContext,
  ) => Promise<GetProjectOverviewQueryData>;
};
export declare function queryKeyGetProjectOverview(parameters: {
  gramKey?: string | undefined;
  gramSession?: string | undefined;
  gramProject?: string | undefined;
}): QueryKey;
//# sourceMappingURL=getProjectOverview.core.d.ts.map
