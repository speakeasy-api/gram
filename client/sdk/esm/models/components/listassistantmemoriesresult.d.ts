import * as z from "zod/v4-mini";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
import { AssistantMemory } from "./assistantmemory.js";
export type ListAssistantMemoriesResult = {
  /**
   * Assistant memories matching the query.
   */
  memories: Array<AssistantMemory>;
  /**
   * The cursor to be used for the next page of results.
   */
  nextCursor?: string | undefined;
};
/** @internal */
export declare const ListAssistantMemoriesResult$inboundSchema: z.ZodMiniType<
  ListAssistantMemoriesResult,
  unknown
>;
export declare function listAssistantMemoriesResultFromJSON(
  jsonString: string,
): SafeParseResult<ListAssistantMemoriesResult, SDKValidationError>;
//# sourceMappingURL=listassistantmemoriesresult.d.ts.map
