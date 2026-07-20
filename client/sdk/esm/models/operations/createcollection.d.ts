import * as z from "zod/v4-mini";
import {
  CreateRequestBody2,
  CreateRequestBody2$Outbound,
} from "../components/createrequestbody2.js";
export type CreateCollectionSecurity = {
  sessionHeaderGramSession?: string | undefined;
  apikeyHeaderGramKey?: string | undefined;
};
export type CreateCollectionRequest = {
  /**
   * Session header
   */
  gramSession?: string | undefined;
  /**
   * API Key header
   */
  gramKey?: string | undefined;
  createRequestBody2: CreateRequestBody2;
};
/** @internal */
export type CreateCollectionSecurity$Outbound = {
  "session_header_Gram-Session"?: string | undefined;
  "apikey_header_Gram-Key"?: string | undefined;
};
/** @internal */
export declare const CreateCollectionSecurity$outboundSchema: z.ZodMiniType<
  CreateCollectionSecurity$Outbound,
  CreateCollectionSecurity
>;
export declare function createCollectionSecurityToJSON(
  createCollectionSecurity: CreateCollectionSecurity,
): string;
/** @internal */
export type CreateCollectionRequest$Outbound = {
  "Gram-Session"?: string | undefined;
  "Gram-Key"?: string | undefined;
  CreateRequestBody2: CreateRequestBody2$Outbound;
};
/** @internal */
export declare const CreateCollectionRequest$outboundSchema: z.ZodMiniType<
  CreateCollectionRequest$Outbound,
  CreateCollectionRequest
>;
export declare function createCollectionRequestToJSON(
  createCollectionRequest: CreateCollectionRequest,
): string;
//# sourceMappingURL=createcollection.d.ts.map
