import { act, renderHook } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it } from "vitest";

import { usePylonChat } from "@/hooks/usePylonChat";

import {
  bindPylonChatListeners,
  isPylonChatOpen,
  showPylonChat,
  subscribePylonChatOpen,
  togglePylonChat,
} from "./pylon";
import { installMockPylon, type MockPylon } from "./pylon-test-mock";

function closeChat(): void {
  if (typeof window.Pylon !== "function") {
    installMockPylon();
  }
  bindPylonChatListeners();
  window.Pylon("hide");
}

beforeEach(() => {
  closeChat();
  installMockPylon();
});

afterEach(() => {
  closeChat();
  Reflect.deleteProperty(window, "Pylon");
});

describe("pylon chat visibility", () => {
  it("starts closed", () => {
    expect(isPylonChatOpen()).toBe(false);
  });

  it("tracks Pylon show and hide through the shared store", () => {
    bindPylonChatListeners();

    togglePylonChat();
    expect(isPylonChatOpen()).toBe(true);

    togglePylonChat();
    expect(isPylonChatOpen()).toBe(false);
  });

  it("opens via showPylonChat so non-menu buttons attach listeners", () => {
    showPylonChat();
    expect(isPylonChatOpen()).toBe(true);
  });

  it("updates when the widget is closed outside the menu", () => {
    const pylon = window.Pylon as MockPylon;
    bindPylonChatListeners();
    togglePylonChat();
    expect(isPylonChatOpen()).toBe(true);

    pylon.emitHide();
    expect(isPylonChatOpen()).toBe(false);
  });

  it("notifies subscribers when the widget hides", () => {
    bindPylonChatListeners();
    togglePylonChat();

    const seen: boolean[] = [];
    const unsubscribe = subscribePylonChatOpen(() => {
      seen.push(isPylonChatOpen());
    });

    (window.Pylon as MockPylon).emitHide();
    unsubscribe();

    expect(seen).toEqual([false]);
  });

  it("keeps hook consumers in sync with widget hide", () => {
    const first = renderHook(() => usePylonChat());
    const second = renderHook(() => usePylonChat());

    act(() => {
      first.result.current.toggle();
    });

    expect(first.result.current.isOpen).toBe(true);
    expect(second.result.current.isOpen).toBe(true);

    act(() => {
      (window.Pylon as MockPylon).emitHide();
    });

    expect(first.result.current.isOpen).toBe(false);
    expect(second.result.current.isOpen).toBe(false);

    first.unmount();
    second.unmount();
  });
});
