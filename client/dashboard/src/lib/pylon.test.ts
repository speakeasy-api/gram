import { act, renderHook } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it } from "vitest";

import {
  bindPylonChatListeners,
  isPylonChatOpen,
  subscribePylonChatOpen,
  togglePylonChat,
} from "./pylon";
import { usePylonChat } from "@/hooks/usePylonChat";

type MockPylon = typeof window.Pylon & {
  emitShow: () => void;
  emitHide: () => void;
};

function installMockPylon(): MockPylon {
  let onShow: (() => void) | null = null;
  let onHide: (() => void) | null = null;

  const pylon = Object.assign(
    (action: string, ...args: unknown[]) => {
      if (action === "onShow" && typeof args[0] === "function") {
        onShow = args[0] as () => void;
      }
      if (action === "onHide" && typeof args[0] === "function") {
        onHide = args[0] as () => void;
      }
      if (action === "show") {
        onShow?.();
      }
      if (action === "hide") {
        onHide?.();
      }
    },
    {
      q: [] as unknown[],
      e: () => undefined,
      emitShow: () => {
        onShow?.();
      },
      emitHide: () => {
        onHide?.();
      },
    },
  ) as MockPylon;

  window.Pylon = pylon;
  return pylon;
}

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
  });
});
