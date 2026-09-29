import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import userEvent from "@testing-library/user-event";
import {
  RolePluginSettings,
  type RolePluginStatus,
  type RolePluginConfiguration,
} from "./RolePluginSettings";

afterEach(cleanup);
function status(overrides: Partial<RolePluginStatus> = {}): RolePluginStatus {
  return {
    enabled: false,
    version: 0,
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
    ...overrides,
  };
}
function setup(data = status(), preferredProjectId?: string) {
  const save = vi.fn<(configuration: RolePluginConfiguration) => void>();
  const reload = vi.fn<() => void>();
  render(
    <RolePluginSettings
      status={data}
      saving={false}
      onSave={save}
      onReload={reload}
      preferredProjectId={preferredProjectId}
    />,
  );
  return { save, reload };
}
describe("role plugin confirmation", () => {
  it("keeps the original conflict token when a background read changes", () => {
    const save = vi.fn<(configuration: RolePluginConfiguration) => void>();
    const view = render(
      <RolePluginSettings
        status={status({ version: 2 })}
        saving={false}
        onSave={save}
        onReload={vi.fn<() => void>()}
      />,
    );
    view.rerender(
      <RolePluginSettings
        status={status({ version: 3 })}
        saving={false}
        onSave={save}
        onReload={vi.fn<() => void>()}
      />,
    );
    fireEvent.click(
      screen.getByRole("button", { name: "Save role plugin settings" }),
    );
    expect(save.mock.calls[0]?.[0].expectedVersion).toBe(2);
  });
  it("uses the backend-ranked fallback and blocks saves while pending", async () => {
    const save = vi.fn<(configuration: RolePluginConfiguration) => void>();
    render(
      <RolePluginSettings
        status={status()}
        saving={true}
        onSave={save}
        onReload={vi.fn<() => void>()}
      />,
    );
    expect(
      screen.getByRole<HTMLButtonElement>("combobox", {
        name: "Destination for Engineering",
      }).textContent,
    ).toBe("Alpha");
    const button = screen.getByRole("button", { name: "Saving…" });
    await userEvent.setup().click(button);
    expect(save).not.toHaveBeenCalled();
  });
  it("preserves configured exclusions and destinations even at version zero", () => {
    const base = status();
    const { save } = setup(
      status({
        projectId: "project-a",
        roles: base.roles.map((role) => ({
          ...role,
          configured: true,
          enabled: false,
          projectId: "project-a",
        })),
      }),
      "project-b",
    );
    expect(
      screen
        .getByRole<HTMLButtonElement>("checkbox", { name: "Engineering" })
        .getAttribute("aria-checked"),
    ).toBe("false");
    expect(
      screen.getByRole<HTMLButtonElement>("combobox", {
        name: "Default destination project",
      }).textContent,
    ).toBe("Alpha");
    expect(
      screen.getByRole<HTMLButtonElement>("combobox", {
        name: "Destination for Engineering",
      }).textContent,
    ).toBe("Alpha");
    expect(screen.getByText(/Saved role setting: Excluded/)).toBeTruthy();
    fireEvent.click(
      screen.getByRole("button", { name: "Save role plugin settings" }),
    );
    expect(save.mock.calls[0]?.[0]).toMatchObject({
      expectedVersion: 0,
      roles: [{ enabled: false, projectId: "project-a" }],
    });
  });
  it("defaults organization off and selects all roles with onboarding destination preferred", () => {
    const { save } = setup(status(), "project-b");
    expect(
      screen
        .getByRole<HTMLButtonElement>("checkbox", {
          name: "Enable automatic role plugins",
        })
        .getAttribute("aria-checked"),
    ).toBe("false");
    expect(
      screen
        .getByRole<HTMLButtonElement>("checkbox", { name: "Engineering" })
        .getAttribute("aria-checked"),
    ).toBe("true");
    expect(
      screen.getByRole<HTMLButtonElement>("combobox", {
        name: "Destination for Engineering",
      }).textContent,
    ).toBe("Beta");
    fireEvent.click(
      screen.getByRole<HTMLButtonElement>("checkbox", {
        name: "Enable automatic role plugins",
      }),
    );
    fireEvent.click(
      screen.getByRole("button", { name: "Save role plugin settings" }),
    );
    expect(save).toHaveBeenCalledWith({
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
    });
    expect(screen.queryByText("MCP Gateway")).toBeNull();
  });
  it("preserves saved exclusions and sticky destinations on re-enable", () => {
    const base = status();
    const { save } = setup(
      status({
        version: 5,
        roles: base.roles.map((r) => ({
          ...r,
          projectId: "project-a",
          enabled: false,
        })),
      }),
      "project-b",
    );
    expect(
      screen
        .getByRole<HTMLButtonElement>("checkbox", { name: "Engineering" })
        .getAttribute("aria-checked"),
    ).toBe("false");
    fireEvent.keyDown(
      screen.getByRole("combobox", { name: "Default destination project" }),
      { key: "ArrowDown" },
    );
    fireEvent.click(screen.getByRole("option", { name: "Beta" }));
    expect(
      screen.getByRole<HTMLButtonElement>("combobox", {
        name: "Destination for Engineering",
      }).textContent,
    ).toBe("Alpha");
    fireEvent.click(
      screen.getByRole<HTMLButtonElement>("checkbox", {
        name: "Enable automatic role plugins",
      }),
    );
    fireEvent.click(
      screen.getByRole("button", { name: "Save role plugin settings" }),
    );
    expect(save.mock.calls[0]?.[0]).toMatchObject({
      expectedVersion: 5,
      roles: [{ enabled: false, projectId: "project-a" }],
    });
  });
  it("sends only organization disable, preserving all role and project configuration", () => {
    const { save } = setup(status({ enabled: true, version: 9 }));
    fireEvent.click(
      screen.getByRole<HTMLButtonElement>("checkbox", {
        name: "Enable automatic role plugins",
      }),
    );
    fireEvent.click(
      screen.getByRole<HTMLButtonElement>("checkbox", { name: "Engineering" }),
    );
    fireEvent.click(
      screen.getByRole("button", { name: "Save role plugin settings" }),
    );
    expect(save).toHaveBeenCalledWith({ expectedVersion: 9, enabled: false });
  });
  it("keeps no-project intent pending without creating a project", () => {
    const { save } = setup(status({ projectId: undefined, projects: [] }));
    expect(screen.getByText(/No project will be created/)).toBeTruthy();
    fireEvent.click(
      screen.getByRole("button", { name: "Save role plugin settings" }),
    );
    expect(save.mock.calls[0]?.[0]).toMatchObject({
      projectId: "00000000-0000-0000-0000-000000000000",
    });
  });
  it("separates desired, applied, audience and publication, including pending approval", () => {
    setup(
      status({
        version: 3,
        enabled: true,
        roles: [
          {
            roleUrn: "urn:role:engineering",
            name: "Engineering",
            configured: false,
            enabled: true,
            projectId: "project-b",
            appliedProjectId: "project-a",
            pluginId: "plugin-one",
            originAudience: "not_assigned",
            publicationStatus: "not_published",
            pendingReason: "audience_approval_required",
          },
        ],
      }),
    );
    expect(
      screen.getByText("Saved desired destination").nextElementSibling
        ?.textContent,
    ).toContain("Beta");
    expect(
      screen.getByText("Applied destination").nextElementSibling?.textContent,
    ).toContain("Alpha");
    expect(
      screen.getByText("Publication").nextElementSibling?.textContent,
    ).toContain("not published");
    expect(screen.getByRole("status")?.textContent).toContain(
      "Pending: audience approval required",
    );
    expect(screen.getByRole("status")?.textContent).toContain(
      "Review the existing audience approval request",
    );
  });
});
