import { act, cleanup, renderHook } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { useComposerDraftHandover } from "./useComposerDraftHandover";

const runtime = vi.hoisted(() => {
  const listeners = new Set<() => void>();
  const state = {
    loading: false,
    bound: false,
    text: "",
  };
  const emit = () => {
    for (const listener of listeners) listener();
  };
  const setText = vi.fn((text: string) => {
    if (!state.bound) return;
    state.text = text;
    emit();
  });
  const aui = {
    composer: () => ({
      setText: (text: string) => setText(text),
      getState: () => ({ text: state.text }),
    }),
    threads: () => ({
      getState: () => ({
        isLoading: state.loading,
      }),
    }),
    subscribe: (listener: () => void) => {
      listeners.add(listener);
      return () => {
        listeners.delete(listener);
      };
    },
  };
  return { state, emit, setText, aui, listeners };
});

vi.mock("@assistant-ui/react", () => ({
  useAui: () => runtime.aui,
  useAuiState: (
    selector: (state: { threads: { isLoading: boolean } }) => unknown,
  ) => selector({ threads: { isLoading: runtime.state.loading } }),
}));

beforeEach(() => {
  runtime.aui.composer = () => ({
    setText: (text: string) => runtime.setText(text),
    getState: () => ({ text: runtime.state.text }),
  });
  runtime.state.loading = false;
  runtime.state.bound = false;
  runtime.state.text = "";
  runtime.setText.mockClear();
});
afterEach(cleanup);

describe("shared composer draft", () => {
  it("retains a dropped draft and retries binding even when loading stays false", async () => {
    const accepted = vi.fn<() => void>();
    const { rerender, unmount } = renderHook(() =>
      useComposerDraftHandover("pending draft", accepted),
    );
    await act(async () => {});
    expect(runtime.setText).toHaveBeenCalledWith("pending draft");
    expect(accepted).not.toHaveBeenCalled();

    act(() => {
      runtime.state.bound = true;
      runtime.emit();
    });
    expect(runtime.state.text).toBe("pending draft");
    expect(accepted).toHaveBeenCalledTimes(1);
    const writes = runtime.setText.mock.calls.length;
    act(() => {
      runtime.state.text = "user edit";
      runtime.emit();
    });
    rerender();
    expect(runtime.state.text).toBe("user edit");
    expect(runtime.setText).toHaveBeenCalledTimes(writes);
    unmount();
    expect(runtime.listeners.size).toBe(0);
  });

  it("waits for loading, then retains the draft until composer binding", async () => {
    runtime.state.loading = true;
    const accepted = vi.fn<() => void>();
    const { rerender } = renderHook(() =>
      useComposerDraftHandover("pending draft", accepted),
    );
    expect(runtime.setText).not.toHaveBeenCalled();
    runtime.state.loading = false;
    rerender();
    await act(async () => {
      runtime.emit();
    });
    expect(accepted).not.toHaveBeenCalled();
    act(() => {
      runtime.state.bound = true;
      runtime.emit();
    });
    expect(accepted).toHaveBeenCalledTimes(1);
    expect(runtime.state.text).toBe("pending draft");
  });
});
