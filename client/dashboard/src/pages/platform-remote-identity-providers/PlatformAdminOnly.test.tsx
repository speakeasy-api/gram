import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

const isPlatformAdmin = vi.fn();

vi.mock("@/contexts/Auth", () => ({
  useIsPlatformAdmin: () => isPlatformAdmin(),
}));

import { PlatformAdminOnly } from "./PlatformAdminOnly";

afterEach(() => {
  cleanup();
  isPlatformAdmin.mockReset();
});

describe("PlatformAdminOnly", () => {
  it("renders the catalog for a platform admin", () => {
    isPlatformAdmin.mockReturnValue(true);

    render(
      <PlatformAdminOnly>
        <div>catalog</div>
      </PlatformAdminOnly>,
    );

    expect(screen.getByText("catalog")).toBeTruthy();
  });

  // The whole point of the separate route: a non-admin who lands here gets no
  // platform controls in the DOM at all, not merely disabled ones.
  it("renders nothing of the catalog for a non-admin", () => {
    isPlatformAdmin.mockReturnValue(false);

    render(
      <PlatformAdminOnly>
        <div>catalog</div>
      </PlatformAdminOnly>,
    );

    expect(screen.queryByText("catalog")).toBeNull();
    expect(screen.getByText(/available to platform admins only/i)).toBeTruthy();
  });
});
