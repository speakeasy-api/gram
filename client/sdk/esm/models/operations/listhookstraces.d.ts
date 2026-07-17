import * as z from "zod/v4-mini";
import {
  ListHooksTracesPayload,
  ListHooksTracesPayload$Outbound,
} from "../components/listhookstracespayload.js";
export type ListHooksTracesSecurityOption1 = {
  apikeyHeaderGramKey: string;
  projectSlugHeaderGramProject: string;
};
export type ListHooksTracesSecurityOption2 = {
  projectSlugHeaderGramProject: string;
  sessionHeaderGramSession: string;
};
export type ListHooksTracesSecurity = {
  option1?: ListHooksTracesSecurityOption1 | undefined;
  option2?: ListHooksTracesSecurityOption2 | undefined;
};
export type ListHooksTracesRequest = {
  /**
   * API Key header
   */
  gramKey?: string | undefined;
  /**
   * Session header
   */
  gramSession?: string | undefined;
  /**
   * project header
   */
  gramProject?: string | undefined;
  listHooksTracesPayload: ListHooksTracesPayload;
};
/** @internal */
export type ListHooksTracesSecurityOption1$Outbound = {
  "apikey_header_Gram-Key": string;
  "project_slug_header_Gram-Project": string;
};
/** @internal */
export declare const ListHooksTracesSecurityOption1$outboundSchema: z.ZodMiniType<
  ListHooksTracesSecurityOption1$Outbound,
  ListHooksTracesSecurityOption1
>;
export declare function listHooksTracesSecurityOption1ToJSON(
  listHooksTracesSecurityOption1: ListHooksTracesSecurityOption1,
): string;
/** @internal */
export type ListHooksTracesSecurityOption2$Outbound = {
  "project_slug_header_Gram-Project": string;
  "session_header_Gram-Session": string;
};
/** @internal */
export declare const ListHooksTracesSecurityOption2$outboundSchema: z.ZodMiniType<
  ListHooksTracesSecurityOption2$Outbound,
  ListHooksTracesSecurityOption2
>;
export declare function listHooksTracesSecurityOption2ToJSON(
  listHooksTracesSecurityOption2: ListHooksTracesSecurityOption2,
): string;
/** @internal */
export type ListHooksTracesSecurity$Outbound = {
  Option1?: ListHooksTracesSecurityOption1$Outbound | undefined;
  Option2?: ListHooksTracesSecurityOption2$Outbound | undefined;
};
/** @internal */
export declare const ListHooksTracesSecurity$outboundSchema: z.ZodMiniType<
  ListHooksTracesSecurity$Outbound,
  ListHooksTracesSecurity
>;
export declare function listHooksTracesSecurityToJSON(
  listHooksTracesSecurity: ListHooksTracesSecurity,
): string;
/** @internal */
export type ListHooksTracesRequest$Outbound = {
  "Gram-Key"?: string | undefined;
  "Gram-Session"?: string | undefined;
  "Gram-Project"?: string | undefined;
  ListHooksTracesPayload: ListHooksTracesPayload$Outbound;
};
/** @internal */
export declare const ListHooksTracesRequest$outboundSchema: z.ZodMiniType<
  ListHooksTracesRequest$Outbound,
  ListHooksTracesRequest
>;
export declare function listHooksTracesRequestToJSON(
  listHooksTracesRequest: ListHooksTracesRequest,
): string;
//# sourceMappingURL=listhookstraces.d.ts.map
