import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
export type DownloadCodexInstallScriptSecurity = {
  projectSlugHeaderGramProject?: string | undefined;
  sessionHeaderGramSession?: string | undefined;
};
export type DownloadCodexInstallScriptRequest = {
  /**
   * Session header
   */
  gramSession?: string | undefined;
  /**
   * project header
   */
  gramProject?: string | undefined;
};
export type DownloadCodexInstallScriptResponse = {
  headers: {
    [k: string]: Array<string>;
  };
  result: ReadableStream<Uint8Array>;
};
/** @internal */
export type DownloadCodexInstallScriptSecurity$Outbound = {
  "project_slug_header_Gram-Project"?: string | undefined;
  "session_header_Gram-Session"?: string | undefined;
};
/** @internal */
export declare const DownloadCodexInstallScriptSecurity$outboundSchema: z.ZodMiniType<
  DownloadCodexInstallScriptSecurity$Outbound,
  DownloadCodexInstallScriptSecurity
>;
export declare function downloadCodexInstallScriptSecurityToJSON(
  downloadCodexInstallScriptSecurity: DownloadCodexInstallScriptSecurity,
): string;
/** @internal */
export type DownloadCodexInstallScriptRequest$Outbound = {
  "Gram-Session"?: string | undefined;
  "Gram-Project"?: string | undefined;
};
/** @internal */
export declare const DownloadCodexInstallScriptRequest$outboundSchema: z.ZodMiniType<
  DownloadCodexInstallScriptRequest$Outbound,
  DownloadCodexInstallScriptRequest
>;
export declare function downloadCodexInstallScriptRequestToJSON(
  downloadCodexInstallScriptRequest: DownloadCodexInstallScriptRequest,
): string;
/** @internal */
export declare const DownloadCodexInstallScriptResponse$inboundSchema: z.ZodMiniType<
  DownloadCodexInstallScriptResponse,
  unknown
>;
export declare function downloadCodexInstallScriptResponseFromJSON(
  jsonString: string,
): SafeParseResult<DownloadCodexInstallScriptResponse, SDKValidationError>;
//# sourceMappingURL=downloadcodexinstallscript.d.ts.map
