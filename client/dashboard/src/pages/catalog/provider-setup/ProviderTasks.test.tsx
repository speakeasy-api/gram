import { useReducer } from "react";
import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";
import { initialState, reduce } from "./state";
import { SetupContext } from "./setup-context";
import { ProviderTask } from "./ProviderTasks";

vi.mock(
  "@gram/client/react-query/newRemoteSessionClientCallbackUrl.js",
  () => ({
    useNewRemoteSessionClientCallbackUrl: () => ({
      data: { callbackUrl: "https://current.example/callback" },
      isError: false,
    }),
  }),
);
afterEach(cleanup);
function SavedCredentials({ provider }: { provider: "slack" | "github" }) {
  const [s, dispatch] = useReducer(reduce, {
    ...initialState(provider),
    step: "credentials",
    saved: true,
    authorized: true,
    tested: true,
  });
  return (
    <SetupContext
      value={{
        s,
        dispatch,
        live: {
          pending: null,
          error: null,
          errorStep: "save",
          toolCount: null,
          loading: false,
          savedClientId: "saved-app-id",
        },
      }}
    >
      <ProviderTask serverName="Demo" />
    </SetupContext>
  );
}
describe("saved credentials", () => {
  it.each(["slack", "github"] as const)(
    "shows the full %s form disabled and enables replacement without revealing the secret",
    async (provider) => {
      const user = userEvent.setup();
      render(<SavedCredentials provider={provider} />);
      const clientId = screen.getByLabelText("Client ID") as HTMLInputElement;
      const secret = screen.getByLabelText("Client Secret") as HTMLInputElement;
      expect(clientId.disabled).toBe(true);
      expect(clientId.value).toBe("saved-app-id");
      expect(secret.disabled).toBe(true);
      expect(secret.type).toBe("password");
      expect(secret.value).toBe("••••••••");
      expect(screen.queryByRole("button", { name: "Show" })).toBeNull();
      expect(
        screen.getByText(`In ${provider === "github" ? "GitHub" : "Slack"}`),
      ).toBeDefined();
      await user.click(
        screen.getByRole("button", { name: "Update credentials" }),
      );
      expect(clientId.disabled).toBe(false);
      expect(clientId.value).toBe("saved-app-id");
      expect(secret.disabled).toBe(false);
      expect(secret.value).toBe("");
      expect(screen.getByRole("button", { name: "Show" })).toBeDefined();
    },
  );
});

it.each([false, true])(
  "uses the callback for the actual retained/replacement client (saved=%s)",
  (saved) => {
    const initial = initialState("slack");
    render(
      <SetupContext
        value={{
          s: {
            ...initial,
            step: "app",
            saved,
            values: { ...initial.values, appChoice: "existing" },
          },
          dispatch: vi.fn<() => void>(),
          live: {
            pending: null,
            error: null,
            errorStep: "save",
            toolCount: null,
            loading: false,
            callbackUrl: "https://previous.example/callback",
          },
        }}
      >
        <ProviderTask serverName="Slack" />
      </SetupContext>,
    );
    expect(screen.getByLabelText("Redirect URL").getAttribute("value")).toBe(
      saved
        ? "https://previous.example/callback"
        : "https://current.example/callback",
    );
  },
);

it.each(["new", "existing"])(
  "uses the real callback and org-owned GitHub app settings (%s)",
  (appChoice) => {
    const initial = initialState("github");
    render(
      <SetupContext
        value={{
          s: {
            ...initial,
            step: "app",
            values: { ...initial.values, org: "example-org", appChoice },
          },
          dispatch: vi.fn<() => void>(),
        }}
      >
        <ProviderTask serverName="GitHub" />
      </SetupContext>,
    );
    expect(screen.getByLabelText("Redirect URI").getAttribute("value")).toBe(
      "https://current.example/callback",
    );
    expect(
      screen
        .getByRole("link", { name: "Open GitHub settings" })
        .getAttribute("href"),
    ).toBe(
      "https://github.com/organizations/example-org/settings/applications",
    );
    expect(screen.getByText("Enable Device Flow")).toBeDefined();
    expect(screen.getByText(/off\./)).toBeDefined();
  },
);
