import * as z from "zod";
import { Result as SafeParseResult } from "../../types/fp.js";
import * as components from "../components/index.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
export type ArchiveNotificationSecurity = {
  projectSlugHeaderGramProject?: string | undefined;
  sessionHeaderGramSession?: string | undefined;
};
export type ArchiveNotificationRequest = {
  /**
   * Session header
   */
  gramSession?: string | undefined;
  /**
   * project header
   */
  gramProject?: string | undefined;
  archiveNotificationRequestBody: components.ArchiveNotificationRequestBody;
};
/** @internal */
export declare const ArchiveNotificationSecurity$inboundSchema: z.ZodType<
  ArchiveNotificationSecurity,
  z.ZodTypeDef,
  unknown
>;
/** @internal */
export type ArchiveNotificationSecurity$Outbound = {
  "project_slug_header_Gram-Project"?: string | undefined;
  "session_header_Gram-Session"?: string | undefined;
};
/** @internal */
export declare const ArchiveNotificationSecurity$outboundSchema: z.ZodType<
  ArchiveNotificationSecurity$Outbound,
  z.ZodTypeDef,
  ArchiveNotificationSecurity
>;
/**
 * @internal
 * @deprecated This namespace will be removed in future versions. Use schemas and types that are exported directly from this module.
 */
export declare namespace ArchiveNotificationSecurity$ {
  /** @deprecated use `ArchiveNotificationSecurity$inboundSchema` instead. */
  const inboundSchema: z.ZodType<
    ArchiveNotificationSecurity,
    z.ZodTypeDef,
    unknown
  >;
  /** @deprecated use `ArchiveNotificationSecurity$outboundSchema` instead. */
  const outboundSchema: z.ZodType<
    ArchiveNotificationSecurity$Outbound,
    z.ZodTypeDef,
    ArchiveNotificationSecurity
  >;
  /** @deprecated use `ArchiveNotificationSecurity$Outbound` instead. */
  type Outbound = ArchiveNotificationSecurity$Outbound;
}
export declare function archiveNotificationSecurityToJSON(
  archiveNotificationSecurity: ArchiveNotificationSecurity,
): string;
export declare function archiveNotificationSecurityFromJSON(
  jsonString: string,
): SafeParseResult<ArchiveNotificationSecurity, SDKValidationError>;
/** @internal */
export declare const ArchiveNotificationRequest$inboundSchema: z.ZodType<
  ArchiveNotificationRequest,
  z.ZodTypeDef,
  unknown
>;
/** @internal */
export type ArchiveNotificationRequest$Outbound = {
  "Gram-Session"?: string | undefined;
  "Gram-Project"?: string | undefined;
  ArchiveNotificationRequestBody: components.ArchiveNotificationRequestBody$Outbound;
};
/** @internal */
export declare const ArchiveNotificationRequest$outboundSchema: z.ZodType<
  ArchiveNotificationRequest$Outbound,
  z.ZodTypeDef,
  ArchiveNotificationRequest
>;
/**
 * @internal
 * @deprecated This namespace will be removed in future versions. Use schemas and types that are exported directly from this module.
 */
export declare namespace ArchiveNotificationRequest$ {
  /** @deprecated use `ArchiveNotificationRequest$inboundSchema` instead. */
  const inboundSchema: z.ZodType<
    ArchiveNotificationRequest,
    z.ZodTypeDef,
    unknown
  >;
  /** @deprecated use `ArchiveNotificationRequest$outboundSchema` instead. */
  const outboundSchema: z.ZodType<
    ArchiveNotificationRequest$Outbound,
    z.ZodTypeDef,
    ArchiveNotificationRequest
  >;
  /** @deprecated use `ArchiveNotificationRequest$Outbound` instead. */
  type Outbound = ArchiveNotificationRequest$Outbound;
}
export declare function archiveNotificationRequestToJSON(
  archiveNotificationRequest: ArchiveNotificationRequest,
): string;
export declare function archiveNotificationRequestFromJSON(
  jsonString: string,
): SafeParseResult<ArchiveNotificationRequest, SDKValidationError>;
//# sourceMappingURL=archivenotification.d.ts.map
