// @vitest-environment node
import { describe, expect, it } from "vitest";
import { initialState, reduce, ready, steps, validate } from "./state";

describe("Slack setup state", () => {
  it.each(["slack", "github"] as const)(
    "clears both %s credentials and connection results when the app changes",
    (provider) => {
      const initial = initialState(provider);
      const state = reduce(
        {
          ...initial,
          saved: true,
          authorized: true,
          tested: true,
          values: {
            ...initial.values,
            clientId: "app-id",
            clientSecret: "secret",
          },
        },
        { type: "field", field: "appChoice", value: "existing" },
      );
      expect(state.values.clientId).toBe("");
      expect(state.values.clientSecret).toBe("");
      expect([state.saved, state.authorized, state.tested]).toEqual([
        false,
        false,
        false,
      ]);
    },
  );
  it("keeps saved credentials but invalidates authorization and testing after a permission change", () => {
    const state = reduce(
      { ...initialState("slack"), saved: true, authorized: true, tested: true },
      { type: "field", field: "access", value: "read" },
    );
    expect(state.saved).toBe(true);
    expect(state.authorized).toBe(false);
    expect(state.tested).toBe(false);
  });
  it("requires both credentials before submitting the credentials step", () => {
    expect(validate({ ...initialState("slack"), step: "credentials" })).toEqual(
      { clientId: "Enter Client ID.", clientSecret: "Enter Client Secret." },
    );
  });
  it("does not simulate credential saving when advancing", () => {
    const initial = initialState("slack");
    const state = reduce(
      {
        ...initial,
        step: "credentials",
        values: { ...initial.values, clientId: "id", clientSecret: "secret" },
      },
      { type: "next" },
    );
    expect(ready(state)).toBe(false);
  });
  it("completes only after saved credentials, authorization and connection testing", () => {
    const initial = initialState("slack");
    expect(reduce(initial, { type: "complete" }).screen).toBe("setup");
    expect(
      reduce(
        { ...initial, saved: true, authorized: true },
        { type: "complete" },
      ).screen,
    ).toBe("setup");
    const state = reduce(
      { ...initial, saved: true, authorized: true, tested: true },
      { type: "complete" },
    );
    expect(state.screen).toBe("success");
    expect(state.done).toEqual(steps(state).map((step) => step.id));
  });
  it("returns from permission correction to connection testing", () => {
    const state = reduce(
      { ...initialState("slack"), step: "test" },
      { type: "goto", step: "app", correction: true },
    );
    expect(state.returnToTest).toBe(true);
    expect(reduce(state, { type: "next" }).step).toBe("test");
  });
});

it("validates GitHub organizations and requires one for completion", () => {
  const s = initialState("github");
  for (const org of ["", "https://github.com/example", "bad--org", "-bad"])
    expect(
      validate({ ...s, step: "organization", values: { ...s.values, org } })
        .org,
    ).toBeTruthy();
  for (const org of ["", "bad--org"]) {
    expect(
      reduce(
        {
          ...s,
          saved: true,
          authorized: true,
          tested: true,
          values: { ...s.values, org },
        },
        { type: "complete" },
      ).screen,
    ).toBe("setup");
  }
  const valid = { ...s, values: { ...s.values, org: "example-org" } };
  expect(validate({ ...valid, step: "organization" })).toEqual({});
  expect(
    reduce({ ...valid, saved: true, authorized: true }, { type: "complete" })
      .screen,
  ).toBe("setup");
  expect(
    reduce(
      { ...valid, saved: true, authorized: true, tested: true },
      { type: "complete" },
    ).screen,
  ).toBe("success");
});

it("clears the permission-change reminder only after a successful connection test", () => {
  const changed = reduce(
    { ...initialState("slack"), saved: true },
    { type: "field", field: "access", value: "read" },
  );
  expect(
    reduce(changed, { type: "connection", authorized: true }).configChanged,
  ).toBe(true);
  expect(
    reduce(changed, { type: "connection", tested: false }).configChanged,
  ).toBe(true);
  expect(
    reduce(changed, { type: "connection", authorized: true, tested: true })
      .configChanged,
  ).toBe(false);
});

it("does not treat saved-configuration hydration as a permission change", () => {
  const state = reduce(initialState("slack"), {
    type: "field",
    field: "access",
    value: "read",
    source: "configuration",
  });
  expect(state.values.access).toBe("read");
  expect(state.accessChosen).toBe(false);
  expect(state.configChanged).toBe(false);
});

describe("Snowflake setup state", () => {
  it("requires confidential credentials and clears them when server details change", () => {
    const initial = initialState("snowflake");
    const values = {
      ...initial.values,
      host: "example.snowflakecomputing.com",
      database: "APP",
      schema: "PUBLIC",
      serverName: "MCP",
      clientId: "id",
    };
    expect(
      validate({ ...initial, step: "credentials", values }).clientSecret,
    ).toBeTruthy();
    const changed = reduce(
      {
        ...initial,
        saved: true,
        authorized: true,
        tested: true,
        values: { ...values, clientSecret: "secret" },
      },
      { type: "field", field: "database", value: "OTHER" },
    );
    expect(changed.values.clientSecret).toBe("");
    expect([changed.saved, changed.authorized, changed.tested]).toEqual([
      false,
      false,
      false,
    ]);
    expect(changed.configChanged).toBe(true);
  });
  it("omits server creation for an existing server", () => {
    const initial = initialState("snowflake");
    expect(steps(initial).map((x) => x.id)).toContain("create");
    expect(
      steps({
        ...initial,
        values: { ...initial.values, method: "existing" },
      }).map((x) => x.id),
    ).not.toContain("create");
  });
});

it("clears old Snowflake instructions when another editor changes the source", () => {
  const initial = initialState("snowflake");
  const next = reduce(
    {
      ...initial,
      values: {
        ...initial.values,
        agent: "OLD.PUBLIC.AGENT",
        oauthIntegration: "OLD_OAUTH",
      },
      saved: true,
      authorized: true,
      tested: true,
    },
    {
      type: "source",
      values: {
        host: "example.snowflakecomputing.com",
        database: "NEW",
        schema: "PUBLIC",
        serverName: "MCP",
      },
      sourceFingerprint: "new-source",
    },
  );
  expect(next.values.agent).toBe("");
  expect(next.values.oauthIntegration).toBe("");
  expect(next.step).toBe("account");
  expect(next.saved).toBe(false);
});
