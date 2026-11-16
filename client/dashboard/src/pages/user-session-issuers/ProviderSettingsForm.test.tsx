import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { ReactNode } from "react";
import { afterEach, expect, it, vi } from "vitest";
import type { UserSessionIssuer } from "@gram/client/models/components/usersessionissuer.js";
import { ProviderSettingsForm } from "./ProviderSettingsForm";

const mutations = vi.hoisted(() => ({
  organization: vi.fn(),
  project: vi.fn(),
}));

vi.mock(
  "@gram/client/react-query/updateOrganizationUserSessionIssuer.js",
  () => ({
    useUpdateOrganizationUserSessionIssuerMutation: () => ({
      mutate: mutations.organization,
      isPending: false,
      isError: false,
    }),
  }),
);
vi.mock("@gram/client/react-query/updateUserSessionIssuer.js", () => ({
  useUpdateUserSessionIssuerMutation: () => ({
    mutate: mutations.project,
    isPending: false,
    isError: false,
  }),
}));
vi.mock("@/components/require-scope", () => ({
  RequireScope: ({
    children,
  }: {
    children: ReactNode | ((state: { disabled: boolean }) => ReactNode);
  }) =>
    typeof children === "function" ? children({ disabled: false }) : children,
}));
vi.mock(
  "@/pages/mcp/x/tabs/settings/sections/authentication/CimdAdmissionModeField",
  () => ({ AllowedClientsSummary: () => <>Manage allowed clients</> }),
);
vi.mock(
  "@/pages/mcp/x/tabs/settings/sections/authentication/CimdCustomClientsField",
  () => ({ CimdCustomClientsField: () => null }),
);

afterEach(() => {
  cleanup();
  mutations.organization.mockReset();
  mutations.project.mockReset();
});

function issuer(projectId: string): UserSessionIssuer {
  return {
    id: "usi_1",
    slug: "linear",
    projectId,
    projectName: projectId ? "Default" : "",
    sessionDurationHours: 24 * 14,
    clientIdMetadataAdmissionMode: "open",
  } as UserSessionIssuer;
}

function show(provider: UserSessionIssuer, editableProjectId?: string): void {
  render(
    <QueryClientProvider client={new QueryClient()}>
      <ProviderSettingsForm
        issuer={provider}
        editableProjectId={editableProjectId}
      />
    </QueryClientProvider>,
  );
}

it("saves an organization-wide provider's changed settings together through the organization", () => {
  show(issuer(""), "project_a");
  const save = screen.getByRole("button", { name: "Save" });
  expect((save as HTMLButtonElement).disabled).toBe(true);

  fireEvent.change(screen.getByLabelText("Session length"), {
    target: { value: "3" },
  });
  fireEvent.click(screen.getByLabelText("Verified clients"));
  fireEvent.click(save);

  expect(mutations.organization).toHaveBeenCalledWith({
    request: {
      updateOrganizationUserSessionIssuerForm: {
        id: "usi_1",
        sessionDurationHours: 24 * 7 * 3,
        clientIdMetadataAdmissionMode: "presets",
      },
    },
  });
  expect(mutations.project).not.toHaveBeenCalled();
});

it("sends only what changed for this project's own provider", () => {
  show(issuer("project_a"), "project_a");
  fireEvent.click(screen.getByLabelText("Off"));
  fireEvent.click(screen.getByRole("button", { name: "Save" }));
  expect(mutations.project).toHaveBeenCalledWith({
    request: {
      updateUserSessionIssuerForm: {
        id: "usi_1",
        clientIdMetadataAdmissionMode: "disabled",
      },
    },
  });
});

it("discards unsaved changes", () => {
  show(issuer("project_a"), "project_a");
  fireEvent.click(screen.getByLabelText("Off"));
  fireEvent.click(screen.getByRole("button", { name: "Discard" }));
  expect(
    (screen.getByLabelText("Any client") as HTMLButtonElement).getAttribute(
      "data-state",
    ),
  ).toBe("checked");
  expect(screen.queryByRole("button", { name: "Discard" })).toBeNull();
});

it("shows another project's provider read-only and says where to change it", () => {
  show(issuer("project_b"));
  expect(screen.queryByRole("button", { name: "Save" })).toBeNull();
  expect(
    (screen.getByLabelText("Session length") as HTMLInputElement).disabled,
  ).toBe(true);
  expect(
    screen.getByText("Change these in Default's MCP Sessions configuration."),
  ).toBeTruthy();
});
