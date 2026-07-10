import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
/**
 * Service information
 */
export type ServiceInfo = {
  /**
   * Service name
   */
  name: string;
  /**
   * Service version
   */
  version?: string | undefined;
};
/** @internal */
export declare const ServiceInfo$inboundSchema: z.ZodMiniType<
  ServiceInfo,
  unknown
>;
export declare function serviceInfoFromJSON(
  jsonString: string,
): SafeParseResult<ServiceInfo, SDKValidationError>;
//# sourceMappingURL=serviceinfo.d.ts.map
