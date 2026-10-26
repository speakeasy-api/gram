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
  curationRevision: "curation-1",
  eligible: true,
  canOverrideEligibility: false,
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
  fireEvent.click(
    screen.getByRole("combobox", { name: "Public authentication" }),
  );
  fireEvent.click(
    await screen.findByRole("option", { name: "No authentication" }),
  );
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
      curationRevision: "curation-1",
      publicMetadataConfirmed: true,
      overrideEligibility: false,
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

it("prefills suggestions without confirming publication or marking the draft dirty", async () => {
  api.candidate.mockResolvedValue({
    ...candidate,
    suggestedMetadata: { ...metadata, authMode: "oauth" },
  });
  const close = vi.fn<() => void>();
  vi.stubGlobal("confirm", vi.fn().mockReturnValue(false));
  await renderWithApp(
    <RegistryPromotionSheet target={target} onClose={close} />,
  );
  const title = await screen.findByRole("textbox", { name: "Public title" });
  expect((title as HTMLInputElement).value).toBe(metadata.publicTitle);
  expect(
    screen.getByRole("combobox", { name: "Public authentication" }).textContent,
  ).toContain("OAuth");
  expect(
    (
      screen.getByRole("button", {
        name: "Publish catalog entry",
      }) as HTMLButtonElement
    ).disabled,
  ).toBe(true);
  fireEvent.click(screen.getAllByRole("button", { name: "Close" })[0]!);
  expect(close).toHaveBeenCalledOnce();
  expect(window.confirm).not.toHaveBeenCalled();
});

it("keeps edited suggestions when refreshing eligibility after a conflict", async () => {
  api.candidate.mockResolvedValue({
    ...candidate,
    suggestedMetadata: { ...metadata, authMode: "none" },
  });
  api.promote.mockRejectedValue({ statusCode: 409 });
  await renderWithApp(
    <RegistryPromotionSheet target={target} onClose={vi.fn<() => void>()} />,
  );
  await screen.findByRole("textbox", { name: "Public title" });
  fireEvent.change(screen.getByRole("textbox", { name: "Public title" }), {
    target: { value: "Reviewed title" },
  });
  fireEvent.click(screen.getByRole("checkbox"));
  fireEvent.click(
    screen.getByRole("button", { name: "Publish catalog entry" }),
  );
  await screen.findByRole("button", { name: "Refresh eligibility" });
  api.candidate.mockResolvedValue({
    ...candidate,
    sourceRevision: "revision-2",
    suggestedMetadata: { ...metadata, publicTitle: "New source suggestion" },
  });
  fireEvent.click(screen.getByRole("button", { name: "Refresh eligibility" }));
  await waitFor(() =>
    expect(
      screen.queryByRole("button", { name: "Refresh eligibility" }),
    ).toBeNull(),
  );
  expect(
    (screen.getByRole("textbox", { name: "Public title" }) as HTMLInputElement)
      .value,
  ).toBe("Reviewed title");
  expect(
    (
      screen.getByRole("button", {
        name: "Publish catalog entry",
      }) as HTMLButtonElement
    ).disabled,
  ).toBe(true);
});

it.each(["rejected", "archived"])(
  "blocks direct publication review of a %s candidate even with healthy evidence",
  async (status) => {
    api.candidate.mockResolvedValue({ ...candidate, status, eligible: true });
    await renderWithApp(
      <RegistryPromotionSheet target={target} onClose={vi.fn<() => void>()} />,
    );
    await screen.findByText(/Restore it from the promotion queue/);
    expect(
      screen.queryByRole("button", { name: "Publish catalog entry" }),
    ).toBeNull();
  },
);

it("requires staff to choose authentication when the source has no known auth mode", async () => {
  api.candidate.mockResolvedValue({
    ...candidate,
    suggestedMetadata: metadata,
  });
  await renderWithApp(
    <RegistryPromotionSheet target={target} onClose={vi.fn<() => void>()} />,
  );
  await screen.findByRole("textbox", { name: "Public title" });
  expect(
    screen.getByRole("combobox", { name: "Public authentication" }).textContent,
  ).toContain("Select authentication");
  fireEvent.click(screen.getByRole("checkbox"));
  expect(
    (
      screen.getByRole("button", {
        name: "Publish catalog entry",
      }) as HTMLButtonElement
    ).disabled,
  ).toBe(true);
  fireEvent.click(
    screen.getByRole("combobox", { name: "Public authentication" }),
  );
  fireEvent.click(
    await screen.findByRole("option", { name: "No authentication" }),
  );
  // Choosing auth changes reviewed metadata and requires a fresh confirmation.
  expect(
    (
      screen.getByRole("button", {
        name: "Publish catalog entry",
      }) as HTMLButtonElement
    ).disabled,
  ).toBe(true);
  fireEvent.click(screen.getByRole("checkbox"));
  expect(
    (
      screen.getByRole("button", {
        name: "Publish catalog entry",
      }) as HTMLButtonElement
    ).disabled,
  ).toBe(false);
});

it("requires a separate override and public review before promoting despite failed health", async () => {
  api.candidate.mockResolvedValue({
    ...candidate,
    eligible: false,
    reason: "errors_observed",
    canOverrideEligibility: true,
    suggestedMetadata: { ...metadata, authMode: "none" },
  });
  await renderWithApp(
    <RegistryPromotionSheet target={target} onClose={vi.fn<() => void>()} />,
  );
  await screen.findByText("Recent failures need investigation");
  expect(screen.queryByRole("button", { name: "Promote anyway" })).toBeNull();
  fireEvent.click(
    screen.getByRole("checkbox", {
      name: "Promote anyway despite the health criteria",
    }),
  );
  const publish = screen.getByRole("button", {
    name: "Promote anyway",
  }) as HTMLButtonElement;
  expect(publish.disabled).toBe(true);
  fireEvent.click(screen.getByRole("checkbox", { name: /I verified/ }));
  expect(publish.disabled).toBe(false);
  fireEvent.click(publish);
  await screen.findByText("Catalog entry published");
  expect(api.promote.mock.calls[0]?.[0].request.overrideEligibility).toBe(true);
});
it.each([
  "unsupported_configuration",
  "disabled",
  "already_catalogued",
  "source_changed",
])("does not offer a health override for %s", async (reason) => {
  api.candidate.mockResolvedValue({ ...candidate, eligible: false, reason });
  await renderWithApp(
    <RegistryPromotionSheet target={target} onClose={vi.fn<() => void>()} />,
  );
  await screen.findByText("30 successful calls", { exact: false });
  expect(screen.queryByRole("checkbox", { name: /Promote anyway/ })).toBeNull();
});

it("uses the server override capability for unfamiliar health reasons", async () => {
  api.candidate.mockResolvedValue({
    ...candidate,
    eligible: false,
    reason: "new_health_reason",
    canOverrideEligibility: true,
  });
  await renderWithApp(
    <RegistryPromotionSheet target={target} onClose={vi.fn<() => void>()} />,
  );
  expect(
    await screen.findByRole("checkbox", {
      name: "Promote anyway despite the health criteria",
    }),
  ).toBeTruthy();
});

it.each([
  ["publicTitle", "Public title", true],
  ["publicTitle", "Public title", false],
  ["publicDescription", "Public description", true],
  ["publicDescription", "Public description", false],
] as const)(
  "validates %s (%s, eligible=%s) without truncating suggestions",
  async (field, label, eligible) => {
    const value = "a".repeat(126);
    api.candidate.mockResolvedValue({
      ...candidate,
      eligible,
      reason: eligible ? "eligible" : "errors_observed",
      canOverrideEligibility: !eligible,
      suggestedMetadata: { ...metadata, authMode: "none", [field]: value },
    });
    await renderWithApp(
      <RegistryPromotionSheet target={target} onClose={vi.fn<() => void>()} />,
    );
    if (!eligible) {
      fireEvent.click(
        await screen.findByRole("checkbox", { name: /Promote anyway/ }),
      );
    }
    const input = (await screen.findByRole("textbox", {
      name: label,
    })) as HTMLInputElement;
    expect(input.value).toBe(value);
    const alert = screen.getByRole("alert");
    const errorText = `${label} must be 100 characters or fewer.`;
    expect(alert.textContent).toBe(errorText);
    expect(
      document
        .getElementById(input.getAttribute("aria-describedby")!)
        ?.contains(alert),
    ).toBe(true);
    fireEvent.click(screen.getByRole("checkbox", { name: /I verified/ }));
    const publish = screen.getByRole("button", {
      name: eligible ? "Publish catalog entry" : "Promote anyway",
    }) as HTMLButtonElement;
    expect(publish.disabled).toBe(true);
    fireEvent.submit(input.closest("form")!);
    expect(api.promote).not.toHaveBeenCalled();
    for (const oversized of ["a".repeat(101), "🌍".repeat(101)]) {
      fireEvent.change(input, { target: { value: oversized } });
      expect(input.value).toBe(oversized);
      expect(input.getAttribute("aria-invalid")).toBe("true");
      expect(publish.disabled).toBe(true);
      expect(screen.getByRole("alert")).toBe(alert);
      expect(alert.textContent).toBe(errorText);
      expect(screen.getByText("101 / 100 characters.")).toBeTruthy();
      fireEvent.submit(input.closest("form")!);
      expect(api.promote).not.toHaveBeenCalled();
    }
    for (const valid of ["a".repeat(100), "🌍".repeat(100)]) {
      fireEvent.change(input, { target: { value: valid } });
      expect(input.value).toBe(valid);
      expect(input.getAttribute("aria-invalid")).toBe("false");
      expect(screen.queryByRole("alert")).toBeNull();
      expect(screen.getByText("100 / 100 characters.")).toBeTruthy();
    }
    expect(publish.disabled).toBe(true);
    fireEvent.submit(input.closest("form")!);
    expect(api.promote).not.toHaveBeenCalled();
    fireEvent.click(screen.getByRole("checkbox", { name: /I verified/ }));
    expect(publish.disabled).toBe(false);
    fireEvent.click(publish);
    await screen.findByText("Catalog entry published");
    expect(api.promote.mock.calls[0]?.[0].request[field]).toBe(
      "🌍".repeat(100),
    );
    expect(api.promote.mock.calls[0]?.[0].request.overrideEligibility).toBe(
      !eligible,
    );
  },
);

it("retains reviewed fields and confirmation on an unknown validation failure without echoing its payload", async () => {
  api.promote.mockRejectedValueOnce({
    statusCode: 400,
    message: "private backend detail",
    body: { publicTitle: "untrusted response text" },
  });
  await renderWithApp(
    <RegistryPromotionSheet target={target} onClose={vi.fn<() => void>()} />,
  );
  await fill();
  fireEvent.click(screen.getByRole("checkbox"));
  fireEvent.click(
    screen.getByRole("button", { name: "Publish catalog entry" }),
  );
  await screen.findByRole("alert");
  expect(
    screen.queryByText(/private backend detail|untrusted response text/),
  ).toBeNull();
  expect(
    (screen.getByRole("textbox", { name: "Public title" }) as HTMLInputElement)
      .value,
  ).toBe(metadata.publicTitle);
  expect(
    (
      screen.getByRole("textbox", {
        name: "Public description",
      }) as HTMLTextAreaElement
    ).value,
  ).toBe(metadata.publicDescription);
  expect(screen.getByRole("checkbox").getAttribute("aria-checked")).toBe(
    "true",
  );
  const retry = screen.getByRole("button", {
    name: "Publish catalog entry",
  }) as HTMLButtonElement;
  expect(retry.disabled).toBe(false);
  fireEvent.click(retry);
  await screen.findByText("Catalog entry published");
  expect(api.promote.mock.calls[1]?.[0].request).toEqual(
    api.promote.mock.calls[0]?.[0].request,
  );
});
