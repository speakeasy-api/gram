import * as z from "zod/v4-mini";
export type ListRemoteMcpServersSecurityOption1 = {
    projectSlugHeaderGramProject: string;
    sessionHeaderGramSession: string;
};
export type ListRemoteMcpServersSecurityOption2 = {
    apikeyHeaderGramKey: string;
    projectSlugHeaderGramProject: string;
};
export type ListRemoteMcpServersSecurity = {
    option1?: ListRemoteMcpServersSecurityOption1 | undefined;
    option2?: ListRemoteMcpServersSecurityOption2 | undefined;
};
export type ListRemoteMcpServersRequest = {
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
export type ListRemoteMcpServersSecurityOption1$Outbound = {
    "project_slug_header_Gram-Project": string;
    "session_header_Gram-Session": string;
};
/** @internal */
export declare const ListRemoteMcpServersSecurityOption1$outboundSchema: z.ZodMiniType<ListRemoteMcpServersSecurityOption1$Outbound, ListRemoteMcpServersSecurityOption1>;
export declare function listRemoteMcpServersSecurityOption1ToJSON(listRemoteMcpServersSecurityOption1: ListRemoteMcpServersSecurityOption1): string;
/** @internal */
export type ListRemoteMcpServersSecurityOption2$Outbound = {
    "apikey_header_Gram-Key": string;
    "project_slug_header_Gram-Project": string;
};
/** @internal */
export declare const ListRemoteMcpServersSecurityOption2$outboundSchema: z.ZodMiniType<ListRemoteMcpServersSecurityOption2$Outbound, ListRemoteMcpServersSecurityOption2>;
export declare function listRemoteMcpServersSecurityOption2ToJSON(listRemoteMcpServersSecurityOption2: ListRemoteMcpServersSecurityOption2): string;
/** @internal */
export type ListRemoteMcpServersSecurity$Outbound = {
    Option1?: ListRemoteMcpServersSecurityOption1$Outbound | undefined;
    Option2?: ListRemoteMcpServersSecurityOption2$Outbound | undefined;
};
/** @internal */
export declare const ListRemoteMcpServersSecurity$outboundSchema: z.ZodMiniType<ListRemoteMcpServersSecurity$Outbound, ListRemoteMcpServersSecurity>;
export declare function listRemoteMcpServersSecurityToJSON(listRemoteMcpServersSecurity: ListRemoteMcpServersSecurity): string;
/** @internal */
export type ListRemoteMcpServersRequest$Outbound = {
    "Gram-Session"?: string | undefined;
    "Gram-Key"?: string | undefined;
    "Gram-Project"?: string | undefined;
};
/** @internal */
export declare const ListRemoteMcpServersRequest$outboundSchema: z.ZodMiniType<ListRemoteMcpServersRequest$Outbound, ListRemoteMcpServersRequest>;
export declare function listRemoteMcpServersRequestToJSON(listRemoteMcpServersRequest: ListRemoteMcpServersRequest): string;
//# sourceMappingURL=listremotemcpservers.d.ts.map