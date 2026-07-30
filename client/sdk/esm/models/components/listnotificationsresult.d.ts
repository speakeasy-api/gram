import * as z from "zod";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
import { Notification, Notification$Outbound } from "./notification.js";
/**
 * Result type for listing notifications
 */
export type ListNotificationsResult = {
  /**
   * Cursor for the next page of results
   */
  nextCursor?: string | undefined;
  /**
   * The list of notifications
   */
  notifications: Array<Notification>;
};
/** @internal */
export declare const ListNotificationsResult$inboundSchema: z.ZodType<
  ListNotificationsResult,
  z.ZodTypeDef,
  unknown
>;
/** @internal */
export type ListNotificationsResult$Outbound = {
  nextCursor?: string | undefined;
  notifications: Array<Notification$Outbound>;
};
/** @internal */
export declare const ListNotificationsResult$outboundSchema: z.ZodType<
  ListNotificationsResult$Outbound,
  z.ZodTypeDef,
  ListNotificationsResult
>;
/**
 * @internal
 * @deprecated This namespace will be removed in future versions. Use schemas and types that are exported directly from this module.
 */
export declare namespace ListNotificationsResult$ {
  /** @deprecated use `ListNotificationsResult$inboundSchema` instead. */
  const inboundSchema: z.ZodType<
    ListNotificationsResult,
    z.ZodTypeDef,
    unknown
  >;
  /** @deprecated use `ListNotificationsResult$outboundSchema` instead. */
  const outboundSchema: z.ZodType<
    ListNotificationsResult$Outbound,
    z.ZodTypeDef,
    ListNotificationsResult
  >;
  /** @deprecated use `ListNotificationsResult$Outbound` instead. */
  type Outbound = ListNotificationsResult$Outbound;
}
export declare function listNotificationsResultToJSON(
  listNotificationsResult: ListNotificationsResult,
): string;
export declare function listNotificationsResultFromJSON(
  jsonString: string,
): SafeParseResult<ListNotificationsResult, SDKValidationError>;
//# sourceMappingURL=listnotificationsresult.d.ts.map
