import * as z from "zod/v4-mini";
import {
  OtelForwardingHeaderInput,
  OtelForwardingHeaderInput$Outbound,
} from "./otelforwardingheaderinput.js";
export type UpsertConfigRequestBody2 = {
  /**
   * Whether forwarding should be active.
   */
  enabled: boolean;
  /**
   * URL to forward OTEL payloads to.
   */
  endpointUrl: string;
  /**
   * Full set of headers to attach. Replaces any existing headers.
   */
  headers?: Array<OtelForwardingHeaderInput> | undefined;
};
/** @internal */
export type UpsertConfigRequestBody2$Outbound = {
  enabled: boolean;
  endpoint_url: string;
  headers?: Array<OtelForwardingHeaderInput$Outbound> | undefined;
};
/** @internal */
export declare const UpsertConfigRequestBody2$outboundSchema: z.ZodMiniType<
  UpsertConfigRequestBody2$Outbound,
  UpsertConfigRequestBody2
>;
export declare function upsertConfigRequestBody2ToJSON(
  upsertConfigRequestBody2: UpsertConfigRequestBody2,
): string;
//# sourceMappingURL=upsertconfigrequestbody2.d.ts.map
