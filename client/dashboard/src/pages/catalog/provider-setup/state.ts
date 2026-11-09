import { salesforceConnection } from "./salesforce";
/** Provider setup state only. Network actions live in the page integration. */
export type Provider = "slack" | "github" | "salesforce";
export const names = {
  slack: "Slack",
  github: "GitHub",
  salesforce: "Salesforce",
};
const defaultValues = {
  org: "",
  integration: "native",
  environment: "production",
  salesforceServer: "sobject-reads",
  instanceUrl: "",
  path: "own",
  appChoice: "new",
  clientId: "",
  clientSecret: "",
  access: "all",
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
  if (s.provider === "salesforce") {
    const start = [
      { id: "begin", title: "Setup plan" },
      { id: "choices", title: "Choose your connection" },
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
    if (!salesforceConnection(s.values)) {
      errors.salesforceServer =
        "Choose a documented Salesforce server and environment.";
    }
  }
  if (s.step === "credentials") {
    if (!s.values.clientId.trim())
      errors.clientId =
        s.provider === "salesforce"
          ? "Enter Consumer Key."
          : "Enter Client ID.";
    if (s.provider !== "salesforce" && !s.values.clientSecret.trim())
      errors.clientSecret = "Enter Client Secret.";
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
        values: { ...s.values, ...a.values, clientId: "", clientSecret: "" },
        sourceFingerprint: a.sourceFingerprint,
        step: "choices",
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
      const boundary = steps(s).findIndex((t) => t.id === s.step);
      const next: State = {
        ...s,
        values: { ...s.values, [a.field]: a.value },
        saved: a.field === "access" && s.saved,
        authorized: false,
        tested: false,
        errors: {},
        done: s.done.filter(
          (id) => steps(s).findIndex((t) => t.id === id) < boundary,
        ),
      };
      if (
        a.field === "appChoice" ||
        (s.provider === "salesforce" &&
          [
            "integration",
            "environment",
            "salesforceServer",
            "instanceUrl",
            "path",
          ].includes(a.field))
      ) {
        next.values.clientId = "";
        next.values.clientSecret = "";
        next.showSecret = false;
      }
      if (s.provider === "salesforce" && a.source !== "configuration")
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
