import { ClientSDK, RequestOptions } from "../lib/sdks.js";
import { AssistantMemory } from "../models/components/assistantmemory.js";
import {
  DeleteAssistantMemoryRequest,
  DeleteAssistantMemorySecurity,
} from "../models/operations/deleteassistantmemory.js";
import {
  GetAssistantMemoryRequest,
  GetAssistantMemorySecurity,
} from "../models/operations/getassistantmemory.js";
import {
  ListAssistantMemoriesRequest,
  ListAssistantMemoriesResponse,
  ListAssistantMemoriesSecurity,
} from "../models/operations/listassistantmemories.js";
import { PageIterator } from "../types/operations.js";
export declare class AssistantMemories extends ClientSDK {
  /**
   * deleteAssistantMemory assistantMemories
   *
   * @remarks
   * Delete an assistant memory by ID.
   */
  delete(
    request: DeleteAssistantMemoryRequest,
    security?: DeleteAssistantMemorySecurity | undefined,
    options?: RequestOptions,
  ): Promise<void>;
  /**
   * getAssistantMemory assistantMemories
   *
   * @remarks
   * Get an assistant memory by ID.
   */
  get(
    request: GetAssistantMemoryRequest,
    security?: GetAssistantMemorySecurity | undefined,
    options?: RequestOptions,
  ): Promise<AssistantMemory>;
  /**
   * listAssistantMemories assistantMemories
   *
   * @remarks
   * List assistant memories for an assistant.
   */
  list(
    request: ListAssistantMemoriesRequest,
    security?: ListAssistantMemoriesSecurity | undefined,
    options?: RequestOptions,
  ): Promise<
    PageIterator<
      ListAssistantMemoriesResponse,
      {
        cursor: string;
      }
    >
  >;
}
//# sourceMappingURL=assistantmemories.d.ts.map
