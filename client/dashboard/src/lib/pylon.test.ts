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

    // happy-dom throws if a remote script is connected. Capture the widget
    // script on insert without attaching it, then fire onload ourselves.
    const placeholder = document.createElement("script");
    document.head.appendChild(placeholder);

    const inserted: HTMLScriptElement[] = [];
    const originalInsertBefore = Object.getOwnPropertyDescriptor(
      Node.prototype,
      "insertBefore",
    )?.value as typeof Node.prototype.insertBefore | undefined;
    if (typeof originalInsertBefore !== "function") {
      throw new Error("expected Node.prototype.insertBefore");
    }
    const insertSpy = vi
      .spyOn(Node.prototype, "insertBefore")
      .mockImplementation(function (this: Node, node, child) {
        if (
          node instanceof HTMLScriptElement &&
          node.src.includes("widget.usepylon.com")
        ) {
          inserted.push(node);
          return node;
        }
        return originalInsertBefore.call(this, node, child);
      });

    try {
      initializePylon({
        app_id: PYLON_APP_ID,
        email: "test@example.com",
        name: "Test User",
      });
    } finally {
      insertSpy.mockRestore();
      placeholder.remove();
    }

    expect(window.Pylon.q).toEqual(
      expect.arrayContaining([
        ["onShow", expect.any(Function)],
        ["onHide", expect.any(Function)],
      ]),
    );

    const script = inserted[0];
    expect(script).toBeDefined();
    if (!script) {
      throw new Error("expected widget script to be inserted");
    }

    const widget = installMockPylon();
    script.onload?.(new Event("load"));

    showPylonChat();
    expect(isPylonChatOpen()).toBe(true);

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
