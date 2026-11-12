import { salesforceConnection, salesforceMyDomain } from "./salesforce";
import {
  snowflakeConnection,
  publicSnowflakeHost,
  idValid,
  mcpNameValid,
  agentValid,
  roleAllowed,
  users,
} from "./snowflake";
/** Provider setup state only. Network actions live in the page integration. */
export type Provider = "slack" | "github" | "salesforce" | "snowflake";
export const names = {
  slack: "Slack",
  github: "GitHub",
  salesforce: "Salesforce",
  snowflake: "Snowflake",
};
const defaultValues = {
  org: "",
  integration: "native",
  environment: "production",
  salesforceServer: "sobject-reads",
  salesforceDomain: "",
  instanceUrl: "",
  path: "own",
  appChoice: "new",
  clientId: "",
  clientSecret: "",
  access: "all",
  host: "",
  network: "public",
  method: "new",
  database: "",
  schema: "",
  serverName: "SPEAKEASY_MCP",
  agent: "",
  toolName: "business_data_agent",
  toolTitle: "Business data agent",
  toolDescription: "Answers questions using the configured Cortex Agent.",
  creatorRole: "",
  accessRole: "SPEAKEASY_MCP_USER",
  warehouse: "",
  users: "",
  oauthIntegration: "",
};
type Values = typeof defaultValues;
export type FieldName = keyof Values;
export type State = {
  provider: Provider;
  values: Values;
  step: string;
  screen: "setup" | "success";
  done: string[];
  errors: Partial<Record<FieldName, string>>;
  saved: boolean;
  authorized: boolean;
  tested: boolean;
  returnToTest: boolean;
  showSecret: boolean;
  configChanged: boolean;
  accessChosen: boolean;
  sourceFingerprint?: string;
};
export function initialState(provider: Provider): State {
  return {
    provider,
    values: { ...defaultValues },
    step: "begin",
    screen: "setup",
    done: [],
    errors: {},
    saved: false,
    authorized: false,
    tested: false,
    returnToTest: false,
    showSecret: false,
    configChanged: false,
    accessChosen: false,
  };
}
export function steps(s: State): { id: string; title: string }[] {
  if (s.provider === "snowflake")
    return [
      { id: "begin", title: "Setup plan" },
      { id: "account", title: "Snowflake account" },
      { id: "workspace", title: "Prepare SQL workspace" },
      { id: "server", title: "MCP server details" },
      ...(s.values.method === "new"
        ? [
            { id: "creator", title: "Server-creator role" },
            { id: "create", title: "Create MCP server" },
          ]
        : []),
      { id: "access", title: "Grant user access" },
      { id: "oauth", title: "OAuth integration" },
      { id: "credentials", title: "Enter app credentials" },
      { id: "test", title: "Connect and test" },
    ];
  if (s.provider === "salesforce") {
    const start = [
      { id: "begin", title: "Setup plan" },
      { id: "choices", title: "Choose your connection" },
      { id: "salesforce-org", title: "Salesforce organization" },
    ];
    if (s.values.path === "support")
      return [
        ...start,
        { id: "install", title: "Install Speakeasy’s app" },
        { id: "support", title: "Finish with support" },
      ];
    return [
      ...start,
      { id: "app", title: "App basics" },
      { id: "oauth", title: "OAuth settings" },
      { id: "security", title: "Security settings" },
      { id: "activation", title: "Enable Salesforce MCP" },
      { id: "credentials", title: "Enter app credentials" },
      { id: "test", title: "Connect and test" },
    ];
  }
  if (s.provider === "github")
    return [
      { id: "begin", title: "Setup plan" },
      { id: "organization", title: "GitHub organization" },
      { id: "app", title: "Set up OAuth app" },
      { id: "credentials", title: "Enter app credentials" },
      { id: "test", title: "Connect and test" },
    ];
  return [
    { id: "begin", title: "Setup plan" },
    {
      id: "app",
      title:
        s.values.appChoice === "new"
          ? "Create your Slack app"
          : "Configure your Slack app",
    },
    { id: "credentials", title: "Enter app credentials" },
    { id: "test", title: "Connect and test" },
  ];
}
export function orgValid(s: State): boolean {
  const org = s.values.org.trim();
  return (
    /^[A-Za-z0-9](?:[A-Za-z0-9-]{0,37}[A-Za-z0-9])?$/.test(org) &&
    !org.includes("--")
  );
}
export function ready(s: State): boolean {
  return (
    s.saved &&
    (s.provider !== "snowflake" ||
      (!s.configChanged &&
        !!snowflakeConnection(s.values) &&
        roleAllowed(s.values.accessRole))) &&
    (s.provider !== "salesforce" ||
      (s.values.path === "own" &&
        !s.configChanged &&
        !!salesforceConnection(s.values))) &&
    (s.provider !== "github" || orgValid(s))
  );
}
export function validate(s: State): State["errors"] {
  const errors: State["errors"] = {};
  if (s.step === "organization" && !orgValid(s))
    errors.org = "Enter the GitHub organization name, without a URL.";
  if (
    s.provider === "salesforce" &&
    ["choices", "credentials"].includes(s.step)
  ) {
    if (!salesforceConnection({ ...s.values, salesforceDomain: "" })) {
      errors.salesforceServer =
        "Choose a documented Salesforce server and environment.";
    }
  }
  if (
    s.provider === "salesforce" &&
    ["salesforce-org", "credentials"].includes(s.step) &&
    s.values.salesforceDomain.trim() &&
    !salesforceMyDomain(s.values.salesforceDomain, s.values.environment)
  )
    errors.salesforceDomain =
      s.values.environment === "sandbox"
        ? "Enter your sandbox’s Current My Domain URL, such as example--uat.sandbox.my.salesforce.com."
        : "Enter your production org’s Current My Domain URL, such as example.my.salesforce.com.";
  if (s.step === "credentials") {
    if (!s.values.clientId.trim())
      errors.clientId =
        s.provider === "salesforce"
          ? "Enter Consumer Key."
          : "Enter Client ID.";
    if (s.provider !== "salesforce" && !s.values.clientSecret.trim())
      errors.clientSecret = "Enter Client Secret.";
  }
  if (s.provider === "snowflake") {
    const v = s.values;
    if (s.step === "account" && !publicSnowflakeHost(v.host))
      errors.host =
        "Enter the public HTTPS Snowflake account hostname, without a path or PrivateLink hostname.";
    if (s.step === "server") {
      for (const field of ["database", "schema", "serverName"] as const)
        if (!mcpNameValid(v[field]))
          errors[field] =
            "Enter a valid MCP object name; dot-only names are unsupported. Use double quotes for case-sensitive names.";
      if (!agentValid(v.agent))
        errors.agent =
          "Enter the existing Cortex Agent as DATABASE.SCHEMA.AGENT_NAME.";
      if (v.method === "new") {
        if (!/^[A-Za-z_][A-Za-z0-9_]*$/.test(v.toolName))
          errors.toolName =
            "Use letters, numbers, and underscores for the tool name.";
        for (const field of ["toolTitle", "toolDescription"] as const)
          if (!v[field].trim() || v[field].includes("$$"))
            errors[field] =
              "Enter the tool title and description without $$ delimiters.";
        if (v.agent.includes("$$"))
          errors.agent =
            "Use an agent identifier without $$ delimiters in the server specification.";
      }
    }
    if (["creator", "create"].includes(s.step) && !idValid(v.creatorRole))
      errors.creatorRole = "Enter the server-creator role.";
    if (s.step === "access") {
      if (!roleAllowed(v.accessRole))
        errors.accessRole =
          "Use an approved non-privileged role, not PUBLIC or an administrator role.";
      if (!idValid(v.warehouse))
        errors.warehouse = "Enter the default warehouse.";
      if (!users(v).length || !users(v).every(idValid))
        errors.users =
          "Enter at least one valid Snowflake username, one per line. Quote case-sensitive usernames.";
    }
    if (s.step === "oauth" && !idValid(v.oauthIntegration))
      errors.oauthIntegration = "Enter the OAuth integration name.";
    if (s.step === "credentials" && !snowflakeConnection(v))
      errors.host =
        "Review the Snowflake account and server details before saving credentials.";
  }
  return errors;
}
export type Action =
  | {
      type: "connection";
      saved?: boolean;
      authorized?: boolean;
      tested?: boolean;
      configChanged?: boolean;
      sourceFingerprint?: string;
    }
  | { type: "source"; values: Partial<Values>; sourceFingerprint: string }
  | { type: "field"; field: FieldName; value: string; source?: "configuration" }
  | { type: "goto"; step: string; correction?: boolean }
  | { type: "next" }
  | { type: "back" }
  | { type: "complete" }
  | { type: "secret" };
export function reduce(s: State, a: Action): State {
  switch (a.type) {
    case "connection":
      return {
        ...s,
        saved: a.saved ?? s.saved,
        authorized: a.authorized ?? s.authorized,
        tested: a.tested ?? s.tested,
        configChanged:
          a.tested === true ? false : (a.configChanged ?? s.configChanged),
        sourceFingerprint: a.sourceFingerprint ?? s.sourceFingerprint,
      };
    case "source":
      return {
        ...s,
        values: {
          ...(s.provider === "snowflake" ? defaultValues : s.values),
          ...a.values,
          clientId: "",
          clientSecret: "",
        },
        configChanged: s.provider === "snowflake" ? false : s.configChanged,
        sourceFingerprint: a.sourceFingerprint,
        step: s.provider === "snowflake" ? "account" : "choices",
        screen: "setup",
        done: [],
        errors: {},
        saved: false,
        authorized: false,
        tested: false,
        returnToTest: false,
        showSecret: false,
      };
    case "field": {
      if (s.values[a.field] === a.value) return s;
      const connectionChange =
        s.provider === "snowflake" &&
        [
          "host",
          "database",
          "schema",
          "serverName",
          "network",
          "oauthIntegration",
          "accessRole",
        ].includes(a.field);
      const credentialChange = ["clientId", "clientSecret"].includes(a.field);
      const boundary = steps(s).findIndex((t) => t.id === s.step);
      const next: State = {
        ...s,
        values: { ...s.values, [a.field]: a.value },
        saved:
          s.provider === "snowflake" && !connectionChange && !credentialChange
            ? s.saved
            : a.field === "access" && s.saved,
        authorized: false,
        tested: false,
        errors: {},
        done: s.done.filter(
          (id) => steps(s).findIndex((t) => t.id === id) < boundary,
        ),
      };
      if (
        a.field === "appChoice" ||
        connectionChange ||
        (s.provider === "salesforce" &&
          [
            "integration",
            "environment",
            "salesforceServer",
            "salesforceDomain",
            "instanceUrl",
            "path",
          ].includes(a.field))
      ) {
        next.values.clientId = "";
        next.values.clientSecret = "";
        next.showSecret = false;
      }
      if (
        (s.provider === "salesforce" ||
          connectionChange ||
          (s.provider === "snowflake" && credentialChange)) &&
        a.source !== "configuration"
      )
        next.configChanged = true;
      if (a.field === "access" && a.source !== "configuration") {
        next.configChanged = true;
        next.accessChosen = true;
      }
      return next;
    }
    case "goto":
      return steps(s).some((t) => t.id === a.step)
        ? {
            ...s,
            screen: "setup",
            step: a.step,
            errors: {},
            returnToTest:
              !!a.correction && (s.step === "test" || s.returnToTest),
          }
        : s;
    case "next": {
      if (
        s.provider === "salesforce" &&
        s.values.path === "support" &&
        s.step === "support"
      )
        return s;
      const errors = validate(s);
      if (Object.keys(errors).length) return { ...s, errors };
      const list = steps(s),
        i = list.findIndex((t) => t.id === s.step);
      return {
        ...s,
        errors: {},
        done: [...new Set([...s.done, s.step])],
        step: s.returnToTest ? "test" : (list[i + 1]?.id ?? "test"),
        returnToTest: false,
      };
    }
    case "back": {
      const list = steps(s),
        i = list.findIndex((t) => t.id === s.step);
      return {
        ...s,
        step: list[Math.max(0, i - 1)]!.id,
        errors: {},
        returnToTest: false,
      };
    }
    case "complete":
      return ready(s) && s.authorized && s.tested
        ? {
            ...s,
            screen: "success",
            done: steps(s).map((t) => t.id),
          }
        : s;
    case "secret":
      return { ...s, showSecret: !s.showSecret };
  }
}
