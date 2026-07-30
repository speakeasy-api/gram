import * as z from "zod";
import { ClosedEnum } from "../../types/enums.js";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
/**
 * The severity level of the notification
 */
export declare const CreateNotificationFormLevel: {
  readonly Info: "info";
  readonly Success: "success";
  readonly Warning: "warning";
  readonly Error: "error";
};
/**
 * The severity level of the notification
 */
export type CreateNotificationFormLevel = ClosedEnum<
  typeof CreateNotificationFormLevel
>;
/**
 * The type of notification
 */
export declare const CreateNotificationFormType: {
  readonly System: "system";
  readonly UserAction: "user_action";
};
/**
 * The type of notification
 */
export type CreateNotificationFormType = ClosedEnum<
  typeof CreateNotificationFormType
>;
/**
 * Form for creating a new notification
 */
export type CreateNotificationForm = {
  /**
   * The severity level of the notification
   */
  level: CreateNotificationFormLevel;
  /**
   * The notification message
   */
  message?: string | undefined;
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
  type: CreateNotificationFormType;
};
/** @internal */
export declare const CreateNotificationFormLevel$inboundSchema: z.ZodNativeEnum<
  typeof CreateNotificationFormLevel
>;
/** @internal */
export declare const CreateNotificationFormLevel$outboundSchema: z.ZodNativeEnum<
  typeof CreateNotificationFormLevel
>;
/**
 * @internal
 * @deprecated This namespace will be removed in future versions. Use schemas and types that are exported directly from this module.
 */
export declare namespace CreateNotificationFormLevel$ {
  /** @deprecated use `CreateNotificationFormLevel$inboundSchema` instead. */
  const inboundSchema: z.ZodNativeEnum<{
    readonly Info: "info";
    readonly Success: "success";
    readonly Warning: "warning";
    readonly Error: "error";
  }>;
  /** @deprecated use `CreateNotificationFormLevel$outboundSchema` instead. */
  const outboundSchema: z.ZodNativeEnum<{
    readonly Info: "info";
    readonly Success: "success";
    readonly Warning: "warning";
    readonly Error: "error";
  }>;
}
/** @internal */
export declare const CreateNotificationFormType$inboundSchema: z.ZodNativeEnum<
  typeof CreateNotificationFormType
>;
/** @internal */
export declare const CreateNotificationFormType$outboundSchema: z.ZodNativeEnum<
  typeof CreateNotificationFormType
>;
/**
 * @internal
 * @deprecated This namespace will be removed in future versions. Use schemas and types that are exported directly from this module.
 */
export declare namespace CreateNotificationFormType$ {
  /** @deprecated use `CreateNotificationFormType$inboundSchema` instead. */
  const inboundSchema: z.ZodNativeEnum<{
    readonly System: "system";
    readonly UserAction: "user_action";
  }>;
  /** @deprecated use `CreateNotificationFormType$outboundSchema` instead. */
  const outboundSchema: z.ZodNativeEnum<{
    readonly System: "system";
    readonly UserAction: "user_action";
  }>;
}
/** @internal */
export declare const CreateNotificationForm$inboundSchema: z.ZodType<
  CreateNotificationForm,
  z.ZodTypeDef,
  unknown
>;
/** @internal */
export type CreateNotificationForm$Outbound = {
  level: string;
  message?: string | undefined;
  resourceId?: string | undefined;
  resourceType?: string | undefined;
  title: string;
  type: string;
};
/** @internal */
export declare const CreateNotificationForm$outboundSchema: z.ZodType<
  CreateNotificationForm$Outbound,
  z.ZodTypeDef,
  CreateNotificationForm
>;
/**
 * @internal
 * @deprecated This namespace will be removed in future versions. Use schemas and types that are exported directly from this module.
 */
export declare namespace CreateNotificationForm$ {
  /** @deprecated use `CreateNotificationForm$inboundSchema` instead. */
  const inboundSchema: z.ZodType<CreateNotificationForm, z.ZodTypeDef, unknown>;
  /** @deprecated use `CreateNotificationForm$outboundSchema` instead. */
  const outboundSchema: z.ZodType<
    CreateNotificationForm$Outbound,
    z.ZodTypeDef,
    CreateNotificationForm
  >;
  /** @deprecated use `CreateNotificationForm$Outbound` instead. */
  type Outbound = CreateNotificationForm$Outbound;
}
export declare function createNotificationFormToJSON(
  createNotificationForm: CreateNotificationForm,
): string;
export declare function createNotificationFormFromJSON(
  jsonString: string,
): SafeParseResult<CreateNotificationForm, SDKValidationError>;
//# sourceMappingURL=createnotificationform.d.ts.map
