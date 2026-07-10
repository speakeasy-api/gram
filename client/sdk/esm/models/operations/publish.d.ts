import * as z from "zod/v4-mini";
import { PublishPackageForm, PublishPackageForm$Outbound } from "../components/publishpackageform.js";
export type PublishSecurityOption1 = {
    apikeyHeaderGramKey: string;
    projectSlugHeaderGramProject: string;
};
export type PublishSecurityOption2 = {
    projectSlugHeaderGramProject: string;
    sessionHeaderGramSession: string;
};
export type PublishSecurity = {
    option1?: PublishSecurityOption1 | undefined;
    option2?: PublishSecurityOption2 | undefined;
};
export type PublishRequest = {
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
    publishPackageForm: PublishPackageForm;
};
/** @internal */
export type PublishSecurityOption1$Outbound = {
    "apikey_header_Gram-Key": string;
    "project_slug_header_Gram-Project": string;
};
/** @internal */
export declare const PublishSecurityOption1$outboundSchema: z.ZodMiniType<PublishSecurityOption1$Outbound, PublishSecurityOption1>;
export declare function publishSecurityOption1ToJSON(publishSecurityOption1: PublishSecurityOption1): string;
/** @internal */
export type PublishSecurityOption2$Outbound = {
    "project_slug_header_Gram-Project": string;
    "session_header_Gram-Session": string;
};
/** @internal */
export declare const PublishSecurityOption2$outboundSchema: z.ZodMiniType<PublishSecurityOption2$Outbound, PublishSecurityOption2>;
export declare function publishSecurityOption2ToJSON(publishSecurityOption2: PublishSecurityOption2): string;
/** @internal */
export type PublishSecurity$Outbound = {
    Option1?: PublishSecurityOption1$Outbound | undefined;
    Option2?: PublishSecurityOption2$Outbound | undefined;
};
/** @internal */
export declare const PublishSecurity$outboundSchema: z.ZodMiniType<PublishSecurity$Outbound, PublishSecurity>;
export declare function publishSecurityToJSON(publishSecurity: PublishSecurity): string;
/** @internal */
export type PublishRequest$Outbound = {
    "Gram-Key"?: string | undefined;
    "Gram-Session"?: string | undefined;
    "Gram-Project"?: string | undefined;
    PublishPackageForm: PublishPackageForm$Outbound;
};
/** @internal */
export declare const PublishRequest$outboundSchema: z.ZodMiniType<PublishRequest$Outbound, PublishRequest>;
export declare function publishRequestToJSON(publishRequest: PublishRequest): string;
//# sourceMappingURL=publish.d.ts.map