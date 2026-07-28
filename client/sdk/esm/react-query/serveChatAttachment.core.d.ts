import {
  QueryClient,
  QueryFunctionContext,
  QueryKey,
} from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import {
  ServeChatAttachmentRequest,
  ServeChatAttachmentResponse,
  ServeChatAttachmentSecurity,
} from "../models/operations/servechatattachment.js";
export type ServeChatAttachmentQueryData = ServeChatAttachmentResponse;
export declare function prefetchServeChatAttachment(
  queryClient: QueryClient,
  client$: GramCore,
  request: ServeChatAttachmentRequest,
  security?: ServeChatAttachmentSecurity | undefined,
  options?: RequestOptions,
): Promise<void>;
export declare function buildServeChatAttachmentQuery(
  client$: GramCore,
  request: ServeChatAttachmentRequest,
  security?: ServeChatAttachmentSecurity | undefined,
  options?: RequestOptions,
): {
  queryKey: QueryKey;
  queryFn: (
    context: QueryFunctionContext,
  ) => Promise<ServeChatAttachmentQueryData>;
};
export declare function queryKeyServeChatAttachment(parameters: {
  id: string;
  projectId: string;
  gramKey?: string | undefined;
  gramSession?: string | undefined;
  gramChatSession?: string | undefined;
}): QueryKey;
//# sourceMappingURL=serveChatAttachment.core.d.ts.map
