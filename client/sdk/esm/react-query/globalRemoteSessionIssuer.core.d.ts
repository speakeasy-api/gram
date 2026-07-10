import {
  QueryClient,
  QueryFunctionContext,
  QueryKey,
} from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { RemoteSessionIssuer } from "../models/components/remotesessionissuer.js";
import {
  GetGlobalRemoteSessionIssuerRequest,
  GetGlobalRemoteSessionIssuerSecurity,
} from "../models/operations/getglobalremotesessionissuer.js";
export type GlobalRemoteSessionIssuerQueryData = RemoteSessionIssuer;
export declare function prefetchGlobalRemoteSessionIssuer(
  queryClient: QueryClient,
  client$: GramCore,
  request: GetGlobalRemoteSessionIssuerRequest,
  security?: GetGlobalRemoteSessionIssuerSecurity | undefined,
  options?: RequestOptions,
): Promise<void>;
export declare function buildGlobalRemoteSessionIssuerQuery(
  client$: GramCore,
  request: GetGlobalRemoteSessionIssuerRequest,
  security?: GetGlobalRemoteSessionIssuerSecurity | undefined,
  options?: RequestOptions,
): {
  queryKey: QueryKey;
  queryFn: (
    context: QueryFunctionContext,
  ) => Promise<GlobalRemoteSessionIssuerQueryData>;
};
export declare function queryKeyGlobalRemoteSessionIssuer(parameters: {
  id: string;
  gramSession?: string | undefined;
}): QueryKey;
//# sourceMappingURL=globalRemoteSessionIssuer.core.d.ts.map
