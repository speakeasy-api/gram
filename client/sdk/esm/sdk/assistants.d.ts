import { ClientSDK, RequestOptions } from "../lib/sdks.js";
import { Assistant } from "../models/components/assistant.js";
import { ListAssistantsResult } from "../models/components/listassistantsresult.js";
import { SendMessageResult } from "../models/components/sendmessageresult.js";
import {
  CreateAssistantRequest,
  CreateAssistantSecurity,
} from "../models/operations/createassistant.js";
import {
  DeleteAssistantRequest,
  DeleteAssistantSecurity,
} from "../models/operations/deleteassistant.js";
import {
  EnsureManagedAssistantRequest,
  EnsureManagedAssistantSecurity,
} from "../models/operations/ensuremanagedassistant.js";
import {
  GetAssistantRequest,
  GetAssistantSecurity,
} from "../models/operations/getassistant.js";
import {
  GetManagedAssistantRequest,
  GetManagedAssistantSecurity,
} from "../models/operations/getmanagedassistant.js";
import {
  ListAssistantsRequest,
  ListAssistantsSecurity,
} from "../models/operations/listassistants.js";
import {
  SendAssistantMessageRequest,
  SendAssistantMessageSecurity,
} from "../models/operations/sendassistantmessage.js";
import {
  UpdateAssistantRequest,
  UpdateAssistantSecurity,
} from "../models/operations/updateassistant.js";
export declare class Assistants extends ClientSDK {
  /**
   * createAssistant assistants
   *
   * @remarks
   * Create an assistant.
   */
  create(
    request: CreateAssistantRequest,
    security?: CreateAssistantSecurity | undefined,
    options?: RequestOptions,
  ): Promise<Assistant>;
  /**
   * deleteAssistant assistants
   *
   * @remarks
   * Delete an assistant.
   */
  delete(
    request: DeleteAssistantRequest,
    security?: DeleteAssistantSecurity | undefined,
    options?: RequestOptions,
  ): Promise<void>;
  /**
   * ensureManagedAssistant assistants
   *
   * @remarks
   * Get the project's built-in Project Assistant, provisioning it on first access. Idempotent — safe to call on every sidebar open.
   */
  ensureManaged(
    request?: EnsureManagedAssistantRequest | undefined,
    security?: EnsureManagedAssistantSecurity | undefined,
    options?: RequestOptions,
  ): Promise<Assistant>;
  /**
   * getAssistant assistants
   *
   * @remarks
   * Get an assistant by ID.
   */
  get(
    request: GetAssistantRequest,
    security?: GetAssistantSecurity | undefined,
    options?: RequestOptions,
  ): Promise<Assistant>;
  /**
   * getManagedAssistant assistants
   *
   * @remarks
   * Get the project's built-in Project Assistant if it exists. Returns 404 when no managed assistant has been provisioned yet — call ensureManagedAssistant to create one.
   */
  getManaged(
    request?: GetManagedAssistantRequest | undefined,
    security?: GetManagedAssistantSecurity | undefined,
    options?: RequestOptions,
  ): Promise<Assistant>;
  /**
   * listAssistants assistants
   *
   * @remarks
   * List assistants for the current project.
   */
  list(
    request?: ListAssistantsRequest | undefined,
    security?: ListAssistantsSecurity | undefined,
    options?: RequestOptions,
  ): Promise<ListAssistantsResult>;
  /**
   * sendMessage assistants
   *
   * @remarks
   * Send a message from the dashboard to an assistant as the calling user. Continue an existing conversation by passing its chat_id (from listChats), or omit chat_id to start a new conversation — the server mints and returns a fresh chat id. The reply is delivered asynchronously; poll the chat service (loadChat) to read it.
   */
  sendMessage(
    request: SendAssistantMessageRequest,
    security?: SendAssistantMessageSecurity | undefined,
    options?: RequestOptions,
  ): Promise<SendMessageResult>;
  /**
   * updateAssistant assistants
   *
   * @remarks
   * Update an assistant.
   */
  update(
    request: UpdateAssistantRequest,
    security?: UpdateAssistantSecurity | undefined,
    options?: RequestOptions,
  ): Promise<Assistant>;
}
//# sourceMappingURL=assistants.d.ts.map
