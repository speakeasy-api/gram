import { cleanup, fireEvent, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { queryOptions, useMutation } from "@tanstack/react-query";
import { renderWithApp } from "@/test/harness";
import { RegistryPromotionSheet } from "./RegistryPromotionSheet";

const api = vi.hoisted(() => ({
  candidate: vi.fn(),
  promote: vi.fn(),
  retry: vi.fn(),
}));
vi.mock("@/lib/gramAdminClient", () => ({
  useRetryRegistryPromotionDocsMutation: () =>
    useMutation({ mutationFn: api.retry }),
  registryPromotionCandidateQuery: (target: unknown) =>
    queryOptions({
      queryKey: ["promotion", target],
      queryFn: () => api.candidate(target),
      retry: false,
    }),
  usePromoteRegistryCandidateMutation: () =>
    useMutation({ mutationFn: api.promote, retry: false }),
}));
const target = {
  organizationId: "00000000-0000-4000-8000-000000000001",
  projectId: "00000000-0000-4000-8000-000000000002",
  mcpServerId: "00000000-0000-4000-8000-000000000003",
};
const candidate = {
  ...target,
  id: "candidate",
  remoteMcpServerId: "remote",
  sourceRevision: "revision-1",
  eligible: true,
  reason: "eligible",
  status: "candidate",
  observedSuccesses: 30,
  observedFailures: 0,
  observedDays: 4,
};
const metadata = {
  publicName: "com.example/server",
  publicTitle: "Example server",
  publicUrl: "https://mcp.example.com/mcp",
  documentationUrl: "https://docs.example.com/mcp",
  docsSlug: "example-mcp",
  publicDescription: "A public example MCP server.",
};
beforeEach(() => {
  api.candidate.mockResolvedValue(candidate);
  api.promote.mockResolvedValue({
    ...candidate,
    status: "promoted",
    registryEntryId: "entry",
    docsStatus: "pending",
  });
});
afterEach(() => {
  cleanup();
  vi.resetAllMocks();
  vi.unstubAllGlobals();
});
async function fill(): Promise<void> {
  await screen.findByText("Ready for staff review");
  for (const [name, value] of [
    ["Public catalog name", metadata.publicName],
    ["Public title", metadata.publicTitle],
    ["Public MCP endpoint", metadata.publicUrl],
    ["Official documentation URL", metadata.documentationUrl],
    ["Setup documentation slug", metadata.docsSlug],
    ["Public description", metadata.publicDescription],
  ])
    fireEvent.change(screen.getByRole("textbox", { name }), {
      target: { value },
    });
}
it("publishes only explicitly reviewed public metadata against the canonical target", async () => {
  await renderWithApp(
    <RegistryPromotionSheet target={target} onClose={vi.fn<() => void>()} />,
  );
  await fill();
  const button = screen.getByRole("button", {
    name: "Publish catalog entry",
  }) as HTMLButtonElement;
  expect(button.disabled).toBe(true);
  fireEvent.click(screen.getByRole("checkbox"));
  expect(button.disabled).toBe(false);
  fireEvent.click(button);
  await screen.findByText("Catalog entry published");
  expect(api.promote).toHaveBeenCalledTimes(1);
  expect(api.promote.mock.calls[0]?.[0]).toEqual({
    request: {
      ...target,
      ...metadata,
      authMode: "none",
      sourceRevision: "revision-1",
      publicMetadataConfirmed: true,
      retryKey: expect.any(String),
    },
  });
  expect(screen.getByText("Pending")).toBeTruthy();
});
it("does not offer publishing when evidence is insufficient", async () => {
  api.candidate.mockResolvedValue({
    ...candidate,
    eligible: false,
    reason: "logging_disabled",
  });
  await renderWithApp(
    <RegistryPromotionSheet target={target} onClose={vi.fn<() => void>()} />,
  );
  await screen.findByText("Tool call logging is disabled");
  expect(
    screen.queryByRole("button", { name: "Publish catalog entry" }),
  ).toBeNull();
  expect(api.promote).not.toHaveBeenCalled();
});
it("clears publication confirmation when reviewed fields change", async () => {
  await renderWithApp(
    <RegistryPromotionSheet target={target} onClose={vi.fn<() => void>()} />,
  );
  await fill();
  fireEvent.click(screen.getByRole("checkbox"));
  fireEvent.change(screen.getByRole("textbox", { name: "Public title" }), {
    target: { value: "Revised title" },
  });
  expect(
    (
      screen.getByRole("button", {
        name: "Publish catalog entry",
      }) as HTMLButtonElement
    ).disabled,
  ).toBe(true);
});
it("retains draft after a source conflict and requires fresh review", async () => {
  api.promote.mockRejectedValue({ statusCode: 409 });
  await renderWithApp(
    <RegistryPromotionSheet target={target} onClose={vi.fn<() => void>()} />,
  );
  await fill();
  fireEvent.click(screen.getByRole("checkbox"));
  fireEvent.click(
    screen.getByRole("button", { name: "Publish catalog entry" }),
  );
  await screen.findByRole("button", { name: "Refresh eligibility" });
  expect(
    (screen.getByRole("textbox", { name: "Public title" }) as HTMLInputElement)
      .value,
  ).toBe(metadata.publicTitle);
  expect(
    (
      screen.getByRole("button", {
        name: "Publish catalog entry",
      }) as HTMLButtonElement
    ).disabled,
  ).toBe(true);
  api.candidate.mockResolvedValue({
    ...candidate,
    sourceRevision: "revision-2",
  });
  fireEvent.click(screen.getByRole("button", { name: "Refresh eligibility" }));
  await waitFor(() =>
    expect(
      screen.queryByRole("button", { name: "Refresh eligibility" }),
    ).toBeNull(),
  );
  expect(
    (
      screen.getByRole("button", {
        name: "Publish catalog entry",
      }) as HTMLButtonElement
    ).disabled,
  ).toBe(true);
});

it("limits a public title to the server's 200-character maximum", async () => {
  await renderWithApp(
    <RegistryPromotionSheet target={target} onClose={vi.fn<() => void>()} />,
  );
  await screen.findByText("Ready for staff review");
  expect(
    (screen.getByRole("textbox", { name: "Public title" }) as HTMLInputElement)
      .maxLength,
  ).toBe(200);
});
it("confirms closing a review when only authentication was changed", async () => {
  const close = vi.fn<() => void>();
  await renderWithApp(
    <RegistryPromotionSheet target={target} onClose={close} />,
  );
  await screen.findByText("Ready for staff review");
  fireEvent.click(
    screen.getByRole("combobox", { name: "Public authentication" }),
  );
  fireEvent.click(await screen.findByRole("option", { name: "OAuth" }));
  vi.stubGlobal("confirm", vi.fn().mockReturnValue(false));
  fireEvent.click(screen.getAllByRole("button", { name: "Close" })[0]!);
  expect(window.confirm).toHaveBeenCalledWith("Discard this promotion review?");
  expect(close).not.toHaveBeenCalled();
});
