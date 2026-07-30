import * as z from "zod";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
export type PromoteDraftSecurityOption1 = {
  projectSlugHeaderGramProject: string;
  sessionHeaderGramSession: string;
};
export type PromoteDraftSecurityOption2 = {
  apikeyHeaderGramKey: string;
  projectSlugHeaderGramProject: string;
};
export type PromoteDraftSecurity = {
  option1?: PromoteDraftSecurityOption1 | undefined;
  option2?: PromoteDraftSecurityOption2 | undefined;
};
export type PromoteDraftRequest = {
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
export declare const PromoteDraftSecurityOption1$inboundSchema: z.ZodType<
  PromoteDraftSecurityOption1,
  z.ZodTypeDef,
  unknown
>;
/** @internal */
export type PromoteDraftSecurityOption1$Outbound = {
  "project_slug_header_Gram-Project": string;
  "session_header_Gram-Session": string;
};
/** @internal */
export declare const PromoteDraftSecurityOption1$outboundSchema: z.ZodType<
  PromoteDraftSecurityOption1$Outbound,
  z.ZodTypeDef,
  PromoteDraftSecurityOption1
>;
/**
 * @internal
 * @deprecated This namespace will be removed in future versions. Use schemas and types that are exported directly from this module.
 */
export declare namespace PromoteDraftSecurityOption1$ {
  /** @deprecated use `PromoteDraftSecurityOption1$inboundSchema` instead. */
  const inboundSchema: z.ZodType<
    PromoteDraftSecurityOption1,
    z.ZodTypeDef,
    unknown
  >;
  /** @deprecated use `PromoteDraftSecurityOption1$outboundSchema` instead. */
  const outboundSchema: z.ZodType<
    PromoteDraftSecurityOption1$Outbound,
    z.ZodTypeDef,
    PromoteDraftSecurityOption1
  >;
  /** @deprecated use `PromoteDraftSecurityOption1$Outbound` instead. */
  type Outbound = PromoteDraftSecurityOption1$Outbound;
}
export declare function promoteDraftSecurityOption1ToJSON(
  promoteDraftSecurityOption1: PromoteDraftSecurityOption1,
): string;
export declare function promoteDraftSecurityOption1FromJSON(
  jsonString: string,
): SafeParseResult<PromoteDraftSecurityOption1, SDKValidationError>;
/** @internal */
export declare const PromoteDraftSecurityOption2$inboundSchema: z.ZodType<
  PromoteDraftSecurityOption2,
  z.ZodTypeDef,
  unknown
>;
/** @internal */
export type PromoteDraftSecurityOption2$Outbound = {
  "apikey_header_Gram-Key": string;
  "project_slug_header_Gram-Project": string;
};
/** @internal */
export declare const PromoteDraftSecurityOption2$outboundSchema: z.ZodType<
  PromoteDraftSecurityOption2$Outbound,
  z.ZodTypeDef,
  PromoteDraftSecurityOption2
>;
/**
 * @internal
 * @deprecated This namespace will be removed in future versions. Use schemas and types that are exported directly from this module.
 */
export declare namespace PromoteDraftSecurityOption2$ {
  /** @deprecated use `PromoteDraftSecurityOption2$inboundSchema` instead. */
  const inboundSchema: z.ZodType<
    PromoteDraftSecurityOption2,
    z.ZodTypeDef,
    unknown
  >;
  /** @deprecated use `PromoteDraftSecurityOption2$outboundSchema` instead. */
  const outboundSchema: z.ZodType<
    PromoteDraftSecurityOption2$Outbound,
    z.ZodTypeDef,
    PromoteDraftSecurityOption2
  >;
  /** @deprecated use `PromoteDraftSecurityOption2$Outbound` instead. */
  type Outbound = PromoteDraftSecurityOption2$Outbound;
}
export declare function promoteDraftSecurityOption2ToJSON(
  promoteDraftSecurityOption2: PromoteDraftSecurityOption2,
): string;
export declare function promoteDraftSecurityOption2FromJSON(
  jsonString: string,
): SafeParseResult<PromoteDraftSecurityOption2, SDKValidationError>;
/** @internal */
export declare const PromoteDraftSecurity$inboundSchema: z.ZodType<
  PromoteDraftSecurity,
  z.ZodTypeDef,
  unknown
>;
/** @internal */
export type PromoteDraftSecurity$Outbound = {
  Option1?: PromoteDraftSecurityOption1$Outbound | undefined;
  Option2?: PromoteDraftSecurityOption2$Outbound | undefined;
};
/** @internal */
export declare const PromoteDraftSecurity$outboundSchema: z.ZodType<
  PromoteDraftSecurity$Outbound,
  z.ZodTypeDef,
  PromoteDraftSecurity
>;
/**
 * @internal
 * @deprecated This namespace will be removed in future versions. Use schemas and types that are exported directly from this module.
 */
export declare namespace PromoteDraftSecurity$ {
  /** @deprecated use `PromoteDraftSecurity$inboundSchema` instead. */
  const inboundSchema: z.ZodType<PromoteDraftSecurity, z.ZodTypeDef, unknown>;
  /** @deprecated use `PromoteDraftSecurity$outboundSchema` instead. */
  const outboundSchema: z.ZodType<
    PromoteDraftSecurity$Outbound,
    z.ZodTypeDef,
    PromoteDraftSecurity
  >;
  /** @deprecated use `PromoteDraftSecurity$Outbound` instead. */
  type Outbound = PromoteDraftSecurity$Outbound;
}
export declare function promoteDraftSecurityToJSON(
  promoteDraftSecurity: PromoteDraftSecurity,
): string;
export declare function promoteDraftSecurityFromJSON(
  jsonString: string,
): SafeParseResult<PromoteDraftSecurity, SDKValidationError>;
/** @internal */
export declare const PromoteDraftRequest$inboundSchema: z.ZodType<
  PromoteDraftRequest,
  z.ZodTypeDef,
  unknown
>;
/** @internal */
export type PromoteDraftRequest$Outbound = {
  slug: string;
  "Gram-Session"?: string | undefined;
  "Gram-Key"?: string | undefined;
  "Gram-Project"?: string | undefined;
};
/** @internal */
export declare const PromoteDraftRequest$outboundSchema: z.ZodType<
  PromoteDraftRequest$Outbound,
  z.ZodTypeDef,
  PromoteDraftRequest
>;
/**
 * @internal
 * @deprecated This namespace will be removed in future versions. Use schemas and types that are exported directly from this module.
 */
export declare namespace PromoteDraftRequest$ {
  /** @deprecated use `PromoteDraftRequest$inboundSchema` instead. */
  const inboundSchema: z.ZodType<PromoteDraftRequest, z.ZodTypeDef, unknown>;
  /** @deprecated use `PromoteDraftRequest$outboundSchema` instead. */
  const outboundSchema: z.ZodType<
    PromoteDraftRequest$Outbound,
    z.ZodTypeDef,
    PromoteDraftRequest
  >;
  /** @deprecated use `PromoteDraftRequest$Outbound` instead. */
  type Outbound = PromoteDraftRequest$Outbound;
}
export declare function promoteDraftRequestToJSON(
  promoteDraftRequest: PromoteDraftRequest,
): string;
export declare function promoteDraftRequestFromJSON(
  jsonString: string,
): SafeParseResult<PromoteDraftRequest, SDKValidationError>;
//# sourceMappingURL=promotedraft.d.ts.map
