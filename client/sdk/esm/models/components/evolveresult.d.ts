import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
import { Deployment } from "./deployment.js";
export type EvolveResult = {
  deployment?: Deployment | undefined;
};
/** @internal */
export declare const EvolveResult$inboundSchema: z.ZodMiniType<
  EvolveResult,
  unknown
>;
export declare function evolveResultFromJSON(
  jsonString: string,
): SafeParseResult<EvolveResult, SDKValidationError>;
//# sourceMappingURL=evolveresult.d.ts.map
