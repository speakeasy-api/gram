import * as z from "zod/v4-mini";
import { CreateUserSessionIssuerForm, CreateUserSessionIssuerForm$Outbound } from "../components/createusersessionissuerform.js";
export type CreateUserSessionIssuerSecurityOption1 = {
    projectSlugHeaderGramProject: string;
    sessionHeaderGramSession: string;
};
export type CreateUserSessionIssuerSecurityOption2 = {
    apikeyHeaderGramKey: string;
    projectSlugHeaderGramProject: string;
};
export type CreateUserSessionIssuerSecurity = {
    option1?: CreateUserSessionIssuerSecurityOption1 | undefined;
    option2?: CreateUserSessionIssuerSecurityOption2 | undefined;
};
export type CreateUserSessionIssuerRequest = {
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
    createUserSessionIssuerForm: CreateUserSessionIssuerForm;
};
/** @internal */
export type CreateUserSessionIssuerSecurityOption1$Outbound = {
    "project_slug_header_Gram-Project": string;
    "session_header_Gram-Session": string;
};
/** @internal */
export declare const CreateUserSessionIssuerSecurityOption1$outboundSchema: z.ZodMiniType<CreateUserSessionIssuerSecurityOption1$Outbound, CreateUserSessionIssuerSecurityOption1>;
export declare function createUserSessionIssuerSecurityOption1ToJSON(createUserSessionIssuerSecurityOption1: CreateUserSessionIssuerSecurityOption1): string;
/** @internal */
export type CreateUserSessionIssuerSecurityOption2$Outbound = {
    "apikey_header_Gram-Key": string;
    "project_slug_header_Gram-Project": string;
};
/** @internal */
export declare const CreateUserSessionIssuerSecurityOption2$outboundSchema: z.ZodMiniType<CreateUserSessionIssuerSecurityOption2$Outbound, CreateUserSessionIssuerSecurityOption2>;
export declare function createUserSessionIssuerSecurityOption2ToJSON(createUserSessionIssuerSecurityOption2: CreateUserSessionIssuerSecurityOption2): string;
/** @internal */
export type CreateUserSessionIssuerSecurity$Outbound = {
    Option1?: CreateUserSessionIssuerSecurityOption1$Outbound | undefined;
    Option2?: CreateUserSessionIssuerSecurityOption2$Outbound | undefined;
};
/** @internal */
export declare const CreateUserSessionIssuerSecurity$outboundSchema: z.ZodMiniType<CreateUserSessionIssuerSecurity$Outbound, CreateUserSessionIssuerSecurity>;
export declare function createUserSessionIssuerSecurityToJSON(createUserSessionIssuerSecurity: CreateUserSessionIssuerSecurity): string;
/** @internal */
export type CreateUserSessionIssuerRequest$Outbound = {
    "Gram-Session"?: string | undefined;
    "Gram-Key"?: string | undefined;
    "Gram-Project"?: string | undefined;
    CreateUserSessionIssuerForm: CreateUserSessionIssuerForm$Outbound;
};
/** @internal */
export declare const CreateUserSessionIssuerRequest$outboundSchema: z.ZodMiniType<CreateUserSessionIssuerRequest$Outbound, CreateUserSessionIssuerRequest>;
export declare function createUserSessionIssuerRequestToJSON(createUserSessionIssuerRequest: CreateUserSessionIssuerRequest): string;
//# sourceMappingURL=createusersessionissuer.d.ts.map