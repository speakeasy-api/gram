import * as z from "zod/v4-mini";
export type PauseTriggerInstanceSecurity = {
    projectSlugHeaderGramProject?: string | undefined;
    sessionHeaderGramSession?: string | undefined;
};
export type PauseTriggerInstanceRequest = {
    /**
     * The trigger instance ID.
     */
    id: string;
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
export type PauseTriggerInstanceSecurity$Outbound = {
    "project_slug_header_Gram-Project"?: string | undefined;
    "session_header_Gram-Session"?: string | undefined;
};
/** @internal */
export declare const PauseTriggerInstanceSecurity$outboundSchema: z.ZodMiniType<PauseTriggerInstanceSecurity$Outbound, PauseTriggerInstanceSecurity>;
export declare function pauseTriggerInstanceSecurityToJSON(pauseTriggerInstanceSecurity: PauseTriggerInstanceSecurity): string;
/** @internal */
export type PauseTriggerInstanceRequest$Outbound = {
    id: string;
    "Gram-Session"?: string | undefined;
    "Gram-Project"?: string | undefined;
};
/** @internal */
export declare const PauseTriggerInstanceRequest$outboundSchema: z.ZodMiniType<PauseTriggerInstanceRequest$Outbound, PauseTriggerInstanceRequest>;
export declare function pauseTriggerInstanceRequestToJSON(pauseTriggerInstanceRequest: PauseTriggerInstanceRequest): string;
//# sourceMappingURL=pausetriggerinstance.d.ts.map