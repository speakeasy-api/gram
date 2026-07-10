import { ClientSDK, RequestOptions } from "../lib/sdks.js";
import { TestDetectionRuleResult } from "../models/components/testdetectionruleresult.js";
import {
  TestDetectionRuleRequest,
  TestDetectionRuleSecurity,
} from "../models/operations/testdetectionrule.js";
export declare class Rules extends ClientSDK {
  /**
   * testDetectionRule risk
   *
   * @remarks
   * Run a single detection rule against pasted sample text and return any matches. Reuses the same scanner code (gitleaks, Presidio, prompt-injection, custom regex) that the analyzer runs in production so the playground match shape mirrors the chat-message path.
   */
  test(
    request: TestDetectionRuleRequest,
    security?: TestDetectionRuleSecurity | undefined,
    options?: RequestOptions,
  ): Promise<TestDetectionRuleResult>;
}
//# sourceMappingURL=rules.d.ts.map
