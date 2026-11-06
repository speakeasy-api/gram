import { scopesFor } from "./provider-data";
import { connectionFixture } from "./connection-fixture";
import { useReducer } from "react";
import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { initialState, reduce } from "./state";
import { SetupContext } from "./setup-context";
import { ProviderTask } from "./ProviderTasks";

const callback = vi.hoisted(() => ({
  value: "https://current.example/callback",
  failed: false,
}));
beforeEach(() => {
  callback.value = "https://current.example/callback";
  callback.failed = false;
});
vi.mock(
  "@gram/client/react-query/newRemoteSessionClientCallbackUrl.js",
  () => ({
    useNewRemoteSessionClientCallbackUrl: () => ({
      data: callback.value ? { callbackUrl: callback.value } : undefined,
      isError: callback.failed,
      refetch: vi.fn<() => void>(),
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
it("retains the unresolved support branch with a real correction action and no simulated success", () => {
  render(<SalesforceFixture step="support" path="support" />);
  expect(
    screen.getByRole("button", { name: "Choose another setup method" }),
  ).toBeDefined();
  expect(screen.queryByText(/Simulate/)).toBeNull();
});

describe("Snowflake instructions", () => {
  function task(step: string, values = {}, saved = false) {
    const initial = initialState("snowflake"),
      dispatch = vi.fn<() => void>();
    return render(
      <SetupContext
        value={{
          s: {
            ...initial,
            step,
            saved,
            values: { ...initial.values, ...values },
          },
          dispatch,
          live: connectionFixture(dispatch, {
            loading: false,
            callbackUrl: "https://retained.example/callback",
          }),
        }}
      >
        <ProviderTask serverName="Snowflake" />
      </SetupContext>,
    );
  }
  it("withholds incomplete SQL", () => {
    const { container } = task("create", {
      creatorRole: "EXAMPLE_CREATOR",
      database: "",
      schema: "",
      serverName: "",
    });
    expect(container.querySelector("[data-sql]")).toBeNull();
    expect(screen.queryByRole("button", { name: "Copy SQL" })).toBeNull();
  });

  it.each([false, true])(
    "shows callback recovery without an empty SQL instruction (failed=%s)",
    (failed) => {
      callback.value = "";
      callback.failed = failed;
      const { container } = task("oauth", {
        oauthIntegration: "EXAMPLE_OAUTH",
      });
      expect(container.querySelector('[data-sql="oauth"]')).toBeNull();
      expect(
        screen.queryByText("Create the integration in Snowflake"),
      ).toBeNull();
      expect(
        screen.getByText(
          failed ? /Couldn’t load the callback URL/ : /Loading callback URL/,
        ),
      ).toBeDefined();
    },
  );
  it.each([false, true])("uses the actual callback (saved=%s)", (saved) => {
    const { container } = task(
      "oauth",
      { oauthIntegration: "SPEAKEASY_OAUTH" },
      saved,
    );
    const text = container.querySelector('[data-sql="oauth"]')?.textContent;
    expect(text).toContain(
      saved
        ? "https://retained.example/callback"
        : "https://current.example/callback",
    );
    expect(text).toContain("OAUTH_CLIENT_TYPE = 'CONFIDENTIAL'");
  });
});
