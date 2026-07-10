import {
  QueryClient,
  QueryFunctionContext,
  QueryKey,
} from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { RemoteSessionIssuer } from "../models/components/remotesessionissuer.js";
import {
  GetRemoteSessionIssuerRequest,
  GetRemoteSessionIssuerSecurity,
} from "../models/operations/getremotesessionissuer.js";
export type RemoteSessionIssuerQueryData = RemoteSessionIssuer;
export declare function prefetchRemoteSessionIssuer(
  queryClient: QueryClient,
  client$: GramCore,
  request?: GetRemoteSessionIssuerRequest | undefined,
  security?: GetRemoteSessionIssuerSecurity | undefined,
  options?: RequestOptions,
): Promise<void>;
export declare function buildRemoteSessionIssuerQuery(
  client$: GramCore,
  request?: GetRemoteSessionIssuerRequest | undefined,
  security?: GetRemoteSessionIssuerSecurity | undefined,
  options?: RequestOptions,
): {
  queryKey: QueryKey;
  queryFn: (
    context: QueryFunctionContext,
  ) => Promise<RemoteSessionIssuerQueryData>;
};
export declare function queryKeyRemoteSessionIssuer(parameters: {
  id?: string | undefined;
  slug?: string | undefined;
  gramSession?: string | undefined;
  gramKey?: string | undefined;
  gramProject?: string | undefined;
}): QueryKey;
//# sourceMappingURL=remoteSessionIssuer.core.d.ts.map
