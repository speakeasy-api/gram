import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
import { Deployment } from "./deployment.js";
export type CreateDeploymentResult = {
    deployment?: Deployment | undefined;
};
/** @internal */
export declare const CreateDeploymentResult$inboundSchema: z.ZodMiniType<CreateDeploymentResult, unknown>;
export declare function createDeploymentResultFromJSON(jsonString: string): SafeParseResult<CreateDeploymentResult, SDKValidationError>;
//# sourceMappingURL=createdeploymentresult.d.ts.map