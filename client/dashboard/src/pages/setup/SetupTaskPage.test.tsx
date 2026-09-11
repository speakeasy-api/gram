import { cleanup, render, screen } from "@testing-library/react";
import { MemoryRouter, Route, Routes, useLocation } from "react-router";
import { afterEach, describe, expect, it, vi } from "vitest";
import SetupTaskPage from "./SetupTaskPage";
import { SETUP_TASK_SLUGS, canonicalSetupSearch } from "./task-slugs";

vi.mock("@/routes", () => ({
  useOrgRoutes: () => ({ setup: { href: () => "/org/setup" } }),
}));
afterEach(cleanup);
function Location() {
  const location = useLocation();
  return <output>{location.pathname + location.search + location.hash}</output>;
}
function renderLink(path: string) {
  render(
    <MemoryRouter initialEntries={[path]}>
      <Routes>
        <Route path="/org/setup/:taskSlug" element={<SetupTaskPage />} />
        <Route path="/org/setup" element={<Location />} />
      </Routes>
    </MemoryRouter>,
  );
}
describe("canonical setup links", () => {
  it.each(Object.entries(SETUP_TASK_SLUGS))(
    "normalizes %s from %s",
    (key, slug) => {
      renderLink(`/org/setup/${slug}?projectSlug=selected&step=2#details`);
      expect(screen.getByRole("status").textContent).toBe(
        `/org/setup?projectSlug=selected&step=2&task=${key}#details`,
      );
    },
  );
  it("keeps unknown paths unknown rather than choosing another task", () => {
    renderLink("/org/setup/not-real?projectSlug=selected");
    expect(screen.getByRole("status").textContent).toBe(
      "/org/setup?projectSlug=selected&task=not-real",
    );
  });
  it("honors canonical task over both legacy selectors", () => {
    renderLink(
      "/org/setup/idp?task=enable-logging&step=connect-idp&projectSlug=selected",
    );
    expect(screen.getByRole("status").textContent).toBe(
      "/org/setup?task=enable-logging&step=connect-idp&projectSlug=selected",
    );
  });
  it("normalizes task-valued step without mutating other parameters", () => {
    const input = new URLSearchParams(
      "step=enable-logging&projectSlug=selected&return=home",
    );
    const result = canonicalSetupSearch(input);
    expect(result.get("task")).toBe("enable-logging");
    expect(result.has("step")).toBe(false);
    expect(result.get("projectSlug")).toBe("selected");
    expect(result.get("return")).toBe("home");
    expect(input.get("step")).toBe("enable-logging");
  });
  it("preserves a section that shares a task key on a legacy task path", () => {
    renderLink(
      "/org/setup/anthropic-observability?step=confirm-traffic&projectSlug=selected#details",
    );
    expect(screen.getByRole("status").textContent).toBe(
      "/org/setup?step=confirm-traffic&projectSlug=selected&task=anthropic-observability#details",
    );
  });
  it.each(["2", "connect-cowork", "not-real"])(
    "does not reinterpret substep %s as a task",
    (step) => {
      const result = canonicalSetupSearch(new URLSearchParams({ step }));
      expect(result.has("task")).toBe(false);
      expect(result.get("step")).toBe(step);
    },
  );
});
