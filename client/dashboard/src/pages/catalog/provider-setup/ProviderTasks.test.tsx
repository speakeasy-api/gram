import { scopesFor } from "./provider-data";
import { connectionFixture } from "./connection-fixture";
import { useReducer } from "react";
import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";
import { initialState, reduce, type Action } from "./state";
import { SetupContext } from "./setup-context";
import { ProviderTask } from "./ProviderTasks";

const callback = vi.hoisted(() => ({
  data: { callbackUrl: "https://current.example/callback" },
  isError: false,
  refetch: vi.fn(),
}));
vi.mock(
  "@gram/client/react-query/newRemoteSessionClientCallbackUrl.js",
  () => ({
    useNewRemoteSessionClientCallbackUrl: () => callback,
  }),
);
afterEach(() => {
  cleanup();
  callback.data.callbackUrl = "https://current.example/callback";
  callback.isError = false;
  callback.refetch.mockClear();
});
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
  salesforceServer = "sobject-reads",
  saved = false,
  savedTokenEndpointAuthMethod,
}: {
  step: string;
  integration?: string;
  path?: string;
  salesforceServer?: string;
  saved?: boolean;
  savedTokenEndpointAuthMethod?: string;
}) {
  const [s, dispatch] = useReducer(reduce, {
    ...initialState("salesforce"),
    step,
    saved,
    values: {
      ...initialState("salesforce").values,
      integration,
      path,
      salesforceServer,
    },
  });
  return (
    <SetupContext
      value={{
        s,
        dispatch,
        live: connectionFixture(dispatch, { savedTokenEndpointAuthMethod }),
      }}
    >
      <ProviderTask serverName="Salesforce" />
      <button onClick={() => dispatch({ type: "next" })}>Continue</button>
    </SetupContext>
  );
}
it("shows both Salesforce connection choices and the hosted instance field", async () => {
  const user = userEvent.setup();
  render(<SalesforceFixture step="choices" />);
  expect(
    screen.getByRole("radio", { name: "Salesforce native MCP" }),
  ).toBeDefined();
  expect(screen.getByLabelText("Salesforce server")).toBeDefined();
  await user.click(screen.getByText("Speakeasy-hosted Salesforce CRM"));
  expect(screen.getByLabelText("Salesforce instance URL")).toBeDefined();
  expect(screen.queryByLabelText("Salesforce server")).toBeNull();
});
it.each([
  ["none", "", "No secret required"],
  ["client_secret_post", "••••••••", "Paste Client Secret"],
  [undefined, "", "Secret status unavailable"],
])(
  "presents the saved hosted secret according to auth method %s",
  (method, value, placeholder) => {
    render(
      <SalesforceFixture
        step="credentials"
        integration="hosted"
        saved
        savedTokenEndpointAuthMethod={method}
      />,
    );
    const secret = screen.getByLabelText("Client Secret (optional)");
    expect(secret).toHaveProperty("disabled", true);
    expect(secret).toHaveProperty("value", value);
    expect(secret.getAttribute("placeholder")).toBe(placeholder);
  },
);
it.each([
  ["data360", "Data 360", /Data 360 license/, /View Data 360/],
  [
    "headless-360",
    "Headless 360 (Beta)",
    /67.0 or later/,
    /Salesforce beta service/,
  ],
  [
    "tableau-next",
    "Tableau Next",
    /Tableau Next guided or manual setup/,
    /access to the Tableau Next models/,
  ],
])(
  "enables %s and explains its prerequisites",
  async (id, title, requirement, permissions) => {
    const user = userEvent.setup();
    render(<SalesforceFixture step="choices" />);
    const option = screen.getByRole("option", { name: title });
    expect(option).toHaveProperty("disabled", false);
    await user.selectOptions(screen.getByLabelText("Salesforce server"), id);
    expect(screen.getByText(requirement)).toBeDefined();
    expect(screen.getByText(permissions)).toBeDefined();
  },
);
it.each([
  ["data360", "Data 360", /Data 360 license/],
  ["headless-360", "Headless 360 (Beta)", /67.0 or later/],
  ["tableau-next", "Tableau Next", /Tableau Next guided or manual setup/],
])("uses %s activation instructions", (id, title, requirement) => {
  render(<SalesforceFixture step="activation" salesforceServer={id} />);
  expect(screen.getByText(title)).toBeDefined();
  expect(screen.getByText(id)).toBeDefined();
  expect(screen.getByText(requirement)).toBeDefined();
  expect(
    screen.getByText(/only a successful connection test confirms access/),
  ).toBeDefined();
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
it.each([
  ["hosted", "sobject-reads", /Enter an HTTPS Salesforce instance URL/],
  ["native", "unsupported", /Choose a supported Salesforce server/],
])(
  "shows connection validation from credentials for %s and offers correction",
  async (integration, salesforceServer, message) => {
    const user = userEvent.setup();
    render(
      <SalesforceFixture
        step="credentials"
        integration={integration}
        salesforceServer={salesforceServer}
      />,
    );
    await user.type(screen.getByLabelText("Consumer Key"), "consumer-key");
    await user.click(screen.getByRole("button", { name: "Continue" }));
    expect(screen.getByText(message)).toBeDefined();
    await user.click(
      screen.getByRole("button", { name: "Review connection choices" }),
    );
    expect(
      screen.getByRole("radio", { name: "Salesforce native MCP" }),
    ).toBeDefined();
    expect(screen.queryByLabelText("Consumer Key")).toBeNull();
  },
);
it.each([
  ["salesforce", "oauth", "callback URL"],
  ["github", "app", "authorization callback URL"],
  ["slack", "app", "redirect URL"],
] as const)(
  "shows callback loading, failure and retry consistently for %s",
  async (provider, step, label) => {
    callback.data.callbackUrl = "";
    const user = userEvent.setup();
    const s = { ...initialState(provider), step };
    const dispatch = vi.fn<(action: Action) => void>();
    const task = (
      <SetupContext value={{ s, dispatch, live: connectionFixture(dispatch) }}>
        <ProviderTask serverName="Demo" />
      </SetupContext>
    );
    const view = render(task);
    expect(screen.getByText(`Loading ${label}…`).className).toContain(
      "text-muted-foreground",
    );
    callback.isError = true;
    view.rerender(
      <SetupContext value={{ s, dispatch, live: connectionFixture(dispatch) }}>
        <ProviderTask serverName="Demo" />
      </SetupContext>,
    );
    expect(screen.getByText(`Couldn’t load the ${label}.`)).toBeDefined();
    await user.click(screen.getByRole("button", { name: "Retry" }));
    expect(callback.refetch).toHaveBeenCalledOnce();
  },
);
it("retains the unresolved support branch with a real correction action and no simulated success", () => {
  render(<SalesforceFixture step="support" path="support" />);
  expect(
    screen.getByRole("button", { name: "Choose another setup method" }),
  ).toBeDefined();
  expect(screen.queryByText(/Simulate/)).toBeNull();
});
