import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
import { Integration } from "./integration.js";
export type GetIntegrationResult = {
  integration?: Integration | undefined;
};
/** @internal */
export declare const GetIntegrationResult$inboundSchema: z.ZodMiniType<
  GetIntegrationResult,
  unknown
>;
export declare function getIntegrationResultFromJSON(
  jsonString: string,
): SafeParseResult<GetIntegrationResult, SDKValidationError>;
//# sourceMappingURL=getintegrationresult.d.ts.map
