import { afterEach, describe, expect, it } from "vitest";
import { cleanup, render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { ConnectionNextSteps } from "./ConnectionNextSteps";

afterEach(cleanup);

describe("connection next steps", () => {
  it.each(["pending", "degraded", "revoked"] as const)(
    "does not suggest syncing when %s",
    (status) => {
      render(<ConnectionNextSteps connection={{ status }} />, {
        wrapper: MemoryRouter,
      });
      expect(screen.queryByRole("region", { name: "Next steps" })).toBeNull();
    },
  );

  it("offers optional ID recording without implying authentication is configured", () => {
    render(<ConnectionNextSteps connection={{ status: "verified" }} />, {
      wrapper: MemoryRouter,
    });
    expect(
      screen.getByText(/Agent authentication is not connected by this setup/),
    ).toBeTruthy();
    expect(
      screen.getByText(/Recording an existing agent is optional/),
    ).toBeTruthy();
    expect(
      screen
        .getByRole("link", { name: "View applications" })
        .getAttribute("href"),
    ).toBe("/?tab=enterprise-managed-auth&provider=okta&view=applications");
    expect(
      screen
        .getByRole("link", { name: "Record existing agent" })
        .getAttribute("href"),
    ).toBe("/?tab=enterprise-managed-auth&provider=okta&view=setup#agent");
  });

  it("links directly to readiness once an agent is recorded", () => {
    render(
      <ConnectionNextSteps
        connection={{ status: "verified", agentId: "test-agent" }}
      />,
      { wrapper: MemoryRouter },
    );
    expect(
      screen
        .getByRole("link", { name: "Set up Cross App Access" })
        .getAttribute("href"),
    ).toBe("/?tab=enterprise-managed-auth&provider=okta&view=cross-app-access");
    expect(
      screen.queryByRole("link", { name: "Record existing agent" }),
    ).toBeNull();
  });
});
