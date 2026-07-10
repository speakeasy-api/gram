import * as z from "zod/v4-mini";
import { ClosedEnum } from "../../types/enums.js";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
/**
 * The trigger instance status.
 */
export declare const TriggerInstanceStatus: {
    readonly Active: "active";
    readonly Paused: "paused";
    readonly Fired: "fired";
    readonly Cancelled: "cancelled";
};
/**
 * The trigger instance status.
 */
export type TriggerInstanceStatus = ClosedEnum<typeof TriggerInstanceStatus>;
export type TriggerInstance = {
    /**
     * The trigger config payload.
     */
    config: {
        [k: string]: any;
    };
    /**
     * Creation timestamp.
     */
    createdAt: Date;
    /**
     * The trigger definition slug.
     */
    definitionSlug: string;
    /**
     * The linked environment ID.
     */
    environmentId?: string | undefined;
    /**
     * The trigger instance ID.
     */
    id: string;
    /**
     * The trigger instance name.
     */
    name: string;
    /**
     * The project ID owning the trigger instance.
     */
    projectId: string;
    /**
     * The trigger instance status.
     */
    status: TriggerInstanceStatus;
    /**
     * The user-facing target display value.
     */
    targetDisplay: string;
    /**
     * The target kind for the trigger instance.
     */
    targetKind: string;
    /**
     * The opaque target reference.
     */
    targetRef: string;
    /**
     * Last update timestamp.
     */
    updatedAt: Date;
    /**
     * Webhook URL for webhook-backed triggers.
     */
    webhookUrl?: string | undefined;
};
/** @internal */
export declare const TriggerInstanceStatus$inboundSchema: z.ZodMiniEnum<typeof TriggerInstanceStatus>;
/** @internal */
export declare const TriggerInstance$inboundSchema: z.ZodMiniType<TriggerInstance, unknown>;
export declare function triggerInstanceFromJSON(jsonString: string): SafeParseResult<TriggerInstance, SDKValidationError>;
//# sourceMappingURL=triggerinstance.d.ts.map