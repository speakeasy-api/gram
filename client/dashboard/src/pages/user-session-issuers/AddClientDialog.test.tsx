import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
} from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { AddClientDialog } from "./AddClientDialog";
import type { OrganizationClients } from "./useOrganizationClients";

type Variables = {
  request: { verifyURLRequestBody: { clientIdMetadataUri: string } };
};

type CallOptions = { onSuccess?: (result: unknown) => void };

const verify = vi.hoisted(() => ({ mutate: vi.fn() }));

vi.mock(
  "@gram/client/react-query/verifyUserSessionIssuerCimdClientURL.js",
  () => ({
    useVerifyUserSessionIssuerCimdClientURLMutation: () => ({
      mutate: verify.mutate,
      isPending: false,
    }),
  }),
);

afterEach(() => {
  cleanup();
  verify.mutate.mockReset();
});

const clients: OrganizationClients = {
  items: [],
  isLoading: false,
  isError: false,
  retry: () => {},
  add: () => {},
  adding: false,
  addError: null,
  clearAddError: () => {},
  remove: () => {},
  removing: false,
};

function typeUrl(value: string): void {
  fireEvent.change(screen.getByLabelText("Document address"), {
    target: { value },
  });
}

function show(): void {
  render(
    <AddClientDialog open onOpenChange={() => {}} clients={clients} canCheck />,
  );
}

function check(): { variables: Variables; options: CallOptions } {
  fireEvent.click(screen.getByRole("button", { name: "Check" }));
  const call = verify.mutate.mock.calls.at(-1);
  return {
    variables: call?.[0] as Variables,
    options: call?.[1] as CallOptions,
  };
}

function addButton(): HTMLElement {
  return screen.getByRole("button", { name: "Add client" });
}

const a = "https://a.example.com/client.json";

it("allows adding the address once its check passes", () => {
  show();
  typeUrl(a);
  const { variables, options } = check();
  expect(variables.request.verifyURLRequestBody.clientIdMetadataUri).toBe(a);
  act(() => options.onSuccess?.({ verified: true }));
  expect(addButton().hasAttribute("disabled")).toBe(false);
});

it("keeps a check with the address it ran on, not one typed since", () => {
  show();
  typeUrl(a);
  const { options } = check();
  // The answer for A arrives after the field was changed to B.
  typeUrl("https://b.example.com/client.json");
  act(() => options.onSuccess?.({ verified: true }));
  expect(addButton().hasAttribute("disabled")).toBe(true);
});

it("ignores a check that finishes after the dialog was cancelled", () => {
  show();
  typeUrl(a);
  const { options } = check();
  fireEvent.click(screen.getByRole("button", { name: "Cancel" }));
  act(() => options.onSuccess?.({ verified: true }));
  typeUrl(a);
  expect(addButton().hasAttribute("disabled")).toBe(true);
});
