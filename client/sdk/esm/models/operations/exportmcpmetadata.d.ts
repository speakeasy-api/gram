import * as z from "zod/v4-mini";
import { ExportMcpMetadataRequestBody, ExportMcpMetadataRequestBody$Outbound } from "../components/exportmcpmetadatarequestbody.js";
export type ExportMcpMetadataSecurityOption1 = {
    apikeyHeaderGramKey: string;
    projectSlugHeaderGramProject: string;
};
export type ExportMcpMetadataSecurityOption2 = {
    projectSlugHeaderGramProject: string;
    sessionHeaderGramSession: string;
};
export type ExportMcpMetadataSecurity = {
    option1?: ExportMcpMetadataSecurityOption1 | undefined;
    option2?: ExportMcpMetadataSecurityOption2 | undefined;
};
export type ExportMcpMetadataRequest = {
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
    exportMcpMetadataRequestBody: ExportMcpMetadataRequestBody;
};
/** @internal */
export type ExportMcpMetadataSecurityOption1$Outbound = {
    "apikey_header_Gram-Key": string;
    "project_slug_header_Gram-Project": string;
};
/** @internal */
export declare const ExportMcpMetadataSecurityOption1$outboundSchema: z.ZodMiniType<ExportMcpMetadataSecurityOption1$Outbound, ExportMcpMetadataSecurityOption1>;
export declare function exportMcpMetadataSecurityOption1ToJSON(exportMcpMetadataSecurityOption1: ExportMcpMetadataSecurityOption1): string;
/** @internal */
export type ExportMcpMetadataSecurityOption2$Outbound = {
    "project_slug_header_Gram-Project": string;
    "session_header_Gram-Session": string;
};
/** @internal */
export declare const ExportMcpMetadataSecurityOption2$outboundSchema: z.ZodMiniType<ExportMcpMetadataSecurityOption2$Outbound, ExportMcpMetadataSecurityOption2>;
export declare function exportMcpMetadataSecurityOption2ToJSON(exportMcpMetadataSecurityOption2: ExportMcpMetadataSecurityOption2): string;
/** @internal */
export type ExportMcpMetadataSecurity$Outbound = {
    Option1?: ExportMcpMetadataSecurityOption1$Outbound | undefined;
    Option2?: ExportMcpMetadataSecurityOption2$Outbound | undefined;
};
/** @internal */
export declare const ExportMcpMetadataSecurity$outboundSchema: z.ZodMiniType<ExportMcpMetadataSecurity$Outbound, ExportMcpMetadataSecurity>;
export declare function exportMcpMetadataSecurityToJSON(exportMcpMetadataSecurity: ExportMcpMetadataSecurity): string;
/** @internal */
export type ExportMcpMetadataRequest$Outbound = {
    "Gram-Key"?: string | undefined;
    "Gram-Session"?: string | undefined;
    "Gram-Project"?: string | undefined;
    ExportMcpMetadataRequestBody: ExportMcpMetadataRequestBody$Outbound;
};
/** @internal */
export declare const ExportMcpMetadataRequest$outboundSchema: z.ZodMiniType<ExportMcpMetadataRequest$Outbound, ExportMcpMetadataRequest>;
export declare function exportMcpMetadataRequestToJSON(exportMcpMetadataRequest: ExportMcpMetadataRequest): string;
//# sourceMappingURL=exportmcpmetadata.d.ts.map