import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
} from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { ReactNode } from "react";
import OrgApiKeys from "./OrgApiKeys";
import { createKeyFormToJSON } from "@gram/client/models/components/createkeyform.js";

const mocks = vi.hoisted(() => ({
  organization: {
    id: "org_test",
    projects: [
      {
        id: "11111111-1111-4111-8111-111111111111",
        name: "Test project",
        slug: "test-project",
      },
    ],
  },
  mutate: vi.fn(),
  pending: false,
  onCreated: async (_key: object) => {},
  keys: [] as object[],
  admin: true,
}));
vi.mock("@/contexts/Auth", () => ({
  useOrganization: () => mocks.organization,
}));
vi.mock("@/components/require-scope", () => ({
  RequireScope: ({ children }: { children: ReactNode }) =>
    mocks.admin ? children : null,
}));
vi.mock("@/components/page-templates", () => ({
  ResourceListPage: ({
    children,
    primaryAction,
  }: {
    children: ReactNode;
    primaryAction: ReactNode;
  }) => (
    <>
      {primaryAction}
      {children}
    </>
  ),
}));
vi.mock("@gram/client/react-query/createAPIKey", () => ({
  useCreateAPIKeyMutation: (options: {
    onSuccess: (key: object) => Promise<void>;
  }) => {
    mocks.onCreated = options.onSuccess;
    return { mutate: mocks.mutate, isPending: mocks.pending };
  },
}));
vi.mock("@gram/client/react-query/listAPIKeys", () => ({
  useListAPIKeysSuspense: () => ({ data: { keys: mocks.keys } }),
  invalidateListAPIKeys: vi.fn(),
}));
vi.mock("@gram/client/react-query/revokeAPIKey", () => ({
  useRevokeAPIKeyMutation: () => ({ mutate: vi.fn() }),
}));
vi.mock("@tanstack/react-query", () => ({
  useQueryClient: () => ({ refetchQueries: vi.fn() }),
}));

beforeEach(() => {
  mocks.mutate.mockClear();
  mocks.pending = false;
  mocks.keys = [];
  mocks.admin = true;
  mocks.organization = {
    id: "org_test",
    projects: [
      {
        id: "11111111-1111-4111-8111-111111111111",
        name: "Test project",
        slug: "test-project",
      },
    ],
  };
  // Radix scrolls the selected option into view in browsers.
  Element.prototype.scrollIntoView = vi.fn<() => void>();
});
afterEach(cleanup);

async function openForm() {
  const user = userEvent.setup();
  await user.click(screen.getByRole("button", { name: "New API Key" }));
  await user.type(screen.getByLabelText("Key name"), "Test key");
  return user;
}
async function selectProject(
  user: ReturnType<typeof userEvent.setup>,
  name: string,
) {
  await user.click(screen.getByRole("combobox", { name: "Project" }));
  await user.click(screen.getByRole("option", { name }));
}

describe("API key project binding", () => {
  it("defaults to organization-wide and preserves permission scope", async () => {
    render(<OrgApiKeys />);
    const user = await openForm();
    await user.click(screen.getByRole("radio", { name: /^Producer:/ }));
    await user.click(screen.getByRole("button", { name: "Create" }));
    expect(
      JSON.parse(
        createKeyFormToJSON(
          mocks.mutate.mock.calls[0]?.[0].request.createKeyForm,
        ),
      ),
    ).toEqual({ name: "Test key", scopes: ["producer"] });
    expect(mocks.mutate.mock.calls[0]?.[0]).toEqual({
      security: { sessionHeaderGramSession: "" },
      request: {
        createKeyForm: {
          name: "Test key",
          scopes: ["producer"],
          projectId: undefined,
        },
      },
    });
  });

  it("sends the selected project ID in the body, not a project header", async () => {
    render(<OrgApiKeys />);
    const user = await openForm();
    await selectProject(user, "Test project");
    await user.click(screen.getByRole("button", { name: "Create" }));
    expect(
      JSON.parse(
        createKeyFormToJSON(
          mocks.mutate.mock.calls[0]?.[0].request.createKeyForm,
        ),
      ),
    ).toEqual({
      name: "Test key",
      scopes: ["consumer"],
      project_id: mocks.organization.projects[0]!.id,
    });
    expect(mocks.mutate.mock.calls[0]?.[0].request).toEqual({
      createKeyForm: {
        name: "Test key",
        scopes: ["consumer"],
        projectId: mocks.organization.projects[0]!.id,
      },
    });
  });

  it("can explicitly switch back to organization-wide", async () => {
    render(<OrgApiKeys />);
    const user = await openForm();
    await selectProject(user, "Test project");
    await selectProject(user, "Organization-wide");
    await user.click(screen.getByRole("button", { name: "Create" }));
    expect(
      mocks.mutate.mock.calls[0]?.[0].request.createKeyForm.projectId,
    ).toBeUndefined();
  });

  it("clears selection on cancel and organization changes", async () => {
    const view = render(<OrgApiKeys />);
    const user = await openForm();
    await selectProject(user, "Test project");
    await user.click(screen.getByRole("button", { name: "Cancel" }));
    await openForm();
    expect(screen.getByRole("combobox").textContent).toContain(
      "Organization-wide",
    );
    await selectProject(user, "Test project");
    mocks.organization = { id: "org_other", projects: [] };
    view.rerender(<OrgApiKeys />);
    expect(screen.queryByRole("dialog")).toBeNull();
    await openForm();
    expect(screen.getByRole("combobox").textContent).toContain(
      "Organization-wide",
    );
    await user.click(screen.getByRole("button", { name: "Create" }));
    expect(
      mocks.mutate.mock.calls[0]?.[0].request.createKeyForm.projectId,
    ).toBeUndefined();
  });

  it("does not silently broaden a no-longer-authorized project selection", async () => {
    const view = render(<OrgApiKeys />);
    const user = await openForm();
    await selectProject(user, "Test project");
    mocks.organization = { ...mocks.organization, projects: [] };
    view.rerender(<OrgApiKeys />);
    expect(
      (
        screen.getByRole("button", {
          name: "Create",
        }) as HTMLButtonElement
      ).disabled,
    ).toBe(true);
    fireEvent.submit(screen.getByLabelText("Key name").closest("form")!);
    expect(mocks.mutate).not.toHaveBeenCalled();
    expect(screen.getByRole("combobox", { name: "Project" }).textContent).toBe(
      "Unavailable project",
    );
    expect(
      screen.getByText(
        "This project is no longer available. Select a project or Organization-wide.",
      ),
    ).toBeTruthy();
    await selectProject(user, "Organization-wide");
    await user.click(screen.getByRole("button", { name: "Create" }));
    expect(
      mocks.mutate.mock.calls[0]?.[0].request.createKeyForm.projectId,
    ).toBeUndefined();
  });

  it("blocks duplicate submissions while creation is pending", async () => {
    const view = render(<OrgApiKeys />);
    await openForm();
    mocks.pending = true;
    view.rerender(<OrgApiKeys />);
    fireEvent.submit(screen.getByLabelText("Key name").closest("form")!);
    expect(mocks.mutate).not.toHaveBeenCalled();
  });

  it("does not show an old organization's delayed creation result", async () => {
    const view = render(<OrgApiKeys />);
    const user = await openForm();
    await selectProject(user, "Test project");
    await user.click(screen.getByRole("button", { name: "Create" }));
    const oldOnCreated = mocks.onCreated;
    mocks.organization = { id: "org_other", projects: [] };
    view.rerender(<OrgApiKeys />);
    await openForm();
    await act(() =>
      oldOnCreated({
        id: "old_key",
        name: "Old key",
        key: "synthetic-old-secret",
        projectId: "11111111-1111-4111-8111-111111111111",
      }),
    );
    expect(screen.queryByText("synthetic-old-secret")).toBeNull();
    expect(screen.getByRole("combobox").textContent).toContain(
      "Organization-wide",
    );
  });

  it("shows binding separately from permission scopes in the list", () => {
    mocks.keys = [
      {
        id: "key_test",
        name: "Bound key",
        keyPrefix: "test-prefix",
        scopes: ["consumer"],
        projectId: mocks.organization.projects[0]!.id,
        createdAt: new Date(),
      },
      {
        id: "key_org",
        name: "Org key",
        keyPrefix: "test-prefix",
        scopes: ["consumer"],
        createdAt: new Date(),
      },
    ];
    render(<OrgApiKeys />);
    expect(screen.getByText("Project binding")).toBeTruthy();
    expect(screen.getByText("Test project")).toBeTruthy();
    expect(screen.getByText("Organization-wide")).toBeTruthy();
  });

  it("shows the returned binding with the one-time secret and clears both on close", async () => {
    render(<OrgApiKeys />);
    const user = await openForm();
    await act(() =>
      mocks.onCreated({
        id: "key_created",
        name: "Test key",
        key: "synthetic-test-secret",
        projectId: mocks.organization.projects[0]!.id,
      }),
    );
    expect(screen.getByText("Project binding: Test project")).toBeTruthy();
    expect(screen.getByText("synthetic-test-secret")).toBeTruthy();
    await user.click(screen.getAllByRole("button", { name: "Close" })[0]!);
    await openForm();
    expect(screen.queryByText("synthetic-test-secret")).toBeNull();
    expect(screen.getByRole("combobox").textContent).toContain(
      "Organization-wide",
    );
  });

  it("retains the admin gate", () => {
    mocks.admin = false;
    render(<OrgApiKeys />);
    expect(screen.queryByRole("button", { name: "New API Key" })).toBeNull();
  });
});
