import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

const isPlatformAdmin = vi.fn();

vi.mock("@/contexts/Auth", () => ({
  useIsPlatformAdmin: () => isPlatformAdmin(),
}));

import { PlatformAdminOnlyPanel } from "./platform-admin-only-panel";

afterEach(() => {
  cleanup();
  isPlatformAdmin.mockReset();
});

describe("PlatformAdminOnlyPanel", () => {
  it("renders the title and children for a platform admin", () => {
    isPlatformAdmin.mockReturnValue(true);

    render(
      <PlatformAdminOnlyPanel>
        <div>admin content</div>
      </PlatformAdminOnlyPanel>,
    );

    expect(screen.getByText("Platform Admin Only")).toBeTruthy();
    expect(screen.getByText("admin content")).toBeTruthy();
  });

  it("renders nothing for a non-admin", () => {
    isPlatformAdmin.mockReturnValue(false);

    render(
      <PlatformAdminOnlyPanel>
        <div>admin content</div>
      </PlatformAdminOnlyPanel>,
    );

    expect(screen.queryByText("Platform Admin Only")).toBeNull();
    expect(screen.queryByText("admin content")).toBeNull();
  });
});
