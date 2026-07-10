import { QueryClient, QueryFunctionContext, QueryKey } from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { GetInstanceResult } from "../models/components/getinstanceresult.js";
import { GetInstanceRequest, GetInstanceSecurity } from "../models/operations/getinstance.js";
export type InstanceQueryData = GetInstanceResult;
export declare function prefetchInstance(queryClient: QueryClient, client$: GramCore, request: GetInstanceRequest, security?: GetInstanceSecurity | undefined, options?: RequestOptions): Promise<void>;
export declare function buildInstanceQuery(client$: GramCore, request: GetInstanceRequest, security?: GetInstanceSecurity | undefined, options?: RequestOptions): {
    queryKey: QueryKey;
    queryFn: (context: QueryFunctionContext) => Promise<InstanceQueryData>;
};
export declare function queryKeyInstance(parameters: {
    toolsetSlug: string;
    gramSession?: string | undefined;
    gramProject?: string | undefined;
    gramKey?: string | undefined;
    gramChatSession?: string | undefined;
}): QueryKey;
//# sourceMappingURL=instance.core.d.ts.map