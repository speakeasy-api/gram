import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import {
  CreationIdentityChoice,
  type CreationIdentityMode,
} from "./CreationIdentityChoice";
import type { AgentCredentialFields } from "./useAgentCredentialDraft";

function credential(
  overrides: Partial<AgentCredentialFields> = {},
): AgentCredentialFields {
  return {
    format: "bearer",
    setFormat: vi.fn(() => {}),
    prefix: "Bearer",
    setPrefix: vi.fn(() => {}),
    token: "",
    setToken: vi.fn(() => {}),
    username: "",
    setUsername: vi.fn(() => {}),
    password: "",
    setPassword: vi.fn(() => {}),
    manualValue: "",
    setManualValue: vi.fn(() => {}),
    reveal: false,
    toggleReveal: vi.fn(() => {}),
    preview: "Bearer <token>",
    authorizationValue: "",
    clear: vi.fn(() => {}),
    ...overrides,
  };
}

function renderChoice(
  props: Partial<React.ComponentProps<typeof CreationIdentityChoice>> = {},
) {
  return render(
    <CreationIdentityChoice
      value={props.value ?? ("user" as CreationIdentityMode)}
      onChange={props.onChange ?? vi.fn(() => {})}
      credential={props.credential ?? credential()}
      upstreamName={props.upstreamName ?? "Linear"}
      advertisesOAuth={props.advertisesOAuth ?? true}
      authenticationRequired={props.authenticationRequired ?? false}
      canCreateIdentity={props.canCreateIdentity ?? true}
      rbacLoading={props.rbacLoading ?? false}
    />,
  );
}

afterEach(() => {
  cleanup();
  vi.clearAllMocks();
});

describe("CreationIdentityChoice", () => {
  it("explains the modes inline rather than in a dialog", () => {
    renderChoice();

    const trigger = screen.getByRole("button", {
      name: "What do these identity modes mean?",
    });
    expect(trigger.getAttribute("aria-expanded")).toBe("false");
    expect(screen.queryByText(/whose permissions you manage/i)).toBeNull();

    fireEvent.click(trigger);

    expect(screen.getByText(/whose permissions you manage/i)).toBeDefined();
    // These flows already run inside a dialog; the explainer must not stack a
    // second one on top of the step.
    expect(screen.queryByRole("dialog")).toBeNull();

    fireEvent.click(trigger);
    expect(screen.queryByText(/whose permissions you manage/i)).toBeNull();
  });

  it("marks User Identity the default only when the probe saw OAuth", () => {
    const { rerender } = renderChoice();
    expect(screen.getByText("Default")).toBeDefined();
    expect(
      screen.getByText(
        "Linear advertises OAuth sign-in, so User Identity is the default.",
      ),
    ).toBeDefined();

    rerender(
      <CreationIdentityChoice
        value="user"
        onChange={vi.fn(() => {})}
        credential={credential()}
        upstreamName="Linear"
        advertisesOAuth={false}
        authenticationRequired={false}
        canCreateIdentity
        rbacLoading={false}
      />,
    );
    expect(screen.queryByText("Default")).toBeNull();
    expect(
      screen.getByText("How callers are identified to Linear."),
    ).toBeDefined();
  });

  it("shows the credential form with its preview under Agent Identity", () => {
    renderChoice({ value: "agent" });

    expect(screen.getByRole("button", { name: "Bearer" })).toBeDefined();
    expect(
      screen.getByRole("status", { name: "Authorization preview" }).textContent,
    ).toContain("Bearer <token>");
  });

  it("warns when No Identity is chosen for a server that demands auth", () => {
    renderChoice({ value: "none", authenticationRequired: true });

    expect(
      screen.getByText(/This server requires authentication/i),
    ).toBeDefined();
  });
});
