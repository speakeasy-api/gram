import { render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { describe, expect, it, vi } from "vitest";

vi.mock("@/contexts/Sdk", () => ({
  useSlugs: () => ({ orgSlug: "org", projectSlug: "project" }),
}));

import { useRoutes } from "./routes";

function GuideHref(): JSX.Element {
  const routes = useRoutes();
  return <output>{routes.guide.href()}</output>;
}

describe("project routes", () => {
  it("exposes the standalone guide route", () => {
    render(
      <MemoryRouter initialEntries={["/org/projects/project"]}>
        <GuideHref />
      </MemoryRouter>,
    );

    expect(screen.getByText("/org/projects/project/guide")).toBeTruthy();
  });
});
