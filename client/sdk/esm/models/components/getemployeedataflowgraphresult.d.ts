import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
import { EmployeeDataFlowEdge } from "./employeedataflowedge.js";
import { EmployeeDataFlowNode } from "./employeedataflownode.js";
/**
 * Result of employee data flow graph query
 */
export type GetEmployeeDataFlowGraphResult = {
    /**
     * Weighted graph edges between adjacent populated tiers
     */
    edges: Array<EmployeeDataFlowEdge>;
    /**
     * Graph nodes grouped by tier
     */
    nodes: Array<EmployeeDataFlowNode>;
};
/** @internal */
export declare const GetEmployeeDataFlowGraphResult$inboundSchema: z.ZodMiniType<GetEmployeeDataFlowGraphResult, unknown>;
export declare function getEmployeeDataFlowGraphResultFromJSON(jsonString: string): SafeParseResult<GetEmployeeDataFlowGraphResult, SDKValidationError>;
//# sourceMappingURL=getemployeedataflowgraphresult.d.ts.map