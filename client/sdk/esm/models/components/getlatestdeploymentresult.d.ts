import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
import { Deployment } from "./deployment.js";
export type GetLatestDeploymentResult = {
    deployment?: Deployment | undefined;
};
/** @internal */
export declare const GetLatestDeploymentResult$inboundSchema: z.ZodMiniType<GetLatestDeploymentResult, unknown>;
export declare function getLatestDeploymentResultFromJSON(jsonString: string): SafeParseResult<GetLatestDeploymentResult, SDKValidationError>;
//# sourceMappingURL=getlatestdeploymentresult.d.ts.map