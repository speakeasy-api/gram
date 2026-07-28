import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
import { OtelForwardingDestination } from "./otelforwardingdestination.js";
/**
 * Wraps a list of forwarding destinations.
 */
export type OtelForwardingDestinationList = {
  destinations: Array<OtelForwardingDestination>;
};
/** @internal */
export declare const OtelForwardingDestinationList$inboundSchema: z.ZodMiniType<
  OtelForwardingDestinationList,
  unknown
>;
export declare function otelForwardingDestinationListFromJSON(
  jsonString: string,
): SafeParseResult<OtelForwardingDestinationList, SDKValidationError>;
//# sourceMappingURL=otelforwardingdestinationlist.d.ts.map
