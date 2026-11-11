import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { AIIntegrationCredentialInput } from "./ai-integration-credential-input";
import {
  AI_INTEGRATION_PROVIDERS,
  providerSchedules,
} from "./ai-integration-providers";

afterEach(cleanup);
const gemini = AI_INTEGRATION_PROVIDERS.find(
  (provider) => provider.provider === "gemini_compliance",
)!;

it("registers Gemini text exports with the matching hourly event stream", () => {
  expect(gemini.providerId).toBe("gemini");
  expect(gemini.requiresOrganizationId).toBe(true);
  expect(gemini.organizationIdLabel).toBe("Vault matter ID");
  expect(providerSchedules(gemini)).toEqual([
    expect.objectContaining({
      schedule: "gemini_compliance",
      signal: "gemini.conversations",
      cadence: "Hourly",
      kind: "events",
      destination: { label: "Agent Sessions", path: "agent-sessions" },
    }),
  ]);
  const guide = gemini
    .setupGuide!.steps.map((step) => step.description)
    .join(" ");
  expect(guide).toContain("https://www.googleapis.com/auth/ediscovery");
  expect(guide).toContain(
    "https://www.googleapis.com/auth/devstorage.read_only",
  );
  expect(guide).toContain("org_unit_id");
  expect(guide).toContain("root organizational-unit ID");
  expect(guide).toContain("completed UTC days");
});

it("accepts multiline JSON while masking secrets and opting out of browser persistence", () => {
  const onChange = vi.fn<(value: string) => void>();
  render(
    <AIIntegrationCredentialInput
      id="credentials"
      provider={gemini}
      value=""
      onChange={onChange}
      hasSavedKey={false}
      disabled={false}
    />,
  );
  const field = screen.getByRole("textbox") as HTMLTextAreaElement;
  expect(field.tagName).toBe("TEXTAREA");
  expect(field.parentElement?.className).toContain(
    "[&_textarea]:[-webkit-text-security:disc]",
  );
  expect(field.autocomplete).toBe("off");
  expect(field.getAttribute("spellcheck")).toBe("false");
  expect(field.getAttribute("data-1p-ignore")).toBe("true");
  expect(field.getAttribute("data-lpignore")).toBe("true");
  const credentials =
    '{\n  "service_account": {"private_key": "DEMO_KEY"},\n  "delegated_user": "admin@example.com",\n  "org_unit_id": "id:EXAMPLE"\n}';
  fireEvent.change(field, { target: { value: credentials } });
  expect(onChange).toHaveBeenCalledWith(credentials);
});

it("keeps existing providers on a password input and never fills saved secrets", () => {
  const provider = AI_INTEGRATION_PROVIDERS.find(
    (provider) => provider.provider === "cursor",
  )!;
  const { container } = render(
    <AIIntegrationCredentialInput
      id="credentials"
      provider={provider}
      value=""
      onChange={vi.fn<(value: string) => void>()}
      hasSavedKey
      disabled={false}
    />,
  );
  const field = container.querySelector("input")!;
  expect(field.type).toBe("password");
  // Browsers ignore autocomplete="off" on password inputs.
  expect(field.autocomplete).toBe("new-password");
  expect(field.getAttribute("data-1p-ignore")).toBe("true");
  expect(field.value).toBe("");
  expect(field.placeholder).toContain("saved");
});
