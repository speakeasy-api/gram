import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
} from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { TooltipProvider } from "@/components/ui/Tooltip";
import { RegisterClientSheet } from "./RegisterClientSheet";

const { createMutate, createHook, invalidateAllUserSessionClients } =
  vi.hoisted(() => ({
    createMutate: vi.fn(),
    createHook: {} as {
      options?: { onSuccess?: (data: unknown) => Promise<void> };
    },
    invalidateAllUserSessionClients: vi.fn(),
  }));

vi.mock("@gram/client/react-query/createUserSessionClient.js", () => ({
  useCreateUserSessionClientMutation: (options?: {
    onSuccess?: (data: unknown) => Promise<void>;
  }) => {
    createHook.options = options;
    return {
      mutate: (...args: unknown[]) => createMutate(...args),
      isPending: false,
      error: null,
      reset: () => {},
    };
  },
}));

vi.mock("@gram/client/react-query/userSessionClients.js", () => ({
  invalidateAllUserSessionClients: (...args: unknown[]) =>
    invalidateAllUserSessionClients(...args),
}));

vi.mock("@/contexts/Sdk", () => ({
  useProjectSlugForRequests: () => "project-1",
}));

function renderSheet() {
  const queryClient = new QueryClient();
  return render(
    <QueryClientProvider client={queryClient}>
      <TooltipProvider>
        <RegisterClientSheet issuerId="issuer-1" open onOpenChange={() => {}} />
      </TooltipProvider>
    </QueryClientProvider>,
  );
}

describe("RegisterClientSheet", () => {
  afterEach(() => {
    cleanup();
    vi.clearAllMocks();
  });

  it("waits for a name and a redirect URI before registering", () => {
    renderSheet();

    const submit = screen.getByRole("button", { name: "Register agent" });
    expect(submit).toHaveProperty("disabled", true);

    fireEvent.change(screen.getByLabelText("Name"), {
      target: { value: "Support triage agent" },
    });
    expect(submit).toHaveProperty("disabled", true);

    fireEvent.change(screen.getByLabelText("Redirect URI 1"), {
      target: { value: "https://agent.example.com/cb" },
    });
    expect(submit).toHaveProperty("disabled", false);
  });

  it("adds a redirect URI field without submitting the form", () => {
    renderSheet();

    fireEvent.change(screen.getByLabelText("Name"), {
      target: { value: "Support triage agent" },
    });
    fireEvent.change(screen.getByLabelText("Redirect URI 1"), {
      target: { value: "https://agent.example.com/cb" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Add redirect URI" }));
    fireEvent.click(
      screen.getByRole("button", { name: "Remove redirect URI 2" }),
    );

    expect(createMutate).not.toHaveBeenCalled();
    expect(screen.getByLabelText("Redirect URI 1")).toBeDefined();
  });

  it("registers with the chosen auth method and shows the secret once", async () => {
    renderSheet();

    fireEvent.change(screen.getByLabelText("Name"), {
      target: { value: "  Support triage agent  " },
    });
    fireEvent.change(screen.getByLabelText("Redirect URI 1"), {
      target: { value: "https://agent.example.com/cb" },
    });
    // A blank extra field is dropped rather than sent.
    fireEvent.click(screen.getByRole("button", { name: "Add redirect URI" }));
    fireEvent.click(screen.getByRole("radio", { name: /client_secret_post/ }));
    fireEvent.click(screen.getByRole("button", { name: "Register agent" }));

    expect(createMutate).toHaveBeenCalledWith({
      request: {
        gramProject: "project-1",
        createUserSessionClientRequestBody: {
          userSessionIssuerId: "issuer-1",
          clientName: "Support triage agent",
          redirectUris: ["https://agent.example.com/cb"],
          tokenEndpointAuthMethod: "client_secret_post",
        },
      },
    });

    await act(async () => {
      await createHook.options?.onSuccess?.({
        client: {
          id: "client-1",
          clientId: "client_7c2e",
          clientName: "Support triage agent",
          redirectUris: ["https://agent.example.com/cb"],
          tokenEndpointAuthMethod: "client_secret_post",
        },
        clientSecret: "minted-secret",
      });
    });

    expect(
      screen.getByText("Support triage agent is registered"),
    ).toBeDefined();
    expect(
      screen.getByText("Save the secret now. It will only be shown once."),
    ).toBeDefined();
    expect(screen.getByText("client_7c2e")).toBeDefined();
    expect(screen.getByText("minted-secret")).toBeDefined();
    expect(invalidateAllUserSessionClients).toHaveBeenCalled();
  });
});
