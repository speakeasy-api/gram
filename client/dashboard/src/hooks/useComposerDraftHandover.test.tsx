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

  it("retries a throwing placeholder on runtime events without rerendering", () => {
    const emptyThreadError = new Error(
      "Cannot call setText on an empty thread",
    );
    runtime.setText.mockImplementationOnce(() => {
      throw emptyThreadError;
    });
    const accepted = vi.fn<() => void>();
    const { unmount } = renderHook(() =>
      useComposerDraftHandover("pending draft", accepted),
    );
    expect(accepted).not.toHaveBeenCalled();
    expect(runtime.listeners.size).toBe(1);

    act(() => {
      runtime.state.bound = true;
      runtime.emit();
    });
    expect(runtime.state.text).toBe("pending draft");
    expect(accepted).toHaveBeenCalledTimes(1);
    expect(runtime.setText).toHaveBeenCalledTimes(2);
    unmount();
    expect(runtime.listeners.size).toBe(0);
  });

  it("propagates non-placeholder errors from runtime events", () => {
    runtime.state.loading = true;
    const accepted = vi.fn<() => void>();
    renderHook(() => useComposerDraftHandover("pending draft", accepted));
    const failure = new Error("Composer failed");
    runtime.setText.mockImplementationOnce(() => {
      throw failure;
    });
    expect(() => {
      act(() => {
        runtime.state.loading = false;
        runtime.emit();
      });
    }).toThrow(failure);
    expect(accepted).not.toHaveBeenCalled();
  });

  it.each(["", " \n\t"])(
    "does not subscribe for an empty draft %j",
    (value) => {
      const accepted = vi.fn<() => void>();
      renderHook(() => useComposerDraftHandover(value, accepted));
      expect(runtime.listeners.size).toBe(0);
      act(() => runtime.emit());
      expect(runtime.setText).not.toHaveBeenCalled();
      expect(accepted).not.toHaveBeenCalled();
    },
  );

  it("keeps the subscription stable and accepts using the latest callback", () => {
    const firstAccepted = vi.fn<() => void>();
    const latestAccepted = vi.fn<() => void>();
    const { rerender } = renderHook(
      ({ onAccepted }) => useComposerDraftHandover("pending draft", onAccepted),
      { initialProps: { onAccepted: firstAccepted } },
    );
    const listener = [...runtime.listeners][0];
    const writes = runtime.setText.mock.calls.length;
    rerender({ onAccepted: latestAccepted });
    expect([...runtime.listeners]).toEqual([listener]);
    expect(runtime.setText).toHaveBeenCalledTimes(writes);

    act(() => {
      runtime.state.bound = true;
      runtime.emit();
    });
    expect(firstAccepted).not.toHaveBeenCalled();
    expect(latestAccepted).toHaveBeenCalledTimes(1);
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
