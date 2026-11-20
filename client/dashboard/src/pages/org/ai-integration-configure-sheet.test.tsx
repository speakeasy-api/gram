import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { ConfigureSheet } from "./ai-integration-configure-sheet";
import { AI_INTEGRATION_PROVIDERS } from "./ai-integration-providers";
import type { AIIntegrationConfigForm } from "./use-ai-integration-config-form";

vi.mock("@gram/client/react-query/aiIntegrationOrgUnits", () => ({
  useAiIntegrationOrgUnits: () => ({
    data: undefined,
    isLoading: false,
    isError: false,
    error: null,
  }),
}));

vi.mock("@/hooks/useRBAC", () => ({
  useRBAC: () => ({
    hasAllScopes: () => true,
    hasAnyScope: () => true,
    isLoading: false,
  }),
}));

afterEach(cleanup);

const noop = (): void => {};

function stubForm(
  setApiKey: AIIntegrationConfigForm["setApiKey"],
): AIIntegrationConfigForm {
  return {
    data: undefined,
    isLoading: false,
    enabled: false,
    setEnabled: noop,
    apiKey: '{"service_account": {"private_key": "DEMO_KEY"}}',
    setApiKey,
    organizationId: "",
    setOrganizationId: noop,
    delegatedUser: "",
    setDelegatedUser: noop,
    orgUnitId: "",
    setOrgUnitId: noop,
    awaitingOrgUnit: false,
    billingMode: "",
    setBillingMode: noop,
    isConfigured: false,
    hasSavedKey: false,
    hasSavedDelegatedUser: false,
    isMutating: false,
    canSave: true,
    save: noop,
    saveEnabled: noop,
    remove: noop,
  };
}

it("clears a pasted Gemini service-account key when Cancel closes the sheet", () => {
  const gemini = AI_INTEGRATION_PROVIDERS.find(
    (provider) => provider.provider === "gemini_compliance",
  )!;
  const setApiKey = vi.fn<AIIntegrationConfigForm["setApiKey"]>();
  const onOpenChange = vi.fn<(open: boolean) => void>();
  render(
    <ConfigureSheet
      provider={gemini}
      form={stubForm(setApiKey)}
      open
      onOpenChange={onOpenChange}
    />,
  );

  fireEvent.click(screen.getByRole("button", { name: "Cancel" }));

  expect(setApiKey).toHaveBeenCalledWith("");
  expect(onOpenChange).toHaveBeenCalledWith(false);
});
