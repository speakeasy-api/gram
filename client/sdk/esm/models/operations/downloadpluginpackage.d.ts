import * as z from "zod/v4-mini";
import { ClosedEnum } from "../../types/enums.js";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
export type DownloadPluginPackageSecurity = {
    projectSlugHeaderGramProject?: string | undefined;
    sessionHeaderGramSession?: string | undefined;
};
/**
 * Target platform to download plugins for.
 */
export declare const QueryParamPlatform: {
    readonly Claude: "claude";
    readonly Cursor: "cursor";
    readonly Codex: "codex";
};
/**
 * Target platform to download plugins for.
 */
export type QueryParamPlatform = ClosedEnum<typeof QueryParamPlatform>;
export type DownloadPluginPackageRequest = {
    /**
     * The plugin to download.
     */
    pluginId: string;
    /**
     * Target platform to download plugins for.
     */
    platform: QueryParamPlatform;
    /**
     * Session header
     */
    gramSession?: string | undefined;
    /**
     * project header
     */
    gramProject?: string | undefined;
};
export type DownloadPluginPackageResponse = {
    headers: {
        [k: string]: Array<string>;
    };
    result: ReadableStream<Uint8Array>;
};
/** @internal */
export type DownloadPluginPackageSecurity$Outbound = {
    "project_slug_header_Gram-Project"?: string | undefined;
    "session_header_Gram-Session"?: string | undefined;
};
/** @internal */
export declare const DownloadPluginPackageSecurity$outboundSchema: z.ZodMiniType<DownloadPluginPackageSecurity$Outbound, DownloadPluginPackageSecurity>;
export declare function downloadPluginPackageSecurityToJSON(downloadPluginPackageSecurity: DownloadPluginPackageSecurity): string;
/** @internal */
export declare const QueryParamPlatform$outboundSchema: z.ZodMiniEnum<typeof QueryParamPlatform>;
/** @internal */
export type DownloadPluginPackageRequest$Outbound = {
    plugin_id: string;
    platform: string;
    "Gram-Session"?: string | undefined;
    "Gram-Project"?: string | undefined;
};
/** @internal */
export declare const DownloadPluginPackageRequest$outboundSchema: z.ZodMiniType<DownloadPluginPackageRequest$Outbound, DownloadPluginPackageRequest>;
export declare function downloadPluginPackageRequestToJSON(downloadPluginPackageRequest: DownloadPluginPackageRequest): string;
/** @internal */
export declare const DownloadPluginPackageResponse$inboundSchema: z.ZodMiniType<DownloadPluginPackageResponse, unknown>;
export declare function downloadPluginPackageResponseFromJSON(jsonString: string): SafeParseResult<DownloadPluginPackageResponse, SDKValidationError>;
//# sourceMappingURL=downloadpluginpackage.d.ts.map