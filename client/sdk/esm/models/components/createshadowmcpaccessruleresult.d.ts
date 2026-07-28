import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
import { ShadowMCPAccessRule } from "./shadowmcpaccessrule.js";
export type CreateShadowMCPAccessRuleResult = {
  rules: Array<ShadowMCPAccessRule>;
};
/** @internal */
export declare const CreateShadowMCPAccessRuleResult$inboundSchema: z.ZodMiniType<
  CreateShadowMCPAccessRuleResult,
  unknown
>;
export declare function createShadowMCPAccessRuleResultFromJSON(
  jsonString: string,
): SafeParseResult<CreateShadowMCPAccessRuleResult, SDKValidationError>;
//# sourceMappingURL=createshadowmcpaccessruleresult.d.ts.map
