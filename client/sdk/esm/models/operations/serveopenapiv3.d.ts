import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
export type ServeOpenAPIv3Security = {
  apikeyHeaderGramKey?: string | undefined;
  sessionHeaderGramSession?: string | undefined;
};
export type ServeOpenAPIv3Request = {
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
export type ServeOpenAPIv3Response = {
  headers: {
    [k: string]: Array<string>;
  };
  result: ReadableStream<Uint8Array>;
};
/** @internal */
export type ServeOpenAPIv3Security$Outbound = {
  "apikey_header_Gram-Key"?: string | undefined;
  "session_header_Gram-Session"?: string | undefined;
};
/** @internal */
export declare const ServeOpenAPIv3Security$outboundSchema: z.ZodMiniType<
  ServeOpenAPIv3Security$Outbound,
  ServeOpenAPIv3Security
>;
export declare function serveOpenAPIv3SecurityToJSON(
  serveOpenAPIv3Security: ServeOpenAPIv3Security,
): string;
/** @internal */
export type ServeOpenAPIv3Request$Outbound = {
  id: string;
  project_id: string;
  "Gram-Key"?: string | undefined;
  "Gram-Session"?: string | undefined;
};
/** @internal */
export declare const ServeOpenAPIv3Request$outboundSchema: z.ZodMiniType<
  ServeOpenAPIv3Request$Outbound,
  ServeOpenAPIv3Request
>;
export declare function serveOpenAPIv3RequestToJSON(
  serveOpenAPIv3Request: ServeOpenAPIv3Request,
): string;
/** @internal */
export declare const ServeOpenAPIv3Response$inboundSchema: z.ZodMiniType<
  ServeOpenAPIv3Response,
  unknown
>;
export declare function serveOpenAPIv3ResponseFromJSON(
  jsonString: string,
): SafeParseResult<ServeOpenAPIv3Response, SDKValidationError>;
//# sourceMappingURL=serveopenapiv3.d.ts.map
