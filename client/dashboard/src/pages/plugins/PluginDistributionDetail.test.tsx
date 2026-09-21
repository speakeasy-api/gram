import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import type { ReactNode } from "react";
import { MemoryRouter, Route, Routes } from "react-router";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const state = vi.hoisted(() => ({
  discovery: vi.fn(),
  skills: vi.fn(),
  success: vi.fn(),
}));
vi.mock("@/components/page-layout", () => {
  const Container = ({ children }: { children: ReactNode }) => (
    <div>{children}</div>
  );
  return {
    Page: Object.assign(Container, {
      Header: Object.assign(Container, { Breadcrumbs: () => null }),
      Body: Container,
    }),
  };
});
vi.mock("@gram/client/react-query/distributionPlugin.js", () => ({
  useDistributionPlugin: state.discovery,
}));
vi.mock("@gram/client/react-query/skills.js", () => ({
  useSkillsInfinite: state.skills,
}));
vi.mock("sonner", () => ({ toast: { success: state.success } }));
vi.mock("./PluginSkillsSection", () => ({
  PluginSkillsSection: ({
    pluginId,
    skillId,
    onMutated,
  }: {
    pluginId: string;
    skillId?: string;
    onMutated: (message: string) => void;
  }) => (
    <button
      data-skill-id={skillId}
      onClick={() => onMutated("Skill added to plugin")}
    >
      Skills for {pluginId}
    </button>
  ),
}));
import { PluginDistributionDetail } from "./PluginDistributionDetail";

function show(path = "/plugins/plugin-a") {
  return render(
    <MemoryRouter initialEntries={[path]}>
      <Routes>
        <Route
          path="/plugins/:pluginId"
          element={<PluginDistributionDetail />}
        />
      </Routes>
    </MemoryRouter>,
  );
}
afterEach(cleanup);
beforeEach(() => {
  vi.clearAllMocks();
  state.skills.mockReturnValue({
    data: { pages: [{ result: { skills: [{ id: "readable-skill" }] } }] },
    isPending: false,
  });
  state.discovery.mockReturnValue({
    data: {
      id: "plugin-a",
      name: "Example plugin",
      description: "Skill bundle",
      isDefault: true,
    },
    isPending: false,
  });
});

describe("distribution-only plugin detail", () => {
  it("uses a concrete readable skill for discovery on direct navigation", () => {
    show();
    expect(state.discovery).toHaveBeenCalledWith(
      { id: "plugin-a", skillId: "readable-skill" },
      undefined,
      expect.objectContaining({ enabled: true }),
    );
    expect(screen.getByText("Example plugin")).toBeTruthy();
    expect(screen.queryByText("Assignments")).toBeNull();
    expect(screen.queryByText("Publish now")).toBeNull();
  });
  it("preserves scoped skill context and does not request the skills list", () => {
    show("/plugins/plugin-a?skillId=scoped-skill");
    expect(state.skills).toHaveBeenCalledWith(
      { limit: 1 },
      undefined,
      expect.objectContaining({ enabled: false }),
    );
    expect(state.discovery).toHaveBeenCalledWith(
      { id: "plugin-a", skillId: "scoped-skill" },
      undefined,
      expect.objectContaining({ enabled: true }),
    );
    expect(
      screen.getByText("Skills for plugin-a").getAttribute("data-skill-id"),
    ).toBe("scoped-skill");
    fireEvent.click(screen.getByText("Skills for plugin-a"));
    expect(state.success).toHaveBeenCalledWith("Skill added to plugin");
  });
  it("never submits discovery with an empty authorization context", () => {
    state.skills.mockReturnValue({
      data: { pages: [{ result: { skills: [] } }] },
      isPending: false,
    });
    state.discovery.mockReturnValue({ isPending: true });
    show();
    expect(state.discovery).toHaveBeenCalledWith(
      { id: "plugin-a", skillId: "" },
      undefined,
      expect.objectContaining({ enabled: false }),
    );
    expect(
      screen.getByText("No readable skills are available in this project."),
    ).toBeTruthy();
  });
});
