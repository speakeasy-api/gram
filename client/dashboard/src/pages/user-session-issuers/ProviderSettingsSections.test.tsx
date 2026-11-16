import {
  cleanup,
  fireEvent,
  render,
  screen,
  within,
} from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { ReactNode } from "react";
import { afterEach, expect, it, vi } from "vitest";
import type { UserSessionIssuer } from "@gram/client/models/components/usersessionissuer.js";
import { ClientAccessSection } from "./ClientAccessSection";
import { SessionLengthSection } from "./SessionLengthSection";

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
      error: null,
    }),
  }),
);
vi.mock("@gram/client/react-query/updateUserSessionIssuer.js", () => ({
  useUpdateUserSessionIssuerMutation: () => ({
    mutate: mutations.project,
    isPending: false,
    error: null,
  }),
}));
vi.mock("@gram/client/react-query/cimdClientPresets.js", () => ({
  useCimdClientPresets: () => ({
    isLoading: false,
    isError: false,
    data: {
      items: [
        {
          displayName: "Zed",
          clientIdMetadataUri: "https://zed.example/client.json",
          enabled: true,
          isPattern: false,
          vendorKey: "zed",
        },
      ],
    },
  }),
}));
vi.mock(
  "@gram/client/react-query/verifyUserSessionIssuerCimdClientURL.js",
  () => ({
    useVerifyUserSessionIssuerCimdClientURLMutation: () => ({
      mutate: vi.fn(),
      isPending: false,
    }),
  }),
);
vi.mock("./useOrganizationClients", () => ({
  useOrganizationClients: () => ({
    items: [],
    isLoading: false,
    isError: false,
    retry: vi.fn(),
    add: vi.fn(),
    adding: false,
    addError: null,
    clearAddError: vi.fn(),
    remove: vi.fn(),
    removing: false,
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

afterEach(() => {
  cleanup();
  mutations.organization.mockReset();
  mutations.project.mockReset();
});

function issuer(projectId: string, mode = "open"): UserSessionIssuer {
  return {
    id: "usi_1",
    slug: "linear",
    projectId,
    projectName: projectId ? "Default" : "",
    sessionDurationHours: 24 * 14,
    clientIdMetadataAdmissionMode: mode,
  } as UserSessionIssuer;
}

function show(node: ReactNode): void {
  render(
    <QueryClientProvider client={new QueryClient()}>
      {node}
    </QueryClientProvider>,
  );
}

it("saves session length on its own through the organization for an organization-wide provider", () => {
  show(<SessionLengthSection issuer={issuer("")} editableProjectId="p_a" />);
  const save = screen.getByRole("button", { name: "Save" });
  expect((save as HTMLButtonElement).disabled).toBe(true);
  fireEvent.change(screen.getByLabelText("Session length"), {
    target: { value: "3" },
  });
  fireEvent.click(save);
  expect(mutations.organization).toHaveBeenCalledWith({
    request: {
      updateOrganizationUserSessionIssuerForm: {
        id: "usi_1",
        sessionDurationHours: 24 * 7 * 3,
      },
    },
  });
});

it("offers Known clients and Any client only, and warns before narrowing to known clients", () => {
  show(<ClientAccessSection issuer={issuer("p_a")} editableProjectId="p_a" />);
  expect(screen.getAllByRole("radio")).toHaveLength(2);
  expect(screen.queryByText(/can't sign in again/)).toBeNull();

  fireEvent.click(screen.getByLabelText("Known clients"));
  expect(screen.getByText(/can't sign in again/)).toBeTruthy();
  fireEvent.click(screen.getByRole("button", { name: "See all" }));
  expect(
    within(
      screen.getByRole("list", { name: "Allowed by Speakeasy" }),
    ).getByText("Zed"),
  ).toBeTruthy();
  fireEvent.keyDown(document.activeElement ?? document.body, { key: "Escape" });
  expect(screen.getByRole("button", { name: "Add" })).toBeTruthy();

  fireEvent.click(screen.getByRole("button", { name: "Save" }));
  expect(mutations.project).toHaveBeenCalledWith({
    request: {
      updateUserSessionIssuerForm: {
        id: "usi_1",
        clientIdMetadataAdmissionMode: "presets",
      },
    },
  });
});

it("selects neither option for a provider that refuses client documents", () => {
  show(
    <ClientAccessSection
      issuer={issuer("p_a", "disabled")}
      editableProjectId="p_a"
    />,
  );
  for (const radio of screen.getAllByRole("radio")) {
    expect(radio.getAttribute("data-state")).toBe("unchecked");
  }
  expect(screen.getByText(/each one registers itself/)).toBeTruthy();
});

it("shows another project's provider read-only and says where to change it", () => {
  show(<ClientAccessSection issuer={issuer("p_b", "presets")} />);
  expect(screen.queryByRole("button", { name: "Save" })).toBeNull();
  expect(screen.queryByRole("button", { name: "Add" })).toBeNull();
  expect(
    screen.getByText("Change this in Default's MCP Sessions configuration."),
  ).toBeTruthy();
});
