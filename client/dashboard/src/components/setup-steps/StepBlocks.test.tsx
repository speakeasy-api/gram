import { cleanup, fireEvent, render } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { TooltipProvider } from "@/components/ui/Tooltip";
import { BlockList, type BlockRenderers } from "./blockRegistry";
import { genericBlockRenderers } from "./genericBlocks";
import {
  ChecklistItem,
  CopyableValue,
  SetupImage,
  SetupLink,
  SetupText,
} from "./StepBlocks";

afterEach(cleanup);

it("renders no image from definition text, whatever its source", () => {
  const { container, getByText } = render(
    <SetupText
      markdown={
        "Before ![x](https://evil.example.com/px) after ![y][ref]\n\n[ref]: /ok.png"
      }
    />,
  );
  expect(container.querySelector("img")).toBeNull();
  expect(getByText(/Before/)).toBeTruthy();
});

it("escapes raw HTML in definition text", () => {
  const { container } = render(
    <SetupText markdown={'<script>alert(1)</script><b onclick="x">hi</b>'} />,
  );
  expect(container.querySelector("script")).toBeNull();
  expect(container.querySelector("b")).toBeNull();
});

it("shows an image only from the dashboard's origin", () => {
  const { container, rerender } = render(
    <SetupImage src="/access-hub/console.png" alt="Console" caption="Here" />,
  );
  expect(container.querySelector("img")?.getAttribute("src")).toBe(
    "/access-hub/console.png",
  );
  rerender(<SetupImage src="https://evil.example.com/x.png" alt="Console" />);
  expect(container.querySelector("img")).toBeNull();
  rerender(<SetupImage src="//evil.example.com/x.png" alt="Console" />);
  expect(container.querySelector("img")).toBeNull();
});

it("opens a link in a new tab without access to this window", () => {
  const { getByRole } = render(
    <SetupLink href="https://docs.example.com" label="Read the docs" />,
  );
  const link = getByRole("link", { name: "Read the docs" });
  expect(link.getAttribute("target")).toBe("_blank");
  expect(link.getAttribute("rel")).toBe("noopener noreferrer");
});

it("ticks a checklist item off and shows its instruction", () => {
  const onCheckedChange = vi.fn<(checked: boolean) => void>();
  const { getByLabelText, getByText } = render(
    <ChecklistItem
      id="check-1"
      label="Resource (optional)"
      detail={{ kind: "markdown", markdown: "Leave **empty**." }}
      help="Only some consoles show it."
      checked={false}
      onCheckedChange={onCheckedChange}
    />,
  );
  expect(getByText("empty").tagName).toBe("STRONG");
  expect(getByText("Only some consoles show it.")).toBeTruthy();
  fireEvent.click(getByLabelText("Resource (optional)"));
  expect(onCheckedChange).toHaveBeenCalledWith(true);
});

it("offers to copy a value only once there is one", () => {
  const { queryByRole, getByText, rerender } = render(
    <TooltipProvider>
      <CopyableValue label="Token endpoint" value={undefined} />
    </TooltipProvider>,
  );
  expect(getByText("—")).toBeTruthy();
  expect(queryByRole("button")).toBeNull();
  rerender(
    <TooltipProvider>
      <CopyableValue label="Token endpoint" value="https://x.example.com" />
    </TooltipProvider>,
  );
  expect(getByText("https://x.example.com")).toBeTruthy();
  expect(queryByRole("button")).not.toBeNull();
});

type DemoBlock =
  | { type: "text"; markdown: string }
  | { type: "count"; n: number };

it("renders each block with the renderer for its type", () => {
  const seen: string[] = [];
  const renderers: BlockRenderers<DemoBlock> = {
    text: genericBlockRenderers.text,
    count: (block, blockKey) => {
      seen.push(blockKey);
      return <span>{`Count ${block.n}`}</span>;
    },
  };
  const { getByText } = render(
    <BlockList
      blocks={[
        { type: "text", markdown: "Hello" },
        { type: "count", n: 3 },
      ]}
      renderers={renderers}
      keyPrefix="step"
    />,
  );
  expect(getByText("Hello")).toBeTruthy();
  expect(getByText("Count 3")).toBeTruthy();
  expect(seen).toEqual(["step-1"]);
});
