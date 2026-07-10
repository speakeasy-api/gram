import {
  QueryClient,
  QueryFunctionContext,
  QueryKey,
} from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { UserSessionClient } from "../models/components/usersessionclient.js";
import {
  GetUserSessionClientRequest,
  GetUserSessionClientSecurity,
} from "../models/operations/getusersessionclient.js";
export type UserSessionClientQueryData = UserSessionClient;
export declare function prefetchUserSessionClient(
  queryClient: QueryClient,
  client$: GramCore,
  request: GetUserSessionClientRequest,
  security?: GetUserSessionClientSecurity | undefined,
  options?: RequestOptions,
): Promise<void>;
export declare function buildUserSessionClientQuery(
  client$: GramCore,
  request: GetUserSessionClientRequest,
  security?: GetUserSessionClientSecurity | undefined,
  options?: RequestOptions,
): {
  queryKey: QueryKey;
  queryFn: (
    context: QueryFunctionContext,
  ) => Promise<UserSessionClientQueryData>;
};
export declare function queryKeyUserSessionClient(parameters: {
  id: string;
  gramSession?: string | undefined;
  gramKey?: string | undefined;
  gramProject?: string | undefined;
}): QueryKey;
//# sourceMappingURL=userSessionClient.core.d.ts.map
