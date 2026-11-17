import { cleanup, render, screen, within } from "@testing-library/react";
import { afterEach, expect, it } from "vitest";
import { SkeletonTable } from ".";

afterEach(cleanup);

it("announces a labeled loading table with five placeholder rows", () => {
  render(<SkeletonTable label="Loading records" />);

  const status = screen.getByRole("status", { name: "Loading records" });
  expect(within(status).getAllByRole("row")).toHaveLength(5);
  expect(within(status).getAllByRole("cell")).toHaveLength(15);
  expect(within(status).queryByRole("columnheader")).toBeNull();
});

it("keeps custom column counts and table classes without an unsolicited status", () => {
  const { container } = render(
    <SkeletonTable columns={["1fr", "2fr"]} className="custom-table" />,
  );

  expect(screen.getAllByRole("cell")).toHaveLength(10);
  expect(screen.queryByRole("status")).toBeNull();
  expect(container.querySelector(".custom-table")).not.toBeNull();
});
