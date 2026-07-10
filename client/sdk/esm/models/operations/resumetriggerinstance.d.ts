import * as z from "zod/v4-mini";
export type ResumeTriggerInstanceSecurity = {
    projectSlugHeaderGramProject?: string | undefined;
    sessionHeaderGramSession?: string | undefined;
};
export type ResumeTriggerInstanceRequest = {
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
export type ResumeTriggerInstanceSecurity$Outbound = {
    "project_slug_header_Gram-Project"?: string | undefined;
    "session_header_Gram-Session"?: string | undefined;
};
/** @internal */
export declare const ResumeTriggerInstanceSecurity$outboundSchema: z.ZodMiniType<ResumeTriggerInstanceSecurity$Outbound, ResumeTriggerInstanceSecurity>;
export declare function resumeTriggerInstanceSecurityToJSON(resumeTriggerInstanceSecurity: ResumeTriggerInstanceSecurity): string;
/** @internal */
export type ResumeTriggerInstanceRequest$Outbound = {
    id: string;
    "Gram-Session"?: string | undefined;
    "Gram-Project"?: string | undefined;
};
/** @internal */
export declare const ResumeTriggerInstanceRequest$outboundSchema: z.ZodMiniType<ResumeTriggerInstanceRequest$Outbound, ResumeTriggerInstanceRequest>;
export declare function resumeTriggerInstanceRequestToJSON(resumeTriggerInstanceRequest: ResumeTriggerInstanceRequest): string;
//# sourceMappingURL=resumetriggerinstance.d.ts.map