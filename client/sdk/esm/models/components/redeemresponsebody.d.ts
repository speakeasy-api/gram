import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
export type RedeemResponseBody = {
  /**
   * The raw gram_ API key, carrying the [agent,hooks] scopes. Returned exactly once.
   */
  accessToken: string;
  /**
   * Slug of the project the key is scoped to.
   */
  projectSlug: string;
  /**
   * Email of the user the key was minted for.
   */
  userEmail: string;
};
/** @internal */
export declare const RedeemResponseBody$inboundSchema: z.ZodMiniType<
  RedeemResponseBody,
  unknown
>;
export declare function redeemResponseBodyFromJSON(
  jsonString: string,
): SafeParseResult<RedeemResponseBody, SDKValidationError>;
//# sourceMappingURL=redeemresponsebody.d.ts.map
