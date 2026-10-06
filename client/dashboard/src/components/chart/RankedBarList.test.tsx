import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";
import { MemoryRouter } from "react-router";
import { RankedBarList } from "./RankedBarList";

afterEach(cleanup);

describe("RankedBarList", () => {
  it("links an item when a drill-down href is provided", () => {
    render(
      <MemoryRouter>
        <RankedBarList
          items={[
            {
              key: "tool",
              label: "list_issues",
              value: 12,
              href: "/logs?server=hosted%3Aissues&af=gram.tool.name%3Aeq%3Alist_issues",
            },
          ]}
        />
      </MemoryRouter>,
    );

    expect(
      screen.getByRole("link", { name: "list_issues" }).getAttribute("href"),
    ).toBe("/logs?server=hosted%3Aissues&af=gram.tool.name%3Aeq%3Alist_issues");
  });
});
