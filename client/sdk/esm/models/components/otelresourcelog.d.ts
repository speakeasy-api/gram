import * as z from "zod/v4-mini";
import { OTELResource, OTELResource$Outbound } from "./otelresource.js";
import { OTELScopeLog, OTELScopeLog$Outbound } from "./otelscopelog.js";
/**
 * OTEL resource logs container
 */
export type OTELResourceLog = {
  /**
   * OTEL resource information
   */
  resource?: OTELResource | undefined;
  /**
   * Array of scope logs
   */
  scopeLogs?: Array<OTELScopeLog> | undefined;
};
/** @internal */
export type OTELResourceLog$Outbound = {
  resource?: OTELResource$Outbound | undefined;
  scopeLogs?: Array<OTELScopeLog$Outbound> | undefined;
};
/** @internal */
export declare const OTELResourceLog$outboundSchema: z.ZodMiniType<
  OTELResourceLog$Outbound,
  OTELResourceLog
>;
export declare function otelResourceLogToJSON(
  otelResourceLog: OTELResourceLog,
): string;
//# sourceMappingURL=otelresourcelog.d.ts.map
