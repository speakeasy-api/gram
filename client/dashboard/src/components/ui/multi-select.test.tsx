import { render, screen, fireEvent, cleanup } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

// moonshine's bundle imports lucide-react/dynamicIconImports which can't be
// resolved in the test environment. Mock it so the local Button renders plainly.
vi.mock("@/components/ui/moonshine", () => ({
  Icon: ({ name }: { name: string }) => <span>{name}</span>,
}));

import { MultiSelect } from "./multi-select";

// This vitest project does not enable `globals`, so RTL's automatic cleanup is
// not registered. Without this, mounted popovers accumulate across tests.
afterEach(cleanup);

const EMAILS = [
  "alex@speakeasyapi.dev",
  "alec@speakeasyapi.dev",
  "alice@speakeasyapi.dev",
  "bob@speakeasyapi.dev",
  "carol@speakeasyapi.dev",
];

function openEmailSelect() {
  const onValueChange = vi.fn();
  render(
    <MultiSelect
      options={EMAILS.map((e) => ({ label: e, value: e }))}
      onValueChange={() => void onValueChange()}
      placeholder="Filter by user email"
      hideSelectAll
      singleLine
    />,
  );
  fireEvent.click(screen.getByText("Filter by user email"));
  const input = screen.getByPlaceholderText("Search options...");
  return { onValueChange, input };
}

// Footer actions ("Close"/"Clear") are also role="option"; exclude them so we
// assert on the actual data rows the user is filtering.
const FOOTER_ACTIONS = new Set(["Close", "Clear"]);

// Options that cmdk has filtered out are marked aria-hidden; the visible set is
// what the user actually sees in the dropdown.
function visibleOptionLabels(): string[] {
  return screen
    .queryAllByRole("option")
    .filter((el) => el.getAttribute("aria-hidden") !== "true")
    .map((el) => el.textContent?.trim() ?? "")
    .filter((label) => !FOOTER_ACTIONS.has(label));
}

function type(input: HTMLElement, value: string) {
  fireEvent.change(input, { target: { value } });
}

describe("MultiSelect search filtering (AIS-84)", () => {
  it("shows exactly the substring matches as the query narrows", () => {
    const { input } = openEmailSelect();

    type(input, "al");
    expect(visibleOptionLabels()).toEqual([
      "alex@speakeasyapi.dev",
      "alec@speakeasyapi.dev",
      "alice@speakeasyapi.dev",
    ]);

    type(input, "ale");
    expect(visibleOptionLabels()).toEqual([
      "alex@speakeasyapi.dev",
      "alec@speakeasyapi.dev",
    ]);

    type(input, "alex");
    expect(visibleOptionLabels()).toEqual(["alex@speakeasyapi.dev"]);
  });

  it("restores broader results when characters are deleted", () => {
    const { input } = openEmailSelect();

    type(input, "alex");
    type(input, "ale");

    const labels = visibleOptionLabels();
    expect(labels).toContain("alex@speakeasyapi.dev");
    expect(labels).toContain("alec@speakeasyapi.dev");
    expect(labels).not.toContain("alice@speakeasyapi.dev"); // "ale" not in "alice"
  });

  it("keeps footer actions available while searching (cmdk filtering disabled)", () => {
    // Regression guard: previously cmdk's own filter ran on top of our filter
    // and removed every item that did not match the query — including the
    // always-present "Close" action. With shouldFilter disabled, our list is
    // the single source of truth and the footer stays put.
    const { input } = openEmailSelect();

    type(input, "alex");

    expect(visibleOptionLabels()).toContain("alex@speakeasyapi.dev");
    expect(screen.getByText("Close")).toBeTruthy();
  });

  it("shows the empty state only for a genuine non-match", () => {
    const { input } = openEmailSelect();

    type(input, "alex");
    expect(screen.queryByText("No results found.")).toBeNull();

    type(input, "zzz");
    expect(visibleOptionLabels()).toEqual([]);
    expect(screen.getByText("No results found.")).toBeTruthy();
  });
});
