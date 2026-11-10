import { scopesFor } from "./provider-data";
import { connectionFixture } from "./connection-fixture";
import { useReducer } from "react";
import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";
import {
  initialState,
  reduce,
  validate,
  type State,
  type Action,
} from "./state";
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
  salesforceDomain = "",
  saved = false,
  savedTokenEndpointAuthMethod,
}: {
  step: string;
  integration?: string;
  path?: string;
  salesforceServer?: string;
  salesforceDomain?: string;
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
      salesforceDomain,
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
it("offers the documented app methods, org types and seven native servers", async () => {
  const user = userEvent.setup();
  render(<SalesforceFixture step="choices" />);
  expect(screen.getByRole("radio", { name: "Production" })).toBeDefined();
  expect(screen.getByRole("radio", { name: "Sandbox" })).toBeDefined();
  expect(
    screen.getAllByRole("option").map((option) => option.textContent),
  ).toEqual([
    "SObject Reads",
    "SObject Mutations",
    "SObject Deletes",
    "SObject All",
    "Data 360",
    "Headless 360 (Beta)",
    "Tableau Next",
  ]);
  expect(
    screen.queryByRole("group", { name: "Salesforce connection" }),
  ).toBeNull();
  expect(screen.getByRole("group", { name: "Setup method" })).toBeDefined();
  expect(screen.getByRole("radio", { name: "Use your own app" })).toBeDefined();
  expect(
    screen.getByRole("radio", { name: "Use Speakeasy’s app" }),
  ).toBeDefined();
  expect(screen.queryByLabelText("Salesforce instance URL")).toBeNull();
  await user.selectOptions(
    screen.getByLabelText("Salesforce server"),
    "sobject-mutations",
  );
  await user.click(screen.getByText("Sandbox", { exact: true }));
  await user.click(screen.getByText("View selected endpoint"));
  expect(
    screen.getByText(
      "https://api.salesforce.com/platform/mcp/v1/sandbox/platform/sobject-mutations",
    ),
  ).toBeDefined();
});
it("reuses shared app registration controls for the own-app instructions", () => {
  render(<SalesforceFixture step="app" />);
  expect(screen.getByText("New External Client App")).toBeDefined();
  expect(screen.getByRole("radiogroup", { name: "OAuth app" })).toBeDefined();
  expect(screen.queryByText("Use Speakeasy’s app")).toBeNull();
});
it("validates My Domain before opening the app setup in that org", async () => {
  const user = userEvent.setup();
  render(<SalesforceFixture step="salesforce-org" />);
  const input = screen.getByLabelText("Salesforce Domain (optional)");
  await user.type(input, "example.salesforce.com.evil.test");
  await user.click(screen.getByRole("button", { name: "Continue" }));
  expect(
    screen.getByText(/Enter your production org’s Current My Domain URL/),
  ).toBeDefined();
  await user.clear(input);
  await user.type(input, "https://example.my.salesforce.com/");
  await user.click(screen.getByRole("button", { name: "Continue" }));
  expect(
    screen.getByRole("link", { name: "Open Salesforce" }).getAttribute("href"),
  ).toBe("https://example.my.salesforce.com");
});
it("corrects a pasted Developer Edition Setup URL in the domain field", async () => {
  const user = userEvent.setup();
  render(<SalesforceFixture step="salesforce-org" />);
  const input = screen.getByLabelText("Salesforce Domain (optional)");
  await user.click(input);
  await user.paste(
    "https://example-dev-ed.develop.my.salesforce-setup.com/lightning/setup/SetupOneHome/home",
  );
  await user.tab();
  expect(input).toHaveProperty(
    "value",
    "https://example-dev-ed.develop.my.salesforce.com",
  );
  await user.click(screen.getByRole("button", { name: "Continue" }));
  expect(
    screen.getByRole("link", { name: "Open Salesforce" }).getAttribute("href"),
  ).toBe("https://example-dev-ed.develop.my.salesforce.com");
});
it("keeps standard production login when My Domain is blank", async () => {
  const user = userEvent.setup();
  render(<SalesforceFixture step="salesforce-org" />);
  await user.click(screen.getByRole("button", { name: "Continue" }));
  expect(
    screen.getByRole("link", { name: "Open Salesforce" }).getAttribute("href"),
  ).toBe("https://login.salesforce.com");
});
it.each(["app", "install"])(
  "offers domain correction for an invalid Salesforce domain on %s",
  async (step) => {
    const user = userEvent.setup();
    render(
      <SalesforceFixture
        step={step}
        path={step === "install" ? "support" : "own"}
        salesforceDomain="example--uat.sandbox.my.salesforce.com"
      />,
    );
    const label =
      step === "install" ? "Open Salesforce installation" : "Open Salesforce";
    expect(screen.getByRole("button", { name: label })).toHaveProperty(
      "disabled",
      true,
    );
    await user.click(
      screen.getByRole("button", { name: "Review Salesforce domain" }),
    );
    expect(
      screen.getByLabelText("Salesforce Domain (optional)"),
    ).toHaveProperty("value", "example--uat.sandbox.my.salesforce.com");
  },
);
it("uses the real Salesforce callback and native scopes", () => {
  render(<SalesforceFixture step="oauth" />);
  expect(screen.getByLabelText("Callback URL")).toHaveProperty(
    "value",
    "https://current.example/callback",
  );
  expect(screen.getByText("Access MCP servers (mcp_api)")).toBeDefined();
  expect(
    screen.getByText("Perform requests at any time (refresh_token)"),
  ).toBeDefined();
});
it("shows only the documented Consumer Key credential", () => {
  render(<SalesforceFixture step="credentials" />);
  expect(screen.getByLabelText("Consumer Key")).toBeDefined();
  expect(screen.queryByLabelText(/Client Secret/)).toBeNull();
});
it("shows invalid SObject selection from credentials and offers correction", async () => {
  const user = userEvent.setup();
  render(
    <SalesforceFixture step="credentials" salesforceServer="unsupported" />,
  );
  await user.type(screen.getByLabelText("Consumer Key"), "consumer-key");
  await user.click(screen.getByRole("button", { name: "Continue" }));
  expect(
    screen.getByText("Choose a documented Salesforce server and environment."),
  ).toBeDefined();
  await user.click(
    screen.getByRole("button", { name: "Review connection choices" }),
  );
  expect(screen.getByLabelText("Salesforce server")).toBeDefined();
  expect(screen.queryByLabelText("Consumer Key")).toBeNull();
});
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

it.each([
  ["github", "GitHub", "permission to manage OAuth apps"],
  ["slack", "Slack", "permission to manage an app"],
  ["salesforce", "Salesforce", "Salesforce System Administrator access"],
] as const)(
  "uses the shared setup-plan card with %s content",
  (provider, name, requirement) => {
    const s = { ...initialState(provider), step: "begin" };
    const dispatch = vi.fn<(action: Action) => void>();
    render(
      <SetupContext value={{ s, dispatch, live: connectionFixture(dispatch) }}>
        <ProviderTask serverName={name} />
      </SetupContext>,
    );
    const heading = screen.getByRole("heading", {
      name: `Let’s connect ${name} to Speakeasy.`,
    });
    expect(heading).toBeDefined();
    expect(screen.getByText(requirement, { exact: false })).toBeDefined();
    if (provider === "salesforce") {
      expect(
        screen.getByText("create your own External Client App", {
          exact: false,
        }),
      ).toBeDefined();
      expect(
        screen.queryByText("Using an existing app or an IP allowlist?"),
      ).toBeNull();
      expect(screen.queryByText("Organization approval and SSO")).toBeNull();
    }
  },
);

it("renders the published installation link and support handoff details", async () => {
  const user = userEvent.setup();
  render(<SalesforceFixture step="choices" />);
  await user.click(screen.getByText("Sandbox", { exact: true }));
  await user.click(screen.getByText("Use Speakeasy’s app", { exact: true }));
  await user.click(screen.getByRole("button", { name: "Continue" }));
  await user.click(screen.getByRole("button", { name: "Continue" }));
  expect(
    screen
      .getByRole("link", { name: "Open Salesforce installation" })
      .getAttribute("href"),
  ).toBe(
    "https://test.salesforce.com/packaging/installPackage.apexp?p0=04tdM000000cNGXQA2",
  );
  expect(
    screen.getByText(/Installation alone does not complete OAuth/),
  ).toBeDefined();
  await user.click(screen.getByRole("button", { name: "Continue" }));
  expect(
    screen
      .getByRole("link", { name: "Read support instructions" })
      .getAttribute("href"),
  ).toBe(
    "https://www.speakeasy.com/docs/ai-control-plane/guides/salesforce#contact-speakeasy-support",
  );
  expect(screen.getByLabelText("Selected MCP endpoint")).toHaveProperty(
    "value",
    "https://api.salesforce.com/platform/mcp/v1/sandbox/platform/sobject-reads",
  );
  expect(
    screen.getByText(/Your next step is with Speakeasy support/),
  ).toBeDefined();
  expect(screen.queryByLabelText("Consumer Key")).toBeNull();
});

describe("Snowflake instructions", () => {
  it("keeps SQL workspace instructions on their own step without input fields", () => {
    const account = task("account");
    expect(screen.queryByText(/Projects → Workspaces/)).toBeNull();
    account.unmount();
    task("workspace");
    expect(screen.getByText("Projects → Workspaces")).toBeDefined();
    expect(screen.getByText("SQL File")).toBeDefined();
    expect(screen.getByText(/Choose an available warehouse/)).toBeDefined();
    expect(screen.queryByRole("textbox")).toBeNull();
  });
  it("normalizes a pasted account URL to its hostname when leaving the field", async () => {
    function Account() {
      const [s, dispatch] = useReducer(reduce, {
        ...initialState("snowflake"),
        step: "account",
      });
      return (
        <SetupContext
          value={{ s, dispatch, live: connectionFixture(dispatch) }}
        >
          <ProviderTask serverName="Snowflake" />
        </SetupContext>
      );
    }
    const user = userEvent.setup();
    render(<Account />);
    const input = screen.getByLabelText("Public Account/Server URL");
    await user.click(input);
    await user.paste("https://EXAMPLE-ACCOUNT.snowflakecomputing.com/");
    await user.tab();
    expect(input).toHaveProperty(
      "value",
      "example-account.snowflakecomputing.com",
    );
  });
  function task(
    step: string,
    values = {},
    saved = false,
    errors: State["errors"] = {},
  ) {
    const initial = initialState("snowflake"),
      dispatch = vi.fn<() => void>();
    return render(
      <SetupContext
        value={{
          s: {
            ...initial,
            step,
            saved,
            errors,
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

  it("uses the approved shared plan card before collecting account details", () => {
    task("begin");
    expect(
      screen.getByRole("heading", {
        name: "Let’s connect Snowflake to Speakeasy.",
      }),
    ).toBeDefined();
    expect(
      screen.getByText("Check permissions and region availability"),
    ).toBeDefined();
    expect(screen.queryByRole("textbox")).toBeNull();
    expect(screen.queryByText("Identity")).toBeNull();
  });

  it("routes credentials to the Snowflake SQL and required fields", () => {
    const { container } = task("credentials", {
      oauthIntegration: "EXAMPLE_OAUTH",
    });
    expect(
      container.querySelector('[data-sql="credentials"]')?.textContent,
    ).toContain("SYSTEM$SHOW_OAUTH_CLIENT_SECRETS('EXAMPLE_OAUTH')");
    expect(screen.getByLabelText("Client ID")).toBeDefined();
    expect(screen.getByLabelText("Client Secret")).toBeDefined();
  });
  it("shows invalid connection recovery on the credentials step", () => {
    const state = { ...initialState("snowflake"), step: "credentials" };
    task("credentials", {}, false, validate(state));
    expect(
      screen.getByText(/Review the Snowflake account and server details/),
    ).toBeDefined();
    expect(
      screen.getByRole("button", { name: "Review account" }),
    ).toBeDefined();
    expect(
      screen.getByRole("button", { name: "Review server details" }),
    ).toBeDefined();
  });
  it("keeps creation instructions visible while incomplete SQL cannot be copied", () => {
    const { container } = task("create", {
      creatorRole: "EXAMPLE_CREATOR",
      database: "",
      schema: "",
      serverName: "",
    });
    expect(container.querySelector("[data-sql]")).not.toBeNull();
    expect(screen.getByRole("button", { name: "Copy SQL" })).toHaveProperty(
      "disabled",
      true,
    );
    expect(
      screen.getByText(/Copy the SQL below into your SQL file/),
    ).toBeDefined();
  });

  it.each([
    ["creator", "Grant access"],
    ["access", "4. Assign the role and user defaults"],
    ["oauth", "Create the integration in Snowflake"],
  ])(
    "keeps %s instructions visible before inputs are filled",
    (step, heading) => {
      task(step, {
        creatorRole: "",
        accessRole: "",
        warehouse: "",
        users: "",
        oauthIntegration: "",
      });
      expect(screen.getByRole("heading", { name: heading })).toBeDefined();
      for (const button of screen.getAllByRole("button", { name: "Copy SQL" }))
        expect(button).toHaveProperty("disabled", true);
      for (const summary of screen.getAllByText("View SQL"))
        expect(summary.closest("details")).toHaveProperty("open", false);
    },
  );

  it.each([false, true])(
    "keeps OAuth instructions visible during callback recovery (failed=%s)",
    (failed) => {
      callback.data.callbackUrl = "";
      callback.isError = failed;
      const { container } = task("oauth", {
        oauthIntegration: "EXAMPLE_OAUTH",
      });
      expect(container.querySelector('[data-sql="oauth"]')).not.toBeNull();
      expect(
        screen.queryByText("Create the integration in Snowflake"),
      ).not.toBeNull();
      expect(screen.getByRole("button", { name: "Copy SQL" })).toHaveProperty(
        "disabled",
        true,
      );
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

it.each([
  ["github", "GitHub", "permission to manage OAuth apps"],
  ["slack", "Slack", "permission to manage an app"],
  ["salesforce", "Salesforce", "Salesforce System Administrator access"],
] as const)(
  "uses the shared setup-plan card with %s content",
  (provider, name, requirement) => {
    const s = { ...initialState(provider), step: "begin" };
    const dispatch = vi.fn<(action: Action) => void>();
    render(
      <SetupContext value={{ s, dispatch, live: connectionFixture(dispatch) }}>
        <ProviderTask serverName={name} />
      </SetupContext>,
    );
    const heading = screen.getByRole("heading", {
      name: `Let’s connect ${name} to Speakeasy.`,
    });
    expect(heading).toBeDefined();
    expect(screen.getByText(requirement, { exact: false })).toBeDefined();
    if (provider === "salesforce") {
      expect(
        screen.getByText("create your own External Client App", {
          exact: false,
        }),
      ).toBeDefined();
      expect(
        screen.queryByText("Using an existing app or an IP allowlist?"),
      ).toBeNull();
      expect(screen.queryByText("Organization approval and SSO")).toBeNull();
    }
  },
);

it("uses the published app installation URL and stops at the documented support handoff", async () => {
  const user = userEvent.setup();
  render(<SalesforceFixture step="choices" />);
  await user.click(screen.getByText("Use Speakeasy’s app", { exact: true }));
  await user.click(screen.getByRole("button", { name: "Continue" }));
  expect(
    screen
      .getByRole("link", { name: "Open Salesforce installation" })
      .getAttribute("href"),
  ).toBe(
    "https://login.salesforce.com/packaging/installPackage.apexp?p0=04tdM000000cNGXQA2",
  );
  expect(
    screen.getByText(/Installation alone does not complete OAuth/),
  ).toBeDefined();
  await user.click(screen.getByRole("button", { name: "Continue" }));
  expect(
    screen
      .getByRole("link", { name: "Read support instructions" })
      .getAttribute("href"),
  ).toBe(
    "https://www.speakeasy.com/docs/ai-control-plane/guides/salesforce#contact-speakeasy-support",
  );
  expect(screen.getByLabelText("Selected MCP endpoint")).toHaveProperty(
    "value",
    "https://api.salesforce.com/platform/mcp/v1/platform/sobject-reads",
  );
  await user.click(screen.getByRole("button", { name: "Continue" }));
  expect(
    screen.getByText(/Your next step is with Speakeasy support/),
  ).toBeDefined();
  expect(screen.queryByLabelText("Consumer Key")).toBeNull();
});

it.each(["web", "native"])(
  "renders Okta %s app instructions with the actual callback and matching credentials",
  (appType) => {
    const dispatch = vi.fn<() => void>();
    const s = initialState("okta");
    Object.assign(s.values, {
      oktaOrg: "https://example.okta.com",
      oktaAppType: appType,
    });
    const content = (step: string) => (
      <SetupContext
        value={{
          s: { ...s, step },
          dispatch,
          live: connectionFixture(dispatch),
        }}
      >
        <ProviderTask serverName="Okta" />
      </SetupContext>
    );
    const view = render(content("app"));
    expect(
      screen.getByLabelText("Sign-in redirect URI").getAttribute("value"),
    ).toBe("https://current.example/callback");
    expect(
      screen.getByText("Proof Key for Code Exchange (PKCE)"),
    ).toBeDefined();
    const instructions = screen.getByText("In Okta").parentElement!.textContent;
    expect(instructions).toContain(
      `Set Client authentication to ${appType === "web" ? "Client Secret" : "None"}.`,
    );
    expect(instructions).not.toContain(
      appType === "web" ? "Native app" : "Web app",
    );
    view.rerender(content("credentials"));
    expect(screen.getByLabelText("Client ID")).toBeDefined();
    expect(!!screen.queryByLabelText("Client Secret")).toBe(appType === "web");
    view.rerender(content("permissions"));
    expect(screen.getByLabelText("Okta API scopes")).toBeDefined();
    expect(
      screen
        .getByRole("link", { name: "Scope-to-tool guide" })
        .getAttribute("href"),
    ).toContain("scope-based-tool-loading");
    expect(screen.getByText(/Assign the people or groups/)).toBeDefined();
  },
);

it("explains why Open Okta is disabled on hover and keyboard focus", async () => {
  const { TooltipProvider } = await import("@/components/ui/Tooltip");
  const dispatch = vi.fn<() => void>();
  const s = { ...initialState("okta"), step: "app" };
  render(
    <TooltipProvider delayDuration={0}>
      <SetupContext value={{ s, dispatch, live: connectionFixture(dispatch) }}>
        <ProviderTask serverName="Okta" />
      </SetupContext>
    </TooltipProvider>,
  );
  expect(
    screen.getByRole<HTMLButtonElement>("button", { name: "Open Okta" })
      .disabled,
  ).toBe(true);
  const user = userEvent.setup();
  const trigger = screen.getByRole("group", { name: "Open Okta unavailable" });
  await user.hover(trigger);
  expect((await screen.findByRole("tooltip")).textContent).toContain(
    "Enter a valid Okta organization URL",
  );
  await user.unhover(trigger);
  trigger.focus();
  expect((await screen.findByRole("tooltip")).textContent).toContain(
    "Okta organization step",
  );
});

function OktaTaskFixture({
  step,
  appType = "web",
  appChoice = "new",
  scopes = "okta.users.read",
  saved = false,
}: {
  step: string;
  appType?: string;
  appChoice?: string;
  scopes?: string;
  saved?: boolean;
}) {
  const dispatch = vi.fn<() => void>();
  const s = initialState("okta");
  Object.assign(s.values, {
    oktaOrg: "https://example.okta.com",
    oktaAppType: appType,
    appChoice,
    oktaScopes: scopes,
  });
  return (
    <SetupContext
      value={{
        s: { ...s, step, saved },
        dispatch,
        live: connectionFixture(dispatch),
      }}
    >
      <ProviderTask serverName="Okta" />
    </SetupContext>
  );
}

it.each(["web", "native"])(
  "shows only the chosen %s app creation or editing instructions",
  (appType) => {
    const view = render(<OktaTaskFixture step="app" appType={appType} />);
    expect(screen.getByText("Create App Integration")).toBeDefined();
    expect(screen.getByText("App integration name")).toBeDefined();
    view.rerender(
      <OktaTaskFixture step="app" appType={appType} appChoice="existing" />,
    );
    expect(screen.queryByText("Create App Integration")).toBeNull();
    expect(screen.queryByText("App integration name")).toBeNull();
    expect(screen.getByText(/Keep any redirect URIs/)).toBeDefined();
  },
);

it("shows refresh-token actions only when offline_access is selected", () => {
  const view = render(<OktaTaskFixture step="permissions" />);
  expect(screen.queryByText("Refresh Token")).toBeNull();
  expect(screen.queryByText(/refresh tokens/i)).toBeNull();
  expect(
    screen.getByRole("heading", { name: "Grant API scopes" }),
  ).toBeDefined();
  expect(screen.getByRole("heading", { name: "Assign people" })).toBeDefined();
  view.rerender(
    <OktaTaskFixture
      step="permissions"
      scopes="okta.users.read offline_access"
    />,
  );
  expect(screen.getByText("Refresh Token")).toBeDefined();
  expect(screen.getByLabelText("Scopes to grant").getAttribute("value")).toBe(
    "okta.users.read",
  );
});

it("keeps credential instructions specific to the selected app and hides them after saving", () => {
  const view = render(<OktaTaskFixture step="credentials" appType="native" />);
  expect(screen.queryByText(/client secret/i)).toBeNull();
  expect(screen.queryByText(/confirm/i)).toBeNull();
  view.rerender(<OktaTaskFixture step="credentials" appType="web" />);
  expect(screen.getByText("Client Credentials")).toBeDefined();
  view.rerender(<OktaTaskFixture step="credentials" appType="web" saved />);
  expect(screen.queryByText("In Okta")).toBeNull();
  expect(
    screen.getByRole("button", { name: "Update credentials" }),
  ).toBeDefined();
});
