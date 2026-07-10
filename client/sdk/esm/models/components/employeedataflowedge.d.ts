import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
/**
 * A weighted edge in the employee data flow graph
 */
export type EmployeeDataFlowEdge = {
    /**
     * Total calls represented by this edge
     */
    callCount: number;
    /**
     * Failed or blocked calls represented by this edge
     */
    failureCount: number;
    /**
     * Stable edge ID
     */
    id: string;
    /**
     * Source node ID
     */
    source: string;
    /**
     * Successful calls represented by this edge
     */
    successCount: number;
    /**
     * Target node ID
     */
    target: string;
};
/** @internal */
export declare const EmployeeDataFlowEdge$inboundSchema: z.ZodMiniType<EmployeeDataFlowEdge, unknown>;
export declare function employeeDataFlowEdgeFromJSON(jsonString: string): SafeParseResult<EmployeeDataFlowEdge, SDKValidationError>;
//# sourceMappingURL=employeedataflowedge.d.ts.map