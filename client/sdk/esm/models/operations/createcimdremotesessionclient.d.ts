import * as z from "zod/v4-mini";
import { CreateCimdForm, CreateCimdForm$Outbound } from "../components/createcimdform.js";
export type CreateCimdRemoteSessionClientSecurityOption1 = {
    projectSlugHeaderGramProject: string;
    sessionHeaderGramSession: string;
};
export type CreateCimdRemoteSessionClientSecurityOption2 = {
    apikeyHeaderGramKey: string;
    projectSlugHeaderGramProject: string;
};
export type CreateCimdRemoteSessionClientSecurity = {
    option1?: CreateCimdRemoteSessionClientSecurityOption1 | undefined;
    option2?: CreateCimdRemoteSessionClientSecurityOption2 | undefined;
};
export type CreateCimdRemoteSessionClientRequest = {
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
    createCimdForm: CreateCimdForm;
};
/** @internal */
export type CreateCimdRemoteSessionClientSecurityOption1$Outbound = {
    "project_slug_header_Gram-Project": string;
    "session_header_Gram-Session": string;
};
/** @internal */
export declare const CreateCimdRemoteSessionClientSecurityOption1$outboundSchema: z.ZodMiniType<CreateCimdRemoteSessionClientSecurityOption1$Outbound, CreateCimdRemoteSessionClientSecurityOption1>;
export declare function createCimdRemoteSessionClientSecurityOption1ToJSON(createCimdRemoteSessionClientSecurityOption1: CreateCimdRemoteSessionClientSecurityOption1): string;
/** @internal */
export type CreateCimdRemoteSessionClientSecurityOption2$Outbound = {
    "apikey_header_Gram-Key": string;
    "project_slug_header_Gram-Project": string;
};
/** @internal */
export declare const CreateCimdRemoteSessionClientSecurityOption2$outboundSchema: z.ZodMiniType<CreateCimdRemoteSessionClientSecurityOption2$Outbound, CreateCimdRemoteSessionClientSecurityOption2>;
export declare function createCimdRemoteSessionClientSecurityOption2ToJSON(createCimdRemoteSessionClientSecurityOption2: CreateCimdRemoteSessionClientSecurityOption2): string;
/** @internal */
export type CreateCimdRemoteSessionClientSecurity$Outbound = {
    Option1?: CreateCimdRemoteSessionClientSecurityOption1$Outbound | undefined;
    Option2?: CreateCimdRemoteSessionClientSecurityOption2$Outbound | undefined;
};
/** @internal */
export declare const CreateCimdRemoteSessionClientSecurity$outboundSchema: z.ZodMiniType<CreateCimdRemoteSessionClientSecurity$Outbound, CreateCimdRemoteSessionClientSecurity>;
export declare function createCimdRemoteSessionClientSecurityToJSON(createCimdRemoteSessionClientSecurity: CreateCimdRemoteSessionClientSecurity): string;
/** @internal */
export type CreateCimdRemoteSessionClientRequest$Outbound = {
    "Gram-Session"?: string | undefined;
    "Gram-Key"?: string | undefined;
    "Gram-Project"?: string | undefined;
    CreateCimdForm: CreateCimdForm$Outbound;
};
/** @internal */
export declare const CreateCimdRemoteSessionClientRequest$outboundSchema: z.ZodMiniType<CreateCimdRemoteSessionClientRequest$Outbound, CreateCimdRemoteSessionClientRequest>;
export declare function createCimdRemoteSessionClientRequestToJSON(createCimdRemoteSessionClientRequest: CreateCimdRemoteSessionClientRequest): string;
//# sourceMappingURL=createcimdremotesessionclient.d.ts.map