import * as z from "zod/v4-mini";
import { ClosedEnum } from "../../types/enums.js";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
export type DownloadObservabilityPluginSecurity = {
  projectSlugHeaderGramProject?: string | undefined;
  sessionHeaderGramSession?: string | undefined;
};
/**
 * Target platform.
 */
export declare const Platform: {
  readonly Claude: "claude";
  readonly Cursor: "cursor";
  readonly Codex: "codex";
};
/**
 * Target platform.
 */
export type Platform = ClosedEnum<typeof Platform>;
export type DownloadObservabilityPluginRequest = {
  /**
   * Target platform.
   */
  platform: Platform;
  /**
   * Session header
   */
  gramSession?: string | undefined;
  /**
   * project header
   */
  gramProject?: string | undefined;
};
export type DownloadObservabilityPluginResponse = {
  headers: {
    [k: string]: Array<string>;
  };
  result: ReadableStream<Uint8Array>;
};
/** @internal */
export type DownloadObservabilityPluginSecurity$Outbound = {
  "project_slug_header_Gram-Project"?: string | undefined;
  "session_header_Gram-Session"?: string | undefined;
};
/** @internal */
export declare const DownloadObservabilityPluginSecurity$outboundSchema: z.ZodMiniType<
  DownloadObservabilityPluginSecurity$Outbound,
  DownloadObservabilityPluginSecurity
>;
export declare function downloadObservabilityPluginSecurityToJSON(
  downloadObservabilityPluginSecurity: DownloadObservabilityPluginSecurity,
): string;
/** @internal */
export declare const Platform$outboundSchema: z.ZodMiniEnum<typeof Platform>;
/** @internal */
export type DownloadObservabilityPluginRequest$Outbound = {
  platform: string;
  "Gram-Session"?: string | undefined;
  "Gram-Project"?: string | undefined;
};
/** @internal */
export declare const DownloadObservabilityPluginRequest$outboundSchema: z.ZodMiniType<
  DownloadObservabilityPluginRequest$Outbound,
  DownloadObservabilityPluginRequest
>;
export declare function downloadObservabilityPluginRequestToJSON(
  downloadObservabilityPluginRequest: DownloadObservabilityPluginRequest,
): string;
/** @internal */
export declare const DownloadObservabilityPluginResponse$inboundSchema: z.ZodMiniType<
  DownloadObservabilityPluginResponse,
  unknown
>;
export declare function downloadObservabilityPluginResponseFromJSON(
  jsonString: string,
): SafeParseResult<DownloadObservabilityPluginResponse, SDKValidationError>;
//# sourceMappingURL=downloadobservabilityplugin.d.ts.map
