import { CommandGroup, CommandItem } from "@/components/ui/Command";
import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const palette = vi.hoisted(() => ({
  isOpen: true,
  close: vi.fn(),
  actions: [] as unknown[],
  contextBadge: undefined,
}));

/** Which shell the palette opens in; cleared to the project shell per test. */
const slugs = vi.hoisted(() => ({
  orgSlug: "acme" as string | undefined,
  projectSlug: "widgets" as string | undefined,
}));

vi.mock("@/contexts/CommandPalette", () => ({
  useCommandPalette: () => palette,
}));
vi.mock("@/contexts/Sdk", () => ({
  useSlugs: () => slugs,
}));
vi.mock("react-router", () => ({ useNavigate: () => vi.fn() }));
vi.mock("./recentlyVisited", () => ({
  useRecentsUserId: () => "user_1",
  useRecentlyVisited: () => [],
  getRecentLabelOverride: () => undefined,
}));
// The groups' own contents are covered by ResourceResults.test.tsx; here they
// stand in as one searchable row each, so the assertions are about which
// groups the palette mounts in which shell.
vi.mock("./ResourceResults", () => ({
  ResourceResults: () => null,
  PeopleResults: () => null,
  ProjectsResults: () => (
    <CommandGroup heading="Projects">
      <CommandItem value="project Widgets widgets">Widgets</CommandItem>
    </CommandGroup>
  ),
}));

import { CommandPalette } from "./CommandPalette";

beforeEach(() => {
  slugs.orgSlug = "acme";
  slugs.projectSlug = "widgets";
});
afterEach(cleanup);

describe("CommandPalette", () => {
  it("offers the Project Assistant for a query that matches nothing", async () => {
    render(<CommandPalette />);

    await userEvent.type(
      screen.getByPlaceholderText("Ask AI or search resources and pages…"),
      "zzzzznomatch",
    );

    // Query by role: cmdk marks a group with no matching items `hidden`, which
    // drops its subtree from the accessibility tree — so this fails if the row
    // renders inside a hidden group (a text query would still find it).
    const row = screen.getByRole("option", { name: /Ask Project Assistant/ });
    expect(row.textContent).toContain("“zzzzznomatch”");
  });

  // The palette used to fall back to page navigation alone at the org level,
  // leaving no way to reach a project from it (S-1028).
  it("offers projects at the organization level", () => {
    slugs.projectSlug = undefined;
    render(<CommandPalette />);

    expect(screen.getByRole("option", { name: "Widgets" })).toBeTruthy();
  });

  it("offers projects inside a project only once there is a query", async () => {
    render(<CommandPalette />);

    expect(screen.queryByRole("option", { name: "Widgets" })).toBeNull();

    await userEvent.type(
      screen.getByPlaceholderText("Ask AI or search resources and pages…"),
      "widgets",
    );

    expect(screen.getByRole("option", { name: "Widgets" })).toBeTruthy();
  });
});
