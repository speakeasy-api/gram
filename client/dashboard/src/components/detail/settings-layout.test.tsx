import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { useState } from "react";
import { Link, MemoryRouter, Route, Routes } from "react-router";
import { afterEach, describe, expect, it } from "vitest";
import { SettingsLayout, useReportUnsavedChanges } from "./settings-layout";
import { SettingsSection } from "./settings-section";

function NameField(): JSX.Element {
  const [value, setValue] = useState("Saved name");
  useReportUnsavedChanges(value !== "Saved name");
  return (
    <input
      aria-label="Display name"
      value={value}
      onChange={(event) => setValue(event.target.value)}
    />
  );
}

function renderSettings(): void {
  render(
    <MemoryRouter initialEntries={["/mcp/server/settings"]}>
      <Routes>
        <Route
          path="/mcp/server/settings"
          element={
            <>
              <Link to="/mcp/server/overview">Overview</Link>
              <SettingsLayout
                groups={[
                  {
                    id: "settings-general",
                    title: "General",
                    content: <NameField />,
                  },
                ]}
              />
            </>
          }
        />
        <Route path="/mcp/server/overview" element={<h1>Overview tab</h1>} />
      </Routes>
    </MemoryRouter>,
  );
}

afterEach(cleanup);

describe("SettingsLayout unsaved changes", () => {
  it("leaves at once when nothing is edited", () => {
    renderSettings();
    fireEvent.click(screen.getByRole("link", { name: "Overview" }));
    expect(screen.getByRole("heading", { name: "Overview tab" })).toBeTruthy();
  });

  it("asks before leaving with an edit, and stays on Keep editing", () => {
    renderSettings();
    fireEvent.change(screen.getByLabelText("Display name"), {
      target: { value: "New name" },
    });
    fireEvent.click(screen.getByRole("link", { name: "Overview" }));

    expect(
      screen.getByRole("dialog", { name: "Discard unsaved changes?" }),
    ).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Keep editing" }));
    expect(screen.queryByRole("heading", { name: "Overview tab" })).toBeNull();
    expect(
      (screen.getByLabelText("Display name") as HTMLInputElement).value,
    ).toBe("New name");
  });

  it("leaves on Discard", () => {
    renderSettings();
    fireEvent.change(screen.getByLabelText("Display name"), {
      target: { value: "New name" },
    });
    fireEvent.click(screen.getByRole("link", { name: "Overview" }));
    fireEvent.click(screen.getByRole("button", { name: "Discard" }));

    expect(screen.getByRole("heading", { name: "Overview tab" })).toBeTruthy();
  });

  it("stops asking once the edit is undone", () => {
    renderSettings();
    const input = screen.getByLabelText("Display name");
    fireEvent.change(input, { target: { value: "New name" } });
    fireEvent.change(input, { target: { value: "Saved name" } });
    fireEvent.click(screen.getByRole("link", { name: "Overview" }));

    expect(screen.getByRole("heading", { name: "Overview tab" })).toBeTruthy();
  });
});

function Section({ id, title }: { id?: string; title: string }): JSX.Element {
  return (
    <SettingsSection id={id}>
      <SettingsSection.Header>
        <SettingsSection.Title>{title}</SettingsSection.Title>
      </SettingsSection.Header>
    </SettingsSection>
  );
}

describe("SettingsLayout index", () => {
  it("lists the section titles that render as one flat list, with no area labels", () => {
    render(
      <MemoryRouter>
        <SettingsLayout
          groups={[
            {
              id: "settings-general",
              title: "General",
              content: (
                <>
                  <Section title="Availability" />
                  <Section id="name" title="Name and icon" />
                </>
              ),
            },
            {
              id: "settings-access",
              title: "Access",
              content: <Section title="Visibility" />,
            },
            { id: "settings-empty", title: "Empty", content: null },
          ]}
        />
      </MemoryRouter>,
    );
    const index = screen.getByRole("navigation", { name: "Settings sections" });
    expect(
      Array.from(index.querySelectorAll("a")).map((link) => [
        link.textContent,
        link.getAttribute("href"),
      ]),
    ).toEqual([
      ["Availability", "#settings-general"],
      ["Name and icon", "#name"],
      ["Visibility", "#settings-access"],
      ["Empty", "#settings-empty"],
    ]);
    expect(index.textContent).not.toContain("General");
    expect(index.textContent).not.toContain("Access");
    expect(index.querySelectorAll("ul ul")).toHaveLength(0);
    // A lone section's heading names its area, so the eyebrow hides on wide screens.
    expect(
      document.getElementById("settings-access-title")?.className,
    ).toContain("lg:sr-only");
    expect(
      document.getElementById("settings-general-title")?.className,
    ).not.toContain("lg:sr-only");
  });
});
