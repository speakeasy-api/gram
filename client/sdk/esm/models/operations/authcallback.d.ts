import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
export type AuthCallbackRequest = {
  /**
   * The auth code for authentication from the speakeasy system
   */
  code: string;
  /**
   * The opaque state string optionally provided during initialization.
   */
  state?: string | undefined;
};
export type AuthCallbackResponse = {
  headers: {
    [k: string]: Array<string>;
  };
};
/** @internal */
export type AuthCallbackRequest$Outbound = {
  code: string;
  state?: string | undefined;
};
/** @internal */
export declare const AuthCallbackRequest$outboundSchema: z.ZodMiniType<
  AuthCallbackRequest$Outbound,
  AuthCallbackRequest
>;
export declare function authCallbackRequestToJSON(
  authCallbackRequest: AuthCallbackRequest,
): string;
/** @internal */
export declare const AuthCallbackResponse$inboundSchema: z.ZodMiniType<
  AuthCallbackResponse,
  unknown
>;
export declare function authCallbackResponseFromJSON(
  jsonString: string,
): SafeParseResult<AuthCallbackResponse, SDKValidationError>;
//# sourceMappingURL=authcallback.d.ts.map
