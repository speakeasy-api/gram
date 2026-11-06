import { act, cleanup, render, screen, waitFor } from "@testing-library/react";
import { StrictMode } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { useFreshAssistantConversation } from "./useFreshAssistantConversation";
import { ComposerDraftHandover } from "./useComposerDraftHandover";

const runtime = vi.hoisted(() => {
  const state = {
    ready: true,
    loading: false,
    threadLoading: false,
    mainThreadId: "old",
    newThreadId: "new",
    text: "old draft",
  };
  const listeners = new Set<() => void>();
  const emit = () => listeners.forEach((listener) => listener());
  const switchToNewThread = vi.fn<() => Promise<void>>();
  const newConversation = vi.fn();
  const setText = vi.fn((value: string) => {
    state.text = value;
  });
  const aui = {
    threads: () => ({
      getState: () => ({ ...state, isLoading: state.loading }),
      switchToNewThread,
    }),
    thread: () => ({ getState: () => ({ isLoading: state.threadLoading }) }),
    composer: () => ({ setText, getState: () => ({ text: state.text }) }),
    subscribe: (listener: () => void) => {
      listeners.add(listener);
      return () => listeners.delete(listener);
    },
  };
  return {
    state,
    listeners,
    emit,
    switchToNewThread,
    newConversation,
    setText,
    aui,
  };
});
vi.mock("@assistant-ui/react", () => ({ useAui: () => runtime.aui }));
vi.mock("@/components/insights-context", () => ({
  useInsightsState: () => ({
    assistantReady: runtime.state.ready,
    newConversation: runtime.newConversation,
  }),
}));

function Landing(): JSX.Element {
  const ready = useFreshAssistantConversation();
  return (
    <>
      <output>{ready ? "ready" : "waiting"}</output>
      {ready && (
        <ComposerDraftHandover value="landing draft" onAccepted={() => {}} />
      )}
    </>
  );
}
beforeEach(() => {
  Object.assign(runtime.state, {
    ready: true,
    loading: false,
    threadLoading: false,
    mainThreadId: "old",
    newThreadId: "new",
    text: "old draft",
  });
  runtime.listeners.clear();
  vi.clearAllMocks();
  runtime.switchToNewThread.mockImplementation(async () => {
    runtime.state.mainThreadId = "new";
  });
});
afterEach(cleanup);

describe("useFreshAssistantConversation", () => {
  it("waits for both the actual switch promise and publication of the bound new thread", async () => {
    let finish!: () => void;
    runtime.switchToNewThread.mockImplementation(
      () =>
        new Promise<void>((resolve) => {
          finish = resolve;
        }),
    );
    render(
      <StrictMode>
        <Landing />
      </StrictMode>,
    );
    expect(runtime.switchToNewThread).toHaveBeenCalledTimes(1);
    expect(runtime.setText).not.toHaveBeenCalled();
    act(() => {
      runtime.state.mainThreadId = "new";
      runtime.emit();
    });
    expect(runtime.setText).not.toHaveBeenCalled();
    await act(async () => {
      finish();
    });
    expect(runtime.setText).toHaveBeenCalledWith("landing draft");
    expect(runtime.newConversation).not.toHaveBeenCalled();
  });

  it("does not seed the old composer when the switch settles before the new thread binds", async () => {
    runtime.switchToNewThread.mockResolvedValue(undefined);
    render(<Landing />);
    await act(async () => {});
    expect(runtime.setText).not.toHaveBeenCalled();
    act(() => {
      runtime.state.mainThreadId = "new";
      runtime.state.threadLoading = true;
      runtime.emit();
    });
    expect(runtime.setText).not.toHaveBeenCalled();
    act(() => {
      runtime.state.threadLoading = false;
      runtime.emit();
    });
    expect(runtime.setText).toHaveBeenCalledWith("landing draft");
  });

  it("waits for list loading and switches only once through Strict Mode effect replay", async () => {
    runtime.state.loading = true;
    render(
      <StrictMode>
        <Landing />
      </StrictMode>,
    );
    expect(runtime.switchToNewThread).not.toHaveBeenCalled();
    await act(async () => {
      runtime.state.loading = false;
      runtime.emit();
    });
    expect(runtime.switchToNewThread).toHaveBeenCalledTimes(1);
    expect(screen.getByText("ready")).toBeTruthy();
  });

  it("reuses an already bound new thread without switching", async () => {
    runtime.state.mainThreadId = "new";
    render(<Landing />);
    await waitFor(() => expect(screen.getByText("ready")).toBeTruthy());
    expect(runtime.switchToNewThread).not.toHaveBeenCalled();
  });

  it("latches readiness when the first send promotes the new thread to an existing chat", async () => {
    render(<Landing />);
    await waitFor(() => expect(screen.getByText("ready")).toBeTruthy());
    act(() => {
      runtime.state.newThreadId = "next";
      runtime.emit();
    });
    expect(screen.getByText("ready")).toBeTruthy();
    expect(runtime.switchToNewThread).toHaveBeenCalledTimes(1);
  });
});
