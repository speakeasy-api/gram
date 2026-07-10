import { ClientSDK, RequestOptions } from "../lib/sdks.js";
import { GetIntegrationResult } from "../models/components/getintegrationresult.js";
import { ListIntegrationsResult } from "../models/components/listintegrationsresult.js";
import {
  IntegrationsNumberGetRequest,
  IntegrationsNumberGetSecurity,
} from "../models/operations/integrationsnumberget.js";
import {
  ListIntegrationsRequest,
  ListIntegrationsSecurity,
} from "../models/operations/listintegrations.js";
export declare class Integrations extends ClientSDK {
  /**
   * get integrations
   *
   * @remarks
   * Get a third-party integration by ID or name.
   */
  integrationsNumberGet(
    request?: IntegrationsNumberGetRequest | undefined,
    security?: IntegrationsNumberGetSecurity | undefined,
    options?: RequestOptions,
  ): Promise<GetIntegrationResult>;
  /**
   * list integrations
   *
   * @remarks
   * List available third-party integrations.
   */
  list(
    request?: ListIntegrationsRequest | undefined,
    security?: ListIntegrationsSecurity | undefined,
    options?: RequestOptions,
  ): Promise<ListIntegrationsResult>;
}
//# sourceMappingURL=integrations.d.ts.map
