import { act, renderHook } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { usePylonChat } from "@/hooks/usePylonChat";

import {
  bindPylonChatListeners,
  hidePylonChat,
  isPylonChatOpen,
  showPylonChat,
  subscribePylonChatOpen,
  togglePylonChat,
} from "./pylon";
import { installMockPylon, type MockPylon } from "./pylon-test-mock";

function removePylonDom(): void {
  document.getElementById("pylon-chat-styles")?.remove();
  document
    .querySelectorAll('script[src*="widget.usepylon.com"]')
    .forEach((el) => {
      el.remove();
    });
}

function closeChat(): void {
  if (typeof window.Pylon !== "function") {
    installMockPylon();
  }
  hidePylonChat();
}

beforeEach(() => {
  closeChat();
  installMockPylon();
});

afterEach(() => {
  closeChat();
  removePylonDom();
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

  it("rebinds listeners after the widget script loads", async () => {
    vi.resetModules();
    const { initializePylon, isPylonChatOpen, showPylonChat, PYLON_APP_ID } =
      await import("./pylon");

    initializePylon({
      app_id: PYLON_APP_ID,
      email: "test@example.com",
      name: "Test User",
    });

    expect(window.Pylon.q).toEqual(
      expect.arrayContaining([
        ["onShow", expect.any(Function)],
        ["onHide", expect.any(Function)],
      ]),
    );

    const widget = installMockPylon();
    const script = document.querySelector('script[src*="widget.usepylon.com"]');
    expect(script).not.toBeNull();
    script!.dispatchEvent(new Event("load"));

    showPylonChat();
    expect(isPylonChatOpen()).toBe(true);
    expect(widget).toHaveBeenCalledWith("show");

    widget.emitHide();
    expect(isPylonChatOpen()).toBe(false);
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
