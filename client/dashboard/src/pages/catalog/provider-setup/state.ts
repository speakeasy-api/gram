/** Slack setup state only. Network actions live in the page integration. */
export type Provider = "slack";
export const names = { slack: "Slack" };
const defaultValues = {
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
  complete: boolean;
  returnToTest: boolean;
  showSecret: boolean;
  copied: string;
  configChanged: boolean;
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
    complete: false,
    returnToTest: false,
    showSecret: false,
    copied: "",
    configChanged: false,
  };
}
export function steps(s: State): { id: string; title: string }[] {
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
export function ready(s: State): boolean {
  return s.saved;
}
export function validate(s: State): State["errors"] {
  const errors: State["errors"] = {};
  if (s.step === "credentials") {
    if (!s.values.clientId.trim()) errors.clientId = "Enter Client ID.";
    if (!s.values.clientSecret.trim())
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
    }
  | { type: "field"; field: FieldName; value: string }
  | { type: "goto"; step: string; correction?: boolean }
  | { type: "next" }
  | { type: "back" }
  | { type: "complete" }
  | { type: "restart" }
  | { type: "modal"; modal: string | null }
  | { type: "secret" }
  | { type: "copy"; key: string };
export function reduce(s: State, a: Action): State {
  switch (a.type) {
    case "connection":
      return {
        ...s,
        saved: a.saved ?? s.saved,
        authorized: a.authorized ?? s.authorized,
        tested: a.tested ?? s.tested,
      };
    case "restart":
      return initialState(s.provider);
    case "field": {
      if (s.values[a.field] === a.value) return s;
      const boundary = steps(s).findIndex((t) => t.id === s.step);
      const next: State = {
        ...s,
        values: { ...s.values, [a.field]: a.value },
        saved: a.field === "access" && s.saved,
        authorized: false,
        tested: false,
        complete: false,
        copied: "",
        errors: {},
        done: s.done.filter(
          (id) => steps(s).findIndex((t) => t.id === id) < boundary,
        ),
      };
      if (a.field === "appChoice") {
        next.values.clientId = "";
        next.values.clientSecret = "";
        next.showSecret = false;
      }
      if (a.field === "access") next.configChanged = true;
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
            complete: true,
            screen: "success",
            done: steps(s).map((t) => t.id),
          }
        : s;
    case "secret":
      return { ...s, showSecret: !s.showSecret };
    case "copy":
      return { ...s, copied: a.key };
    case "modal":
      return s; // The connection hook owns sign-in and testing.
  }
}
