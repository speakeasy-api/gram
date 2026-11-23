import { cleanup, render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { afterEach, describe, expect, it, vi } from "vitest";
import { DetailFrame } from "./detail-frame";

vi.mock("@/components/page-layout", () => {
  const Pass = ({ children }: { children?: React.ReactNode }) => (
    <>{children}</>
  );
  const Header = Object.assign(Pass, { Breadcrumbs: () => null });
  return { Page: Object.assign(Pass, { Header, Body: Pass }) };
});

afterEach(cleanup);

function renderFrame(props: {
  header?: React.ReactNode;
  tabsAside?: React.ReactNode;
}) {
  const { container } = render(
    <MemoryRouter>
      <DetailFrame
        breadcrumbs={{ substitutions: {} }}
        tabs={[{ key: "one", title: "One", href: "/one" }]}
        activeTab="one"
        tabsLabel="Sections"
        {...props}
      >
        {null}
      </DetailFrame>
    </MemoryRouter>,
  );
  return container;
}

describe("DetailFrame", () => {
  it("draws the header band for a header", () => {
    const container = renderFrame({ header: <p>Title</p> });
    expect(container.querySelector("header")).not.toBeNull();
  });

  it("drops the header band for a missing or null header", () => {
    expect(renderFrame({}).querySelector("header")).toBeNull();
    cleanup();
    expect(renderFrame({ header: null }).querySelector("header")).toBeNull();
  });

  it("puts tabsAside on the tab strip", () => {
    renderFrame({ tabsAside: <button type="button">Switch</button> });
    const strip = screen.getByRole("navigation", { name: "Sections" });
    expect(strip.textContent).toContain("Switch");
    expect(strip.textContent).toContain("One");
  });
});
