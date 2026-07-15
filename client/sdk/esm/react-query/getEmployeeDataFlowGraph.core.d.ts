import {
  QueryClient,
  QueryFunctionContext,
  QueryKey,
} from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { GetEmployeeDataFlowGraphResult } from "../models/components/getemployeedataflowgraphresult.js";
import {
  GetEmployeeDataFlowGraphRequest,
  GetEmployeeDataFlowGraphSecurity,
} from "../models/operations/getemployeedataflowgraph.js";
export type GetEmployeeDataFlowGraphQueryData = GetEmployeeDataFlowGraphResult;
export declare function prefetchGetEmployeeDataFlowGraph(
  queryClient: QueryClient,
  client$: GramCore,
  request: GetEmployeeDataFlowGraphRequest,
  security?: GetEmployeeDataFlowGraphSecurity | undefined,
  options?: RequestOptions,
): Promise<void>;
export declare function buildGetEmployeeDataFlowGraphQuery(
  client$: GramCore,
  request: GetEmployeeDataFlowGraphRequest,
  security?: GetEmployeeDataFlowGraphSecurity | undefined,
  options?: RequestOptions,
): {
  queryKey: QueryKey;
  queryFn: (
    context: QueryFunctionContext,
  ) => Promise<GetEmployeeDataFlowGraphQueryData>;
};
export declare function queryKeyGetEmployeeDataFlowGraph(parameters: {
  gramKey?: string | undefined;
  gramSession?: string | undefined;
  gramProject?: string | undefined;
}): QueryKey;
//# sourceMappingURL=getEmployeeDataFlowGraph.core.d.ts.map
