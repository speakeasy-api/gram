import * as z from "zod";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
export type DiscardDraftSecurityOption1 = {
  projectSlugHeaderGramProject: string;
  sessionHeaderGramSession: string;
};
export type DiscardDraftSecurityOption2 = {
  apikeyHeaderGramKey: string;
  projectSlugHeaderGramProject: string;
};
export type DiscardDraftSecurity = {
  option1?: DiscardDraftSecurityOption1 | undefined;
  option2?: DiscardDraftSecurityOption2 | undefined;
};
export type DiscardDraftRequest = {
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
export declare const DiscardDraftSecurityOption1$inboundSchema: z.ZodType<
  DiscardDraftSecurityOption1,
  z.ZodTypeDef,
  unknown
>;
/** @internal */
export type DiscardDraftSecurityOption1$Outbound = {
  "project_slug_header_Gram-Project": string;
  "session_header_Gram-Session": string;
};
/** @internal */
export declare const DiscardDraftSecurityOption1$outboundSchema: z.ZodType<
  DiscardDraftSecurityOption1$Outbound,
  z.ZodTypeDef,
  DiscardDraftSecurityOption1
>;
/**
 * @internal
 * @deprecated This namespace will be removed in future versions. Use schemas and types that are exported directly from this module.
 */
export declare namespace DiscardDraftSecurityOption1$ {
  /** @deprecated use `DiscardDraftSecurityOption1$inboundSchema` instead. */
  const inboundSchema: z.ZodType<
    DiscardDraftSecurityOption1,
    z.ZodTypeDef,
    unknown
  >;
  /** @deprecated use `DiscardDraftSecurityOption1$outboundSchema` instead. */
  const outboundSchema: z.ZodType<
    DiscardDraftSecurityOption1$Outbound,
    z.ZodTypeDef,
    DiscardDraftSecurityOption1
  >;
  /** @deprecated use `DiscardDraftSecurityOption1$Outbound` instead. */
  type Outbound = DiscardDraftSecurityOption1$Outbound;
}
export declare function discardDraftSecurityOption1ToJSON(
  discardDraftSecurityOption1: DiscardDraftSecurityOption1,
): string;
export declare function discardDraftSecurityOption1FromJSON(
  jsonString: string,
): SafeParseResult<DiscardDraftSecurityOption1, SDKValidationError>;
/** @internal */
export declare const DiscardDraftSecurityOption2$inboundSchema: z.ZodType<
  DiscardDraftSecurityOption2,
  z.ZodTypeDef,
  unknown
>;
/** @internal */
export type DiscardDraftSecurityOption2$Outbound = {
  "apikey_header_Gram-Key": string;
  "project_slug_header_Gram-Project": string;
};
/** @internal */
export declare const DiscardDraftSecurityOption2$outboundSchema: z.ZodType<
  DiscardDraftSecurityOption2$Outbound,
  z.ZodTypeDef,
  DiscardDraftSecurityOption2
>;
/**
 * @internal
 * @deprecated This namespace will be removed in future versions. Use schemas and types that are exported directly from this module.
 */
export declare namespace DiscardDraftSecurityOption2$ {
  /** @deprecated use `DiscardDraftSecurityOption2$inboundSchema` instead. */
  const inboundSchema: z.ZodType<
    DiscardDraftSecurityOption2,
    z.ZodTypeDef,
    unknown
  >;
  /** @deprecated use `DiscardDraftSecurityOption2$outboundSchema` instead. */
  const outboundSchema: z.ZodType<
    DiscardDraftSecurityOption2$Outbound,
    z.ZodTypeDef,
    DiscardDraftSecurityOption2
  >;
  /** @deprecated use `DiscardDraftSecurityOption2$Outbound` instead. */
  type Outbound = DiscardDraftSecurityOption2$Outbound;
}
export declare function discardDraftSecurityOption2ToJSON(
  discardDraftSecurityOption2: DiscardDraftSecurityOption2,
): string;
export declare function discardDraftSecurityOption2FromJSON(
  jsonString: string,
): SafeParseResult<DiscardDraftSecurityOption2, SDKValidationError>;
/** @internal */
export declare const DiscardDraftSecurity$inboundSchema: z.ZodType<
  DiscardDraftSecurity,
  z.ZodTypeDef,
  unknown
>;
/** @internal */
export type DiscardDraftSecurity$Outbound = {
  Option1?: DiscardDraftSecurityOption1$Outbound | undefined;
  Option2?: DiscardDraftSecurityOption2$Outbound | undefined;
};
/** @internal */
export declare const DiscardDraftSecurity$outboundSchema: z.ZodType<
  DiscardDraftSecurity$Outbound,
  z.ZodTypeDef,
  DiscardDraftSecurity
>;
/**
 * @internal
 * @deprecated This namespace will be removed in future versions. Use schemas and types that are exported directly from this module.
 */
export declare namespace DiscardDraftSecurity$ {
  /** @deprecated use `DiscardDraftSecurity$inboundSchema` instead. */
  const inboundSchema: z.ZodType<DiscardDraftSecurity, z.ZodTypeDef, unknown>;
  /** @deprecated use `DiscardDraftSecurity$outboundSchema` instead. */
  const outboundSchema: z.ZodType<
    DiscardDraftSecurity$Outbound,
    z.ZodTypeDef,
    DiscardDraftSecurity
  >;
  /** @deprecated use `DiscardDraftSecurity$Outbound` instead. */
  type Outbound = DiscardDraftSecurity$Outbound;
}
export declare function discardDraftSecurityToJSON(
  discardDraftSecurity: DiscardDraftSecurity,
): string;
export declare function discardDraftSecurityFromJSON(
  jsonString: string,
): SafeParseResult<DiscardDraftSecurity, SDKValidationError>;
/** @internal */
export declare const DiscardDraftRequest$inboundSchema: z.ZodType<
  DiscardDraftRequest,
  z.ZodTypeDef,
  unknown
>;
/** @internal */
export type DiscardDraftRequest$Outbound = {
  slug: string;
  "Gram-Session"?: string | undefined;
  "Gram-Key"?: string | undefined;
  "Gram-Project"?: string | undefined;
};
/** @internal */
export declare const DiscardDraftRequest$outboundSchema: z.ZodType<
  DiscardDraftRequest$Outbound,
  z.ZodTypeDef,
  DiscardDraftRequest
>;
/**
 * @internal
 * @deprecated This namespace will be removed in future versions. Use schemas and types that are exported directly from this module.
 */
export declare namespace DiscardDraftRequest$ {
  /** @deprecated use `DiscardDraftRequest$inboundSchema` instead. */
  const inboundSchema: z.ZodType<DiscardDraftRequest, z.ZodTypeDef, unknown>;
  /** @deprecated use `DiscardDraftRequest$outboundSchema` instead. */
  const outboundSchema: z.ZodType<
    DiscardDraftRequest$Outbound,
    z.ZodTypeDef,
    DiscardDraftRequest
  >;
  /** @deprecated use `DiscardDraftRequest$Outbound` instead. */
  type Outbound = DiscardDraftRequest$Outbound;
}
export declare function discardDraftRequestToJSON(
  discardDraftRequest: DiscardDraftRequest,
): string;
export declare function discardDraftRequestFromJSON(
  jsonString: string,
): SafeParseResult<DiscardDraftRequest, SDKValidationError>;
//# sourceMappingURL=discarddraft.d.ts.map
