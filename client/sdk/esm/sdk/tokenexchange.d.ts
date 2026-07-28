import { ClientSDK, RequestOptions } from "../lib/sdks.js";
import * as components from "../models/components/index.js";
import * as operations from "../models/operations/index.js";
export declare class TokenExchange extends ClientSDK {
  /**
   * exchange tokenExchange
   *
   * @remarks
   * Exchange the org-scoped device-agent install credential plus a vouched user email for a long-lived, per-user API key carrying the 'agent_user' scope. Authenticated with an org-scoped API key carrying the 'agent' scope — deliberately broader than the 'agent_user' scope the minted per-user keys carry, so a leaked per-user key cannot re-enter this endpoint to forge another user's key. The raw key is returned exactly once.
   */
  exchange(
    request: operations.TokenExchangeRequest,
    security?: operations.TokenExchangeSecurity | undefined,
    options?: RequestOptions,
  ): Promise<components.TokenResult>;
}
//# sourceMappingURL=tokenexchange.d.ts.map
