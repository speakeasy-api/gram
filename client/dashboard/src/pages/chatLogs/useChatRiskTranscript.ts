import type { Chat, RiskSegment } from "@gram/client/models/components";
import {
  useWindowedTranscript,
  type WindowedTranscript,
} from "./useWindowedTranscript";

// Risk-only view: windowed around messages with active risk findings. The
// window segments arrive in `risk_segments`.
const riskSegmentsOf = (chat: Chat): RiskSegment[] | undefined =>
  chat.riskSegments;

export type ChatRiskTranscript = WindowedTranscript;

// useChatRiskTranscript loads only risk findings plus surrounding context, then
// lets the user expand the edges and fill the gaps between disjoint findings.
// Thin wrapper over the shared windowed-transcript core.
export function useChatRiskTranscript(
  chatId: string,
  enabled: boolean,
): ChatRiskTranscript {
  return useWindowedTranscript(
    chatId,
    enabled,
    { riskOnly: true },
    riskSegmentsOf,
  );
}
