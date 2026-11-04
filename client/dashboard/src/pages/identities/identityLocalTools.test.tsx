import type { ReactNode } from "react";
import { act, cleanup, renderHook } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";
import { MemoryRouter, useLocation } from "react-router";
import {
  agentToolUsagePayload,
  LOCAL_TOOLS_PARAM,
  showsLocalTools,
  useShowLocalTools,
  withShowLocalTools,
} from "./identityLocalTools";

afterEach(cleanup);

const from = new Date("2026-01-01T00:00:00Z");
const to = new Date("2026-01-08T00:00:00Z");

describe("showsLocalTools", () => {
  it("shows local tools unless the page hides them", () => {
    expect(showsLocalTools(new URLSearchParams())).toBe(true);
    expect(showsLocalTools(new URLSearchParams("localTools=hide"))).toBe(false);
    expect(showsLocalTools(new URLSearchParams("localTools=other"))).toBe(true);
  });
});

describe("withShowLocalTools", () => {
  it("writes the param only when local tools are hidden", () => {
    const hidden = withShowLocalTools(new URLSearchParams("range=7d"), false);
    expect(hidden.get(LOCAL_TOOLS_PARAM)).toBe("hide");
    expect(hidden.get("range")).toBe("7d");

    const shown = withShowLocalTools(hidden, true);
    expect(shown.has(LOCAL_TOOLS_PARAM)).toBe(false);
    expect(shown.get("range")).toBe("7d");
  });
});

describe("useShowLocalTools", () => {
  function wrapper(initialUrl: string) {
    return function Wrapper({ children }: { children: ReactNode }) {
      return (
        <MemoryRouter initialEntries={[initialUrl]}>{children}</MemoryRouter>
      );
    };
  }

  it("reads and writes the choice in the URL", () => {
    const { result } = renderHook(
      () => ({ choice: useShowLocalTools(), location: useLocation() }),
      { wrapper: wrapper("/overview?range=7d") },
    );
    expect(result.current.choice[0]).toBe(true);

    act(() => result.current.choice[1](false));
    expect(result.current.choice[0]).toBe(false);
    expect(result.current.location.search).toBe("?range=7d&localTools=hide");

    act(() => result.current.choice[1](true));
    expect(result.current.choice[0]).toBe(true);
    expect(result.current.location.search).toBe("?range=7d");
  });
});

describe("agentToolUsagePayload", () => {
  it("leaves target types unset when local tools are shown", () => {
    const payload = agentToolUsagePayload({
      agentId: "agent-1",
      from,
      to,
      includeLocalTools: true,
    });
    expect(payload).toEqual({
      from,
      to,
      userFilters: [{ kind: "agent_id", key: "agent-1" }],
    });
    expect(payload).not.toHaveProperty("targetTypes");
  });

  it("names every target type but local tools when they are hidden", () => {
    const payload = agentToolUsagePayload({
      agentId: "agent-1",
      from,
      to,
      includeLocalTools: false,
    });
    expect(payload.userFilters).toEqual([{ kind: "agent_id", key: "agent-1" }]);
    expect(payload.targetTypes).toEqual([
      "hosted_mcp_server",
      "tunneled_mcp_server",
      "meta_mcp_server",
      "shadow_mcp_server",
      "skill",
    ]);
  });
});
