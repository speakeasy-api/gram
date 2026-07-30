import * as z from "zod";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
export type ArchiveNotificationRequestBody = {
  /**
   * The notification ID
   */
  id: string;
};
/** @internal */
export declare const ArchiveNotificationRequestBody$inboundSchema: z.ZodType<
  ArchiveNotificationRequestBody,
  z.ZodTypeDef,
  unknown
>;
/** @internal */
export type ArchiveNotificationRequestBody$Outbound = {
  id: string;
};
/** @internal */
export declare const ArchiveNotificationRequestBody$outboundSchema: z.ZodType<
  ArchiveNotificationRequestBody$Outbound,
  z.ZodTypeDef,
  ArchiveNotificationRequestBody
>;
/**
 * @internal
 * @deprecated This namespace will be removed in future versions. Use schemas and types that are exported directly from this module.
 */
export declare namespace ArchiveNotificationRequestBody$ {
  /** @deprecated use `ArchiveNotificationRequestBody$inboundSchema` instead. */
  const inboundSchema: z.ZodType<
    ArchiveNotificationRequestBody,
    z.ZodTypeDef,
    unknown
  >;
  /** @deprecated use `ArchiveNotificationRequestBody$outboundSchema` instead. */
  const outboundSchema: z.ZodType<
    ArchiveNotificationRequestBody$Outbound,
    z.ZodTypeDef,
    ArchiveNotificationRequestBody
  >;
  /** @deprecated use `ArchiveNotificationRequestBody$Outbound` instead. */
  type Outbound = ArchiveNotificationRequestBody$Outbound;
}
export declare function archiveNotificationRequestBodyToJSON(
  archiveNotificationRequestBody: ArchiveNotificationRequestBody,
): string;
export declare function archiveNotificationRequestBodyFromJSON(
  jsonString: string,
): SafeParseResult<ArchiveNotificationRequestBody, SDKValidationError>;
//# sourceMappingURL=archivenotificationrequestbody.d.ts.map
