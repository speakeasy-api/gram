import type * as ReactQuery from "@tanstack/react-query";
import type { ReactNode } from "react";
import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const testState = vi.hoisted(() => ({
  data: {
    id: "config-id",
    organizationId: "org-id",
    enabled: true,
    endpointUrl: "https://collector.example.com",
    headers: [] as Array<{ name: string; hasValue: boolean }>,
  },
  upsert: vi.fn(),
  deleteConfig: vi.fn(),
}));

vi.mock("@/components/require-scope", () => ({
  RequireScope: ({ children }: { children: ReactNode }) => children,
}));

vi.mock("@gram/client/react-query/otelForwardingConfig", () => ({
  invalidateAllOtelForwardingConfig: vi.fn(),
  useOtelForwardingConfig: () => ({
    data: testState.data,
    isLoading: false,
  }),
}));

vi.mock("@gram/client/react-query/upsertOtelForwardingConfig", () => ({
  useUpsertOtelForwardingConfigMutation: () => ({
    mutateAsync: testState.upsert,
    isPending: false,
  }),
}));

vi.mock("@gram/client/react-query/deleteOtelForwardingConfig", () => ({
  useDeleteOtelForwardingConfigMutation: () => ({
    mutateAsync: testState.deleteConfig,
    isPending: false,
  }),
}));

vi.mock("@tanstack/react-query", async (importOriginal) => ({
  ...(await importOriginal<typeof ReactQuery>()),
  useQueryClient: () => ({}),
}));

import { OtelForwardingSection } from "./OtelForwardingSection";

beforeEach(() => {
  testState.data = {
    id: "config-id",
    organizationId: "org-id",
    enabled: true,
    endpointUrl: "https://collector.example.com",
    headers: [],
  };
  testState.upsert.mockReset();
  testState.deleteConfig.mockReset();
  testState.upsert.mockImplementation(async () => testState.data);
  testState.deleteConfig.mockResolvedValue(undefined);
});

afterEach(cleanup);

describe("OtelForwardingSection", () => {
  it("preserves unsaved headers across a background config refresh", () => {
    const { rerender } = render(<OtelForwardingSection />);

    fireEvent.click(screen.getByRole("button", { name: "Add header" }));
    fireEvent.change(screen.getAllByPlaceholderText("Header name")[0]!, {
      target: { value: "Authorization" },
    });
    fireEvent.change(screen.getAllByPlaceholderText("Header value")[0]!, {
      target: { value: "Bearer first" },
    });

    fireEvent.click(screen.getByRole("button", { name: "Add header" }));
    fireEvent.change(screen.getAllByPlaceholderText("Header name")[1]!, {
      target: { value: "X-Tenant" },
    });
    fireEvent.change(screen.getAllByPlaceholderText("Header value")[1]!, {
      target: { value: "second" },
    });

    testState.data = { ...testState.data, headers: [] };
    rerender(<OtelForwardingSection />);

    expect(screen.getAllByPlaceholderText("Header name")).toHaveLength(2);
    expect(
      (screen.getAllByPlaceholderText("Header name")[0] as HTMLInputElement)
        .value,
    ).toBe("Authorization");
    expect(
      (screen.getAllByPlaceholderText("Header value")[0] as HTMLInputElement)
        .value,
    ).toBe("Bearer first");
    expect(
      (screen.getAllByPlaceholderText("Header name")[1] as HTMLInputElement)
        .value,
    ).toBe("X-Tenant");
    expect(
      (screen.getAllByPlaceholderText("Header value")[1] as HTMLInputElement)
        .value,
    ).toBe("second");
  });

  it("shows four bullets for a stored header value", () => {
    testState.data.headers = [{ name: "Authorization", hasValue: true }];

    render(<OtelForwardingSection />);

    const valueInput = screen.getByPlaceholderText("••••") as HTMLInputElement;
    expect(valueInput.value).toBe("");
  });

  it("uses native form submission and explicit button types", () => {
    const { container } = render(<OtelForwardingSection />);

    const formElement = container.querySelector("form");
    expect(formElement).not.toBeNull();
    expect(
      (screen.getByRole("button", { name: "Save" }) as HTMLButtonElement).type,
    ).toBe("submit");
    expect(
      (screen.getByRole("button", { name: "Add header" }) as HTMLButtonElement)
        .type,
    ).toBe("button");
    expect(
      (screen.getByRole("button", { name: "Delete" }) as HTMLButtonElement)
        .type,
    ).toBe("button");
    expect((screen.getByRole("switch") as HTMLButtonElement).type).toBe(
      "button",
    );
    expect(
      (screen.getByLabelText("Endpoint URL") as HTMLInputElement).value,
    ).toBe(testState.data.endpointUrl);

    fireEvent.click(screen.getByRole("button", { name: "Add header" }));
    expect(
      (
        screen.getByRole("button", {
          name: "Remove header row",
        }) as HTMLButtonElement
      ).type,
    ).toBe("button");
  });

  it("preserves a stored header while adding and saving another", async () => {
    testState.data.headers = [{ name: "foo", hasValue: true }];
    const upsert = Promise.withResolvers<typeof testState.data>();
    testState.upsert.mockReturnValue(upsert.promise);
    render(<OtelForwardingSection />);

    fireEvent.click(screen.getByRole("button", { name: "Add header" }));
    fireEvent.change(screen.getAllByPlaceholderText("Header name")[1]!, {
      target: { value: "another" },
    });
    fireEvent.change(screen.getByPlaceholderText("Header value"), {
      target: { value: "happy days" },
    });

    const saveButton = screen.getByRole("button", {
      name: "Save",
    }) as HTMLButtonElement;
    await waitFor(() => expect(saveButton.disabled).toBe(false));
    fireEvent.click(saveButton);

    await waitFor(() =>
      expect(testState.upsert).toHaveBeenCalledWith({
        request: {
          upsertConfigRequestBody3: {
            endpointUrl: testState.data.endpointUrl,
            enabled: testState.data.enabled,
            headers: [
              { name: "foo" },
              { name: "another", value: "happy days" },
            ],
          },
        },
      }),
    );
    await waitFor(() => expect(saveButton.disabled).toBe(true));

    await act(async () => {
      upsert.resolve(testState.data);
    });
  });
});
