import * as z from "zod/v4-mini";
/**
 * Local agent notification payload.
 */
export type HookNotificationData = {
    /**
     * Notification message.
     */
    message?: string | undefined;
    /**
     * Notification title.
     */
    title?: string | undefined;
    /**
     * Notification type.
     */
    type?: string | undefined;
};
/** @internal */
export type HookNotificationData$Outbound = {
    message?: string | undefined;
    title?: string | undefined;
    type?: string | undefined;
};
/** @internal */
export declare const HookNotificationData$outboundSchema: z.ZodMiniType<HookNotificationData$Outbound, HookNotificationData>;
export declare function hookNotificationDataToJSON(hookNotificationData: HookNotificationData): string;
//# sourceMappingURL=hooknotificationdata.d.ts.map