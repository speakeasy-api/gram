import * as z from "zod";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
export type GetDraftToolsetSecurityOption1 = {
  projectSlugHeaderGramProject: string;
  sessionHeaderGramSession: string;
};
export type GetDraftToolsetSecurityOption2 = {
  apikeyHeaderGramKey: string;
  projectSlugHeaderGramProject: string;
};
export type GetDraftToolsetSecurity = {
  option1?: GetDraftToolsetSecurityOption1 | undefined;
  option2?: GetDraftToolsetSecurityOption2 | undefined;
};
export type GetDraftToolsetRequest = {
  /**
   * The slug of the toolset
   */
  slug: string;
  /**
   * Session header
   */
  gramSession?: string | undefined;
  /**
   * API Key header
   */
  gramKey?: string | undefined;
  /**
   * project header
   */
  gramProject?: string | undefined;
};
/** @internal */
export declare const GetDraftToolsetSecurityOption1$inboundSchema: z.ZodType<
  GetDraftToolsetSecurityOption1,
  z.ZodTypeDef,
  unknown
>;
/** @internal */
export type GetDraftToolsetSecurityOption1$Outbound = {
  "project_slug_header_Gram-Project": string;
  "session_header_Gram-Session": string;
};
/** @internal */
export declare const GetDraftToolsetSecurityOption1$outboundSchema: z.ZodType<
  GetDraftToolsetSecurityOption1$Outbound,
  z.ZodTypeDef,
  GetDraftToolsetSecurityOption1
>;
/**
 * @internal
 * @deprecated This namespace will be removed in future versions. Use schemas and types that are exported directly from this module.
 */
export declare namespace GetDraftToolsetSecurityOption1$ {
  /** @deprecated use `GetDraftToolsetSecurityOption1$inboundSchema` instead. */
  const inboundSchema: z.ZodType<
    GetDraftToolsetSecurityOption1,
    z.ZodTypeDef,
    unknown
  >;
  /** @deprecated use `GetDraftToolsetSecurityOption1$outboundSchema` instead. */
  const outboundSchema: z.ZodType<
    GetDraftToolsetSecurityOption1$Outbound,
    z.ZodTypeDef,
    GetDraftToolsetSecurityOption1
  >;
  /** @deprecated use `GetDraftToolsetSecurityOption1$Outbound` instead. */
  type Outbound = GetDraftToolsetSecurityOption1$Outbound;
}
export declare function getDraftToolsetSecurityOption1ToJSON(
  getDraftToolsetSecurityOption1: GetDraftToolsetSecurityOption1,
): string;
export declare function getDraftToolsetSecurityOption1FromJSON(
  jsonString: string,
): SafeParseResult<GetDraftToolsetSecurityOption1, SDKValidationError>;
/** @internal */
export declare const GetDraftToolsetSecurityOption2$inboundSchema: z.ZodType<
  GetDraftToolsetSecurityOption2,
  z.ZodTypeDef,
  unknown
>;
/** @internal */
export type GetDraftToolsetSecurityOption2$Outbound = {
  "apikey_header_Gram-Key": string;
  "project_slug_header_Gram-Project": string;
};
/** @internal */
export declare const GetDraftToolsetSecurityOption2$outboundSchema: z.ZodType<
  GetDraftToolsetSecurityOption2$Outbound,
  z.ZodTypeDef,
  GetDraftToolsetSecurityOption2
>;
/**
 * @internal
 * @deprecated This namespace will be removed in future versions. Use schemas and types that are exported directly from this module.
 */
export declare namespace GetDraftToolsetSecurityOption2$ {
  /** @deprecated use `GetDraftToolsetSecurityOption2$inboundSchema` instead. */
  const inboundSchema: z.ZodType<
    GetDraftToolsetSecurityOption2,
    z.ZodTypeDef,
    unknown
  >;
  /** @deprecated use `GetDraftToolsetSecurityOption2$outboundSchema` instead. */
  const outboundSchema: z.ZodType<
    GetDraftToolsetSecurityOption2$Outbound,
    z.ZodTypeDef,
    GetDraftToolsetSecurityOption2
  >;
  /** @deprecated use `GetDraftToolsetSecurityOption2$Outbound` instead. */
  type Outbound = GetDraftToolsetSecurityOption2$Outbound;
}
export declare function getDraftToolsetSecurityOption2ToJSON(
  getDraftToolsetSecurityOption2: GetDraftToolsetSecurityOption2,
): string;
export declare function getDraftToolsetSecurityOption2FromJSON(
  jsonString: string,
): SafeParseResult<GetDraftToolsetSecurityOption2, SDKValidationError>;
/** @internal */
export declare const GetDraftToolsetSecurity$inboundSchema: z.ZodType<
  GetDraftToolsetSecurity,
  z.ZodTypeDef,
  unknown
>;
/** @internal */
export type GetDraftToolsetSecurity$Outbound = {
  Option1?: GetDraftToolsetSecurityOption1$Outbound | undefined;
  Option2?: GetDraftToolsetSecurityOption2$Outbound | undefined;
};
/** @internal */
export declare const GetDraftToolsetSecurity$outboundSchema: z.ZodType<
  GetDraftToolsetSecurity$Outbound,
  z.ZodTypeDef,
  GetDraftToolsetSecurity
>;
/**
 * @internal
 * @deprecated This namespace will be removed in future versions. Use schemas and types that are exported directly from this module.
 */
export declare namespace GetDraftToolsetSecurity$ {
  /** @deprecated use `GetDraftToolsetSecurity$inboundSchema` instead. */
  const inboundSchema: z.ZodType<
    GetDraftToolsetSecurity,
    z.ZodTypeDef,
    unknown
  >;
  /** @deprecated use `GetDraftToolsetSecurity$outboundSchema` instead. */
  const outboundSchema: z.ZodType<
    GetDraftToolsetSecurity$Outbound,
    z.ZodTypeDef,
    GetDraftToolsetSecurity
  >;
  /** @deprecated use `GetDraftToolsetSecurity$Outbound` instead. */
  type Outbound = GetDraftToolsetSecurity$Outbound;
}
export declare function getDraftToolsetSecurityToJSON(
  getDraftToolsetSecurity: GetDraftToolsetSecurity,
): string;
export declare function getDraftToolsetSecurityFromJSON(
  jsonString: string,
): SafeParseResult<GetDraftToolsetSecurity, SDKValidationError>;
/** @internal */
export declare const GetDraftToolsetRequest$inboundSchema: z.ZodType<
  GetDraftToolsetRequest,
  z.ZodTypeDef,
  unknown
>;
/** @internal */
export type GetDraftToolsetRequest$Outbound = {
  slug: string;
  "Gram-Session"?: string | undefined;
  "Gram-Key"?: string | undefined;
  "Gram-Project"?: string | undefined;
};
/** @internal */
export declare const GetDraftToolsetRequest$outboundSchema: z.ZodType<
  GetDraftToolsetRequest$Outbound,
  z.ZodTypeDef,
  GetDraftToolsetRequest
>;
/**
 * @internal
 * @deprecated This namespace will be removed in future versions. Use schemas and types that are exported directly from this module.
 */
export declare namespace GetDraftToolsetRequest$ {
  /** @deprecated use `GetDraftToolsetRequest$inboundSchema` instead. */
  const inboundSchema: z.ZodType<GetDraftToolsetRequest, z.ZodTypeDef, unknown>;
  /** @deprecated use `GetDraftToolsetRequest$outboundSchema` instead. */
  const outboundSchema: z.ZodType<
    GetDraftToolsetRequest$Outbound,
    z.ZodTypeDef,
    GetDraftToolsetRequest
  >;
  /** @deprecated use `GetDraftToolsetRequest$Outbound` instead. */
  type Outbound = GetDraftToolsetRequest$Outbound;
}
export declare function getDraftToolsetRequestToJSON(
  getDraftToolsetRequest: GetDraftToolsetRequest,
): string;
export declare function getDraftToolsetRequestFromJSON(
  jsonString: string,
): SafeParseResult<GetDraftToolsetRequest, SDKValidationError>;
//# sourceMappingURL=getdrafttoolset.d.ts.map
