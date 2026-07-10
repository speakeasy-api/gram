import * as z from "zod/v4-mini";
import { CreatePackageForm, CreatePackageForm$Outbound } from "../components/createpackageform.js";
export type CreatePackageSecurityOption1 = {
    apikeyHeaderGramKey: string;
    projectSlugHeaderGramProject: string;
};
export type CreatePackageSecurityOption2 = {
    projectSlugHeaderGramProject: string;
    sessionHeaderGramSession: string;
};
export type CreatePackageSecurity = {
    option1?: CreatePackageSecurityOption1 | undefined;
    option2?: CreatePackageSecurityOption2 | undefined;
};
export type CreatePackageRequest = {
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
    createPackageForm: CreatePackageForm;
};
/** @internal */
export type CreatePackageSecurityOption1$Outbound = {
    "apikey_header_Gram-Key": string;
    "project_slug_header_Gram-Project": string;
};
/** @internal */
export declare const CreatePackageSecurityOption1$outboundSchema: z.ZodMiniType<CreatePackageSecurityOption1$Outbound, CreatePackageSecurityOption1>;
export declare function createPackageSecurityOption1ToJSON(createPackageSecurityOption1: CreatePackageSecurityOption1): string;
/** @internal */
export type CreatePackageSecurityOption2$Outbound = {
    "project_slug_header_Gram-Project": string;
    "session_header_Gram-Session": string;
};
/** @internal */
export declare const CreatePackageSecurityOption2$outboundSchema: z.ZodMiniType<CreatePackageSecurityOption2$Outbound, CreatePackageSecurityOption2>;
export declare function createPackageSecurityOption2ToJSON(createPackageSecurityOption2: CreatePackageSecurityOption2): string;
/** @internal */
export type CreatePackageSecurity$Outbound = {
    Option1?: CreatePackageSecurityOption1$Outbound | undefined;
    Option2?: CreatePackageSecurityOption2$Outbound | undefined;
};
/** @internal */
export declare const CreatePackageSecurity$outboundSchema: z.ZodMiniType<CreatePackageSecurity$Outbound, CreatePackageSecurity>;
export declare function createPackageSecurityToJSON(createPackageSecurity: CreatePackageSecurity): string;
/** @internal */
export type CreatePackageRequest$Outbound = {
    "Gram-Key"?: string | undefined;
    "Gram-Session"?: string | undefined;
    "Gram-Project"?: string | undefined;
    CreatePackageForm: CreatePackageForm$Outbound;
};
/** @internal */
export declare const CreatePackageRequest$outboundSchema: z.ZodMiniType<CreatePackageRequest$Outbound, CreatePackageRequest>;
export declare function createPackageRequestToJSON(createPackageRequest: CreatePackageRequest): string;
//# sourceMappingURL=createpackage.d.ts.map