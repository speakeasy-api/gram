import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { RolePluginSettingsPanel } from "./RolePluginSettingsPanel";
import type { RolePluginStatus } from "./RolePluginSettings";

const mocks = vi.hoisted(() => ({
  organizationId: "org-one",
  read: vi.fn<() => Promise<RolePluginStatus>>(),
  mutate: vi.fn(),
  callbacks: {} as {
    onSuccess?: (result: RolePluginStatus) => void;
    onError?: (cause: Error & { statusCode: number }) => void;
  },
}));
vi.mock("@/contexts/Auth", () => ({
  useOrganization: () => ({
    id: mocks.organizationId,
    projects: [{ id: "project-b", slug: "beta" }],
  }),
}));
vi.mock("@gram/client/react-query/_context.js", () => ({
  useGramContext: () => ({}),
}));
vi.mock("@gram/client/react-query/roleProvisioning.js", () => ({
  buildRoleProvisioningQuery: () => ({
    queryKey: ["role-provisioning"],
    queryFn: mocks.read,
  }),
}));
vi.mock("@gram/client/react-query/configureRoleProvisioning.js", () => ({
  useConfigureRoleProvisioningMutation: (callbacks: typeof mocks.callbacks) => {
    mocks.callbacks = callbacks;
    return { mutate: mocks.mutate, isPending: false };
  },
}));
function status(version = 0): RolePluginStatus {
  return {
    version,
    enabled: false,
    projects: [
      { id: "project-a", name: "Alpha" },
      { id: "project-b", name: "Beta" },
    ],
    roles: [
      {
        roleUrn: "urn:role:engineering",
        name: "Engineering",
        configured: false,
        enabled: false,
        originAudience: "not_provisioned",
        publicationStatus: "not_provisioned",
      },
    ],
  };
}
function setup() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  const view = render(
    <QueryClientProvider client={client}>
      <RolePluginSettingsPanel preferredProjectSlug="beta" />
    </QueryClientProvider>,
  );
  return { client, ...view };
}
afterEach(cleanup);
beforeEach(() => {
  mocks.organizationId = "org-one";
  mocks.read.mockReset().mockResolvedValue(status());
  mocks.mutate.mockReset();
  mocks.callbacks = {};
});
describe("shared role provisioning API panel", () => {
  it("renders saved intent without claiming provisioning after a successful save", async () => {
    const { client } = setup();
    await screen.findByRole("checkbox", { name: "Engineering" });
    fireEvent.click(
      screen.getByRole("checkbox", { name: "Enable automatic role plugins" }),
    );
    fireEvent.click(
      screen.getByRole("button", { name: "Save role plugin settings" }),
    );
    const result = { ...status(1), enabled: true };
    mocks.callbacks.onSuccess?.(result);
    await waitFor(() => expect(screen.getByText("Enabled")).toBeTruthy());
    expect(
      screen.getByText("Associated plugin").nextElementSibling?.textContent,
    ).toBe("Not provisioned");
    expect(
      client.getQueryData(["role-provisioning", { organizationId: "org-one" }]),
    ).toEqual(result);
    fireEvent.click(
      screen.getByRole("button", { name: "Save role plugin settings" }),
    );
    expect(
      mocks.mutate.mock.lastCall?.[0].request
        .configureRoleProvisioningRequestBody.expectedVersion,
    ).toBe(1);
  });
  it("loads ranked choices, prefers the onboarding project, and sends expected version", async () => {
    setup();
    await screen.findByRole("button", { name: "Save role plugin settings" });
    expect(
      screen.getByRole<HTMLButtonElement>("combobox", {
        name: "Destination for Engineering",
      }).textContent,
    ).toBe("Beta");
    fireEvent.click(
      screen.getByRole("checkbox", { name: "Enable automatic role plugins" }),
    );
    fireEvent.click(
      screen.getByRole("button", { name: "Save role plugin settings" }),
    );
    expect(mocks.mutate).toHaveBeenCalledWith({
      request: {
        configureRoleProvisioningRequestBody: {
          expectedVersion: 0,
          enabled: true,
          projectId: "project-b",
          roles: [
            {
              roleUrn: "urn:role:engineering",
              enabled: true,
              projectId: "project-b",
            },
          ],
        },
      },
      security: { sessionHeaderGramSession: "" },
    });
  });
  it("retains conflict version and exclusions until explicit reload", async () => {
    setup();
    await screen.findByRole("checkbox", { name: "Engineering" });
    fireEvent.click(screen.getByRole("checkbox", { name: "Engineering" }));
    fireEvent.click(
      screen.getByRole("button", { name: "Save role plugin settings" }),
    );
    mocks.callbacks.onError?.(
      Object.assign(new Error("Conflict"), { statusCode: 409 }),
    );
    expect(await screen.findByRole("alert")).toBeTruthy();
    expect(
      screen
        .getByRole<HTMLButtonElement>("checkbox", { name: "Engineering" })
        .getAttribute("aria-checked"),
    ).toBe("false");
    mocks.read.mockResolvedValue({ ...status(7), enabled: true });
    fireEvent.click(
      screen.getByRole("button", { name: "Reload saved settings" }),
    );
    await waitFor(() => expect(screen.queryByRole("alert")).toBeNull());
    expect(
      screen
        .getByRole("checkbox", { name: "Enable automatic role plugins" })
        .getAttribute("aria-checked"),
    ).toBe("true");
    fireEvent.click(
      screen.getByRole("button", { name: "Save role plugin settings" }),
    );
    expect(
      mocks.mutate.mock.lastCall?.[0].request
        .configureRoleProvisioningRequestBody.expectedVersion,
    ).toBe(7);
  });
  it("isolates cached configuration and discards drafts when switching organizations", async () => {
    const original = {
      ...status(3),
      roles: status().roles.map((role) => ({ ...role, configured: true })),
    };
    mocks.read.mockResolvedValue(original);
    const { client, rerender } = setup();
    fireEvent.click(
      await screen.findByRole("checkbox", { name: "Engineering" }),
    );
    fireEvent.click(
      screen.getByRole("checkbox", { name: "Enable automatic role plugins" }),
    );
    mocks.organizationId = "org-two";
    mocks.read.mockResolvedValue({ ...status(), roles: [] });
    rerender(
      <QueryClientProvider client={client}>
        <RolePluginSettingsPanel />
      </QueryClientProvider>,
    );
    expect(screen.queryByRole("checkbox", { name: "Engineering" })).toBeNull();
    await screen.findByText(/No IdP roles yet/);
    expect(
      screen
        .getByRole("checkbox", { name: "Enable automatic role plugins" })
        .getAttribute("aria-checked"),
    ).toBe("false");
    mocks.organizationId = "org-one";
    mocks.read.mockResolvedValue(original);
    rerender(
      <QueryClientProvider client={client}>
        <RolePluginSettingsPanel />
      </QueryClientProvider>,
    );
    expect(
      screen
        .getByRole("checkbox", { name: "Engineering" })
        .getAttribute("aria-checked"),
    ).toBe("false");
    expect(
      screen
        .getByRole("checkbox", { name: "Enable automatic role plugins" })
        .getAttribute("aria-checked"),
    ).toBe("false");
  });
  it("offers a retry without blocking directory sync on read failure", async () => {
    mocks.read.mockRejectedValue(new Error("Unavailable"));
    setup();
    expect((await screen.findByRole("alert")).textContent).toContain(
      "Directory sync is unaffected",
    );
    mocks.read.mockResolvedValue(status());
    fireEvent.click(
      screen.getByRole("button", { name: "Retry role plugin settings" }),
    );
    expect(
      await screen.findByRole("checkbox", { name: "Engineering" }),
    ).toBeTruthy();
  });
});
