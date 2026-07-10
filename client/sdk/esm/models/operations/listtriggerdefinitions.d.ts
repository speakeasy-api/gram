import * as z from "zod/v4-mini";
export type ListTriggerDefinitionsSecurity = {
    projectSlugHeaderGramProject?: string | undefined;
    sessionHeaderGramSession?: string | undefined;
};
export type ListTriggerDefinitionsRequest = {
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
export type ListTriggerDefinitionsSecurity$Outbound = {
    "project_slug_header_Gram-Project"?: string | undefined;
    "session_header_Gram-Session"?: string | undefined;
};
/** @internal */
export declare const ListTriggerDefinitionsSecurity$outboundSchema: z.ZodMiniType<ListTriggerDefinitionsSecurity$Outbound, ListTriggerDefinitionsSecurity>;
export declare function listTriggerDefinitionsSecurityToJSON(listTriggerDefinitionsSecurity: ListTriggerDefinitionsSecurity): string;
/** @internal */
export type ListTriggerDefinitionsRequest$Outbound = {
    "Gram-Session"?: string | undefined;
    "Gram-Project"?: string | undefined;
};
/** @internal */
export declare const ListTriggerDefinitionsRequest$outboundSchema: z.ZodMiniType<ListTriggerDefinitionsRequest$Outbound, ListTriggerDefinitionsRequest>;
export declare function listTriggerDefinitionsRequestToJSON(listTriggerDefinitionsRequest: ListTriggerDefinitionsRequest): string;
//# sourceMappingURL=listtriggerdefinitions.d.ts.map