import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { useReducer } from "react";
import { afterEach, expect, it, vi } from "vitest";
import { connectionFixture } from "./connection-fixture";
import { GitHubOrgField } from "./GitHubOrgField";
import { SetupContext } from "./setup-context";
import { initialState, reduce } from "./state";

function Fixture() {
  const [s, dispatch] = useReducer(reduce, initialState("github"));
  return (
    <SetupContext value={{ s, dispatch, live: connectionFixture(dispatch) }}>
      <GitHubOrgField />
    </SetupContext>
  );
}

function renderField() {
  render(<Fixture />);
  return screen.getByRole<HTMLInputElement>("combobox", {
    name: "GitHub organization",
  });
}

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

it("lets a pasted organization URL become the selected organization", async () => {
  const user = userEvent.setup();
  const input = renderField();
  await user.click(input);
  await user.paste("https://github.com/example-org/");
  expect(input.value).toBe("example-org");
});

it("searches organizations and selects a suggestion with the keyboard", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn(
      async () =>
        new Response(
          JSON.stringify({
            items: [{ login: "example-team", type: "Organization" }],
          }),
        ),
    ),
  );
  const user = userEvent.setup();
  const input = renderField();
  await user.type(input, "e");
  expect(
    await screen.findByRole("option", { name: "example-team" }),
  ).toBeDefined();
  await user.keyboard("{ArrowDown}{Enter}");
  expect(input.value).toBe("example-team");
  expect(screen.queryByRole("listbox")).toBeNull();
});

it("keeps manual entry available when GitHub search fails", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async () => new Response(null, { status: 403 })),
  );
  const user = userEvent.setup();
  const input = renderField();
  await user.type(input, "example-org");
  expect(await screen.findByText(/Search unavailable/)).toBeDefined();
  expect(screen.getByRole("status").closest('[role="listbox"]')).toBeNull();
  await user.tab();
  expect(input.value).toBe("example-org");
});

it("clears old suggestions immediately when the query changes", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn(
      async () =>
        new Response(
          JSON.stringify({
            items: [{ login: "example-team", type: "Organization" }],
          }),
        ),
    ),
  );
  const user = userEvent.setup();
  const input = renderField();
  await user.type(input, "example");
  await screen.findByRole("option", { name: "example-team" });
  fireEvent.change(input, { target: { value: "different" } });
  expect(screen.queryByRole("option", { name: "example-team" })).toBeNull();
});
