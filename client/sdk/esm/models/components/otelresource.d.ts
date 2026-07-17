import * as z from "zod/v4-mini";
import {
  OTELResourceAttribute,
  OTELResourceAttribute$Outbound,
} from "./otelresourceattribute.js";
/**
 * OTEL resource information
 */
export type OTELResource = {
  /**
   * Resource attributes
   */
  attributes?: Array<OTELResourceAttribute> | undefined;
  /**
   * Number of dropped attributes
   */
  droppedAttributesCount?: number | undefined;
};
/** @internal */
export type OTELResource$Outbound = {
  attributes?: Array<OTELResourceAttribute$Outbound> | undefined;
  droppedAttributesCount?: number | undefined;
};
/** @internal */
export declare const OTELResource$outboundSchema: z.ZodMiniType<
  OTELResource$Outbound,
  OTELResource
>;
export declare function otelResourceToJSON(otelResource: OTELResource): string;
//# sourceMappingURL=otelresource.d.ts.map
