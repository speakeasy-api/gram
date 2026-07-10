import * as z from "zod/v4-mini";
import { MigrateLegacyGramRegistrationsForm, MigrateLegacyGramRegistrationsForm$Outbound } from "../components/migratelegacygramregistrationsform.js";
export type MigrateLegacyGramRegistrationsSecurityOption1 = {
    projectSlugHeaderGramProject: string;
    sessionHeaderGramSession: string;
};
export type MigrateLegacyGramRegistrationsSecurityOption2 = {
    apikeyHeaderGramKey: string;
    projectSlugHeaderGramProject: string;
};
export type MigrateLegacyGramRegistrationsSecurity = {
    option1?: MigrateLegacyGramRegistrationsSecurityOption1 | undefined;
    option2?: MigrateLegacyGramRegistrationsSecurityOption2 | undefined;
};
export type MigrateLegacyGramRegistrationsRequest = {
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
    migrateLegacyGramRegistrationsForm: MigrateLegacyGramRegistrationsForm;
};
/** @internal */
export type MigrateLegacyGramRegistrationsSecurityOption1$Outbound = {
    "project_slug_header_Gram-Project": string;
    "session_header_Gram-Session": string;
};
/** @internal */
export declare const MigrateLegacyGramRegistrationsSecurityOption1$outboundSchema: z.ZodMiniType<MigrateLegacyGramRegistrationsSecurityOption1$Outbound, MigrateLegacyGramRegistrationsSecurityOption1>;
export declare function migrateLegacyGramRegistrationsSecurityOption1ToJSON(migrateLegacyGramRegistrationsSecurityOption1: MigrateLegacyGramRegistrationsSecurityOption1): string;
/** @internal */
export type MigrateLegacyGramRegistrationsSecurityOption2$Outbound = {
    "apikey_header_Gram-Key": string;
    "project_slug_header_Gram-Project": string;
};
/** @internal */
export declare const MigrateLegacyGramRegistrationsSecurityOption2$outboundSchema: z.ZodMiniType<MigrateLegacyGramRegistrationsSecurityOption2$Outbound, MigrateLegacyGramRegistrationsSecurityOption2>;
export declare function migrateLegacyGramRegistrationsSecurityOption2ToJSON(migrateLegacyGramRegistrationsSecurityOption2: MigrateLegacyGramRegistrationsSecurityOption2): string;
/** @internal */
export type MigrateLegacyGramRegistrationsSecurity$Outbound = {
    Option1?: MigrateLegacyGramRegistrationsSecurityOption1$Outbound | undefined;
    Option2?: MigrateLegacyGramRegistrationsSecurityOption2$Outbound | undefined;
};
/** @internal */
export declare const MigrateLegacyGramRegistrationsSecurity$outboundSchema: z.ZodMiniType<MigrateLegacyGramRegistrationsSecurity$Outbound, MigrateLegacyGramRegistrationsSecurity>;
export declare function migrateLegacyGramRegistrationsSecurityToJSON(migrateLegacyGramRegistrationsSecurity: MigrateLegacyGramRegistrationsSecurity): string;
/** @internal */
export type MigrateLegacyGramRegistrationsRequest$Outbound = {
    "Gram-Session"?: string | undefined;
    "Gram-Key"?: string | undefined;
    "Gram-Project"?: string | undefined;
    MigrateLegacyGramRegistrationsForm: MigrateLegacyGramRegistrationsForm$Outbound;
};
/** @internal */
export declare const MigrateLegacyGramRegistrationsRequest$outboundSchema: z.ZodMiniType<MigrateLegacyGramRegistrationsRequest$Outbound, MigrateLegacyGramRegistrationsRequest>;
export declare function migrateLegacyGramRegistrationsRequestToJSON(migrateLegacyGramRegistrationsRequest: MigrateLegacyGramRegistrationsRequest): string;
//# sourceMappingURL=migratelegacygramregistrations.d.ts.map