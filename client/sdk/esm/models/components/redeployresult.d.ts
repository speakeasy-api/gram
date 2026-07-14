import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
import { Deployment } from "./deployment.js";
export type RedeployResult = {
  deployment?: Deployment | undefined;
};
/** @internal */
export declare const RedeployResult$inboundSchema: z.ZodMiniType<
  RedeployResult,
  unknown
>;
export declare function redeployResultFromJSON(
  jsonString: string,
): SafeParseResult<RedeployResult, SDKValidationError>;
//# sourceMappingURL=redeployresult.d.ts.map
