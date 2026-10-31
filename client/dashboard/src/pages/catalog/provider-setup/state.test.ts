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
          complete: true,
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
      expect([
        state.saved,
        state.authorized,
        state.tested,
        state.complete,
      ]).toEqual([false, false, false, false]);
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
