import * as z from "zod/v4-mini";
import { ClosedEnum } from "../../types/enums.js";
/**
 * Optional initial status.
 */
export declare const CreateTriggerInstanceFormStatus: {
    readonly Active: "active";
    readonly Paused: "paused";
};
/**
 * Optional initial status.
 */
export type CreateTriggerInstanceFormStatus = ClosedEnum<typeof CreateTriggerInstanceFormStatus>;
/**
 * The trigger target kind.
 */
export declare const CreateTriggerInstanceFormTargetKind: {
    readonly Assistant: "assistant";
    readonly Noop: "noop";
};
/**
 * The trigger target kind.
 */
export type CreateTriggerInstanceFormTargetKind = ClosedEnum<typeof CreateTriggerInstanceFormTargetKind>;
export type CreateTriggerInstanceForm = {
    /**
     * The trigger config payload.
     */
    config: {
        [k: string]: any;
    };
    /**
     * The trigger definition slug.
     */
    definitionSlug: string;
    /**
     * The linked environment ID.
     */
    environmentId?: string | undefined;
    /**
     * The trigger instance name.
     */
    name: string;
    /**
     * Optional initial status.
     */
    status?: CreateTriggerInstanceFormStatus | undefined;
    /**
     * The user-facing target display value.
     */
    targetDisplay: string;
    /**
     * The trigger target kind.
     */
    targetKind: CreateTriggerInstanceFormTargetKind;
    /**
     * The opaque target reference.
     */
    targetRef: string;
};
/** @internal */
export declare const CreateTriggerInstanceFormStatus$outboundSchema: z.ZodMiniEnum<typeof CreateTriggerInstanceFormStatus>;
/** @internal */
export declare const CreateTriggerInstanceFormTargetKind$outboundSchema: z.ZodMiniEnum<typeof CreateTriggerInstanceFormTargetKind>;
/** @internal */
export type CreateTriggerInstanceForm$Outbound = {
    config: {
        [k: string]: any;
    };
    definition_slug: string;
    environment_id?: string | undefined;
    name: string;
    status?: string | undefined;
    target_display: string;
    target_kind: string;
    target_ref: string;
};
/** @internal */
export declare const CreateTriggerInstanceForm$outboundSchema: z.ZodMiniType<CreateTriggerInstanceForm$Outbound, CreateTriggerInstanceForm>;
export declare function createTriggerInstanceFormToJSON(createTriggerInstanceForm: CreateTriggerInstanceForm): string;
//# sourceMappingURL=createtriggerinstanceform.d.ts.map