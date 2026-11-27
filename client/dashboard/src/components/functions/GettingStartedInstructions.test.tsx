import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { GettingStartedInstructions } from "./GettingStartedInstructions";

const serverURL = vi.hoisted(() => ({ current: "" }));
vi.mock("@/lib/utils", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/lib/utils")>()),
  getServerURL: () => serverURL.current,
}));
vi.mock("@/components/code", () => ({
  CodeBlock: ({ children }: { children: string }) => <pre>{children}</pre>,
}));

afterEach(() => {
  cleanup();
});

describe("GettingStartedInstructions", () => {
  it("signs the CLI in to a host its default does not reach", () => {
    serverURL.current = "https://ai.eu.example.com";
    render(<GettingStartedInstructions />);

    expect(
      screen.getByText("speakeasy auth --api-url https://ai.eu.example.com"),
    ).toBeTruthy();
  });

  it("leaves the CLI on its default host", () => {
    serverURL.current = "https://app.getgram.ai";
    render(<GettingStartedInstructions />);

    expect(screen.queryByText(/speakeasy auth/)).toBeNull();
    expect(screen.getByText("speakeasy functions push")).toBeTruthy();
  });
});
