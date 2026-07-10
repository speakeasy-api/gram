import * as z from "zod/v4-mini";
import {
  OTELAttributeValue,
  OTELAttributeValue$Outbound,
} from "./otelattributevalue.js";
/**
 * OTEL log attribute with key and typed value
 */
export type OTELAttribute = {
  /**
   * Attribute key
   */
  key: string;
  /**
   * OTEL attribute value - any of the OTLP/JSON value kinds
   */
  value?: OTELAttributeValue | undefined;
};
/** @internal */
export type OTELAttribute$Outbound = {
  key: string;
  value?: OTELAttributeValue$Outbound | undefined;
};
/** @internal */
export declare const OTELAttribute$outboundSchema: z.ZodMiniType<
  OTELAttribute$Outbound,
  OTELAttribute
>;
export declare function otelAttributeToJSON(
  otelAttribute: OTELAttribute,
): string;
//# sourceMappingURL=otelattribute.d.ts.map
