import type { Chat, RiskSegment } from "@gram/client/models/components";
import {
  useWindowedTranscript,
  type WindowedTranscript,
} from "./useWindowedTranscript";

// Search view: windowed around messages whose text matches the query. The
// window segments arrive in `match_segments`; the matched seqs (jump targets)
// arrive in `match_seqs`.
const matchSegmentsOf = (chat: Chat): RiskSegment[] | undefined =>
  chat.matchSegments;

export interface ChatSearchTranscript extends WindowedTranscript {
  /** Seqs of messages that matched the query, ascending — the jump targets. */
  matchSeqs: number[];
}

// Stable empty array so consumers' memo/effect deps don't see a new identity
// every render while there are no matches (or before the first response).
const EMPTY_SEQS: number[] = [];

// useChatSearchTranscript loads the messages matching a text query plus
// surrounding context, with the same edge/gap expansion as the risk view, and
// additionally surfaces the matched seqs for next/prev navigation.
export function useChatSearchTranscript(
  chatId: string,
  query: string,
  enabled: boolean,
): ChatSearchTranscript {
  const windowed = useWindowedTranscript(
    chatId,
    enabled,
    { query },
    matchSegmentsOf,
  );
  // matchSeqs come from the initial query response and don't change as the user
  // expands the window, so read them straight off the base chat. The base chat
  // ref is stable across renders (react-query data), so this identity is stable.
  return { ...windowed, matchSeqs: windowed.chat?.matchSeqs ?? EMPTY_SEQS };
}
