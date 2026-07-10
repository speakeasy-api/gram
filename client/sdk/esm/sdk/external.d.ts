import { ClientSDK, RequestOptions } from "../lib/sdks.js";
import { ReceiveWorkOSWebhookRequest } from "../models/operations/receiveworkoswebhook.js";
export declare class External extends ClientSDK {
  /**
   * receiveWorkOSWebhook external
   *
   * @remarks
   * Receive and enqueue a WorkOS webhook event.
   */
  receiveWorkOSWebhook(
    request?: ReceiveWorkOSWebhookRequest | undefined,
    options?: RequestOptions,
  ): Promise<void>;
}
//# sourceMappingURL=external.d.ts.map
