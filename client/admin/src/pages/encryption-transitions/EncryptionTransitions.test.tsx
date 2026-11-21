import { createRootRoute, createRoute } from "@tanstack/react-router";
import { cleanup, fireEvent, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { AdminOrganizationEncryptionTransitionList } from "@gram/admin-client/models/components/adminorganizationencryptiontransitionlist";
import type { AdminOrganizationEncryptionTransitionRow } from "@gram/admin-client/models/components/adminorganizationencryptiontransitionrow";

import {
  ENROLL_INTERVAL_MS,
  EncryptionTransitions,
} from "@/pages/encryption-transitions/EncryptionTransitions";
import { encryptionTransitionsSearch } from "@/pages/encryption-transitions/encryptionTransitionsSearch";
import { renderRouteTree } from "@/test/harness";

const mocks = vi.hoisted(() => ({
  list: vi.fn(),
  enroll: vi.fn(),
}));

vi.mock("@/lib/gramAdminClient", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/lib/gramAdminClient")>();
  return {
    ...actual,
    organizationEncryptionTransitionsQuery: (request: object) => ({
      queryKey: [...actual.organizationEncryptionTransitionsKeyPrefix, request],
      queryFn: () => mocks.list(request),
      retry: false,
    }),
    enrollOrganizationEncryption: mocks.enroll,
  };
});

function aRow(
  id: string,
  overrides: Partial<AdminOrganizationEncryptionTransitionRow> = {},
): AdminOrganizationEncryptionTransitionRow {
  return {
    organizationId: id,
    name: `Org ${id}`,
    slug: `org-${id}`,
    disabled: false,
    state: "platform_key",
    ...overrides,
  };
}

function aList(
  organizations: AdminOrganizationEncryptionTransitionRow[],
): AdminOrganizationEncryptionTransitionList {
  return {
    useCase: "oauth-client-secrets",
    useCases: ["oauth-client-secrets", "oauth-session-tokens"],
    organizations,
    stateCounts: [
      { state: "platform_key", organizations: organizations.length },
    ],
    hasMore: false,
  };
}

async function mount() {
  const root = createRootRoute();
  const route = createRoute({
    getParentRoute: () => root,
    path: "/encryption-transitions",
    component: EncryptionTransitions,
    validateSearch: encryptionTransitionsSearch,
  });
  return renderRouteTree(root.addChildren([route]), {
    initialPath: "/encryption-transitions",
  });
}

beforeEach(() => {
  mocks.list.mockReset();
  mocks.enroll.mockReset();
});

afterEach(() => {
  cleanup();
});

describe("EncryptionTransitions", () => {
  it("enrolls the selected organizations one at a time and reports failures without stopping", async () => {
    mocks.list.mockResolvedValue(
      aList([aRow("a"), aRow("b"), aRow("off", { disabled: true })]),
    );
    let failFirst: (error: Error) => void = () => {};
    mocks.enroll.mockImplementation((organizationID: string) =>
      organizationID === "a"
        ? new Promise((_, reject) => {
            failFirst = reject;
          })
        : Promise.resolve({}),
    );
    await mount();

    expect(await screen.findByText("Org a")).toBeTruthy();
    expect(
      screen.queryByRole("checkbox", { name: "Select Org off" }),
      "a disabled organization is not offered",
    ).toBeNull();

    fireEvent.click(screen.getByRole("checkbox", { name: "Select Org a" }));
    fireEvent.click(screen.getByRole("checkbox", { name: "Select Org b" }));
    fireEvent.click(
      screen.getByRole("button", { name: "Enroll selected (2)" }),
    );

    const dialog = await screen.findByRole("dialog");
    fireEvent.click(
      screen
        .getAllByRole("button", { name: "Enroll" })
        .find((button) => dialog.contains(button))!,
    );

    await waitFor(() => expect(mocks.enroll).toHaveBeenCalledTimes(1));
    // The second enrollment waits for the first, however long it takes.
    await new Promise((resolve) => {
      setTimeout(resolve, ENROLL_INTERVAL_MS + 200);
    });
    expect(mocks.enroll).toHaveBeenCalledTimes(1);

    failFirst(new Error("kms quota exceeded"));
    await waitFor(() => expect(mocks.enroll).toHaveBeenCalledTimes(2), {
      timeout: ENROLL_INTERVAL_MS + 3000,
    });
    expect(mocks.enroll.mock.calls.map((call) => call[0])).toEqual(["a", "b"]);
    const alert = await screen.findByRole("alert");
    expect(alert.textContent).toContain("1 organization could not be enrolled");
    expect(alert.textContent).toContain("Org a: kms quota exceeded");
  });

  it("selects the next organizations in platform key", async () => {
    mocks.list.mockResolvedValue(
      aList([aRow("a"), aRow("off", { disabled: true }), aRow("b"), aRow("c")]),
    );
    await mount();

    expect(await screen.findByText("Org a")).toBeTruthy();
    fireEvent.change(screen.getByLabelText("Batch size"), {
      target: { value: "2" },
    });
    fireEvent.click(
      screen.getByRole("button", { name: "Select next in platform key" }),
    );

    expect(
      await screen.findByRole("button", { name: "Enroll selected (2)" }),
    ).toBeTruthy();
    expect(
      screen
        .getByRole<HTMLButtonElement>("checkbox", { name: "Select Org a" })
        .getAttribute("data-state"),
    ).toBe("checked");
    expect(
      screen
        .getByRole("checkbox", { name: "Select Org c" })
        .getAttribute("data-state"),
    ).toBe("unchecked");
  });
});
