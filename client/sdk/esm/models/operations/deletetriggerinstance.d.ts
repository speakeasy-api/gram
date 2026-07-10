import * as z from "zod/v4-mini";
export type DeleteTriggerInstanceSecurity = {
    projectSlugHeaderGramProject?: string | undefined;
    sessionHeaderGramSession?: string | undefined;
};
export type DeleteTriggerInstanceRequest = {
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
export type DeleteTriggerInstanceSecurity$Outbound = {
    "project_slug_header_Gram-Project"?: string | undefined;
    "session_header_Gram-Session"?: string | undefined;
};
/** @internal */
export declare const DeleteTriggerInstanceSecurity$outboundSchema: z.ZodMiniType<DeleteTriggerInstanceSecurity$Outbound, DeleteTriggerInstanceSecurity>;
export declare function deleteTriggerInstanceSecurityToJSON(deleteTriggerInstanceSecurity: DeleteTriggerInstanceSecurity): string;
/** @internal */
export type DeleteTriggerInstanceRequest$Outbound = {
    id: string;
    "Gram-Session"?: string | undefined;
    "Gram-Project"?: string | undefined;
};
/** @internal */
export declare const DeleteTriggerInstanceRequest$outboundSchema: z.ZodMiniType<DeleteTriggerInstanceRequest$Outbound, DeleteTriggerInstanceRequest>;
export declare function deleteTriggerInstanceRequestToJSON(deleteTriggerInstanceRequest: DeleteTriggerInstanceRequest): string;
//# sourceMappingURL=deletetriggerinstance.d.ts.map