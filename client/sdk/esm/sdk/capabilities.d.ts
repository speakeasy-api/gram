import { ClientSDK, RequestOptions } from "../lib/sdks.js";
import * as components from "../models/components/index.js";
import * as operations from "../models/operations/index.js";
export declare class Capabilities extends ClientSDK {
  /**
   * getRiskCapabilities risk
   *
   * @remarks
   * Get server-side risk analysis capabilities for the current project.
   */
  get(
    request?: operations.GetRiskCapabilitiesRequest | undefined,
    security?: operations.GetRiskCapabilitiesSecurity | undefined,
    options?: RequestOptions,
  ): Promise<components.RiskCapabilitiesResult>;
}
//# sourceMappingURL=capabilities.d.ts.map
