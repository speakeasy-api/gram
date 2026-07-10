import { ClientSDK, RequestOptions } from "../lib/sdks.js";
import { RiskBlock } from "../models/components/riskblock.js";
import {
  GetRiskBlockRequest,
  GetRiskBlockSecurity,
} from "../models/operations/getriskblock.js";
import {
  SubmitRiskBlockFeedbackRequest,
  SubmitRiskBlockFeedbackSecurity,
} from "../models/operations/submitriskblockfeedback.js";
export declare class Blocks extends ClientSDK {
  /**
   * getRiskBlock risk
   *
   * @remarks
   * Get a tool call block by its risk result ID for the durable block page.
   */
  get(
    request: GetRiskBlockRequest,
    security?: GetRiskBlockSecurity | undefined,
    options?: RequestOptions,
  ): Promise<RiskBlock>;
  /**
   * submitRiskBlockFeedback risk
   *
   * @remarks
   * Record thumbs-up/thumbs-down feedback for a tool call block from the block page.
   */
  submitFeedback(
    request: SubmitRiskBlockFeedbackRequest,
    security?: SubmitRiskBlockFeedbackSecurity | undefined,
    options?: RequestOptions,
  ): Promise<RiskBlock>;
}
//# sourceMappingURL=blocks.d.ts.map
