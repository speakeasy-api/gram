import * as z from "zod";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
export type ListNotificationsSecurity = {
  projectSlugHeaderGramProject?: string | undefined;
  sessionHeaderGramSession?: string | undefined;
};
export type ListNotificationsRequest = {
  /**
   * Filter by archived status. If not provided, returns non-archived notifications.
   */
  archived?: boolean | undefined;
  /**
   * Maximum number of notifications to return
   */
  limit?: number | undefined;
  /**
   * Cursor for pagination (notification ID)
   */
  cursor?: string | undefined;
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
export declare const ListNotificationsSecurity$inboundSchema: z.ZodType<
  ListNotificationsSecurity,
  z.ZodTypeDef,
  unknown
>;
/** @internal */
export type ListNotificationsSecurity$Outbound = {
  "project_slug_header_Gram-Project"?: string | undefined;
  "session_header_Gram-Session"?: string | undefined;
};
/** @internal */
export declare const ListNotificationsSecurity$outboundSchema: z.ZodType<
  ListNotificationsSecurity$Outbound,
  z.ZodTypeDef,
  ListNotificationsSecurity
>;
/**
 * @internal
 * @deprecated This namespace will be removed in future versions. Use schemas and types that are exported directly from this module.
 */
export declare namespace ListNotificationsSecurity$ {
  /** @deprecated use `ListNotificationsSecurity$inboundSchema` instead. */
  const inboundSchema: z.ZodType<
    ListNotificationsSecurity,
    z.ZodTypeDef,
    unknown
  >;
  /** @deprecated use `ListNotificationsSecurity$outboundSchema` instead. */
  const outboundSchema: z.ZodType<
    ListNotificationsSecurity$Outbound,
    z.ZodTypeDef,
    ListNotificationsSecurity
  >;
  /** @deprecated use `ListNotificationsSecurity$Outbound` instead. */
  type Outbound = ListNotificationsSecurity$Outbound;
}
export declare function listNotificationsSecurityToJSON(
  listNotificationsSecurity: ListNotificationsSecurity,
): string;
export declare function listNotificationsSecurityFromJSON(
  jsonString: string,
): SafeParseResult<ListNotificationsSecurity, SDKValidationError>;
/** @internal */
export declare const ListNotificationsRequest$inboundSchema: z.ZodType<
  ListNotificationsRequest,
  z.ZodTypeDef,
  unknown
>;
/** @internal */
export type ListNotificationsRequest$Outbound = {
  archived: boolean;
  limit: number;
  cursor?: string | undefined;
  "Gram-Session"?: string | undefined;
  "Gram-Project"?: string | undefined;
};
/** @internal */
export declare const ListNotificationsRequest$outboundSchema: z.ZodType<
  ListNotificationsRequest$Outbound,
  z.ZodTypeDef,
  ListNotificationsRequest
>;
/**
 * @internal
 * @deprecated This namespace will be removed in future versions. Use schemas and types that are exported directly from this module.
 */
export declare namespace ListNotificationsRequest$ {
  /** @deprecated use `ListNotificationsRequest$inboundSchema` instead. */
  const inboundSchema: z.ZodType<
    ListNotificationsRequest,
    z.ZodTypeDef,
    unknown
  >;
  /** @deprecated use `ListNotificationsRequest$outboundSchema` instead. */
  const outboundSchema: z.ZodType<
    ListNotificationsRequest$Outbound,
    z.ZodTypeDef,
    ListNotificationsRequest
  >;
  /** @deprecated use `ListNotificationsRequest$Outbound` instead. */
  type Outbound = ListNotificationsRequest$Outbound;
}
export declare function listNotificationsRequestToJSON(
  listNotificationsRequest: ListNotificationsRequest,
): string;
export declare function listNotificationsRequestFromJSON(
  jsonString: string,
): SafeParseResult<ListNotificationsRequest, SDKValidationError>;
//# sourceMappingURL=listnotifications.d.ts.map
