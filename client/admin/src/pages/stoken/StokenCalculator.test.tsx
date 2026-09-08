import type { AnyRouter } from "@tanstack/react-router";
import {
  cleanup,
  fireEvent,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import {
  decodeProviderRows,
  encodeProviderRows,
  type ProviderRow,
} from "@/lib/stoken/url-state";
import { routeTree } from "@/routeTree.gen";
import { renderRouteTree } from "@/test/harness";

const mocks = vi.hoisted(() => ({
  getSession: vi.fn(),
}));

// The layout's user menu is the only request this page's frame makes.
vi.mock("@/lib/gramAdminApi", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/lib/gramAdminApi")>();
  return { ...actual, getSession: mocks.getSession };
});

// The row every fresh worksheet opens on, spelled out so a change to the
// default is a change to this file too.
const OPENAI_ROW: ProviderRow = {
  id: "row-1",
  provider: "openai",
  customName: "",
  providerTokens: "",
  tokenizerMin: "1.00",
  tokenizerMax: "1.00",
};

// The worked example the method notes quote: 120M at R = 1.20–1.60 is
// 25M–50M s-tokens, which makes the figures on screen checkable by hand.
const ANTHROPIC_ROW: ProviderRow = {
  id: "row-1",
  provider: "anthropic",
  customName: "",
  providerTokens: "120M",
  tokenizerMin: "1.20",
  tokenizerMax: "1.60",
};

function currentRows(router: AnyRouter): ProviderRow[] | null {
  const rows = new URLSearchParams(router.state.location.searchStr).get("rows");
  return rows === null ? null : decodeProviderRows(rows);
}

function tokensInput(rowId = "row-1"): HTMLInputElement {
  return document.getElementById(`${rowId}-tokens`) as HTMLInputElement;
}

function resultPanel(): HTMLElement {
  return screen.getByRole("complementary", { name: "Monthly s-token range" });
}

async function renderCalculator(search = ""): Promise<AnyRouter> {
  const { router } = await renderRouteTree(routeTree, {
    initialPath: `/stoken-calculator${search}`,
  });
  await screen.findByRole("heading", { name: "Monthly usage" });
  return router;
}

beforeEach(() => {
  mocks.getSession.mockReset();
  mocks.getSession.mockResolvedValue({
    email: "ops@example.test",
    name: "Ops",
  });
});

afterEach(cleanup);

describe("stoken calculator route", () => {
  it("is reachable from the sidebar", async () => {
    const { router } = await renderRouteTree(routeTree, {
      initialPath: "/projects",
    });

    fireEvent.click(screen.getByRole("link", { name: "S-token calculator" }));

    await screen.findByRole("heading", { name: "Monthly usage" });
    expect(router.state.location.pathname).toBe("/stoken-calculator");
  });

  it("names itself in the breadcrumb", async () => {
    await renderCalculator();

    expect(
      screen.getByRole("navigation", { name: "breadcrumb" }).textContent,
    ).toBe("S-token calculator");
  });

  it("opens on the default worksheet with nothing in the URL", async () => {
    const router = await renderCalculator();

    expect(currentRows(router)).toBeNull();
    expect(tokensInput().value).toBe("");
    expect(resultPanel().textContent).toContain(
      "Add provider usage to estimate s-tokens.",
    );
  });

  it("restores a worksheet from a link the standalone estimator wrote", async () => {
    await renderCalculator(`?rows=${encodeProviderRows([ANTHROPIC_ROW])}`);

    expect(tokensInput().value).toBe("120M");
    expect(screen.getByRole("combobox").textContent).toBe("Anthropic");
    // Low = 120M ÷ 3 ÷ 1.60 = 25M; high = 120M ÷ 2 ÷ 1.20 = 50M.
    expect(within(resultPanel()).getByText("25M–50M")).toBeTruthy();
  });

  it("falls back to the default worksheet for a link that does not decode", async () => {
    const router = await renderCalculator("?rows=not+base64");

    expect(currentRows(router)).toBeNull();
    expect(tokensInput().value).toBe("");
  });
});

describe("stoken calculator worksheet", () => {
  it("writes a typed total to the URL and shows its range", async () => {
    const router = await renderCalculator();

    fireEvent.change(tokensInput(), { target: { value: "120M" } });

    await waitFor(() => {
      expect(currentRows(router)).toEqual([
        { ...OPENAI_ROW, providerTokens: "120M" },
      ]);
    });
    // OpenAI's preset is 1.00–1.00: low = 120M ÷ 3 = 40M, high = 120M ÷ 2 = 60M.
    expect(within(resultPanel()).getByText("40M–60M")).toBeTruthy();
    expect(tokensInput().value).toBe("120M");
  });

  it("replaces history rather than pushing an entry per keystroke", async () => {
    const router = await renderCalculator();
    const before = router.history.length;

    fireEvent.change(tokensInput(), { target: { value: "1" } });
    fireEvent.change(tokensInput(), { target: { value: "12" } });
    await waitFor(() => {
      expect(tokensInput().value).toBe("12");
    });

    expect(router.history.length).toBe(before);
  });

  it("adds a provider row and moves focus to its picker", async () => {
    const router = await renderCalculator();

    fireEvent.click(screen.getByRole("button", { name: "Add provider" }));

    await waitFor(() => {
      expect(currentRows(router)?.map((row) => row.id)).toEqual([
        "row-1",
        "row-2",
      ]);
    });
    expect(document.activeElement?.id).toBe("row-2-provider");
  });

  it("removes a row and writes an explicitly empty worksheet", async () => {
    const router = await renderCalculator();

    fireEvent.click(
      screen.getByRole("button", { name: "Remove OpenAI provider row 1" }),
    );

    await waitFor(() => {
      expect(currentRows(router)).toEqual([]);
    });
    expect(
      screen.getByText("No provider rows. Add provider usage to continue."),
    ).toBeTruthy();
    expect(document.activeElement?.id).toBe("add-provider");
  });

  it("applies the preset when the provider changes", async () => {
    const router = await renderCalculator();

    const select = screen.getByRole("combobox");
    fireEvent.keyDown(select, { key: "ArrowDown" });
    fireEvent.click(await screen.findByRole("option", { name: "Anthropic" }));

    await waitFor(() => {
      expect(currentRows(router)?.[0]).toMatchObject({
        provider: "anthropic",
        tokenizerMin: "1.20",
        tokenizerMax: "1.60",
      });
    });
    expect(screen.getByText("Tokenizer assumption · 1.20–1.60")).toBeTruthy();
  });

  it("flags a total it cannot parse and counts it as needing attention", async () => {
    await renderCalculator();

    fireEvent.change(tokensInput(), { target: { value: "twelve" } });

    await waitFor(() => {
      expect(tokensInput().getAttribute("aria-invalid")).toBe("true");
    });
    expect(
      screen
        .getByText(/Enter a whole monthly token count/)
        .hasAttribute("hidden"),
    ).toBe(false);
    expect(resultPanel().textContent).toContain("1 row needs attention");
  });

  it("sums the rows into the combined envelope", async () => {
    await renderCalculator(
      `?rows=${encodeProviderRows([
        ANTHROPIC_ROW,
        { ...OPENAI_ROW, id: "row-2", providerTokens: "120M" },
      ])}`,
    );

    // 25M–50M from the Anthropic row plus 40M–60M from the OpenAI one.
    expect(within(resultPanel()).getByText("65M–110M")).toBeTruthy();
    expect(resultPanel().textContent).toContain("Low 65M");
    expect(resultPanel().textContent).toContain("High 110M");
  });
});
