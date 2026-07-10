import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
import { DeploymentSummary } from "./deploymentsummary.js";
export type ListDeploymentResult = {
    /**
     * A list of deployments
     */
    items: Array<DeploymentSummary>;
    /**
     * The cursor to fetch results from
     */
    nextCursor?: string | undefined;
};
/** @internal */
export declare const ListDeploymentResult$inboundSchema: z.ZodMiniType<ListDeploymentResult, unknown>;
export declare function listDeploymentResultFromJSON(jsonString: string): SafeParseResult<ListDeploymentResult, SDKValidationError>;
//# sourceMappingURL=listdeploymentresult.d.ts.map