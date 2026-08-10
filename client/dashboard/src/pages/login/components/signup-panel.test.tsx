import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter } from "react-router";
import { afterEach, describe, expect, it, vi } from "vitest";

import { SignUpPanel } from "./signup-panel";

const telemetryCapture = vi.hoisted(() => vi.fn());

vi.mock("@/contexts/Telemetry", () => ({
  useTelemetry: () => ({ capture: telemetryCapture }),
}));

afterEach(() => {
  cleanup();
  telemetryCapture.mockReset();
});

function renderPanel(initialEntry = "/sign-up") {
  return render(
    <MemoryRouter initialEntries={[initialEntry]}>
      <SignUpPanel />
    </MemoryRouter>,
  );
}

describe("SignUpPanel", () => {
  it("asks for the email and company name only", () => {
    renderPanel();
    expect(screen.getByLabelText("Work email")).toBeTruthy();
    expect(screen.getByLabelText("Company name")).toBeTruthy();
    // The person's own name is deliberately absent: AuthKit has no parameter
    // to pre-fill it, so asking here would mean typing it twice.
    expect(screen.queryByLabelText("Full name")).toBeNull();
  });

  it("leaves the CTA enabled on a pristine empty form", () => {
    renderPanel();
    const cta = screen.getByRole("button", { name: /start trial/i });
    expect(cta.hasAttribute("disabled")).toBe(false);
  });

  it("requires a company name before handing off", async () => {
    const user = userEvent.setup();
    renderPanel();

    await user.type(screen.getByLabelText("Work email"), "someone@example.com");
    await user.click(screen.getByRole("button", { name: /start trial/i }));

    expect(await screen.findByText("Company name is required")).toBeTruthy();
  });

  it("requires an email before handing off", async () => {
    const user = userEvent.setup();
    renderPanel();

    await user.type(screen.getByLabelText("Company name"), "Acme Inc");
    await user.click(screen.getByRole("button", { name: /start trial/i }));

    expect(await screen.findByText("Email is required")).toBeTruthy();
  });

  it("rejects a malformed email and disables the CTA", async () => {
    const user = userEvent.setup();
    renderPanel();

    await user.type(screen.getByLabelText("Work email"), "not-an-address");

    expect(await screen.findByText(/valid email/i)).toBeTruthy();
    expect(
      screen
        .getByRole("button", { name: /start trial/i })
        .hasAttribute("disabled"),
    ).toBe(true);
  });

  it("rejects characters the server would reject and disables the CTA", async () => {
    const user = userEvent.setup();
    renderPanel();

    await user.type(screen.getByLabelText("Company name"), "Bob's Bakery");

    expect(
      await screen.findByText(/contains invalid characters/i),
    ).toBeTruthy();
    expect(
      screen
        .getByRole("button", { name: /start trial/i })
        .hasAttribute("disabled"),
    ).toBe(true);
  });

  // Only [a-z0-9] survives Slugify, so the floor counts those and nothing
  // else. "A" and "A-" both yield a one-character slug; "-----" yields none.
  it.each(["A", "A-", "-----", "___", "- _ -"])(
    "rejects %j, which cannot make a usable slug",
    async (input) => {
      const user = userEvent.setup();
      renderPanel();

      await user.type(screen.getByLabelText("Company name"), input);

      expect(
        await screen.findByText(/at least 2 letters or numbers/i),
      ).toBeTruthy();
      expect(
        screen
          .getByRole("button", { name: /start trial/i })
          .hasAttribute("disabled"),
      ).toBe(true);
    },
  );

  it.each(["Ab", "3M", "-a1-"])("accepts %j", async (input) => {
    const user = userEvent.setup();
    renderPanel();

    await user.type(screen.getByLabelText("Company name"), input);

    expect(screen.queryByText(/at least 2 letters or numbers/i)).toBeNull();
    expect(
      screen
        .getByRole("button", { name: /start trial/i })
        .hasAttribute("disabled"),
    ).toBe(false);
  });

  it("hands off to the login endpoint with the company name", async () => {
    const assign = vi
      .spyOn(window.location, "assign")
      .mockImplementation(() => {});

    const user = userEvent.setup();
    renderPanel();

    await user.type(screen.getByLabelText("Work email"), "someone@example.com");
    await user.type(screen.getByLabelText("Company name"), "Acme Inc");
    await user.click(screen.getByRole("button", { name: /start trial/i }));

    expect(assign).toHaveBeenCalledTimes(1);
    const target = assign.mock.calls[0]?.[0] as string;
    expect(target).toContain("/rpc/auth.login");
    expect(target).toContain("org_name=Acme+Inc");
    expect(target).toContain("email=someone%40example.com");

    assign.mockRestore();
  });

  it("normalizes pasted whitespace the server would reject", async () => {
    const assign = vi
      .spyOn(window.location, "assign")
      .mockImplementation(() => {});

    const user = userEvent.setup();
    renderPanel();

    // A non-breaking space, as arrives from pasting out of a document or a web
    // page. JavaScript's \s matches it and the server's Go regex does not, so
    // without normalizing, this passes here and 500s there — on a top-level
    // navigation the panel cannot catch.
    await user.type(screen.getByLabelText("Work email"), "someone@example.com");
    await user.type(screen.getByLabelText("Company name"), "Acme Inc");
    await user.click(screen.getByRole("button", { name: /start trial/i }));

    expect(assign).toHaveBeenCalledTimes(1);
    const target = assign.mock.calls[0]?.[0] as string;
    expect(target).toContain("org_name=Acme+Inc");
    expect(target).not.toContain("%C2%A0");

    assign.mockRestore();
  });

  it("surfaces an inbound signin error", () => {
    renderPanel("/sign-up?signin_error=init_error");
    expect(screen.getByText(/failed to initialize account/i)).toBeTruthy();
  });

  it("captures a signup_started event on handoff", async () => {
    const assign = vi
      .spyOn(window.location, "assign")
      .mockImplementation(() => {});

    const user = userEvent.setup();
    renderPanel();

    await user.type(screen.getByLabelText("Work email"), "someone@example.com");
    await user.type(screen.getByLabelText("Company name"), "Acme Inc");
    await user.click(screen.getByRole("button", { name: /start trial/i }));

    expect(telemetryCapture).toHaveBeenCalledWith("onboarding_event", {
      action: "signup_started",
      created_via: "signup",
    });

    assign.mockRestore();
  });

  it("locks the CTA after a handoff so a second click cannot double-fire", async () => {
    const assign = vi
      .spyOn(window.location, "assign")
      .mockImplementation(() => {});

    const user = userEvent.setup();
    renderPanel();

    await user.type(screen.getByLabelText("Work email"), "someone@example.com");
    await user.type(screen.getByLabelText("Company name"), "Acme Inc");
    const cta = screen.getByRole("button", { name: /start trial/i });

    await user.click(cta);
    expect(cta.hasAttribute("disabled")).toBe(true);

    // The handoff is a navigation, not an awaited promise, so isSubmitting is
    // already back to false here. Only isSubmitted keeps the button locked.
    await user.click(cta);

    expect(assign).toHaveBeenCalledTimes(1);
    expect(telemetryCapture).toHaveBeenCalledTimes(1);

    assign.mockRestore();
  });

  it("does not capture signup_started when validation fails", async () => {
    const user = userEvent.setup();
    renderPanel();

    await user.click(screen.getByRole("button", { name: /start trial/i }));

    expect(await screen.findByText("Company name is required")).toBeTruthy();
    expect(telemetryCapture).not.toHaveBeenCalled();
  });
});
