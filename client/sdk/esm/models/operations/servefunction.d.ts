import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
export type ServeFunctionSecurity = {
  apikeyHeaderGramKey?: string | undefined;
  sessionHeaderGramSession?: string | undefined;
};
export type ServeFunctionRequest = {
  /**
   * The ID of the asset to serve
   */
  id: string;
  /**
   * The procect ID that the asset belongs to
   */
  projectId: string;
  /**
   * API Key header
   */
  gramKey?: string | undefined;
  /**
   * Session header
   */
  gramSession?: string | undefined;
};
export type ServeFunctionResponse = {
  headers: {
    [k: string]: Array<string>;
  };
  result: ReadableStream<Uint8Array>;
};
/** @internal */
export type ServeFunctionSecurity$Outbound = {
  "apikey_header_Gram-Key"?: string | undefined;
  "session_header_Gram-Session"?: string | undefined;
};
/** @internal */
export declare const ServeFunctionSecurity$outboundSchema: z.ZodMiniType<
  ServeFunctionSecurity$Outbound,
  ServeFunctionSecurity
>;
export declare function serveFunctionSecurityToJSON(
  serveFunctionSecurity: ServeFunctionSecurity,
): string;
/** @internal */
export type ServeFunctionRequest$Outbound = {
  id: string;
  project_id: string;
  "Gram-Key"?: string | undefined;
  "Gram-Session"?: string | undefined;
};
/** @internal */
export declare const ServeFunctionRequest$outboundSchema: z.ZodMiniType<
  ServeFunctionRequest$Outbound,
  ServeFunctionRequest
>;
export declare function serveFunctionRequestToJSON(
  serveFunctionRequest: ServeFunctionRequest,
): string;
/** @internal */
export declare const ServeFunctionResponse$inboundSchema: z.ZodMiniType<
  ServeFunctionResponse,
  unknown
>;
export declare function serveFunctionResponseFromJSON(
  jsonString: string,
): SafeParseResult<ServeFunctionResponse, SDKValidationError>;
//# sourceMappingURL=servefunction.d.ts.map
