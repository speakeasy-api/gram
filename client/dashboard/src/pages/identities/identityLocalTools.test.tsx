import type { ReactNode } from "react";
import { act, cleanup, renderHook } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";
import { MemoryRouter, useLocation } from "react-router";
import {
  LOCAL_TOOLS_PARAM,
  showsLocalTools,
  useShowLocalTools,
  withShowLocalTools,
} from "./identityLocalTools";

afterEach(cleanup);

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
