import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { UserSessionIssuersRoot } from "./UserSessionIssuers";

const flag = vi.hoisted(() => ({ status: "loading" as string }));

vi.mock("@/hooks/useFeatureFlag", () => ({
  useFeatureFlag: () => ({ status: flag.status }),
}));

vi.mock("react-router", () => ({
  Navigate: ({ to }: { to: string }) => <output>redirect {to}</output>,
  Outlet: () => <output>page</output>,
}));

afterEach(() => {
  cleanup();
});

describe("UserSessionIssuersRoot", () => {
  it("waits for the rollout flag", () => {
    flag.status = "loading";
    const { container } = render(<UserSessionIssuersRoot />);
    expect(container.textContent).toBe("");
  });

  it.each(["disabled", "missing", "error"])(
    "leaves while the flag is %s",
    (status) => {
      flag.status = status;
      render(<UserSessionIssuersRoot />);
      expect(screen.getByText("redirect ..")).toBeDefined();
    },
  );

  it("shows the page once rolled out", () => {
    flag.status = "enabled";
    render(<UserSessionIssuersRoot />);
    expect(screen.getByText("page")).toBeDefined();
  });
});
