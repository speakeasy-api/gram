import { cleanup, render } from "@testing-library/react";
import { afterEach, expect, it } from "vitest";
import { toCatalogEntry } from "./platforms";
import { SetupBlockView, type SetupBlockContext } from "./SetupBlocks";
import { testPlatform } from "./testPlatform";

afterEach(cleanup);

const context: SetupBlockContext = {
  entry: toCatalogEntry(testPlatform),
  values: {},
  onValueChange: () => {},
  agents: [],
  agentsUnavailable: null,
  agentId: "",
  onAgentChange: () => {},
  tags: [],
  onTagsChange: () => {},
  setupValues: { values: undefined, unavailableReason: null },
};

it("renders no image from definition text, whatever its source", () => {
  const { container, getByText } = render(
    <SetupBlockView
      block={{
        type: "text",
        markdown:
          "Before ![x](https://evil.example.com/px) after ![y][ref]\n\n[ref]: /ok.png",
      }}
      context={context}
    />,
  );
  expect(container.querySelector("img")).toBeNull();
  expect(getByText(/Before/)).toBeTruthy();
});
