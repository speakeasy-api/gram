import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
/**
 * Result of capturing a telemetry event
 */
export type CaptureEventResult = {
  /**
   * Whether the event was successfully captured
   */
  success: boolean;
};
/** @internal */
export declare const CaptureEventResult$inboundSchema: z.ZodMiniType<
  CaptureEventResult,
  unknown
>;
export declare function captureEventResultFromJSON(
  jsonString: string,
): SafeParseResult<CaptureEventResult, SDKValidationError>;
//# sourceMappingURL=captureeventresult.d.ts.map
