import * as z from "zod";
import { ClosedEnum } from "../../types/enums.js";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
/**
 * The severity level of the notification
 */
export declare const Level: {
  readonly Info: "info";
  readonly Success: "success";
  readonly Warning: "warning";
  readonly Error: "error";
};
/**
 * The severity level of the notification
 */
export type Level = ClosedEnum<typeof Level>;
/**
 * The type of notification
 */
export declare const Type: {
  readonly System: "system";
  readonly UserAction: "user_action";
};
/**
 * The type of notification
 */
export type Type = ClosedEnum<typeof Type>;
/**
 * A notification in the system
 */
export type Notification = {
  /**
   * The user ID of the actor who triggered the notification
   */
  actorUserId?: string | undefined;
  /**
   * When the notification was archived
   */
  archivedAt?: Date | undefined;
  /**
   * When the notification was created
   */
  createdAt: Date;
  /**
   * The notification ID
   */
  id: string;
  /**
   * The severity level of the notification
   */
  level: Level;
  /**
   * The notification message
   */
  message?: string | undefined;
  /**
   * The project ID
   */
  projectId: string;
  /**
   * The ID of the resource this notification relates to
   */
  resourceId?: string | undefined;
  /**
   * The type of resource this notification relates to
   */
  resourceType?: string | undefined;
  /**
   * The notification title
   */
  title: string;
  /**
   * The type of notification
   */
  type: Type;
};
/** @internal */
export declare const Level$inboundSchema: z.ZodNativeEnum<typeof Level>;
/** @internal */
export declare const Level$outboundSchema: z.ZodNativeEnum<typeof Level>;
/**
 * @internal
 * @deprecated This namespace will be removed in future versions. Use schemas and types that are exported directly from this module.
 */
export declare namespace Level$ {
  /** @deprecated use `Level$inboundSchema` instead. */
  const inboundSchema: z.ZodNativeEnum<{
    readonly Info: "info";
    readonly Success: "success";
    readonly Warning: "warning";
    readonly Error: "error";
  }>;
  /** @deprecated use `Level$outboundSchema` instead. */
  const outboundSchema: z.ZodNativeEnum<{
    readonly Info: "info";
    readonly Success: "success";
    readonly Warning: "warning";
    readonly Error: "error";
  }>;
}
/** @internal */
export declare const Type$inboundSchema: z.ZodNativeEnum<typeof Type>;
/** @internal */
export declare const Type$outboundSchema: z.ZodNativeEnum<typeof Type>;
/**
 * @internal
 * @deprecated This namespace will be removed in future versions. Use schemas and types that are exported directly from this module.
 */
export declare namespace Type$ {
  /** @deprecated use `Type$inboundSchema` instead. */
  const inboundSchema: z.ZodNativeEnum<{
    readonly System: "system";
    readonly UserAction: "user_action";
  }>;
  /** @deprecated use `Type$outboundSchema` instead. */
  const outboundSchema: z.ZodNativeEnum<{
    readonly System: "system";
    readonly UserAction: "user_action";
  }>;
}
/** @internal */
export declare const Notification$inboundSchema: z.ZodType<
  Notification,
  z.ZodTypeDef,
  unknown
>;
/** @internal */
export type Notification$Outbound = {
  actorUserId?: string | undefined;
  archivedAt?: string | undefined;
  createdAt: string;
  id: string;
  level: string;
  message?: string | undefined;
  projectId: string;
  resourceId?: string | undefined;
  resourceType?: string | undefined;
  title: string;
  type: string;
};
/** @internal */
export declare const Notification$outboundSchema: z.ZodType<
  Notification$Outbound,
  z.ZodTypeDef,
  Notification
>;
/**
 * @internal
 * @deprecated This namespace will be removed in future versions. Use schemas and types that are exported directly from this module.
 */
export declare namespace Notification$ {
  /** @deprecated use `Notification$inboundSchema` instead. */
  const inboundSchema: z.ZodType<Notification, z.ZodTypeDef, unknown>;
  /** @deprecated use `Notification$outboundSchema` instead. */
  const outboundSchema: z.ZodType<
    Notification$Outbound,
    z.ZodTypeDef,
    Notification
  >;
  /** @deprecated use `Notification$Outbound` instead. */
  type Outbound = Notification$Outbound;
}
export declare function notificationToJSON(notification: Notification): string;
export declare function notificationFromJSON(
  jsonString: string,
): SafeParseResult<Notification, SDKValidationError>;
//# sourceMappingURL=notification.d.ts.map
