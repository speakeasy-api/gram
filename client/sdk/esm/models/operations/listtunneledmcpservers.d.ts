import * as z from "zod/v4-mini";
export type ListTunneledMcpServersSecurityOption1 = {
    projectSlugHeaderGramProject: string;
    sessionHeaderGramSession: string;
};
export type ListTunneledMcpServersSecurityOption2 = {
    apikeyHeaderGramKey: string;
    projectSlugHeaderGramProject: string;
};
export type ListTunneledMcpServersSecurity = {
    option1?: ListTunneledMcpServersSecurityOption1 | undefined;
    option2?: ListTunneledMcpServersSecurityOption2 | undefined;
};
export type ListTunneledMcpServersRequest = {
    /**
     * Session header
     */
    gramSession?: string | undefined;
    /**
     * API Key header
     */
    gramKey?: string | undefined;
    /**
     * project header
     */
    gramProject?: string | undefined;
};
/** @internal */
export type ListTunneledMcpServersSecurityOption1$Outbound = {
    "project_slug_header_Gram-Project": string;
    "session_header_Gram-Session": string;
};
/** @internal */
export declare const ListTunneledMcpServersSecurityOption1$outboundSchema: z.ZodMiniType<ListTunneledMcpServersSecurityOption1$Outbound, ListTunneledMcpServersSecurityOption1>;
export declare function listTunneledMcpServersSecurityOption1ToJSON(listTunneledMcpServersSecurityOption1: ListTunneledMcpServersSecurityOption1): string;
/** @internal */
export type ListTunneledMcpServersSecurityOption2$Outbound = {
    "apikey_header_Gram-Key": string;
    "project_slug_header_Gram-Project": string;
};
/** @internal */
export declare const ListTunneledMcpServersSecurityOption2$outboundSchema: z.ZodMiniType<ListTunneledMcpServersSecurityOption2$Outbound, ListTunneledMcpServersSecurityOption2>;
export declare function listTunneledMcpServersSecurityOption2ToJSON(listTunneledMcpServersSecurityOption2: ListTunneledMcpServersSecurityOption2): string;
/** @internal */
export type ListTunneledMcpServersSecurity$Outbound = {
    Option1?: ListTunneledMcpServersSecurityOption1$Outbound | undefined;
    Option2?: ListTunneledMcpServersSecurityOption2$Outbound | undefined;
};
/** @internal */
export declare const ListTunneledMcpServersSecurity$outboundSchema: z.ZodMiniType<ListTunneledMcpServersSecurity$Outbound, ListTunneledMcpServersSecurity>;
export declare function listTunneledMcpServersSecurityToJSON(listTunneledMcpServersSecurity: ListTunneledMcpServersSecurity): string;
/** @internal */
export type ListTunneledMcpServersRequest$Outbound = {
    "Gram-Session"?: string | undefined;
    "Gram-Key"?: string | undefined;
    "Gram-Project"?: string | undefined;
};
/** @internal */
export declare const ListTunneledMcpServersRequest$outboundSchema: z.ZodMiniType<ListTunneledMcpServersRequest$Outbound, ListTunneledMcpServersRequest>;
export declare function listTunneledMcpServersRequestToJSON(listTunneledMcpServersRequest: ListTunneledMcpServersRequest): string;
//# sourceMappingURL=listtunneledmcpservers.d.ts.map