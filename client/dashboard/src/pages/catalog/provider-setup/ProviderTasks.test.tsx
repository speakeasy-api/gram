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
