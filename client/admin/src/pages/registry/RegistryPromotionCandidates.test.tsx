import { cleanup, fireEvent, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { queryOptions, useMutation } from "@tanstack/react-query";
import { renderWithApp } from "@/test/harness";
import { RegistryPromotionCandidates } from "./RegistryPromotionCandidates";
const api = vi.hoisted(() => ({
  list: vi.fn(),
  retry: vi.fn(),
  curate: vi.fn(),
}));
vi.mock("@/lib/gramAdminClient", () => ({
  useCurateRegistryPromotionCandidateMutation: () =>
    useMutation({ mutationFn: api.curate, retry: false }),
  useRetryRegistryPromotionDocsMutation: () =>
    useMutation({ mutationFn: api.retry }),
  registryPromotionCandidatesQuery: (request: unknown) =>
    queryOptions({
      queryKey: ["registryCandidates", request],
      queryFn: () => api.list(request),
      retry: false,
    }),
}));
vi.mock("./RegistryPromotionSheet", () => ({
  RegistryPromotionSheet: ({ target }: { target: { mcpServerId: string } }) => (
    <p>Reviewing {target.mcpServerId}</p>
  ),
}));
const candidate = {
  id: "candidate",
  curationRevision: "curation-1",
  organizationId: "org",
  projectId: "project",
  mcpServerId: "server",
  eligible: true,
  reason: "eligible",
  status: "candidate",
  observedSuccesses: 42,
  observedFailures: 0,
  observedDays: 3,
};
beforeEach(() => api.list.mockResolvedValue({ candidates: [candidate] }));
afterEach(() => {
  cleanup();
  vi.resetAllMocks();
});
it("opens a canonical target for current eligibility review", async () => {
  await renderWithApp(
    <RegistryPromotionCandidates onOpenEntry={vi.fn<(id: string) => void>()} />,
  );
  await screen.findByText("Ready for staff review");
  expect(
    screen
      .getByRole("link", { name: "View source · server" })
      .getAttribute("href"),
  ).toContain("project=project");
  fireEvent.click(screen.getByRole("button", { name: "Review" }));
  expect(screen.getByText("Reviewing server")).toBeTruthy();
});
it("shows guide request state and public title for a published entry", async () => {
  api.list.mockResolvedValue({
    candidates: [
      {
        ...candidate,
        status: "promoted",
        registryEntryId: "entry",
        docsStatus: "queued",
        publicTitle: "Example public MCP",
      },
    ],
  });
  const open = vi.fn<(id: string) => void>();
  await renderWithApp(<RegistryPromotionCandidates onOpenEntry={open} />);
  await screen.findByText("Guide requested");
  expect(screen.getByText("Example public MCP")).toBeTruthy();
  fireEvent.click(screen.getByRole("button", { name: "View entry" }));
  expect(open).toHaveBeenCalledWith("entry");
});
it("uses bounded cursor pagination", async () => {
  api.list.mockImplementation(({ cursor }) =>
    Promise.resolve({
      candidates: [candidate],
      nextCursor: cursor ? undefined : "page-2",
    }),
  );
  await renderWithApp(
    <RegistryPromotionCandidates onOpenEntry={vi.fn<(id: string) => void>()} />,
  );
  await screen.findByText("Ready for staff review");
  expect(api.list).toHaveBeenCalledWith({
    limit: 25,
    status: "active",
    cursor: undefined,
  });
  fireEvent.click(screen.getByRole("button", { name: "Next candidates" }));
  await waitFor(() =>
    expect(api.list).toHaveBeenCalledWith({
      limit: 25,
      status: "active",
      cursor: "page-2",
    }),
  );
});

it("preserves publication delivery history after the source is deleted", async () => {
  api.list.mockResolvedValue({
    candidates: [
      {
        ...candidate,
        projectId: "",
        mcpServerId: "",
        status: "promoted",
        docsStatus: "queued",
      },
    ],
  });
  await renderWithApp(
    <RegistryPromotionCandidates onOpenEntry={vi.fn<(id: string) => void>()} />,
  );
  await screen.findByText("Source no longer available");
  expect(screen.getByText("Guide requested")).toBeTruthy();
  expect(screen.queryByRole("button", { name: "Review" })).toBeNull();
  expect(screen.queryByRole("link")).toBeNull();
});

it("resets pagination when changing the server-side status filter", async () => {
  api.list.mockResolvedValue({ candidates: [candidate], nextCursor: "page-2" });
  await renderWithApp(
    <RegistryPromotionCandidates onOpenEntry={vi.fn<(id: string) => void>()} />,
  );
  await screen.findByText("Ready for staff review");
  fireEvent.click(screen.getByRole("button", { name: "Next candidates" }));
  await waitFor(() =>
    expect(api.list).toHaveBeenLastCalledWith({
      limit: 25,
      status: "active",
      cursor: "page-2",
    }),
  );
  fireEvent.click(screen.getByRole("combobox", { name: "Candidate status" }));
  fireEvent.click(await screen.findByRole("option", { name: "Archived" }));
  await waitFor(() =>
    expect(api.list).toHaveBeenLastCalledWith({
      limit: 25,
      status: "archived",
      cursor: undefined,
    }),
  );
  expect(
    (
      screen.getByRole("button", {
        name: "Previous candidates",
      }) as HTMLButtonElement
    ).disabled,
  ).toBe(true);
});

it("shows candidate creation time and newest-first ordering", async () => {
  api.list.mockResolvedValue({
    candidates: [{ ...candidate, createdAt: new Date("2026-01-02T12:00:00Z") }],
  });
  await renderWithApp(
    <RegistryPromotionCandidates onOpenEntry={vi.fn<(id: string) => void>()} />,
  );
  await screen.findByText("Ready for staff review");
  expect(screen.getByRole("columnheader", { name: "Created" })).toBeTruthy();
  expect(screen.getByText(/Jan 2/)).toBeTruthy();
  expect(screen.getByText("Newest first")).toBeTruthy();
});

it.each(["reject", "archive"] as const)(
  "%s uses the exact candidate revision and removes it from active results",
  async (action) => {
    api.curate.mockImplementation(async () => {
      api.list.mockResolvedValue({ candidates: [] });
      return {
        ...candidate,
        status: action === "reject" ? "rejected" : "archived",
        curationRevision: "curation-2",
      };
    });
    await renderWithApp(
      <RegistryPromotionCandidates
        onOpenEntry={vi.fn<(id: string) => void>()}
      />,
    );
    await screen.findByText("Ready for staff review");
    fireEvent.click(
      screen.getByRole("button", {
        name: action === "reject" ? "Reject" : "Archive",
      }),
    );
    await screen.findByText("No promotion candidates match this status.");
    expect(api.curate.mock.calls[0]?.[0]).toEqual({
      request: {
        organizationId: "org",
        promotionId: "candidate",
        curationRevision: "curation-1",
        action,
        retryKey: expect.any(String),
      },
    });
  },
);

it.each(["rejected", "archived"])(
  "requires restoring a %s candidate before review",
  async (status) => {
    api.list.mockResolvedValue({ candidates: [{ ...candidate, status }] });
    api.curate.mockImplementation(async () => {
      api.list.mockResolvedValue({
        candidates: [
          { ...candidate, curationRevision: "curation-2", eligible: false },
        ],
      });
      return { ...candidate, curationRevision: "curation-2", eligible: false };
    });
    await renderWithApp(
      <RegistryPromotionCandidates
        onOpenEntry={vi.fn<(id: string) => void>()}
      />,
    );
    const review = await screen.findByRole("button", { name: "Review" });
    expect((review as HTMLButtonElement).disabled).toBe(true);
    expect(screen.queryByRole("button", { name: "Reject" })).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "Restore" }));
    await waitFor(() =>
      expect(
        (screen.getByRole("button", { name: "Review" }) as HTMLButtonElement)
          .disabled,
      ).toBe(false),
    );
    expect(api.curate.mock.calls[0]?.[0].request.action).toBe("restore");
  },
);
