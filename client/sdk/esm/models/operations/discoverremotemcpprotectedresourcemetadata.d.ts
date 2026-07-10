import * as z from "zod/v4-mini";
import { DiscoverProtectedResourceMetadataRequestBody, DiscoverProtectedResourceMetadataRequestBody$Outbound } from "../components/discoverprotectedresourcemetadatarequestbody.js";
export type DiscoverRemoteMcpProtectedResourceMetadataSecurityOption1 = {
    projectSlugHeaderGramProject: string;
    sessionHeaderGramSession: string;
};
export type DiscoverRemoteMcpProtectedResourceMetadataSecurityOption2 = {
    apikeyHeaderGramKey: string;
    projectSlugHeaderGramProject: string;
};
export type DiscoverRemoteMcpProtectedResourceMetadataSecurity = {
    option1?: DiscoverRemoteMcpProtectedResourceMetadataSecurityOption1 | undefined;
    option2?: DiscoverRemoteMcpProtectedResourceMetadataSecurityOption2 | undefined;
};
export type DiscoverRemoteMcpProtectedResourceMetadataRequest = {
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
    discoverProtectedResourceMetadataRequestBody: DiscoverProtectedResourceMetadataRequestBody;
};
/** @internal */
export type DiscoverRemoteMcpProtectedResourceMetadataSecurityOption1$Outbound = {
    "project_slug_header_Gram-Project": string;
    "session_header_Gram-Session": string;
};
/** @internal */
export declare const DiscoverRemoteMcpProtectedResourceMetadataSecurityOption1$outboundSchema: z.ZodMiniType<DiscoverRemoteMcpProtectedResourceMetadataSecurityOption1$Outbound, DiscoverRemoteMcpProtectedResourceMetadataSecurityOption1>;
export declare function discoverRemoteMcpProtectedResourceMetadataSecurityOption1ToJSON(discoverRemoteMcpProtectedResourceMetadataSecurityOption1: DiscoverRemoteMcpProtectedResourceMetadataSecurityOption1): string;
/** @internal */
export type DiscoverRemoteMcpProtectedResourceMetadataSecurityOption2$Outbound = {
    "apikey_header_Gram-Key": string;
    "project_slug_header_Gram-Project": string;
};
/** @internal */
export declare const DiscoverRemoteMcpProtectedResourceMetadataSecurityOption2$outboundSchema: z.ZodMiniType<DiscoverRemoteMcpProtectedResourceMetadataSecurityOption2$Outbound, DiscoverRemoteMcpProtectedResourceMetadataSecurityOption2>;
export declare function discoverRemoteMcpProtectedResourceMetadataSecurityOption2ToJSON(discoverRemoteMcpProtectedResourceMetadataSecurityOption2: DiscoverRemoteMcpProtectedResourceMetadataSecurityOption2): string;
/** @internal */
export type DiscoverRemoteMcpProtectedResourceMetadataSecurity$Outbound = {
    Option1?: DiscoverRemoteMcpProtectedResourceMetadataSecurityOption1$Outbound | undefined;
    Option2?: DiscoverRemoteMcpProtectedResourceMetadataSecurityOption2$Outbound | undefined;
};
/** @internal */
export declare const DiscoverRemoteMcpProtectedResourceMetadataSecurity$outboundSchema: z.ZodMiniType<DiscoverRemoteMcpProtectedResourceMetadataSecurity$Outbound, DiscoverRemoteMcpProtectedResourceMetadataSecurity>;
export declare function discoverRemoteMcpProtectedResourceMetadataSecurityToJSON(discoverRemoteMcpProtectedResourceMetadataSecurity: DiscoverRemoteMcpProtectedResourceMetadataSecurity): string;
/** @internal */
export type DiscoverRemoteMcpProtectedResourceMetadataRequest$Outbound = {
    "Gram-Session"?: string | undefined;
    "Gram-Key"?: string | undefined;
    "Gram-Project"?: string | undefined;
    DiscoverProtectedResourceMetadataRequestBody: DiscoverProtectedResourceMetadataRequestBody$Outbound;
};
/** @internal */
export declare const DiscoverRemoteMcpProtectedResourceMetadataRequest$outboundSchema: z.ZodMiniType<DiscoverRemoteMcpProtectedResourceMetadataRequest$Outbound, DiscoverRemoteMcpProtectedResourceMetadataRequest>;
export declare function discoverRemoteMcpProtectedResourceMetadataRequestToJSON(discoverRemoteMcpProtectedResourceMetadataRequest: DiscoverRemoteMcpProtectedResourceMetadataRequest): string;
//# sourceMappingURL=discoverremotemcpprotectedresourcemetadata.d.ts.map