import { ClientSDK, RequestOptions } from "../lib/sdks.js";
import { ListResourcesResult } from "../models/components/listresourcesresult.js";
import {
  ListResourcesRequest,
  ListResourcesSecurity,
} from "../models/operations/listresources.js";
export declare class Resources extends ClientSDK {
  /**
   * listResources resources
   *
   * @remarks
   * List all resources for a project
   */
  list(
    request?: ListResourcesRequest | undefined,
    security?: ListResourcesSecurity | undefined,
    options?: RequestOptions,
  ): Promise<ListResourcesResult>;
}
//# sourceMappingURL=resources.d.ts.map
