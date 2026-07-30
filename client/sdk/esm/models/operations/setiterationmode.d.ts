import * as z from "zod";
import { Result as SafeParseResult } from "../../types/fp.js";
import * as components from "../components/index.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
export type SetIterationModeSecurityOption1 = {
  projectSlugHeaderGramProject: string;
  sessionHeaderGramSession: string;
};
export type SetIterationModeSecurityOption2 = {
  apikeyHeaderGramKey: string;
  projectSlugHeaderGramProject: string;
};
export type SetIterationModeSecurity = {
  option1?: SetIterationModeSecurityOption1 | undefined;
  option2?: SetIterationModeSecurityOption2 | undefined;
};
export type SetIterationModeRequest = {
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
  setIterationModeRequestBody: components.SetIterationModeRequestBody;
};
/** @internal */
export declare const SetIterationModeSecurityOption1$inboundSchema: z.ZodType<
  SetIterationModeSecurityOption1,
  z.ZodTypeDef,
  unknown
>;
/** @internal */
export type SetIterationModeSecurityOption1$Outbound = {
  "project_slug_header_Gram-Project": string;
  "session_header_Gram-Session": string;
};
/** @internal */
export declare const SetIterationModeSecurityOption1$outboundSchema: z.ZodType<
  SetIterationModeSecurityOption1$Outbound,
  z.ZodTypeDef,
  SetIterationModeSecurityOption1
>;
/**
 * @internal
 * @deprecated This namespace will be removed in future versions. Use schemas and types that are exported directly from this module.
 */
export declare namespace SetIterationModeSecurityOption1$ {
  /** @deprecated use `SetIterationModeSecurityOption1$inboundSchema` instead. */
  const inboundSchema: z.ZodType<
    SetIterationModeSecurityOption1,
    z.ZodTypeDef,
    unknown
  >;
  /** @deprecated use `SetIterationModeSecurityOption1$outboundSchema` instead. */
  const outboundSchema: z.ZodType<
    SetIterationModeSecurityOption1$Outbound,
    z.ZodTypeDef,
    SetIterationModeSecurityOption1
  >;
  /** @deprecated use `SetIterationModeSecurityOption1$Outbound` instead. */
  type Outbound = SetIterationModeSecurityOption1$Outbound;
}
export declare function setIterationModeSecurityOption1ToJSON(
  setIterationModeSecurityOption1: SetIterationModeSecurityOption1,
): string;
export declare function setIterationModeSecurityOption1FromJSON(
  jsonString: string,
): SafeParseResult<SetIterationModeSecurityOption1, SDKValidationError>;
/** @internal */
export declare const SetIterationModeSecurityOption2$inboundSchema: z.ZodType<
  SetIterationModeSecurityOption2,
  z.ZodTypeDef,
  unknown
>;
/** @internal */
export type SetIterationModeSecurityOption2$Outbound = {
  "apikey_header_Gram-Key": string;
  "project_slug_header_Gram-Project": string;
};
/** @internal */
export declare const SetIterationModeSecurityOption2$outboundSchema: z.ZodType<
  SetIterationModeSecurityOption2$Outbound,
  z.ZodTypeDef,
  SetIterationModeSecurityOption2
>;
/**
 * @internal
 * @deprecated This namespace will be removed in future versions. Use schemas and types that are exported directly from this module.
 */
export declare namespace SetIterationModeSecurityOption2$ {
  /** @deprecated use `SetIterationModeSecurityOption2$inboundSchema` instead. */
  const inboundSchema: z.ZodType<
    SetIterationModeSecurityOption2,
    z.ZodTypeDef,
    unknown
  >;
  /** @deprecated use `SetIterationModeSecurityOption2$outboundSchema` instead. */
  const outboundSchema: z.ZodType<
    SetIterationModeSecurityOption2$Outbound,
    z.ZodTypeDef,
    SetIterationModeSecurityOption2
  >;
  /** @deprecated use `SetIterationModeSecurityOption2$Outbound` instead. */
  type Outbound = SetIterationModeSecurityOption2$Outbound;
}
export declare function setIterationModeSecurityOption2ToJSON(
  setIterationModeSecurityOption2: SetIterationModeSecurityOption2,
): string;
export declare function setIterationModeSecurityOption2FromJSON(
  jsonString: string,
): SafeParseResult<SetIterationModeSecurityOption2, SDKValidationError>;
/** @internal */
export declare const SetIterationModeSecurity$inboundSchema: z.ZodType<
  SetIterationModeSecurity,
  z.ZodTypeDef,
  unknown
>;
/** @internal */
export type SetIterationModeSecurity$Outbound = {
  Option1?: SetIterationModeSecurityOption1$Outbound | undefined;
  Option2?: SetIterationModeSecurityOption2$Outbound | undefined;
};
/** @internal */
export declare const SetIterationModeSecurity$outboundSchema: z.ZodType<
  SetIterationModeSecurity$Outbound,
  z.ZodTypeDef,
  SetIterationModeSecurity
>;
/**
 * @internal
 * @deprecated This namespace will be removed in future versions. Use schemas and types that are exported directly from this module.
 */
export declare namespace SetIterationModeSecurity$ {
  /** @deprecated use `SetIterationModeSecurity$inboundSchema` instead. */
  const inboundSchema: z.ZodType<
    SetIterationModeSecurity,
    z.ZodTypeDef,
    unknown
  >;
  /** @deprecated use `SetIterationModeSecurity$outboundSchema` instead. */
  const outboundSchema: z.ZodType<
    SetIterationModeSecurity$Outbound,
    z.ZodTypeDef,
    SetIterationModeSecurity
  >;
  /** @deprecated use `SetIterationModeSecurity$Outbound` instead. */
  type Outbound = SetIterationModeSecurity$Outbound;
}
export declare function setIterationModeSecurityToJSON(
  setIterationModeSecurity: SetIterationModeSecurity,
): string;
export declare function setIterationModeSecurityFromJSON(
  jsonString: string,
): SafeParseResult<SetIterationModeSecurity, SDKValidationError>;
/** @internal */
export declare const SetIterationModeRequest$inboundSchema: z.ZodType<
  SetIterationModeRequest,
  z.ZodTypeDef,
  unknown
>;
/** @internal */
export type SetIterationModeRequest$Outbound = {
  slug: string;
  "Gram-Session"?: string | undefined;
  "Gram-Key"?: string | undefined;
  "Gram-Project"?: string | undefined;
  SetIterationModeRequestBody: components.SetIterationModeRequestBody$Outbound;
};
/** @internal */
export declare const SetIterationModeRequest$outboundSchema: z.ZodType<
  SetIterationModeRequest$Outbound,
  z.ZodTypeDef,
  SetIterationModeRequest
>;
/**
 * @internal
 * @deprecated This namespace will be removed in future versions. Use schemas and types that are exported directly from this module.
 */
export declare namespace SetIterationModeRequest$ {
  /** @deprecated use `SetIterationModeRequest$inboundSchema` instead. */
  const inboundSchema: z.ZodType<
    SetIterationModeRequest,
    z.ZodTypeDef,
    unknown
  >;
  /** @deprecated use `SetIterationModeRequest$outboundSchema` instead. */
  const outboundSchema: z.ZodType<
    SetIterationModeRequest$Outbound,
    z.ZodTypeDef,
    SetIterationModeRequest
  >;
  /** @deprecated use `SetIterationModeRequest$Outbound` instead. */
  type Outbound = SetIterationModeRequest$Outbound;
}
export declare function setIterationModeRequestToJSON(
  setIterationModeRequest: SetIterationModeRequest,
): string;
export declare function setIterationModeRequestFromJSON(
  jsonString: string,
): SafeParseResult<SetIterationModeRequest, SDKValidationError>;
//# sourceMappingURL=setiterationmode.d.ts.map
