import { afterEach, beforeEach, describe, expect, it } from "vitest";

import { isPylonChatOpen, togglePylonChat } from "./pylon";
import { installMockPylon, type MockPylon } from "./pylon-test-mock";

function closeChat(): void {
  installMockPylon();
  if (isPylonChatOpen()) {
    togglePylonChat();
  }
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
  it("toggles open and tracks widget hide", () => {
    togglePylonChat();
    expect(isPylonChatOpen()).toBe(true);

    (window.Pylon as MockPylon).emitHide();
    expect(isPylonChatOpen()).toBe(false);
  });
});
