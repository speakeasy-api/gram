import { cleanup, render, waitFor } from "@testing-library/react";
import type { ReactNode } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { GramError } from "@gram/client/models/errors/gramerror.js";
import {
  DEMO_LANDING_PATH,
  DEMO_ORG_SLUG,
  DEMO_PROJECT_SLUG,
  PRE_DEMO_ORG_KEY,
} from "@/lib/demo";

const mocks = vi.hoisted(() => ({
  enterDemo: vi.fn(),
  info: vi.fn(),
}));

vi.mock("@/contexts/Sdk", () => ({
  useSdkClient: () => ({
    auth: { enterDemo: mocks.enterDemo, info: mocks.info },
  }),
}));

vi.mock("@/pages/login/components/auth-shell", () => ({
  AuthShell: ({ children }: { children: ReactNode }) => <div>{children}</div>,
}));

function gramError(status: number): GramError {
  return new GramError("request failed", {
    response: new Response(null, { status }),
    request: new Request("https://app.getgram.ai/rpc/auth.enterDemo"),
    body: "",
  });
}

describe("ExploreDemo", () => {
  afterEach(() => {
    cleanup();
    vi.unstubAllGlobals();
    vi.clearAllMocks();
    localStorage.clear();
  });

  it("lands on the demo default project after enterDemo", async () => {
    mocks.info.mockResolvedValue({
      result: {
        activeOrganizationId: "org-1",
        organizations: [{ id: "org-1", slug: "my-org" }],
      },
    });
    mocks.enterDemo.mockResolvedValue({});
    const replace = vi.fn();
    vi.stubGlobal("location", { replace });

    const { default: ExploreDemo } = await import("./ExploreDemo");
    render(<ExploreDemo />);

    await waitFor(() => {
      expect(replace).toHaveBeenCalledWith(DEMO_LANDING_PATH);
    });
    expect(DEMO_LANDING_PATH).toBe(
      `/${DEMO_ORG_SLUG}/projects/${DEMO_PROJECT_SLUG}`,
    );
    expect(localStorage.getItem(PRE_DEMO_ORG_KEY)).toBe("org-1");
  });

  it("does not stash the demo org as the pre-demo return target", async () => {
    localStorage.setItem(PRE_DEMO_ORG_KEY, "existing-org");
    mocks.info.mockResolvedValue({
      result: {
        activeOrganizationId: "demo-org",
        organizations: [{ id: "demo-org", slug: DEMO_ORG_SLUG }],
      },
    });
    mocks.enterDemo.mockResolvedValue({});
    const replace = vi.fn();
    vi.stubGlobal("location", { replace });

    const { default: ExploreDemo } = await import("./ExploreDemo");
    render(<ExploreDemo />);

    await waitFor(() => {
      expect(replace).toHaveBeenCalledWith(DEMO_LANDING_PATH);
    });
    expect(localStorage.getItem(PRE_DEMO_ORG_KEY)).toBe("existing-org");
  });

  it("bounces unauthenticated visitors through login back to /explore-demo", async () => {
    mocks.info.mockRejectedValue(new Error("no session"));
    mocks.enterDemo.mockRejectedValue(gramError(401));
    const replace = vi.fn();
    vi.stubGlobal("location", { replace });

    const { default: ExploreDemo } = await import("./ExploreDemo");
    render(<ExploreDemo />);

    await waitFor(() => {
      expect(replace).toHaveBeenCalledWith(
        `/login?redirect=${encodeURIComponent("/explore-demo")}`,
      );
    });
  });
});
