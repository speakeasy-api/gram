import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
/**
 * Outcome of a remote MCP server URL verification
 */
export type VerifyURLResult = {
  /**
   * HTTP status code returned by the URL, if any
   */
  httpStatus?: number | undefined;
  /**
   * Human-readable summary of the verification outcome
   */
  message: string;
  /**
   * Whether the URL responded in a way consistent with a remote MCP server
   */
  verified: boolean;
};
/** @internal */
export declare const VerifyURLResult$inboundSchema: z.ZodMiniType<
  VerifyURLResult,
  unknown
>;
export declare function verifyURLResultFromJSON(
  jsonString: string,
): SafeParseResult<VerifyURLResult, SDKValidationError>;
//# sourceMappingURL=verifyurlresult.d.ts.map
