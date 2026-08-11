import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";
import { FullPageError } from "./full-page-error";

describe("FullPageError", () => {
  afterEach(cleanup);

  it("redacts token query parameters in the error message", () => {
    render(
      <FullPageError
        error={
          new Error(
            "GET https://app.example.com/rpc/skills.getShared?token=supersecrettoken failed",
          )
        }
      />,
    );

    expect(screen.queryByText(/supersecrettoken/)).toBeNull();
    expect(screen.getByText(/token=REDACTED/)).toBeDefined();
  });

  it("redacts shared skill path tokens in the error message", () => {
    render(
      <FullPageError
        error={new Error("failed loading /shared/skills/supersecrettoken")}
      />,
    );

    expect(screen.queryByText(/supersecrettoken/)).toBeNull();
    expect(screen.getByText(/\/shared\/skills\/REDACTED/)).toBeDefined();
  });
});
