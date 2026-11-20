import { queryOptions } from "@tanstack/react-query";
import {
  cleanup,
  fireEvent,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { GcpIamCredential } from "@gram/admin-client/models/components/gcpiamcredential";

import { routeTree } from "@/routeTree.gen";
import { renderRouteTree } from "@/test/harness";

const mocks = vi.hoisted(() => ({
  getSession: vi.fn(),
  list: vi.fn(),
  get: vi.fn(),
  create: vi.fn(),
  verify: vi.fn(),
  remove: vi.fn(),
}));

vi.mock("@/lib/gramAdminApi", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/lib/gramAdminApi")>();
  return { ...actual, getSession: mocks.getSession };
});

vi.mock("./platformCredentialsClient", async (importOriginal) => {
  const actual =
    await importOriginal<typeof import("./platformCredentialsClient")>();
  return {
    ...actual,
    platformCredentialsListQuery: () =>
      queryOptions({
        queryKey: [
          "@gram/admin-client",
          "admin",
          "listPlatformExternalCredentials",
          {},
        ],
        queryFn: () => mocks.list(),
      }),
    gcpIamPlatformCredentialQuery: (id: string) =>
      queryOptions({
        queryKey: [
          "@gram/admin-client",
          "admin",
          "getGcpIamPlatformCredential",
          { id },
        ],
        queryFn: () => mocks.get(id),
      }),
    createGcpIamPlatformCredential: mocks.create,
    verifyGcpIamPlatformCredential: mocks.verify,
    deleteGcpIamPlatformCredential: mocks.remove,
  };
});

// Invented values only: this repository is public.
function aCredential(
  overrides: Partial<GcpIamCredential> = {},
): GcpIamCredential {
  return {
    id: "11111111-1111-4111-8111-111111111111",
    organizationId: "",
    provider: "gcp_iam",
    name: "Platform signer",
    impersonateServiceAccount: "signer@example-project.iam.gserviceaccount.com",
    createdAt: new Date("2026-01-15T00:00:00Z"),
    updatedAt: new Date("2026-02-01T00:00:00Z"),
    ...overrides,
  };
}

beforeEach(() => {
  for (const mock of Object.values(mocks)) mock.mockReset();
  mocks.getSession.mockResolvedValue({
    email: "ops@example.test",
    name: "Ops",
  });
  const credential = aCredential();
  mocks.list.mockResolvedValue({ credentials: [credential] });
  mocks.get.mockResolvedValue(credential);
});

afterEach(() => {
  cleanup();
});

describe("PlatformCredentialList", () => {
  it("lists credentials with a link to each", async () => {
    await renderRouteTree(routeTree, { initialPath: "/platform-credentials" });

    expect(await screen.findByText("Platform signer")).toBeTruthy();
    expect(screen.getByText("GCP IAM")).toBeTruthy();
    expect(
      screen
        .getByRole("link", { name: "View Platform signer" })
        .getAttribute("href"),
    ).toBe("/platform-credentials/11111111-1111-4111-8111-111111111111");
  });

  it("says so when there are none", async () => {
    mocks.list.mockResolvedValue({ credentials: [] });

    await renderRouteTree(routeTree, { initialPath: "/platform-credentials" });

    expect(await screen.findByText(/No platform credentials/)).toBeTruthy();
  });

  it("creates an impersonation credential from the dialog", async () => {
    mocks.create.mockResolvedValue(aCredential());
    await renderRouteTree(routeTree, { initialPath: "/platform-credentials" });

    fireEvent.click(
      await screen.findByRole("button", { name: "Create credential" }),
    );
    fireEvent.change(screen.getByLabelText("Name"), {
      target: { value: "New signer" },
    });
    fireEvent.change(screen.getByLabelText(/Service account to impersonate/), {
      target: { value: "new@example-project.iam.gserviceaccount.com" },
    });
    fireEvent.click(
      within(screen.getByRole("dialog")).getByRole("button", {
        name: "Create credential",
      }),
    );

    await waitFor(() =>
      expect(mocks.create).toHaveBeenCalledWith({
        name: "New signer",
        impersonateServiceAccount:
          "new@example-project.iam.gserviceaccount.com",
      }),
    );
  });
});

describe("PlatformCredentialDetail", () => {
  const path = "/platform-credentials/11111111-1111-4111-8111-111111111111";

  it("shows the configuration and the verify outcome", async () => {
    mocks.verify.mockResolvedValue({
      verified: true,
      principal: "signer@example-project.iam.gserviceaccount.com",
      identitySource: "impersonation",
    });
    await renderRouteTree(routeTree, { initialPath: path });

    expect(
      await screen.findByRole("heading", { name: "Platform signer" }),
    ).toBeTruthy();
    expect(screen.getByText("Service account impersonation")).toBeTruthy();

    fireEvent.click(screen.getByRole("button", { name: "Verify identity" }));

    const outcome = (await screen.findByText("Verified")).closest(
      "[role=status]",
    ) as HTMLElement;
    expect(
      within(outcome).getByText("Impersonated service account"),
    ).toBeTruthy();
    expect(mocks.verify).toHaveBeenCalledWith(
      "11111111-1111-4111-8111-111111111111",
    );
  });

  it("deletes only after confirmation", async () => {
    mocks.remove.mockResolvedValue(undefined);
    await renderRouteTree(routeTree, { initialPath: path });

    fireEvent.click(await screen.findByRole("button", { name: "Delete" }));
    expect(mocks.remove).not.toHaveBeenCalled();

    fireEvent.click(
      await screen.findByRole("button", { name: "Delete credential" }),
    );

    await waitFor(() =>
      expect(mocks.remove).toHaveBeenCalledWith(
        "11111111-1111-4111-8111-111111111111",
      ),
    );
  });

  it("shows why the server refused a delete", async () => {
    mocks.remove.mockRejectedValue(
      new Error("credential backs 1 managed signing keys; rotate them first"),
    );
    await renderRouteTree(routeTree, { initialPath: path });

    fireEvent.click(await screen.findByRole("button", { name: "Delete" }));
    fireEvent.click(
      await screen.findByRole("button", { name: "Delete credential" }),
    );

    expect(await screen.findByText(/rotate them first/)).toBeTruthy();
  });
});
