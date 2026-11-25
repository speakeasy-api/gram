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
    valueCount: 1,
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
    expect(
      mocks.list,
      "select next follows the values filter and order on screen",
    ).toHaveBeenCalledWith(
      expect.objectContaining({
        limit: 200,
        state: "platform_key",
        values: "present",
        sort: "last_written",
      }),
    );
  });

  it("lists organizations with values, most recently written first, by default", async () => {
    mocks.list.mockResolvedValue(
      aList([
        aRow("a", {
          valueCount: 1234,
          lastWrittenAt: new Date("2026-03-02T12:00:00Z"),
        }),
      ]),
    );
    await mount();

    expect(await screen.findByText("Org a")).toBeTruthy();
    expect(mocks.list).toHaveBeenCalledWith(
      expect.objectContaining({
        values: "present",
        sort: "last_written",
      }),
    );
    expect(screen.getByRole("columnheader", { name: "Values" })).toBeTruthy();
    expect(
      screen.getByRole("columnheader", { name: "Last written" }),
    ).toBeTruthy();
    expect(screen.getByText((1234).toLocaleString())).toBeTruthy();
    expect(
      screen
        .getByRole("button", { name: "With values" })
        .getAttribute("aria-pressed"),
    ).toBe("true");
  });

  it("lists every organization when asked for all", async () => {
    mocks.list.mockResolvedValue(aList([aRow("a")]));
    const { router } = await mount();

    expect(await screen.findByText("Org a")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "All" }));

    await waitFor(() =>
      expect(mocks.list).toHaveBeenCalledWith(
        expect.objectContaining({ values: "all", sort: "last_written" }),
      ),
    );
    expect(router.state.location.search).toMatchObject({ values: "all" });
    expect(
      screen.getByRole("button", { name: "All" }).getAttribute("aria-pressed"),
    ).toBe("true");
  });
});

describe("EncryptionTransitions after leaving or switching", () => {
  it("stops a batch after the current enrollment when the page unmounts", async () => {
    mocks.list.mockResolvedValue(aList([aRow("a"), aRow("b"), aRow("c")]));
    let finishFirst: () => void = () => {};
    mocks.enroll.mockImplementation((organizationID: string) =>
      organizationID === "a"
        ? new Promise((resolve) => {
            finishFirst = () => resolve({});
          })
        : Promise.resolve({}),
    );
    const { unmount } = await mount();

    expect(await screen.findByText("Org a")).toBeTruthy();
    for (const id of ["a", "b", "c"]) {
      fireEvent.click(
        screen.getByRole("checkbox", { name: `Select Org ${id}` }),
      );
    }
    fireEvent.click(
      screen.getByRole("button", { name: "Enroll selected (3)" }),
    );
    const dialog = await screen.findByRole("dialog");
    fireEvent.click(
      screen
        .getAllByRole("button", { name: "Enroll" })
        .find((button) => dialog.contains(button))!,
    );
    await waitFor(() => expect(mocks.enroll).toHaveBeenCalledTimes(1));

    unmount();
    finishFirst();
    await new Promise((resolve) => {
      setTimeout(resolve, ENROLL_INTERVAL_MS + 300);
    });
    expect(mocks.enroll).toHaveBeenCalledTimes(1);
  });

  it("drops a select-next response for a use case the admin has left", async () => {
    let answerClientSecrets: (
      list: AdminOrganizationEncryptionTransitionList,
    ) => void = () => {};
    mocks.list.mockImplementation(
      (request: { useCase?: string; limit?: number }) => {
        if (
          request.limit === 200 &&
          request.useCase === "oauth-client-secrets"
        ) {
          return new Promise((resolve) => {
            answerClientSecrets = resolve;
          });
        }
        return Promise.resolve({
          ...aList([aRow("a"), aRow("b")]),
          useCase: request.useCase ?? "oauth-client-secrets",
        });
      },
    );
    const { router } = await mount();

    expect(await screen.findByText("Org a")).toBeTruthy();
    fireEvent.change(screen.getByLabelText("Batch size"), {
      target: { value: "2" },
    });
    fireEvent.click(
      screen.getByRole("button", { name: "Select next in platform key" }),
    );

    await router.navigate({
      to: "/encryption-transitions",
      search: { useCase: "oauth-session-tokens" },
    });
    answerClientSecrets(aList([aRow("a"), aRow("b")]));
    await new Promise((resolve) => {
      setTimeout(resolve, 100);
    });

    expect(
      screen.getByRole("button", { name: "Enroll selected (0)" }),
      "the late client-secrets candidates are not selected for session tokens",
    ).toBeTruthy();
  });

  it("drops a select-next response for a values filter the admin has left", async () => {
    let answerAll: (
      list: AdminOrganizationEncryptionTransitionList,
    ) => void = () => {};
    mocks.list.mockImplementation(
      (request: { values?: string; limit?: number }) => {
        if (request.limit === 200 && request.values === "all") {
          return new Promise((resolve) => {
            answerAll = resolve;
          });
        }
        return Promise.resolve(aList([aRow("a"), aRow("b")]));
      },
    );
    const { router } = await mount();
    await router.navigate({
      to: "/encryption-transitions",
      search: { values: "all" },
    });

    expect(await screen.findByText("Org a")).toBeTruthy();
    fireEvent.change(screen.getByLabelText("Batch size"), {
      target: { value: "2" },
    });
    fireEvent.click(
      screen.getByRole("button", { name: "Select next in platform key" }),
    );
    fireEvent.click(screen.getByRole("button", { name: "With values" }));
    answerAll(aList([aRow("a"), aRow("b")]));
    await new Promise((resolve) => {
      setTimeout(resolve, 100);
    });

    expect(
      screen.getByRole("button", { name: "Enroll selected (0)" }),
      "candidates read for All are not selected under With values",
    ).toBeTruthy();
  });

  it("drops the values filter from the URL when switching back to with values", async () => {
    mocks.list.mockResolvedValue(aList([aRow("a")]));
    const { router } = await mount();
    await router.navigate({
      to: "/encryption-transitions",
      search: { values: "all" },
    });

    expect(await screen.findByText("Org a")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "With values" }));

    await waitFor(() =>
      expect(router.state.location.search).not.toHaveProperty("values"),
    );
    expect(
      screen
        .getByRole("button", { name: "With values" })
        .getAttribute("aria-pressed"),
    ).toBe("true");
  });
});
