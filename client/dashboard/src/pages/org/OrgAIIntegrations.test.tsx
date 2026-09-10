import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { OrgAIIntegrationsInner } from "./OrgAIIntegrations";

const state = vi.hoisted(() => ({
  isAdmin: false,
}));

vi.mock("@/components/page-layout", () => {
  const Section = ({ children }: { children: React.ReactNode }) => (
    <section>{children}</section>
  );
  Section.Title = ({ children }: { children: React.ReactNode }) => (
    <h1>{children}</h1>
  );
  Section.Description = ({ children }: { children: React.ReactNode }) => (
    <p>{children}</p>
  );
  Section.Body = ({ children }: { children: React.ReactNode }) => (
    <div>{children}</div>
  );
  return { Page: { Section } };
});

vi.mock("@/hooks/useRBAC", () => ({
  useRBAC: () => ({
    hasScope: (scope: string) => scope === "org:admin" && state.isAdmin,
  }),
}));

vi.mock("@/pages/org/ai-integration-connection-row", () => ({
  AIIntegrationConnectionRow: ({
    provider,
  }: {
    provider: { name: string };
  }) => <div>{provider.name}</div>,
}));

vi.mock("@/pages/org/litellm-integration-row", () => ({
  LiteLLMIntegrationRow: () => <div>LiteLLM push integration</div>,
}));

vi.mock("./anthropic-inference-integration-row", () => ({
  AnthropicInferenceIntegrationRow: () => <div>Anthropic inference hooks</div>,
}));

beforeEach(() => {
  state.isAdmin = false;
});

afterEach(cleanup);

describe("OrgAIIntegrationsInner", () => {
  it("hides LiteLLM from non-admins without mounting its query surface", () => {
    render(<OrgAIIntegrationsInner />);

    expect(screen.queryByText("LiteLLM push integration")).toBeNull();
    expect(screen.queryByText("Anthropic inference hooks")).toBeNull();
  });

  it("shows LiteLLM to admins", () => {
    state.isAdmin = true;

    render(<OrgAIIntegrationsInner />);

    expect(screen.getByText("LiteLLM push integration")).toBeDefined();
    expect(screen.getByText("Anthropic inference hooks")).toBeDefined();
  });
});
