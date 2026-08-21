import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";
import { MemberFacepile } from "./member-facepile";

afterEach(cleanup);

describe("MemberFacepile custom trigger", () => {
  it.each(["Enter", " "])("opens its roster with %s", (key) => {
    render(
      <MemberFacepile
        members={[
          {
            id: "member-1",
            name: "Test Member",
            email: "member@example.test",
          },
        ]}
        renderTrigger={({ label, onClick, onKeyDown }) => (
          <span
            role="button"
            tabIndex={0}
            aria-label={`Show ${label}`}
            onClick={onClick}
            onKeyDown={onKeyDown}
          >
            {label}
          </span>
        )}
      />,
    );

    fireEvent.keyDown(screen.getByRole("button", { name: "Show 1 member" }), {
      key,
    });

    expect(screen.getByText("member@example.test")).toBeTruthy();
  });
});
