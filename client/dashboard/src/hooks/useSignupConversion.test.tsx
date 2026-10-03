import { renderHook } from "@testing-library/react";
import type { ReactNode } from "react";
import { MemoryRouter, useLocation } from "react-router";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { useSignupConversion } from "./useSignupConversion";

const { trackConversion, initialize } = vi.hoisted(() => ({
  trackConversion: vi.fn(),
  initialize: vi.fn(() => true),
}));

vi.mock("@/lib/google-ads", () => ({
  googleAds: { trackConversion, initialize },
}));

function renderSignupConversion(initialEntry: string, sessionReady = false) {
  function Wrapper({ children }: { children: ReactNode }) {
    return (
      <MemoryRouter initialEntries={[initialEntry]}>{children}</MemoryRouter>
    );
  }

  return renderHook(
    ({ ready }: { ready: boolean }) => {
      useSignupConversion(ready);
      const location = useLocation();
      return `${location.pathname}${location.search}${location.hash}`;
    },
    { wrapper: Wrapper, initialProps: { ready: sessionReady } },
  );
}

describe("useSignupConversion", () => {
  beforeEach(() => {
    trackConversion.mockClear();
    initialize.mockClear();
  });

  it("does nothing on an ordinary page load", () => {
    const { result, rerender } = renderSignupConversion("/acme?tab=settings");

    rerender({ ready: true });

    expect(trackConversion).not.toHaveBeenCalled();
    expect(initialize).not.toHaveBeenCalled();
    expect(result.current).toBe("/acme?tab=settings");
  });

  it("strips the mark right away and fires once the session has an organization", () => {
    const { result, rerender } = renderSignupConversion(
      "/?signed_up=1&redirect=%2Facme#top",
    );

    // The mark never survives to the next render, but the intent does.
    expect(result.current).toBe("/?redirect=%2Facme#top");
    expect(initialize).toHaveBeenCalled();
    expect(trackConversion).not.toHaveBeenCalled();

    rerender({ ready: true });
    expect(trackConversion).toHaveBeenCalledOnce();
    expect(trackConversion).toHaveBeenCalledWith(
      "platform_signup",
      { product: "AI Control Plane" },
      undefined,
    );

    // Neither a re-render nor a session refetch counts the signup again.
    rerender({ ready: true });
    rerender({ ready: false });
    rerender({ ready: true });
    expect(trackConversion).toHaveBeenCalledOnce();
  });

  it("fires immediately when the session is already ready", () => {
    const { result } = renderSignupConversion(
      "/acme/projects/default/assistants/new?disposition=assistants&signed_up=1",
      true,
    );

    expect(trackConversion).toHaveBeenCalledOnce();
    expect(result.current).toBe(
      "/acme/projects/default/assistants/new?disposition=assistants",
    );
  });
});
