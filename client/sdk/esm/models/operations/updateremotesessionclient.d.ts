import * as z from "zod/v4-mini";
import { UpdateRemoteSessionClientForm, UpdateRemoteSessionClientForm$Outbound } from "../components/updateremotesessionclientform.js";
export type UpdateRemoteSessionClientSecurityOption1 = {
    projectSlugHeaderGramProject: string;
    sessionHeaderGramSession: string;
};
export type UpdateRemoteSessionClientSecurityOption2 = {
    apikeyHeaderGramKey: string;
    projectSlugHeaderGramProject: string;
};
export type UpdateRemoteSessionClientSecurity = {
    option1?: UpdateRemoteSessionClientSecurityOption1 | undefined;
    option2?: UpdateRemoteSessionClientSecurityOption2 | undefined;
};
export type UpdateRemoteSessionClientRequest = {
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
    updateRemoteSessionClientForm: UpdateRemoteSessionClientForm;
};
/** @internal */
export type UpdateRemoteSessionClientSecurityOption1$Outbound = {
    "project_slug_header_Gram-Project": string;
    "session_header_Gram-Session": string;
};
/** @internal */
export declare const UpdateRemoteSessionClientSecurityOption1$outboundSchema: z.ZodMiniType<UpdateRemoteSessionClientSecurityOption1$Outbound, UpdateRemoteSessionClientSecurityOption1>;
export declare function updateRemoteSessionClientSecurityOption1ToJSON(updateRemoteSessionClientSecurityOption1: UpdateRemoteSessionClientSecurityOption1): string;
/** @internal */
export type UpdateRemoteSessionClientSecurityOption2$Outbound = {
    "apikey_header_Gram-Key": string;
    "project_slug_header_Gram-Project": string;
};
/** @internal */
export declare const UpdateRemoteSessionClientSecurityOption2$outboundSchema: z.ZodMiniType<UpdateRemoteSessionClientSecurityOption2$Outbound, UpdateRemoteSessionClientSecurityOption2>;
export declare function updateRemoteSessionClientSecurityOption2ToJSON(updateRemoteSessionClientSecurityOption2: UpdateRemoteSessionClientSecurityOption2): string;
/** @internal */
export type UpdateRemoteSessionClientSecurity$Outbound = {
    Option1?: UpdateRemoteSessionClientSecurityOption1$Outbound | undefined;
    Option2?: UpdateRemoteSessionClientSecurityOption2$Outbound | undefined;
};
/** @internal */
export declare const UpdateRemoteSessionClientSecurity$outboundSchema: z.ZodMiniType<UpdateRemoteSessionClientSecurity$Outbound, UpdateRemoteSessionClientSecurity>;
export declare function updateRemoteSessionClientSecurityToJSON(updateRemoteSessionClientSecurity: UpdateRemoteSessionClientSecurity): string;
/** @internal */
export type UpdateRemoteSessionClientRequest$Outbound = {
    "Gram-Session"?: string | undefined;
    "Gram-Key"?: string | undefined;
    "Gram-Project"?: string | undefined;
    UpdateRemoteSessionClientForm: UpdateRemoteSessionClientForm$Outbound;
};
/** @internal */
export declare const UpdateRemoteSessionClientRequest$outboundSchema: z.ZodMiniType<UpdateRemoteSessionClientRequest$Outbound, UpdateRemoteSessionClientRequest>;
export declare function updateRemoteSessionClientRequestToJSON(updateRemoteSessionClientRequest: UpdateRemoteSessionClientRequest): string;
//# sourceMappingURL=updateremotesessionclient.d.ts.map