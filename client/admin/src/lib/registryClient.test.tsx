import { fireEvent, screen, waitFor } from "@testing-library/react";
import { expect, it, vi } from "vitest";
import { renderWithApp } from "@/test/harness";
const save = vi.hoisted(() => vi.fn());
vi.mock("@gram/admin-client/react-query/adminSaveRegistryEntry", () => ({
  buildAdminSaveRegistryEntryMutation: () => ({
    mutationKey: ["registry-save"],
    mutationFn: save,
  }),
}));
import { useSaveRegistryEntryMutation } from "./gramAdminClient";
const dataJson =
  '{"server":{"name":"example.test/demo"},"_meta":{"n":9007199254740993}}';
const updatedAt = "2026-09-21T12:00:00.123456Z";
const id = "00000000-0000-4000-8000-000000000001";
function Probe() {
  const mutation = useSaveRegistryEntryMutation();
  return (
    <>
      <button
        onClick={() =>
          mutation.mutate({ request: { id, dataJson, updatedAt } })
        }
      >
        Save
      </button>
      <span>{mutation.status}</span>
    </>
  );
}
it("passes raw text and opaque token through the normal mutation layer", async () => {
  save.mockResolvedValue({
    id,
    dataJson,
    updatedAt,
    published: false,
    issues: [],
    createdAt: updatedAt,
  });
  await renderWithApp(<Probe />);
  expect(save).not.toHaveBeenCalled();
  fireEvent.click(screen.getByRole("button", { name: "Save" }));
  await waitFor(() => expect(screen.getByText("success")).toBeTruthy());
  expect(save.mock.calls[0]?.[0]).toEqual({
    request: { id, dataJson, updatedAt },
  });
});
