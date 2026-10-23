import { cleanup, fireEvent, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { queryOptions, useMutation } from "@tanstack/react-query";
import { renderWithApp } from "@/test/harness";
import { RegistryPromotionCandidates } from "./RegistryPromotionCandidates";
const api = vi.hoisted(() => ({ list: vi.fn(), retry: vi.fn() }));
vi.mock("@/lib/gramAdminClient", () => ({
  useRetryRegistryPromotionDocsMutation: () =>
    useMutation({ mutationFn: api.retry }),
  registryPromotionCandidatesQuery: (request: unknown) =>
    queryOptions({
      queryKey: ["candidates", request],
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
  expect(api.list).toHaveBeenCalledWith({ limit: 25, cursor: undefined });
  fireEvent.click(screen.getByRole("button", { name: "Next candidates" }));
  await waitFor(() =>
    expect(api.list).toHaveBeenCalledWith({ limit: 25, cursor: "page-2" }),
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
