import * as z from "zod/v4-mini";
import {
  OTELLogsPayload,
  OTELLogsPayload$Outbound,
} from "../components/otellogspayload.js";
export type HooksNumberLogsSecurity = {
  apikeyHeaderGramKey?: string | undefined;
  projectSlugHeaderGramProject?: string | undefined;
};
export type HooksNumberLogsRequest = {
  /**
   * API Key header
   */
  gramKey?: string | undefined;
  /**
   * project header
   */
  gramProject?: string | undefined;
  otelLogsPayload: OTELLogsPayload;
};
/** @internal */
export type HooksNumberLogsSecurity$Outbound = {
  "apikey_header_Gram-Key"?: string | undefined;
  "project_slug_header_Gram-Project"?: string | undefined;
};
/** @internal */
export declare const HooksNumberLogsSecurity$outboundSchema: z.ZodMiniType<
  HooksNumberLogsSecurity$Outbound,
  HooksNumberLogsSecurity
>;
export declare function hooksNumberLogsSecurityToJSON(
  hooksNumberLogsSecurity: HooksNumberLogsSecurity,
): string;
/** @internal */
export type HooksNumberLogsRequest$Outbound = {
  "Gram-Key"?: string | undefined;
  "Gram-Project"?: string | undefined;
  OTELLogsPayload: OTELLogsPayload$Outbound;
};
/** @internal */
export declare const HooksNumberLogsRequest$outboundSchema: z.ZodMiniType<
  HooksNumberLogsRequest$Outbound,
  HooksNumberLogsRequest
>;
export declare function hooksNumberLogsRequestToJSON(
  hooksNumberLogsRequest: HooksNumberLogsRequest,
): string;
//# sourceMappingURL=hooksnumberlogs.d.ts.map
