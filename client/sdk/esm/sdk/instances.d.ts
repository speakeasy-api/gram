import { ClientSDK, RequestOptions } from "../lib/sdks.js";
import { GetInstanceResult } from "../models/components/getinstanceresult.js";
import { GetInstanceRequest, GetInstanceSecurity } from "../models/operations/getinstance.js";
export declare class Instances extends ClientSDK {
    /**
     * getInstance instances
     *
     * @remarks
     * Load all relevant data for an instance of a toolset and environment
     */
    getBySlug(request: GetInstanceRequest, security?: GetInstanceSecurity | undefined, options?: RequestOptions): Promise<GetInstanceResult>;
}
//# sourceMappingURL=instances.d.ts.map