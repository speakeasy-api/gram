import { afterEach, describe, expect, it } from "vitest";
import { cleanup, render, screen } from "@testing-library/react";
import { ConnectionSetupProgress } from "./ConnectionSetupProgress";

afterEach(cleanup);

describe("Okta setup progress", () => {
  it.each([
    [
      { status: "pending" as const, clientIdSubmitted: false },
      "Set up the Okta app",
    ],
    [
      { status: "pending" as const, clientIdSubmitted: true },
      "Verify connection",
    ],
    [
      { status: "degraded" as const, clientIdSubmitted: true },
      "Verify connection",
    ],
  ])("marks the current setup step for %j", (connection, label) => {
    render(<ConnectionSetupProgress connection={connection} />);
    expect(screen.getByText(label).getAttribute("aria-current")).toBe("step");
    expect(screen.getAllByRole("listitem")).toHaveLength(3);
  });

  it("hides once the connection is verified", () => {
    render(
      <ConnectionSetupProgress
        connection={{ status: "verified", clientIdSubmitted: true }}
      />,
    );
    expect(screen.queryByRole("navigation")).toBeNull();
  });
});
