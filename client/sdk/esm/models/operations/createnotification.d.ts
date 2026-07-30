import * as z from "zod";
import { Result as SafeParseResult } from "../../types/fp.js";
import * as components from "../components/index.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
export type CreateNotificationSecurity = {
  projectSlugHeaderGramProject?: string | undefined;
  sessionHeaderGramSession?: string | undefined;
};
export type CreateNotificationRequest = {
  /**
   * Session header
   */
  gramSession?: string | undefined;
  /**
   * project header
   */
  gramProject?: string | undefined;
  createNotificationForm: components.CreateNotificationForm;
};
/** @internal */
export declare const CreateNotificationSecurity$inboundSchema: z.ZodType<
  CreateNotificationSecurity,
  z.ZodTypeDef,
  unknown
>;
/** @internal */
export type CreateNotificationSecurity$Outbound = {
  "project_slug_header_Gram-Project"?: string | undefined;
  "session_header_Gram-Session"?: string | undefined;
};
/** @internal */
export declare const CreateNotificationSecurity$outboundSchema: z.ZodType<
  CreateNotificationSecurity$Outbound,
  z.ZodTypeDef,
  CreateNotificationSecurity
>;
/**
 * @internal
 * @deprecated This namespace will be removed in future versions. Use schemas and types that are exported directly from this module.
 */
export declare namespace CreateNotificationSecurity$ {
  /** @deprecated use `CreateNotificationSecurity$inboundSchema` instead. */
  const inboundSchema: z.ZodType<
    CreateNotificationSecurity,
    z.ZodTypeDef,
    unknown
  >;
  /** @deprecated use `CreateNotificationSecurity$outboundSchema` instead. */
  const outboundSchema: z.ZodType<
    CreateNotificationSecurity$Outbound,
    z.ZodTypeDef,
    CreateNotificationSecurity
  >;
  /** @deprecated use `CreateNotificationSecurity$Outbound` instead. */
  type Outbound = CreateNotificationSecurity$Outbound;
}
export declare function createNotificationSecurityToJSON(
  createNotificationSecurity: CreateNotificationSecurity,
): string;
export declare function createNotificationSecurityFromJSON(
  jsonString: string,
): SafeParseResult<CreateNotificationSecurity, SDKValidationError>;
/** @internal */
export declare const CreateNotificationRequest$inboundSchema: z.ZodType<
  CreateNotificationRequest,
  z.ZodTypeDef,
  unknown
>;
/** @internal */
export type CreateNotificationRequest$Outbound = {
  "Gram-Session"?: string | undefined;
  "Gram-Project"?: string | undefined;
  CreateNotificationForm: components.CreateNotificationForm$Outbound;
};
/** @internal */
export declare const CreateNotificationRequest$outboundSchema: z.ZodType<
  CreateNotificationRequest$Outbound,
  z.ZodTypeDef,
  CreateNotificationRequest
>;
/**
 * @internal
 * @deprecated This namespace will be removed in future versions. Use schemas and types that are exported directly from this module.
 */
export declare namespace CreateNotificationRequest$ {
  /** @deprecated use `CreateNotificationRequest$inboundSchema` instead. */
  const inboundSchema: z.ZodType<
    CreateNotificationRequest,
    z.ZodTypeDef,
    unknown
  >;
  /** @deprecated use `CreateNotificationRequest$outboundSchema` instead. */
  const outboundSchema: z.ZodType<
    CreateNotificationRequest$Outbound,
    z.ZodTypeDef,
    CreateNotificationRequest
  >;
  /** @deprecated use `CreateNotificationRequest$Outbound` instead. */
  type Outbound = CreateNotificationRequest$Outbound;
}
export declare function createNotificationRequestToJSON(
  createNotificationRequest: CreateNotificationRequest,
): string;
export declare function createNotificationRequestFromJSON(
  jsonString: string,
): SafeParseResult<CreateNotificationRequest, SDKValidationError>;
//# sourceMappingURL=createnotification.d.ts.map
