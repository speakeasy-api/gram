import { useState, type JSX } from "react";
import { cleanup, fireEvent, screen, waitFor } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { useMutation } from "@tanstack/react-query";
import { renderWithApp } from "@/test/harness";
import type { AdminRegistryPromotionCandidate } from "@gram/admin-client/models/components/adminregistrypromotioncandidate";
import { RegistryPromotionDocs } from "./RegistryPromotionDocs";
const api = vi.hoisted(() => ({ retry: vi.fn() }));
vi.mock("@/lib/gramAdminClient", () => ({
  useRetryRegistryPromotionDocsMutation: () =>
    useMutation({ mutationFn: api.retry, retry: false }),
}));
const candidate: AdminRegistryPromotionCandidate = {
  id: "00000000-0000-4000-8000-000000000001",
  organizationId: "org-example",
  projectId: "",
  mcpServerId: "",
  remoteMcpServerId: "",
  sourceRevision: "aabb",
  curationRevision: "00000000-0000-4000-8000-000000000004",
  createdAt: new Date("2026-01-02T12:00:00Z"),
  eligible: false,
  reason: "already_promoted",
  status: "promoted",
  observedSuccesses: 30,
  observedFailures: 0,
  observedDays: 3,
  docsRevision: "00000000-0000-4000-8000-000000000002",
  docsRetryAllowed: true,
  docsAttempts: 8,
  docsStatus: "needs_attention",
  deliveryError: "docs_attempts_exhausted",
};
afterEach(() => {
  cleanup();
  vi.resetAllMocks();
});
it("retries a retained publication using its exact organization, promotion and docs revision", async () => {
  api.retry.mockResolvedValue({
    ...candidate,
    docsStatus: "retrying",
    docsRetryAllowed: false,
    docsRevision: "00000000-0000-4000-8000-000000000003",
  });
  const updated = vi.fn<(candidate: AdminRegistryPromotionCandidate) => void>();
  await renderWithApp(
    <RegistryPromotionDocs candidate={candidate} onUpdated={updated} />,
  );
  fireEvent.click(screen.getByRole("button", { name: "Retry guide request" }));
  await screen.findByText("Guide request scheduled for another attempt.");
  expect(api.retry).toHaveBeenCalledTimes(1);
  expect(api.retry.mock.calls[0]?.[0]).toEqual({
    request: {
      organizationId: candidate.organizationId,
      promotionId: candidate.id,
      docsRevision: candidate.docsRevision,
      retryKey: expect.any(String),
    },
  });
  expect(updated).toHaveBeenCalledTimes(1);
  expect(
    (
      screen.getByRole("button", {
        name: "Retry scheduled",
      }) as HTMLButtonElement
    ).disabled,
  ).toBe(true);
});
it("does not offer retry during a provider delay or after the guide request was queued", async () => {
  await renderWithApp(
    <RegistryPromotionDocs
      candidate={{
        ...candidate,
        docsRetryAllowed: false,
        docsStatus: "retrying",
        docsNextAttemptAt: new Date("2026-10-22T12:00:00Z"),
      }}
    />,
  );
  expect(
    screen.queryByRole("button", { name: "Retry guide request" }),
  ).toBeNull();
  expect(screen.getByText(/Next attempt/)).toBeTruthy();
  cleanup();
  await renderWithApp(
    <RegistryPromotionDocs
      candidate={{
        ...candidate,
        docsRetryAllowed: false,
        docsStatus: "queued",
        docsUrl: "https://github.com/example/docs/issues/1",
        deliveryError: undefined,
      }}
    />,
  );
  expect(
    screen.queryByRole("button", { name: "Retry guide request" }),
  ).toBeNull();
  expect(screen.getByRole("link", { name: "View guide request" })).toBeTruthy();
  expect(
    screen.getByText(/publication happens after the guide is merged/),
  ).toBeTruthy();
});
it("keeps a retry key after an ambiguous response and masks upstream errors", async () => {
  api.retry.mockRejectedValue(
    new Error("private credentials in upstream failure"),
  );
  await renderWithApp(<RegistryPromotionDocs candidate={candidate} />);
  fireEvent.click(screen.getByRole("button", { name: "Retry guide request" }));
  await screen.findByRole("alert");
  expect(screen.queryByText(/private credentials/)).toBeNull();
  fireEvent.click(screen.getByRole("button", { name: "Retry guide request" }));
  await waitFor(() => expect(api.retry).toHaveBeenCalledTimes(2));
  expect(api.retry.mock.calls[1]?.[0]).toEqual(api.retry.mock.calls[0]?.[0]);
});

it("keeps the retry confirmation after adopting the rotated response revision", async () => {
  const retried = {
    ...candidate,
    docsStatus: "retrying",
    docsRetryAllowed: false,
    docsRevision: "00000000-0000-4000-8000-000000000003",
  };
  api.retry.mockResolvedValue(retried);
  function Receipt(): JSX.Element {
    const [current, setCurrent] = useState(candidate);
    return <RegistryPromotionDocs candidate={current} onUpdated={setCurrent} />;
  }
  await renderWithApp(<Receipt />);
  fireEvent.click(screen.getByRole("button", { name: "Retry guide request" }));
  await screen.findByText("Guide request scheduled for another attempt.");
  expect(
    screen.queryByRole("button", { name: "Retry guide request" }),
  ).toBeNull();
});
