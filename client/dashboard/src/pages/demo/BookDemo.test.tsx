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
  it("renders the booking flow without a CTA into the live demo org", () => {
    render(<BookDemo />);

    expect(screen.getByTestId("booking-flow")).toBeTruthy();
    expect(screen.queryByText(/explore a live demo/i)).toBeNull();
  });
});
