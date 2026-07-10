import * as z from "zod/v4-mini";
import * as components from "../components/index.js";
export type RegisterRemoteSessionIssuerSecurityOption1 = {
    projectSlugHeaderGramProject: string;
    sessionHeaderGramSession: string;
};
export type RegisterRemoteSessionIssuerSecurityOption2 = {
    apikeyHeaderGramKey: string;
    projectSlugHeaderGramProject: string;
};
export type RegisterRemoteSessionIssuerSecurity = {
    option1?: RegisterRemoteSessionIssuerSecurityOption1 | undefined;
    option2?: RegisterRemoteSessionIssuerSecurityOption2 | undefined;
};
export type RegisterRemoteSessionIssuerRequest = {
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
    registerRemoteSessionIssuerForm: components.RegisterRemoteSessionIssuerForm;
};
/** @internal */
export type RegisterRemoteSessionIssuerSecurityOption1$Outbound = {
    "project_slug_header_Gram-Project": string;
    "session_header_Gram-Session": string;
};
/** @internal */
export declare const RegisterRemoteSessionIssuerSecurityOption1$outboundSchema: z.ZodMiniType<RegisterRemoteSessionIssuerSecurityOption1$Outbound, RegisterRemoteSessionIssuerSecurityOption1>;
export declare function registerRemoteSessionIssuerSecurityOption1ToJSON(registerRemoteSessionIssuerSecurityOption1: RegisterRemoteSessionIssuerSecurityOption1): string;
/** @internal */
export type RegisterRemoteSessionIssuerSecurityOption2$Outbound = {
    "apikey_header_Gram-Key": string;
    "project_slug_header_Gram-Project": string;
};
/** @internal */
export declare const RegisterRemoteSessionIssuerSecurityOption2$outboundSchema: z.ZodMiniType<RegisterRemoteSessionIssuerSecurityOption2$Outbound, RegisterRemoteSessionIssuerSecurityOption2>;
export declare function registerRemoteSessionIssuerSecurityOption2ToJSON(registerRemoteSessionIssuerSecurityOption2: RegisterRemoteSessionIssuerSecurityOption2): string;
/** @internal */
export type RegisterRemoteSessionIssuerSecurity$Outbound = {
    Option1?: RegisterRemoteSessionIssuerSecurityOption1$Outbound | undefined;
    Option2?: RegisterRemoteSessionIssuerSecurityOption2$Outbound | undefined;
};
/** @internal */
export declare const RegisterRemoteSessionIssuerSecurity$outboundSchema: z.ZodMiniType<RegisterRemoteSessionIssuerSecurity$Outbound, RegisterRemoteSessionIssuerSecurity>;
export declare function registerRemoteSessionIssuerSecurityToJSON(registerRemoteSessionIssuerSecurity: RegisterRemoteSessionIssuerSecurity): string;
/** @internal */
export type RegisterRemoteSessionIssuerRequest$Outbound = {
    "Gram-Session"?: string | undefined;
    "Gram-Key"?: string | undefined;
    "Gram-Project"?: string | undefined;
    RegisterRemoteSessionIssuerForm: components.RegisterRemoteSessionIssuerForm$Outbound;
};
/** @internal */
export declare const RegisterRemoteSessionIssuerRequest$outboundSchema: z.ZodMiniType<RegisterRemoteSessionIssuerRequest$Outbound, RegisterRemoteSessionIssuerRequest>;
export declare function registerRemoteSessionIssuerRequestToJSON(registerRemoteSessionIssuerRequest: RegisterRemoteSessionIssuerRequest): string;
//# sourceMappingURL=registerremotesessionissuer.d.ts.map