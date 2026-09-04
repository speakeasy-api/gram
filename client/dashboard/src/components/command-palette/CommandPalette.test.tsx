import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";

const palette = vi.hoisted(() => ({
  isOpen: true,
  close: vi.fn(),
  actions: [] as unknown[],
  contextBadge: undefined,
}));

vi.mock("@/contexts/CommandPalette", () => ({
  useCommandPalette: () => palette,
}));
vi.mock("@/contexts/Sdk", () => ({
  useSlugs: () => ({ orgSlug: "acme", projectSlug: "widgets" }),
}));
vi.mock("react-router", () => ({ useNavigate: () => vi.fn() }));
vi.mock("./recentlyVisited", () => ({
  useRecentsUserId: () => "user_1",
  useRecentlyVisited: () => [],
  getRecentLabelOverride: () => undefined,
}));
vi.mock("./ResourceResults", () => ({
  ResourceResults: () => null,
  PeopleResults: () => null,
}));

import { CommandPalette } from "./CommandPalette";

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
});
