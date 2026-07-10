import { ClientSDK, RequestOptions } from "../lib/sdks.js";
import { ListToolsResult } from "../models/components/listtoolsresult.js";
import { ListToolsRequest, ListToolsSecurity } from "../models/operations/listtools.js";
export declare class Tools extends ClientSDK {
    /**
     * listResources resources
     *
     * @remarks
     * List all resources for a project
     */
    list(request?: ListToolsRequest | undefined, security?: ListToolsSecurity | undefined, options?: RequestOptions): Promise<ListToolsResult>;
}
//# sourceMappingURL=tools.d.ts.map