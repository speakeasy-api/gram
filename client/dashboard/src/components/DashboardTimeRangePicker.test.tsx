import { render } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

// Capture the props the wrapper forwards to the Elements picker. The
// /chat/completions proxy requires BOTH Gram-Session and Gram-Project, so the
// wrapper must inject the request project slug (useProjectSlugForRequests)
// alongside the session header — callers that omit projectSlug otherwise 401
// and natural-language parsing silently no-ops.
const pickerProps = vi.fn();

vi.mock("@/elements", () => ({
  TimeRangePicker: (props: Record<string, unknown>) => {
    pickerProps(props);
    return null;
  },
}));

vi.mock("@/contexts/Auth", () => ({
  useSession: () => ({ session: "test-session-token" }),
}));

vi.mock("@/contexts/Sdk", () => ({
  useProjectSlugForRequests: () => "route-project",
}));

vi.mock("@/lib/utils", async (importOriginal) => ({
  ...(await importOriginal<Record<string, unknown>>()),
  getServerURL: () => "https://server.test",
}));

import { TimeRangePicker } from "./DashboardTimeRangePicker";

describe("DashboardTimeRangePicker", () => {
  beforeEach(() => {
    pickerProps.mockClear();
  });

  it("injects the request project slug when the caller does not pass one", () => {
    render(<TimeRangePicker />);

    expect(pickerProps).toHaveBeenCalledWith(
      expect.objectContaining({ projectSlug: "route-project" }),
    );
  });

  it("prefers an explicitly passed project slug over the request slug", () => {
    render(<TimeRangePicker projectSlug="explicit-project" />);

    expect(pickerProps).toHaveBeenCalledWith(
      expect.objectContaining({ projectSlug: "explicit-project" }),
    );
  });

  it("injects the server URL and session auth header", () => {
    render(<TimeRangePicker />);

    expect(pickerProps).toHaveBeenCalledWith(
      expect.objectContaining({
        apiUrl: "https://server.test",
        authHeaders: { "Gram-Session": "test-session-token" },
      }),
    );
  });
});
