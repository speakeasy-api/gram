import { describe, expect, it } from "vitest";
import { pageHostsOwnAssistantRuntime } from "./insights-dock-routes";

const PREFIX = "/acme/projects/widgets";

describe("pageHostsOwnAssistantRuntime", () => {
  it("matches pages that host their own chat runtime", () => {
    // Playground and Chat Elements bring their own RemoteThreadListRuntime.
    expect(pageHostsOwnAssistantRuntime(`${PREFIX}/playground`)).toBe(true);
    expect(pageHostsOwnAssistantRuntime(`${PREFIX}/elements`)).toBe(true);
    // Assistant onboarding editor (create + edit) hosts its own runtime.
    expect(pageHostsOwnAssistantRuntime(`${PREFIX}/assistants/new`)).toBe(true);
    expect(pageHostsOwnAssistantRuntime(`${PREFIX}/assistants/asst_123`)).toBe(
      true,
    );
  });

  it("does not match the bare assistants list (needs the shared dock composer)", () => {
    expect(pageHostsOwnAssistantRuntime(`${PREFIX}/assistants`)).toBe(false);
    expect(pageHostsOwnAssistantRuntime(`${PREFIX}/assistants/`)).toBe(false);
  });

  it("does not match ordinary pages that show the docked composer", () => {
    expect(pageHostsOwnAssistantRuntime(PREFIX)).toBe(false);
    expect(pageHostsOwnAssistantRuntime(`${PREFIX}/chat`)).toBe(false);
    expect(pageHostsOwnAssistantRuntime(`${PREFIX}/mcp`)).toBe(false);
    expect(pageHostsOwnAssistantRuntime(`${PREFIX}/skills`)).toBe(false);
  });
});
