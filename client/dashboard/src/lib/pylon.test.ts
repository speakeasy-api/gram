import { afterEach, beforeEach, describe, expect, it } from "vitest";

import { isPylonChatOpen, pylonAppId, togglePylonChat } from "./pylon";
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

describe("pylonAppId", () => {
  it("uses the deployment's Pylon app", () => {
    expect(pylonAppId({ pylonAppId: "app" })).toBe("app");
  });

  it("is null without one, so support chat stays off", () => {
    expect(pylonAppId({})).toBeNull();
  });
});
