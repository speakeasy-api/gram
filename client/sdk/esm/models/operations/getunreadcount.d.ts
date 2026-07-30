import * as z from "zod";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
export type GetUnreadCountSecurity = {
  projectSlugHeaderGramProject?: string | undefined;
  sessionHeaderGramSession?: string | undefined;
};
export type GetUnreadCountRequest = {
  /**
   * ISO timestamp to count notifications from
   */
  since?: Date | undefined;
  /**
   * Session header
   */
  gramSession?: string | undefined;
  /**
   * project header
   */
  gramProject?: string | undefined;
};
/** @internal */
export declare const GetUnreadCountSecurity$inboundSchema: z.ZodType<
  GetUnreadCountSecurity,
  z.ZodTypeDef,
  unknown
>;
/** @internal */
export type GetUnreadCountSecurity$Outbound = {
  "project_slug_header_Gram-Project"?: string | undefined;
  "session_header_Gram-Session"?: string | undefined;
};
/** @internal */
export declare const GetUnreadCountSecurity$outboundSchema: z.ZodType<
  GetUnreadCountSecurity$Outbound,
  z.ZodTypeDef,
  GetUnreadCountSecurity
>;
/**
 * @internal
 * @deprecated This namespace will be removed in future versions. Use schemas and types that are exported directly from this module.
 */
export declare namespace GetUnreadCountSecurity$ {
  /** @deprecated use `GetUnreadCountSecurity$inboundSchema` instead. */
  const inboundSchema: z.ZodType<GetUnreadCountSecurity, z.ZodTypeDef, unknown>;
  /** @deprecated use `GetUnreadCountSecurity$outboundSchema` instead. */
  const outboundSchema: z.ZodType<
    GetUnreadCountSecurity$Outbound,
    z.ZodTypeDef,
    GetUnreadCountSecurity
  >;
  /** @deprecated use `GetUnreadCountSecurity$Outbound` instead. */
  type Outbound = GetUnreadCountSecurity$Outbound;
}
export declare function getUnreadCountSecurityToJSON(
  getUnreadCountSecurity: GetUnreadCountSecurity,
): string;
export declare function getUnreadCountSecurityFromJSON(
  jsonString: string,
): SafeParseResult<GetUnreadCountSecurity, SDKValidationError>;
/** @internal */
export declare const GetUnreadCountRequest$inboundSchema: z.ZodType<
  GetUnreadCountRequest,
  z.ZodTypeDef,
  unknown
>;
/** @internal */
export type GetUnreadCountRequest$Outbound = {
  since?: string | undefined;
  "Gram-Session"?: string | undefined;
  "Gram-Project"?: string | undefined;
};
/** @internal */
export declare const GetUnreadCountRequest$outboundSchema: z.ZodType<
  GetUnreadCountRequest$Outbound,
  z.ZodTypeDef,
  GetUnreadCountRequest
>;
/**
 * @internal
 * @deprecated This namespace will be removed in future versions. Use schemas and types that are exported directly from this module.
 */
export declare namespace GetUnreadCountRequest$ {
  /** @deprecated use `GetUnreadCountRequest$inboundSchema` instead. */
  const inboundSchema: z.ZodType<GetUnreadCountRequest, z.ZodTypeDef, unknown>;
  /** @deprecated use `GetUnreadCountRequest$outboundSchema` instead. */
  const outboundSchema: z.ZodType<
    GetUnreadCountRequest$Outbound,
    z.ZodTypeDef,
    GetUnreadCountRequest
  >;
  /** @deprecated use `GetUnreadCountRequest$Outbound` instead. */
  type Outbound = GetUnreadCountRequest$Outbound;
}
export declare function getUnreadCountRequestToJSON(
  getUnreadCountRequest: GetUnreadCountRequest,
): string;
export declare function getUnreadCountRequestFromJSON(
  jsonString: string,
): SafeParseResult<GetUnreadCountRequest, SDKValidationError>;
//# sourceMappingURL=getunreadcount.d.ts.map
