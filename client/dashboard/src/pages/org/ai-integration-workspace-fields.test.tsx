import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import {
  AI_INTEGRATION_PROVIDERS,
  orgUnitLabel,
} from "./ai-integration-providers";
import { AIIntegrationWorkspaceFields } from "./ai-integration-workspace-fields";
import type { AIIntegrationConfigForm } from "./use-ai-integration-config-form";

const query = vi.hoisted(() => ({
  enabled: undefined as boolean | undefined,
  result: {
    data: undefined as
      | { orgUnits: Array<{ id: string; path: string; name: string }> }
      | undefined,
    isLoading: false,
    isError: false,
    error: null as Error | null,
  },
}));

vi.mock("@gram/client/react-query/aiIntegrationOrgUnits", () => ({
  useAiIntegrationOrgUnits: (
    _request: unknown,
    _security: unknown,
    options: { enabled: boolean },
  ) => {
    query.enabled = options.enabled;
    return query.result;
  },
}));

const gemini = AI_INTEGRATION_PROVIDERS.find(
  (provider) => provider.provider === "gemini_compliance",
)!;
const noop = (): void => {};

function stubForm(
  overrides: Partial<AIIntegrationConfigForm>,
): AIIntegrationConfigForm {
  return {
    data: undefined,
    isLoading: false,
    enabled: false,
    setEnabled: noop,
    apiKey: "",
    setApiKey: noop,
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
    isMutating: false,
    canSave: false,
    save: noop,
    saveEnabled: noop,
    remove: noop,
    ...overrides,
  };
}

beforeEach(() => {
  query.enabled = undefined;
  query.result = {
    data: undefined,
    isLoading: false,
    isError: false,
    error: null,
  };
});
afterEach(cleanup);

it("labels the root unit as the whole organization", () => {
  expect(orgUnitLabel({ path: "/", name: "Example Co" })).toBe(
    "Entire organization (Example Co)",
  );
  expect(orgUnitLabel({ path: "/", name: "" })).toBe("Entire organization");
  expect(orgUnitLabel({ path: "/Sales", name: "Sales" })).toBe("/Sales");
});

it("asks for the delegated admin and hides units until the key is saved", () => {
  const setDelegatedUser = vi.fn<AIIntegrationConfigForm["setDelegatedUser"]>();
  render(
    <AIIntegrationWorkspaceFields
      provider={gemini}
      form={stubForm({ setDelegatedUser })}
      active
    />,
  );
  const field = screen.getByLabelText("Delegated admin email");
  expect(field.getAttribute("data-1p-ignore")).toBe("true");
  fireEvent.change(field, { target: { value: "vault-export@example.com" } });
  expect(setDelegatedUser).toHaveBeenCalledWith("vault-export@example.com");
  expect(screen.queryByText("Organizational unit to export")).toBeNull();
  expect(query.enabled).toBe(false);
});

it("lists units once credentials are saved and the panel is open", () => {
  query.result = {
    data: { orgUnits: [{ id: "id:root", path: "/", name: "Example Co" }] },
    isLoading: false,
    isError: false,
    error: null,
  };
  render(
    <AIIntegrationWorkspaceFields
      provider={gemini}
      form={stubForm({
        hasSavedKey: true,
        isConfigured: true,
        awaitingOrgUnit: true,
      })}
      active
    />,
  );
  expect(query.enabled).toBe(true);
  expect(screen.getByText("Organizational unit to export")).toBeTruthy();
  expect(screen.getByText(/Syncing starts once a unit is saved/)).toBeTruthy();
});

it("shows Google's refusal instead of an empty list", () => {
  query.result = {
    data: undefined,
    isLoading: false,
    isError: true,
    error: new Error(
      "Google refused a directory token; add https://www.googleapis.com/auth/admin.directory.orgunit.readonly",
    ),
  };
  render(
    <AIIntegrationWorkspaceFields
      provider={gemini}
      form={stubForm({ hasSavedKey: true, isConfigured: true })}
      active
    />,
  );
  expect(screen.getByRole("alert").textContent).toContain(
    "admin.directory.orgunit.readonly",
  );
});

it("does not fetch units while the panel is closed", () => {
  render(
    <AIIntegrationWorkspaceFields
      provider={gemini}
      form={stubForm({ hasSavedKey: true, isConfigured: true })}
      active={false}
    />,
  );
  expect(query.enabled).toBe(false);
});

it("renders nothing for providers without Workspace settings", () => {
  const cursor = AI_INTEGRATION_PROVIDERS.find(
    (provider) => provider.provider === "cursor",
  )!;
  const { container } = render(
    <AIIntegrationWorkspaceFields
      provider={cursor}
      form={stubForm({})}
      active
    />,
  );
  expect(container.innerHTML).toBe("");
});
