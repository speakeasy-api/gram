import * as z from "zod/v4-mini";
import {
  OTELAttributeValue,
  OTELAttributeValue$Outbound,
} from "./otelattributevalue.js";
/**
 * OTEL resource attribute
 */
export type OTELResourceAttribute = {
  /**
   * Resource attribute key
   */
  key: string;
  /**
   * OTEL attribute value - any of the OTLP/JSON value kinds
   */
  value?: OTELAttributeValue | undefined;
};
/** @internal */
export type OTELResourceAttribute$Outbound = {
  key: string;
  value?: OTELAttributeValue$Outbound | undefined;
};
/** @internal */
export declare const OTELResourceAttribute$outboundSchema: z.ZodMiniType<
  OTELResourceAttribute$Outbound,
  OTELResourceAttribute
>;
export declare function otelResourceAttributeToJSON(
  otelResourceAttribute: OTELResourceAttribute,
): string;
//# sourceMappingURL=otelresourceattribute.d.ts.map
