import { ClientSDK, RequestOptions } from "../lib/sdks.js";
import { CaptureEventResult } from "../models/components/captureeventresult.js";
import { Chat as Chat$Model } from "../models/components/chat.js";
import { CreditUsageResponseBody } from "../models/components/creditusageresponsebody.js";
import { GenerateTitleResponseBody } from "../models/components/generatetitleresponsebody.js";
import { ListChatsResult } from "../models/components/listchatsresult.js";
import { ListSourcesResult } from "../models/components/listsourcesresult.js";
import {
  CreditUsageRequest,
  CreditUsageSecurity,
} from "../models/operations/creditusage.js";
import {
  DeleteChatRequest,
  DeleteChatSecurity,
} from "../models/operations/deletechat.js";
import {
  GenerateTitleRequest,
  GenerateTitleSecurity,
} from "../models/operations/generatetitle.js";
import {
  ListChatsRequest,
  ListChatsSecurity,
} from "../models/operations/listchats.js";
import {
  ListChatSourcesRequest,
  ListChatSourcesSecurity,
} from "../models/operations/listchatsources.js";
import {
  LoadChatRequest,
  LoadChatSecurity,
} from "../models/operations/loadchat.js";
import {
  SetChatPinnedRequest,
  SetChatPinnedSecurity,
} from "../models/operations/setchatpinned.js";
import {
  SubmitFeedbackRequest,
  SubmitFeedbackSecurity,
} from "../models/operations/submitfeedback.js";
export declare class Chat extends ClientSDK {
  /**
   * creditUsage chat
   *
   * @remarks
   * Get the total number of chat credits and usage for the current billing period
   */
  creditUsage(
    request?: CreditUsageRequest | undefined,
    security?: CreditUsageSecurity | undefined,
    options?: RequestOptions,
  ): Promise<CreditUsageResponseBody>;
  /**
   * deleteChat chat
   *
   * @remarks
   * Soft-delete a chat by its ID
   */
  delete(
    request: DeleteChatRequest,
    security?: DeleteChatSecurity | undefined,
    options?: RequestOptions,
  ): Promise<void>;
  /**
   * generateTitle chat
   *
   * @remarks
   * Read or set a chat's title. Omit `title` to return the current/auto-generated title (titles are generated asynchronously after a completion). Provide `title` to set a manual title that auto-generation will never overwrite; provide an empty `title` to clear the manual title and re-enable auto-generation.
   */
  generateTitle(
    request: GenerateTitleRequest,
    security?: GenerateTitleSecurity | undefined,
    options?: RequestOptions,
  ): Promise<GenerateTitleResponseBody>;
  /**
   * listChats chat
   *
   * @remarks
   * List all chats for a project
   */
  list(
    request?: ListChatsRequest | undefined,
    security?: ListChatsSecurity | undefined,
    options?: RequestOptions,
  ): Promise<ListChatsResult>;
  /**
   * listSources chat
   *
   * @remarks
   * List the distinct agent sources present in this project's chats, for populating the agent-type filter on the Agent Sessions page.
   */
  listSources(
    request?: ListChatSourcesRequest | undefined,
    security?: ListChatSourcesSecurity | undefined,
    options?: RequestOptions,
  ): Promise<ListSourcesResult>;
  /**
   * loadChat chat
   *
   * @remarks
   * Load a chat by its ID. Messages within a generation are paginated by `seq` keyset: omit cursors to receive the newest page, pass `before_seq` to load older messages (scroll up) or `after_seq` to load newer ones (scroll down). Set `from_start` to receive the oldest page (the start of the thread) instead of the newest. Omit `generation` to receive the latest generation. Set `risk_only` to return only messages with risk findings plus a few messages of surrounding context per finding. Set `query` to instead return only messages whose text matches a search query plus surrounding context (mutually exclusive with `risk_only`).
   */
  load(
    request: LoadChatRequest,
    security?: LoadChatSecurity | undefined,
    options?: RequestOptions,
  ): Promise<Chat$Model>;
  /**
   * setPinned chat
   *
   * @remarks
   * Pin or unpin a chat. Pinned chats surface in a dedicated section above recents on the chat page.
   */
  setPinned(
    request: SetChatPinnedRequest,
    security?: SetChatPinnedSecurity | undefined,
    options?: RequestOptions,
  ): Promise<void>;
  /**
   * submitFeedback chat
   *
   * @remarks
   * Submit user feedback for a chat (success/failure)
   */
  submitFeedback(
    request: SubmitFeedbackRequest,
    security?: SubmitFeedbackSecurity | undefined,
    options?: RequestOptions,
  ): Promise<CaptureEventResult>;
}
//# sourceMappingURL=chat.d.ts.map
