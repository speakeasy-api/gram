import { ClientSDK, RequestOptions } from "../lib/sdks.js";
import { CreateResponseBody } from "../models/components/createresponsebody.js";
import {
  CreateChatSessionRequest,
  CreateChatSessionSecurity,
} from "../models/operations/createchatsession.js";
import {
  RevokeChatSessionRequest,
  RevokeChatSessionSecurity,
} from "../models/operations/revokechatsession.js";
export declare class ChatSessions extends ClientSDK {
  /**
   * create chatSessions
   *
   * @remarks
   * Creates a new chat session token
   */
  create(
    request: CreateChatSessionRequest,
    security?: CreateChatSessionSecurity | undefined,
    options?: RequestOptions,
  ): Promise<CreateResponseBody>;
  /**
   * revoke chatSessions
   *
   * @remarks
   * Revokes an existing chat session token
   */
  revoke(
    request: RevokeChatSessionRequest,
    security?: RevokeChatSessionSecurity | undefined,
    options?: RequestOptions,
  ): Promise<void>;
}
//# sourceMappingURL=chatsessions.d.ts.map
