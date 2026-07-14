import * as z from "zod/v4-mini";
/**
 * Payload for capturing a telemetry event
 */
export type CaptureEventPayload = {
  /**
   * Distinct ID for the user or entity (defaults to organization ID if not provided)
   */
  distinctId?: string | undefined;
  /**
   * Event name
   */
  event: string;
  /**
   * Event properties as key-value pairs
   */
  properties?:
    | {
        [k: string]: any;
      }
    | undefined;
};
/** @internal */
export type CaptureEventPayload$Outbound = {
  distinct_id?: string | undefined;
  event: string;
  properties?:
    | {
        [k: string]: any;
      }
    | undefined;
};
/** @internal */
export declare const CaptureEventPayload$outboundSchema: z.ZodMiniType<
  CaptureEventPayload$Outbound,
  CaptureEventPayload
>;
export declare function captureEventPayloadToJSON(
  captureEventPayload: CaptureEventPayload,
): string;
//# sourceMappingURL=captureeventpayload.d.ts.map
