import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { SendTestEventSection } from "./SendTestEventSection";

const mutate = vi.fn();
const mutation = {
  mutate,
  data: undefined as { outcome: string; message: string } | undefined,
  error: undefined as unknown,
  isPending: false,
  isError: false,
};

vi.mock("@gram/client/react-query/testDataExportRoute.js", () => ({
  useTestDataExportRouteMutation: () => mutation,
}));

const hasScope = { value: true };
vi.mock("@/components/require-scope", () => ({
  RequireScope: ({ children }: { children: React.ReactNode }) =>
    hasScope.value ? children : null,
}));

beforeEach(() => {
  mutation.data = undefined;
  mutation.error = undefined;
  mutation.isPending = false;
  mutation.isError = false;
  hasScope.value = true;
});

afterEach(() => {
  cleanup();
  vi.clearAllMocks();
});

function renderSection(unsavedChanges = false) {
  render(
    <SendTestEventSection
      projectSlug="default"
      routeId="route-1"
      unsavedChanges={unsavedChanges}
    />,
  );
}

describe("SendTestEventSection", () => {
  it("sends a test event for the saved route", () => {
    renderSection();

    fireEvent.click(screen.getByRole("button", { name: "Send test event" }));

    expect(mutate).toHaveBeenCalledWith({
      request: { id: "route-1", gramProject: "default" },
    });
  });

  it("asks for a save before testing changed settings", () => {
    renderSection(true);

    expect(
      screen.getByRole<HTMLButtonElement>("button", {
        name: "Send test event",
      }).disabled,
    ).toBe(true);
    expect(
      screen.getByText(
        "Save your changes to send a test event to this destination.",
      ),
    ).toBeTruthy();
  });

  it.each([
    ["accepted", "Test event accepted"],
    ["refused", "Test event refused"],
    ["not_found", "Logs endpoint not found"],
    ["unreachable", "Destination not reachable"],
    ["error", "Destination returned an error"],
  ])("shows the %s answer inline", (outcome, title) => {
    mutation.data = { outcome, message: `Message for ${outcome}.` };
    renderSection();

    expect(screen.getByText(title)).toBeTruthy();
    expect(screen.getByText(`Message for ${outcome}.`)).toBeTruthy();
  });

  it("explains a request that could not be sent", () => {
    mutation.isError = true;
    mutation.error = new Error("boom");
    renderSection();

    expect(screen.getByText("Test event not sent")).toBeTruthy();
    expect(
      screen.getByText("Speakeasy could not send the test event. Try again."),
    ).toBeTruthy();
    expect(screen.queryByText("boom")).toBeNull();
  });

  it("is hidden from people who are not org admins", () => {
    hasScope.value = false;
    renderSection();

    expect(
      screen.queryByRole("button", { name: "Send test event" }),
    ).toBeNull();
  });
});
