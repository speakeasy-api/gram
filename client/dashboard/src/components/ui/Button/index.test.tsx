import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { Button } from "./index";

afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
  vi.unstubAllEnvs();
});

describe("Button compound children", () => {
  it("forwards compound labels without development validation warnings", () => {
    vi.stubEnv("NODE_ENV", "development");
    const warning = vi.spyOn(console, "warn").mockImplementation(() => {});
    render(
      <Button>
        <Button.Text>Open thread</Button.Text>
      </Button>,
    );
    expect(screen.getByRole("button", { name: "Open thread" })).toBeDefined();
    expect(warning).not.toHaveBeenCalled();
  });

  it.each([
    ["an icon on its own", <Button key="a" icon="plus" aria-label="New" />],
    [
      "an icon beside raw text",
      <Button key="b" icon="plus">
        Refresh
      </Button>,
    ],
    [
      "an icon beside a compound label",
      <Button key="c" icon="plus">
        <Button.Text>New thread</Button.Text>
      </Button>,
    ],
  ])("renders %s without a React list warning", (_name, node) => {
    const errors = vi.spyOn(console, "error").mockImplementation(() => {});
    render(node);
    expect(errors).not.toHaveBeenCalled();
  });

  it("preserves the raw-text label convenience without wrapping it in a fragment", () => {
    vi.stubEnv("NODE_ENV", "development");
    const warning = vi.spyOn(console, "warn").mockImplementation(() => {});
    render(<Button>Refresh</Button>);
    expect(screen.getByRole("button", { name: "Refresh" })).toBeDefined();
    expect(warning).not.toHaveBeenCalled();
  });
});
