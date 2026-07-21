import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
/**
 * A minted per-user API key for the device agent.
 */
export type TokenResult = {
  /**
   * The raw per-user API key (carries the `agent_user` scope). Returned exactly once; store it securely. Presented as the Gram-Key on downstream user-scoped endpoints.
   */
  accessToken: string;
  /**
   * Always zero. The minted key has no expiry (api_keys has no TTL).
   */
  expiresIn: number;
  /**
   * Always empty. The minted key is long-lived and does not refresh; its lifecycle lever is revocation.
   */
  refreshToken: string;
  /**
   * Email the key was minted for.
   */
  userEmail: string;
};
/** @internal */
export declare const TokenResult$inboundSchema: z.ZodMiniType<
  TokenResult,
  unknown
>;
export declare function tokenResultFromJSON(
  jsonString: string,
): SafeParseResult<TokenResult, SDKValidationError>;
//# sourceMappingURL=tokenresult.d.ts.map
