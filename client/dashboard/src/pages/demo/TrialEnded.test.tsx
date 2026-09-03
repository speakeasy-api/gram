import { render, screen } from "@testing-library/react";
import { MemoryRouter, Route, Routes } from "react-router";
import { expect, it, vi } from "vitest";
import TrialEnded from "./TrialEnded";

const session = vi.hoisted(() => vi.fn());

vi.mock("@/contexts/Auth", () => ({
  useSessionData: session,
}));

it("redirects when the session has no expired trial", () => {
  session.mockReturnValue({ session: { trial: null } });

  render(
    <MemoryRouter initialEntries={["/trial-ended"]}>
      <Routes>
        <Route path="/" element={<div>Workspace</div>} />
        <Route path="/trial-ended" element={<TrialEnded />} />
      </Routes>
    </MemoryRouter>,
  );

  expect(screen.getByText("Workspace")).toBeDefined();
});
