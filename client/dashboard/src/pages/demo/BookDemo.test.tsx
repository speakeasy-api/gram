import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

vi.mock("@/contexts/Auth", () => ({
  useSessionData: () => ({
    session: {
      user: { email: "jane@acme.com" },
      organization: { id: "org-1", name: "Acme", slug: "acme" },
    },
  }),
}));

vi.mock("@/contexts/Sdk", () => ({
  useSdkClient: () => ({ auth: { logout: vi.fn() } }),
}));

vi.mock("@/contexts/Telemetry", () => ({
  useCaptureEnterpriseGateViewed: vi.fn(),
}));

vi.mock("@/pages/demo/components/DemoBookingFlow", () => ({
  DemoBookingFlow: () => <div data-testid="booking-flow" />,
}));

import BookDemo from "./BookDemo";

afterEach(cleanup);

describe("BookDemo", () => {
  it("renders a secondary CTA into the live demo org", () => {
    render(<BookDemo />);

    const cta = screen.getByRole("link", { name: "Explore a Live Demo" });
    expect(cta.getAttribute("href")).toBe("/explore-demo");
    expect(screen.queryByText(/or explore a live demo org/i)).toBeNull();
  });
});
