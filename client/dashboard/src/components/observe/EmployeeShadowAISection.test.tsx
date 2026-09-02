import { render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { EmployeeShadowAISection } from "./EmployeeShadowAISection";

const { useEmployeeAIDetectionsMock } = vi.hoisted(() => ({
  useEmployeeAIDetectionsMock: vi.fn(),
}));

vi.mock("@gram/client/react-query/employeeAIDetections.js", () => ({
  useEmployeeAIDetections: useEmployeeAIDetectionsMock,
}));

describe("EmployeeShadowAISection", () => {
  beforeEach(() => {
    useEmployeeAIDetectionsMock.mockReset();
  });

  it("requests and renders detections for the canonical employee email", () => {
    useEmployeeAIDetectionsMock.mockReturnValue({
      data: {
        detections: [
          {
            category: "harness",
            deviceCount: 2,
            displayName: "Cursor",
            firstSeen: new Date("2026-08-01T10:00:00Z"),
            lastSeen: new Date("2026-08-31T10:00:00Z"),
            signals: ["installed", "running"],
            targetId: "cursor",
            userCount: 1,
            versions: ["1.7.49", "1.7.52"],
          },
        ],
      },
      error: null,
      isError: false,
      isPending: false,
    });

    render(<EmployeeShadowAISection userEmail="employee@example.com" />);

    expect(useEmployeeAIDetectionsMock).toHaveBeenCalledWith(
      { userEmail: "employee@example.com" },
      undefined,
      { enabled: true, throwOnError: false },
    );
    expect(screen.getByText("Shadow AI")).toBeTruthy();
    expect(screen.getByText("Cursor")).toBeTruthy();
    expect(screen.getByText("2 devices")).toBeTruthy();
    expect(screen.getByText("Running")).toBeTruthy();
    expect(screen.getByText("Installed")).toBeTruthy();
    expect(screen.getByText("1.7.49 · 1.7.52")).toBeTruthy();
  });

  it("distinguishes no detections from an unavailable identity", () => {
    useEmployeeAIDetectionsMock.mockReturnValue({
      data: { detections: [] },
      error: null,
      isError: false,
      isPending: false,
    });

    const { rerender } = render(
      <EmployeeShadowAISection userEmail="empty@example.com" />,
    );
    expect(screen.getByText("No detected AI tools")).toBeTruthy();

    rerender(<EmployeeShadowAISection userEmail={null} />);
    expect(screen.getByText("Shadow AI unavailable")).toBeTruthy();
    expect(useEmployeeAIDetectionsMock).toHaveBeenLastCalledWith(
      { userEmail: "" },
      undefined,
      {
        enabled: false,
        throwOnError: false,
      },
    );

    rerender(<EmployeeShadowAISection userEmail="" />);
    expect(useEmployeeAIDetectionsMock).toHaveBeenLastCalledWith(
      { userEmail: "" },
      undefined,
      {
        enabled: false,
        throwOnError: false,
      },
    );
  });
});
