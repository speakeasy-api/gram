import * as z from "zod/v4-mini";
import { SetMcpMetadataRequestBody, SetMcpMetadataRequestBody$Outbound } from "../components/setmcpmetadatarequestbody.js";
export type SetMcpMetadataSecurityOption1 = {
    apikeyHeaderGramKey: string;
    projectSlugHeaderGramProject: string;
};
export type SetMcpMetadataSecurityOption2 = {
    projectSlugHeaderGramProject: string;
    sessionHeaderGramSession: string;
};
export type SetMcpMetadataSecurity = {
    option1?: SetMcpMetadataSecurityOption1 | undefined;
    option2?: SetMcpMetadataSecurityOption2 | undefined;
};
export type SetMcpMetadataRequest = {
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
    setMcpMetadataRequestBody: SetMcpMetadataRequestBody;
};
/** @internal */
export type SetMcpMetadataSecurityOption1$Outbound = {
    "apikey_header_Gram-Key": string;
    "project_slug_header_Gram-Project": string;
};
/** @internal */
export declare const SetMcpMetadataSecurityOption1$outboundSchema: z.ZodMiniType<SetMcpMetadataSecurityOption1$Outbound, SetMcpMetadataSecurityOption1>;
export declare function setMcpMetadataSecurityOption1ToJSON(setMcpMetadataSecurityOption1: SetMcpMetadataSecurityOption1): string;
/** @internal */
export type SetMcpMetadataSecurityOption2$Outbound = {
    "project_slug_header_Gram-Project": string;
    "session_header_Gram-Session": string;
};
/** @internal */
export declare const SetMcpMetadataSecurityOption2$outboundSchema: z.ZodMiniType<SetMcpMetadataSecurityOption2$Outbound, SetMcpMetadataSecurityOption2>;
export declare function setMcpMetadataSecurityOption2ToJSON(setMcpMetadataSecurityOption2: SetMcpMetadataSecurityOption2): string;
/** @internal */
export type SetMcpMetadataSecurity$Outbound = {
    Option1?: SetMcpMetadataSecurityOption1$Outbound | undefined;
    Option2?: SetMcpMetadataSecurityOption2$Outbound | undefined;
};
/** @internal */
export declare const SetMcpMetadataSecurity$outboundSchema: z.ZodMiniType<SetMcpMetadataSecurity$Outbound, SetMcpMetadataSecurity>;
export declare function setMcpMetadataSecurityToJSON(setMcpMetadataSecurity: SetMcpMetadataSecurity): string;
/** @internal */
export type SetMcpMetadataRequest$Outbound = {
    "Gram-Key"?: string | undefined;
    "Gram-Session"?: string | undefined;
    "Gram-Project"?: string | undefined;
    SetMcpMetadataRequestBody: SetMcpMetadataRequestBody$Outbound;
};
/** @internal */
export declare const SetMcpMetadataRequest$outboundSchema: z.ZodMiniType<SetMcpMetadataRequest$Outbound, SetMcpMetadataRequest>;
export declare function setMcpMetadataRequestToJSON(setMcpMetadataRequest: SetMcpMetadataRequest): string;
//# sourceMappingURL=setmcpmetadata.d.ts.map