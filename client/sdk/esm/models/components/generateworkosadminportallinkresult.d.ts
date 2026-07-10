import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
export type GenerateWorkOSAdminPortalLinkResult = {
  /**
   * URL to the WorkOS Admin Portal flow.
   */
  url: string;
};
/** @internal */
export declare const GenerateWorkOSAdminPortalLinkResult$inboundSchema: z.ZodMiniType<
  GenerateWorkOSAdminPortalLinkResult,
  unknown
>;
export declare function generateWorkOSAdminPortalLinkResultFromJSON(
  jsonString: string,
): SafeParseResult<GenerateWorkOSAdminPortalLinkResult, SDKValidationError>;
//# sourceMappingURL=generateworkosadminportallinkresult.d.ts.map
