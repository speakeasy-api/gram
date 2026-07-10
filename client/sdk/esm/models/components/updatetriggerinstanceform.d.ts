import * as z from "zod/v4-mini";
import { ClosedEnum } from "../../types/enums.js";
/**
 * The trigger status.
 */
export declare const UpdateTriggerInstanceFormStatus: {
    readonly Active: "active";
    readonly Paused: "paused";
};
/**
 * The trigger status.
 */
export type UpdateTriggerInstanceFormStatus = ClosedEnum<typeof UpdateTriggerInstanceFormStatus>;
/**
 * The trigger target kind.
 */
export declare const UpdateTriggerInstanceFormTargetKind: {
    readonly Assistant: "assistant";
    readonly Noop: "noop";
};
/**
 * The trigger target kind.
 */
export type UpdateTriggerInstanceFormTargetKind = ClosedEnum<typeof UpdateTriggerInstanceFormTargetKind>;
export type UpdateTriggerInstanceForm = {
    /**
     * The trigger config payload.
     */
    config?: {
        [k: string]: any;
    } | undefined;
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
    name?: string | undefined;
    /**
     * The trigger status.
     */
    status?: UpdateTriggerInstanceFormStatus | undefined;
    /**
     * The user-facing target display value.
     */
    targetDisplay?: string | undefined;
    /**
     * The trigger target kind.
     */
    targetKind?: UpdateTriggerInstanceFormTargetKind | undefined;
    /**
     * The opaque target reference.
     */
    targetRef?: string | undefined;
};
/** @internal */
export declare const UpdateTriggerInstanceFormStatus$outboundSchema: z.ZodMiniEnum<typeof UpdateTriggerInstanceFormStatus>;
/** @internal */
export declare const UpdateTriggerInstanceFormTargetKind$outboundSchema: z.ZodMiniEnum<typeof UpdateTriggerInstanceFormTargetKind>;
/** @internal */
export type UpdateTriggerInstanceForm$Outbound = {
    config?: {
        [k: string]: any;
    } | undefined;
    environment_id?: string | undefined;
    id: string;
    name?: string | undefined;
    status?: string | undefined;
    target_display?: string | undefined;
    target_kind?: string | undefined;
    target_ref?: string | undefined;
};
/** @internal */
export declare const UpdateTriggerInstanceForm$outboundSchema: z.ZodMiniType<UpdateTriggerInstanceForm$Outbound, UpdateTriggerInstanceForm>;
export declare function updateTriggerInstanceFormToJSON(updateTriggerInstanceForm: UpdateTriggerInstanceForm): string;
//# sourceMappingURL=updatetriggerinstanceform.d.ts.map