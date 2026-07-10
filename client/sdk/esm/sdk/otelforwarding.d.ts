import { ClientSDK, RequestOptions } from "../lib/sdks.js";
import { OtelForwardingConfig } from "../models/components/otelforwardingconfig.js";
import {
  DeleteOtelForwardingConfigRequest,
  DeleteOtelForwardingConfigSecurity,
} from "../models/operations/deleteotelforwardingconfig.js";
import {
  GetOtelForwardingConfigRequest,
  GetOtelForwardingConfigSecurity,
} from "../models/operations/getotelforwardingconfig.js";
import {
  UpsertOtelForwardingConfigRequest,
  UpsertOtelForwardingConfigSecurity,
} from "../models/operations/upsertotelforwardingconfig.js";
export declare class OtelForwarding extends ClientSDK {
  /**
   * deleteConfig otelForwarding
   *
   * @remarks
   * Delete the org-wide OTEL forwarding config.
   */
  deleteConfig(
    request?: DeleteOtelForwardingConfigRequest | undefined,
    security?: DeleteOtelForwardingConfigSecurity | undefined,
    options?: RequestOptions,
  ): Promise<void>;
  /**
   * getConfig otelForwarding
   *
   * @remarks
   * Get the org-wide OTEL forwarding config. Returns an empty config (enabled=false, no URL) when none is set.
   */
  getConfig(
    request?: GetOtelForwardingConfigRequest | undefined,
    security?: GetOtelForwardingConfigSecurity | undefined,
    options?: RequestOptions,
  ): Promise<OtelForwardingConfig>;
  /**
   * upsertConfig otelForwarding
   *
   * @remarks
   * Create or update the org-wide OTEL forwarding config. Replaces the full header set on each call.
   */
  upsertConfig(
    request: UpsertOtelForwardingConfigRequest,
    security?: UpsertOtelForwardingConfigSecurity | undefined,
    options?: RequestOptions,
  ): Promise<OtelForwardingConfig>;
}
//# sourceMappingURL=otelforwarding.d.ts.map
