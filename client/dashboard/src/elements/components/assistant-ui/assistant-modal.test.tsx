import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it, vi } from "vitest";

const mocks = vi.hoisted(() => ({
  stateSelector: undefined as ((state: unknown) => unknown) | undefined,
}));

vi.mock("@assistant-ui/react", () => ({
  useAuiState: (selector: (state: unknown) => unknown) => {
    mocks.stateSelector = selector;
    return selector({ thread: { messages: [] } });
  },
}));

vi.mock("@/elements/hooks/useElements", () => ({
  useElements: () => ({
    config: {
      history: { enabled: false },
      modal: { defaultOpen: false, title: "Assistant" },
      theme: {},
      variant: "widget",
    },
    isExpanded: false,
    setIsExpanded: vi.fn(),
  }),
}));

vi.mock("@/elements/components/assistant-ui/thread", () => ({ Thread: () => null }));
vi.mock("@/elements/components/assistant-ui/thread-list", () => ({
  ThreadList: () => null,
}));

import { AssistantModal } from "./assistant-modal";
import { AssistantSidecar } from "./assistant-sidecar";

describe("AssistantModal", () => {
  it("subscribes to the derived running-message state", () => {
    renderToStaticMarkup(<AssistantModal />);

    const selected = mocks.stateSelector?.({
      thread: {
        messages: [{ status: { type: "complete" } }],
        unrelatedState: "changed",
      },
    });

    expect(selected).toBe(false);
  });
});

describe("AssistantSidecar", () => {
  it("subscribes to the derived running-message state", () => {
    renderToStaticMarkup(<AssistantSidecar />);

    const selected = mocks.stateSelector?.({
      thread: {
        messages: [{ status: { type: "complete" } }],
        unrelatedState: "changed",
      },
    });

    expect(selected).toBe(false);
  });
});
