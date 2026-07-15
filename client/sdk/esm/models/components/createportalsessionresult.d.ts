import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
export type CreatePortalSessionResult = {
  /**
   * Front-end token for the webhook portal session.
   */
  token: string;
  /**
   * URL for the webhook portal session.
   */
  url: string;
};
/** @internal */
export declare const CreatePortalSessionResult$inboundSchema: z.ZodMiniType<
  CreatePortalSessionResult,
  unknown
>;
export declare function createPortalSessionResultFromJSON(
  jsonString: string,
): SafeParseResult<CreatePortalSessionResult, SDKValidationError>;
//# sourceMappingURL=createportalsessionresult.d.ts.map
