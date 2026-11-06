import { scopesFor } from "./provider-data";
import { connectionFixture } from "./connection-fixture";
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
        live: connectionFixture(dispatch, {
          pending: null,
          error: null,
          errorStep: "save",
          toolCount: null,
          loading: false,
          savedClientId: "saved-app-id",
        }),
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
    const dispatch = vi.fn<() => void>();
    render(
      <SetupContext
        value={{
          s: {
            ...initial,
            step: "app",
            saved,
            values: { ...initial.values, appChoice: "existing" },
          },
          dispatch,
          live: connectionFixture(dispatch, {
            pending: null,
            error: null,
            errorStep: "save",
            toolCount: null,
            loading: false,
            callbackUrl: "https://previous.example/callback",
          }),
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
    const dispatch = vi.fn<() => void>();
    render(
      <SetupContext
        value={{
          s: {
            ...initial,
            step: "app",
            values: { ...initial.values, org: "example-org", appChoice },
          },
          dispatch,
          live: connectionFixture(dispatch),
        }}
      >
        <ProviderTask serverName="GitHub" />
      </SetupContext>,
    );
    expect(
      screen.getByLabelText("Authorization callback URL").getAttribute("value"),
    ).toBe("https://current.example/callback");
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

it.each(["all", "read"])(
  "keeps scopes and callback consistent across Slack app instructions for %s access",
  (access) => {
    const initial = initialState("slack");
    const dispatch = vi.fn<() => void>();
    const expected = scopesFor({ values: { access } });
    const content = (appChoice: string) => (
      <SetupContext
        value={{
          s: {
            ...initial,
            step: "app",
            values: { ...initial.values, appChoice, access },
          },
          dispatch,
          live: connectionFixture(dispatch),
        }}
      >
        <ProviderTask serverName="Slack" />
      </SetupContext>
    );
    const view = render(content("new"));
    const manifest = JSON.parse(
      view.container.querySelector("pre")!.textContent!,
    );
    const href = screen
      .getByRole("link", { name: /Create Slack app/ })
      .getAttribute("href")!;
    expect(
      JSON.parse(new URL(href).searchParams.get("manifest_json")!),
    ).toEqual(manifest);
    expect(manifest.oauth_config.scopes.user).toEqual(expected);
    expect(manifest.oauth_config.redirect_urls).toEqual([
      "https://current.example/callback",
    ]);
    if (access === "read")
      expect(expected.some((scope) => scope.endsWith(":write"))).toBe(false);
    else expect(expected).toContain("chat:write");
    view.rerender(content("existing"));
    expect(
      [...view.container.querySelectorAll("code")].map((el) => el.textContent),
    ).toEqual(expected);
    expect(screen.getByLabelText("Redirect URL").getAttribute("value")).toBe(
      manifest.oauth_config.redirect_urls[0],
    );
  },
);

function SalesforceFixture({
  step,
  integration = "native",
  path = "own",
}: {
  step: string;
  integration?: string;
  path?: string;
}) {
  const [s, dispatch] = useReducer(reduce, {
    ...initialState("salesforce"),
    step,
    values: { ...initialState("salesforce").values, integration, path },
  });
  return (
    <SetupContext value={{ s, dispatch, live: connectionFixture(dispatch) }}>
      <ProviderTask serverName="Salesforce" />
    </SetupContext>
  );
}
it("shows both Salesforce connection choices and the hosted instance field", async () => {
  const user = userEvent.setup();
  render(<SalesforceFixture step="choices" />);
  await user.click(screen.getByText("Speakeasy-hosted Salesforce CRM"));
  expect(screen.getByLabelText("Salesforce instance URL")).toBeDefined();
  expect(screen.queryByLabelText("Salesforce server")).toBeNull();
});
it("uses the real Salesforce callback and native scopes", () => {
  render(<SalesforceFixture step="oauth" />);
  expect(screen.getByLabelText("Callback URL")).toHaveProperty(
    "value",
    "https://current.example/callback",
  );
  expect(screen.getByText("mcp_api")).toBeDefined();
  expect(screen.getByText("refresh_token")).toBeDefined();
});
it("shows Consumer Key-only native credentials and the optional hosted secret", () => {
  const native = render(<SalesforceFixture step="credentials" />);
  expect(screen.getByLabelText("Consumer Key")).toBeDefined();
  expect(screen.queryByLabelText("Client Secret (optional)")).toBeNull();
  native.unmount();
  render(<SalesforceFixture step="credentials" integration="hosted" />);
  expect(screen.getByLabelText("Client Secret (optional)")).toBeDefined();
});
it("retains the unresolved support branch with a real correction action and no simulated success", () => {
  render(<SalesforceFixture step="support" path="support" />);
  expect(
    screen.getByRole("button", { name: "Choose another setup method" }),
  ).toBeDefined();
  expect(screen.queryByText(/Simulate/)).toBeNull();
});
