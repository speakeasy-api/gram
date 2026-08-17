import { describe, expect, it } from "vitest";

import { enforcementLayer } from "./device-agent-configuration";

// Mirrors config.PlatformMode.Layer() in the device-agent repo: the
// `platforms` map is opt-out, so anything but an explicit false is managed.
describe("enforcementLayer", () => {
  it("treats an absent key as managed at the user layer", () => {
    expect(enforcementLayer({ codex: "managed" }, "opencode")).toBe("user");
    expect(enforcementLayer(undefined, "opencode")).toBe("user");
    expect(enforcementLayer({}, "claude_code")).toBe("user");
  });

  it("reads explicit values back", () => {
    expect(enforcementLayer({ opencode: false }, "opencode")).toBe("off");
    expect(enforcementLayer({ opencode: "user" }, "opencode")).toBe("user");
    expect(enforcementLayer({ opencode: "managed" }, "opencode")).toBe(
      "managed",
    );
  });

  it("degrades a non-object platforms value to the default", () => {
    expect(enforcementLayer("nonsense", "opencode")).toBe("user");
  });
});
