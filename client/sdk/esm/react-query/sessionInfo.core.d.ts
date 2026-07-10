import {
  QueryClient,
  QueryFunctionContext,
  QueryKey,
} from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import {
  SessionInfoRequest,
  SessionInfoResponse,
  SessionInfoSecurity,
} from "../models/operations/sessioninfo.js";
export type SessionInfoQueryData = SessionInfoResponse;
export declare function prefetchSessionInfo(
  queryClient: QueryClient,
  client$: GramCore,
  request?: SessionInfoRequest | undefined,
  security?: SessionInfoSecurity | undefined,
  options?: RequestOptions,
): Promise<void>;
export declare function buildSessionInfoQuery(
  client$: GramCore,
  request?: SessionInfoRequest | undefined,
  security?: SessionInfoSecurity | undefined,
  options?: RequestOptions,
): {
  queryKey: QueryKey;
  queryFn: (context: QueryFunctionContext) => Promise<SessionInfoQueryData>;
};
export declare function queryKeySessionInfo(parameters: {
  gramSession?: string | undefined;
}): QueryKey;
//# sourceMappingURL=sessionInfo.core.d.ts.map
