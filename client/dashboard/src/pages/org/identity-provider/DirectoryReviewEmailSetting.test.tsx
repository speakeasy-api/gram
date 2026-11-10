import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import type { ReactNode } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { DirectoryReviewEmailSetting } from "./DirectoryReviewEmailSetting";

const mocks = vi.hoisted(() => ({
  mutate: vi.fn(),
  settings: undefined as { emailsEnabled: boolean } | undefined,
}));
vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }));
vi.mock("@/components/require-scope", () => ({
  RequireScope: ({ children }: { children: ReactNode }) => children,
}));
vi.mock("@gram/client/react-query/directoryReviewSettings.js", () => ({
  useDirectoryReviewSettings: () => ({ data: mocks.settings }),
  invalidateAllDirectoryReviewSettings: vi.fn(),
}));
vi.mock("@gram/client/react-query/updateDirectoryReviewSettings.js", () => ({
  useUpdateDirectoryReviewSettingsMutation: () => ({
    mutate: mocks.mutate,
    isPending: false,
  }),
}));

function renderSetting() {
  return render(<DirectoryReviewEmailSetting />, {
    wrapper: ({ children }) => (
      <QueryClientProvider client={new QueryClient()}>
        {children}
      </QueryClientProvider>
    ),
  });
}

beforeEach(() => {
  mocks.settings = { emailsEnabled: true };
});

afterEach(() => {
  cleanup();
  vi.clearAllMocks();
});

describe("DirectoryReviewEmailSetting", () => {
  it("renders nothing until the setting loads", () => {
    mocks.settings = undefined;
    const { container } = renderSetting();
    expect(container.innerHTML).toBe("");
  });

  it("turns the emails off", () => {
    renderSetting();
    const toggle = screen.getByRole("switch", {
      name: "Email admins about directory changes to review",
    });
    expect(toggle.getAttribute("aria-checked")).toBe("true");

    fireEvent.click(toggle);
    expect(mocks.mutate).toHaveBeenCalledWith({
      request: { directoryReviewSettings: { emailsEnabled: false } },
    });
  });
});
