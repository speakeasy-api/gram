import type { UserSessionIssuer } from "@gram/client/models/components/usersessionissuer.js";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { MakeOrganizationWideDialog } from "./MakeOrganizationWideDialog";

const mutate = vi.hoisted(() => vi.fn());

vi.mock(
  "@gram/client/react-query/moveOrganizationUserSessionIssuer.js",
  () => ({
    useMoveOrganizationUserSessionIssuerMutation: () => ({
      mutate,
      isPending: false,
      isError: false,
    }),
  }),
);

vi.mock("./invalidateIssuers", () => ({ invalidateIssuers: vi.fn() }));

vi.mock("@tanstack/react-query", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@tanstack/react-query")>()),
  useQueryClient: () => ({}),
}));

vi.mock("sonner", () => ({ toast: { success: vi.fn() } }));

afterEach(() => {
  cleanup();
});

describe("MakeOrganizationWideDialog", () => {
  it("asks before sharing a project's authorization server", () => {
    render(
      <MakeOrganizationWideDialog
        issuer={
          {
            id: "issuer-1",
            slug: "linear",
            projectId: "project-1",
          } as UserSessionIssuer
        }
        onClose={() => {}}
      />,
    );

    expect(screen.getByText("Make linear organization-wide?")).toBeDefined();
    expect(mutate).not.toHaveBeenCalled();

    fireEvent.click(
      screen.getByRole("button", { name: "Make organization-wide" }),
    );
    expect(mutate).toHaveBeenCalledWith({
      request: { moveIssuerRequestBody: { id: "issuer-1" } },
    });
  });
});
