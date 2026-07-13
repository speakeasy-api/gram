import {
  QueryClient,
  QueryFunctionContext,
  QueryKey,
} from "@tanstack/react-query";
import { GramCore } from "../core.js";
import { RequestOptions } from "../lib/sdks.js";
import {
  ServeImageRequest,
  ServeImageResponse,
} from "../models/operations/serveimage.js";
export type ServeImageQueryData = ServeImageResponse;
export declare function prefetchServeImage(
  queryClient: QueryClient,
  client$: GramCore,
  request: ServeImageRequest,
  options?: RequestOptions,
): Promise<void>;
export declare function buildServeImageQuery(
  client$: GramCore,
  request: ServeImageRequest,
  options?: RequestOptions,
): {
  queryKey: QueryKey;
  queryFn: (context: QueryFunctionContext) => Promise<ServeImageQueryData>;
};
export declare function queryKeyServeImage(parameters: {
  id: string;
}): QueryKey;
//# sourceMappingURL=serveImage.core.d.ts.map
