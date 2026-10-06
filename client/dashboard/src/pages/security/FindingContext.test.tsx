import type { ChatMessage } from "@gram/client/models/components/chatmessage.js";
import type { RiskResult } from "@gram/client/models/components/riskresult.js";
import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { FindingContext } from "./FindingContext";
import { findingKind } from "./finding-kind";

const SECRET = "AKIAABCDEFGHIJKLMNOP";
const BEHAVIOR = "rm -rf /";
const TEXT = `deploy with ${SECRET} then run ${BEHAVIOR}`;

// Whether `value` appears anywhere in the rendered text.
const shows = (value: string) => document.body.textContent?.includes(value);

const messages: ChatMessage[] = [
  {
    id: "m1",
    role: "user",
    content: TEXT,
    seq: 1,
    generation: 0,
    model: "",
    createdAt: new Date("2026-09-16T00:00:00Z"),
  },
];

vi.mock("@gram/client/react-query/loadChat.js", () => ({
  useLoadChat: () => ({
    data: { messages, numMessages: 1 },
    isLoading: false,
    isError: false,
    error: null,
  }),
}));

vi.mock("@gram/client/react-query/riskUnmaskResult.js", () => ({
  useRiskUnmaskResultMutation: () => ({
    // Every audited reveal succeeds.
    mutate: (_req: unknown, opts: { onSuccess: (res: object) => void }) =>
      opts.onSuccess({ match: "" }),
    isPending: false,
    isError: false,
  }),
}));

function finding(overrides: Partial<RiskResult>): RiskResult {
  return {
    id: "r",
    policyId: "p1",
    policyVersion: 1,
    createdAt: new Date("2026-09-16T00:00:00Z"),
    source: "gitleaks",
    confidence: 1,
    chatId: "c1",
    chatMessageId: "m1",
    ...overrides,
  };
}

const secret = finding({ id: "secret", source: "gitleaks", match: SECRET });
const behavior = finding({ id: "behavior", source: "regex", match: BEHAVIOR });
const judge = finding({ id: "judge", source: "llm_judge" });

function renderContext(
  result: RiskResult,
  chatResults: RiskResult[],
  { revealed = false, complete = true } = {},
) {
  render(
    <FindingContext
      result={result}
      kind={findingKind(result)}
      chatResults={chatResults}
      chatResultsComplete={complete}
      revealed={revealed}
      canReveal
      rating={null}
      onToggleReveal={vi.fn<() => void>()}
      onRequestReveal={vi.fn<() => void>()}
    />,
  );
}

afterEach(cleanup);

describe("FindingContext masking", () => {
  it("keeps a sibling secret masked when a whole-message judge finding is revealed", () => {
    renderContext(judge, [judge, secret], { revealed: true });
    expect(shows(SECRET)).toBe(false);
    expect(shows("then run")).toBe(true);
  });

  it("keeps the message masked when the chat's findings are incomplete", () => {
    renderContext(judge, [judge], { revealed: true, complete: false });
    expect(shows("then run")).toBe(false);
    expect(screen.getByText(/Flagged event/)).toBeTruthy();
  });

  it("renders behavioral matches directly when the message holds no secret", () => {
    renderContext(behavior, [behavior]);
    expect(screen.getByText(BEHAVIOR)).toBeTruthy();
    expect(screen.queryByText(/reveal/i)).toBeNull();
  });

  it("masks every match in a message that holds a secret", () => {
    renderContext(behavior, [behavior, secret]);
    expect(shows(BEHAVIOR)).toBe(false);
    expect(shows(SECRET)).toBe(false);
  });
});
