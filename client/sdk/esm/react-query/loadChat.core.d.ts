import {
  QueryClient,
  QueryFunctionContext,
  QueryKey,
} from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import { Chat } from "../models/components/chat.js";
import {
  LoadChatRequest,
  LoadChatSecurity,
} from "../models/operations/loadchat.js";
export type LoadChatQueryData = Chat;
export declare function prefetchLoadChat(
  queryClient: QueryClient,
  client$: GramCore,
  request: LoadChatRequest,
  security?: LoadChatSecurity | undefined,
  options?: RequestOptions,
): Promise<void>;
export declare function buildLoadChatQuery(
  client$: GramCore,
  request: LoadChatRequest,
  security?: LoadChatSecurity | undefined,
  options?: RequestOptions,
): {
  queryKey: QueryKey;
  queryFn: (context: QueryFunctionContext) => Promise<LoadChatQueryData>;
};
export declare function queryKeyLoadChat(parameters: {
  id: string;
  generation?: number | undefined;
  limit?: number | undefined;
  beforeSeq?: number | undefined;
  afterSeq?: number | undefined;
  fromStart?: boolean | undefined;
  riskOnly?: boolean | undefined;
  query?: string | undefined;
  gramSession?: string | undefined;
  gramProject?: string | undefined;
  gramChatSession?: string | undefined;
}): QueryKey;
//# sourceMappingURL=loadChat.core.d.ts.map
