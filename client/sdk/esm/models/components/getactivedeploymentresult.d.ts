import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
import { Deployment } from "./deployment.js";
export type GetActiveDeploymentResult = {
    deployment?: Deployment | undefined;
};
/** @internal */
export declare const GetActiveDeploymentResult$inboundSchema: z.ZodMiniType<GetActiveDeploymentResult, unknown>;
export declare function getActiveDeploymentResultFromJSON(jsonString: string): SafeParseResult<GetActiveDeploymentResult, SDKValidationError>;
//# sourceMappingURL=getactivedeploymentresult.d.ts.map