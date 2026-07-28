import { ClientSDK, RequestOptions } from "../lib/sdks.js";
import { ServerNameOverride } from "../models/components/servernameoverride.js";
import {
  DeleteServerNameOverrideRequest,
  DeleteServerNameOverrideSecurity,
} from "../models/operations/deleteservernameoverride.js";
import {
  ListServerNameOverridesRequest,
  ListServerNameOverridesSecurity,
} from "../models/operations/listservernameoverrides.js";
import {
  UpsertServerNameOverrideRequest,
  UpsertServerNameOverrideSecurity,
} from "../models/operations/upsertservernameoverride.js";
export declare class HooksServerNames extends ClientSDK {
  /**
   * delete hooksServerNames
   *
   * @remarks
   * Delete a server name display override
   */
  deleteServerNameOverride(
    request: DeleteServerNameOverrideRequest,
    security?: DeleteServerNameOverrideSecurity | undefined,
    options?: RequestOptions,
  ): Promise<void>;
  /**
   * list hooksServerNames
   *
   * @remarks
   * List all server name display overrides for a project
   */
  listServerNameOverrides(
    request?: ListServerNameOverridesRequest | undefined,
    security?: ListServerNameOverridesSecurity | undefined,
    options?: RequestOptions,
  ): Promise<Array<ServerNameOverride>>;
  /**
   * upsert hooksServerNames
   *
   * @remarks
   * Create or update a server name display override
   */
  upsertServerNameOverride(
    request: UpsertServerNameOverrideRequest,
    security?: UpsertServerNameOverrideSecurity | undefined,
    options?: RequestOptions,
  ): Promise<ServerNameOverride>;
}
//# sourceMappingURL=hooksservernames.d.ts.map
