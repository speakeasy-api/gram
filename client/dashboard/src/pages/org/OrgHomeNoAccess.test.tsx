import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen } from "@testing-library/react";
import { MemoryRouter, Route, Routes, useParams } from "react-router";

import { OrgHomeNoAccess } from "./OrgHomeNoAccess";

type Project = { id: string; name: string; slug: string };
let projects: Project[] = [];

vi.mock("@/contexts/Auth", () => ({
  useOrganization: () => ({ name: "Acme", slug: "acme", projects }),
}));

function ProjectPage() {
  const { slug } = useParams();
  return <p data-testid="project-page">{slug}</p>;
}

function renderAtOrgHome() {
  render(
    <MemoryRouter initialEntries={["/acme"]}>
      <Routes>
        <Route path="/acme" element={<OrgHomeNoAccess />} />
        <Route path="/acme/projects/:slug" element={<ProjectPage />} />
      </Routes>
    </MemoryRouter>,
  );
}

const alpha = { id: "p-1", name: "Alpha", slug: "alpha" };
const beta = { id: "p-2", name: "Beta", slug: "beta" };

beforeEach(() => localStorage.clear());
afterEach(cleanup);

describe("OrgHomeNoAccess", () => {
  it("redirects to the last visited project", () => {
    projects = [alpha, beta];
    localStorage.setItem("preferredProject", "beta");
    renderAtOrgHome();
    expect(screen.getByTestId("project-page").textContent).toBe("beta");
  });

  it("falls back to the default project", () => {
    projects = [alpha, { id: "p-3", name: "Default", slug: "default" }, beta];
    localStorage.setItem("preferredProject", "gone");
    renderAtOrgHome();
    expect(screen.getByTestId("project-page").textContent).toBe("default");
  });

  it("redirects to the only readable project", () => {
    projects = [alpha];
    renderAtOrgHome();
    expect(screen.getByTestId("project-page").textContent).toBe("alpha");
  });

  it("ignores a stored project the viewer can no longer read", () => {
    projects = [alpha, beta];
    localStorage.setItem("preferredProject", "gone");
    renderAtOrgHome();
    expect(screen.getByText("Choose a project")).toBeTruthy();
    expect(screen.getByText("Alpha").closest("a")?.getAttribute("href")).toBe(
      "/acme/projects/alpha",
    );
  });

  it("tells a viewer with no projects who can add them", () => {
    projects = [];
    renderAtOrgHome();
    expect(
      screen.getByText("Ask an admin of Acme to add you to a project."),
    ).toBeTruthy();
  });
});
