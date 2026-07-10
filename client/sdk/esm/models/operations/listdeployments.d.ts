import * as z from "zod/v4-mini";
export type ListDeploymentsSecurityOption1 = {
    apikeyHeaderGramKey: string;
    projectSlugHeaderGramProject: string;
};
export type ListDeploymentsSecurityOption2 = {
    projectSlugHeaderGramProject: string;
    sessionHeaderGramSession: string;
};
export type ListDeploymentsSecurity = {
    option1?: ListDeploymentsSecurityOption1 | undefined;
    option2?: ListDeploymentsSecurityOption2 | undefined;
};
export type ListDeploymentsRequest = {
    /**
     * The cursor to fetch results from
     */
    cursor?: string | undefined;
    /**
     * API Key header
     */
    gramKey?: string | undefined;
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
export type ListDeploymentsSecurityOption1$Outbound = {
    "apikey_header_Gram-Key": string;
    "project_slug_header_Gram-Project": string;
};
/** @internal */
export declare const ListDeploymentsSecurityOption1$outboundSchema: z.ZodMiniType<ListDeploymentsSecurityOption1$Outbound, ListDeploymentsSecurityOption1>;
export declare function listDeploymentsSecurityOption1ToJSON(listDeploymentsSecurityOption1: ListDeploymentsSecurityOption1): string;
/** @internal */
export type ListDeploymentsSecurityOption2$Outbound = {
    "project_slug_header_Gram-Project": string;
    "session_header_Gram-Session": string;
};
/** @internal */
export declare const ListDeploymentsSecurityOption2$outboundSchema: z.ZodMiniType<ListDeploymentsSecurityOption2$Outbound, ListDeploymentsSecurityOption2>;
export declare function listDeploymentsSecurityOption2ToJSON(listDeploymentsSecurityOption2: ListDeploymentsSecurityOption2): string;
/** @internal */
export type ListDeploymentsSecurity$Outbound = {
    Option1?: ListDeploymentsSecurityOption1$Outbound | undefined;
    Option2?: ListDeploymentsSecurityOption2$Outbound | undefined;
};
/** @internal */
export declare const ListDeploymentsSecurity$outboundSchema: z.ZodMiniType<ListDeploymentsSecurity$Outbound, ListDeploymentsSecurity>;
export declare function listDeploymentsSecurityToJSON(listDeploymentsSecurity: ListDeploymentsSecurity): string;
/** @internal */
export type ListDeploymentsRequest$Outbound = {
    cursor?: string | undefined;
    "Gram-Key"?: string | undefined;
    "Gram-Session"?: string | undefined;
    "Gram-Project"?: string | undefined;
};
/** @internal */
export declare const ListDeploymentsRequest$outboundSchema: z.ZodMiniType<ListDeploymentsRequest$Outbound, ListDeploymentsRequest>;
export declare function listDeploymentsRequestToJSON(listDeploymentsRequest: ListDeploymentsRequest): string;
//# sourceMappingURL=listdeployments.d.ts.map