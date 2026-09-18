import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { afterEach, describe, expect, it, vi } from "vitest";
import { ExportMap } from "./DataExports";

vi.mock("@/components/project-menu", () => ({
  ProjectAvatar: () => <span aria-hidden="true" />,
}));

vi.mock("@/components/require-scope", () => ({
  RequireScope: ({ children }: { children: React.ReactNode }) => children,
}));

vi.mock("@/components/ui/MoreActions", () => ({
  MoreActions: ({
    actions,
  }: {
    actions: Array<{ label: string; onClick: () => void }>;
  }) => (
    <>
      {actions.map((action) => (
        <button key={action.label} onClick={action.onClick}>
          {action.label}
        </button>
      ))}
    </>
  ),
}));

vi.mock("@/components/ui/Switch", () => ({
  Switch: ({
    checked,
    disabled,
    "aria-label": ariaLabel,
  }: {
    checked: boolean;
    disabled?: boolean;
    "aria-label"?: string;
  }) => (
    <input
      type="checkbox"
      checked={checked}
      disabled={disabled}
      aria-label={ariaLabel}
      readOnly
    />
  ),
}));

afterEach(() => {
  cleanup();
  vi.clearAllMocks();
});

type ExportRow = Parameters<typeof ExportMap>[0]["exports"][number];

const project = {
  id: "project-1",
  slug: "default",
  name: "Default",
} as ExportRow["project"];

const secondaryProject = {
  id: "project-2",
  slug: "secondary",
  name: "Secondary",
} as ExportRow["project"];

function exportRow({
  routeID,
  dataSource,
  destinationID,
  destinationName,
  project: rowProject = project,
}: {
  routeID: string;
  dataSource: string;
  destinationID: string;
  destinationName: string;
  project?: ExportRow["project"];
}): ExportRow {
  const timestamp = new Date(0);

  return {
    project: rowProject,
    route: {
      createdAt: timestamp,
      id: routeID,
      projectId: rowProject.id,
      dataSource: dataSource as ExportRow["route"]["dataSource"],
      enabled: true,
      otelDestinationId: destinationID,
      updatedAt: timestamp,
    },
    destination: {
      createdAt: timestamp,
      id: destinationID,
      projectId: rowProject.id,
      destinationType: "otel",
      name: destinationName,
      sensitiveData: "exclude",
      otel: {
        endpointUrl: `https://${destinationName.toLowerCase()}.example.com`,
        headers: [],
      },
      updatedAt: timestamp,
    },
  };
}

const callbacks = {
  mutating: false,
  onConfigure: vi.fn(),
  onConfigureDestination: vi.fn(),
  onToggle: vi.fn(),
  onDelete: vi.fn(),
};

describe("ExportMap", () => {
  it("renders one destination node for routes sharing a destination", () => {
    render(
      <MemoryRouter>
        <ExportMap
          exports={[
            exportRow({
              routeID: "route-telemetry",
              dataSource: "product_telemetry",
              destinationID: "destination-1",
              destinationName: "Clickstack",
            }),
            exportRow({
              routeID: "route-risk",
              dataSource: "risk_findings",
              destinationID: "destination-1",
              destinationName: "Clickstack",
              project: secondaryProject,
            }),
            exportRow({
              routeID: "route-tool-calls",
              dataSource: "tool_call_logs",
              destinationID: "destination-1",
              destinationName: "Clickstack",
            }),
          ]}
          descriptionLinks={{
            eventFeed: "/event-feed",
            riskPolicies: (candidate) =>
              `/projects/${candidate.slug}/risk-policies?tab=policies`,
            toolLogs: (candidate) => `/projects/${candidate.slug}/logs`,
          }}
          {...callbacks}
        />
      </MemoryRouter>,
    );

    expect(screen.getByText("Product telemetry")).toBeTruthy();
    expect(screen.getByText("Risk findings")).toBeTruthy();
    expect(screen.getByText("Tool call logs")).toBeTruthy();
    expect(
      screen.getByRole("link", { name: "Event Feed" }).getAttribute("href"),
    ).toBe("/event-feed");
    expect(
      screen.getByRole("link", { name: "risk policies" }).getAttribute("href"),
    ).toBe("/projects/secondary/risk-policies?tab=policies");
    expect(
      screen.getByRole("link", { name: "Tool Logs" }).getAttribute("href"),
    ).toBe("/projects/default/logs");
    expect(screen.getAllByText("Clickstack")).toHaveLength(1);
    expect(screen.getAllByText("https://clickstack.example.com")).toHaveLength(
      1,
    );

    fireEvent.click(
      screen.getByRole("button", {
        name: "Configure Product telemetry export",
      }),
    );
    expect(callbacks.onConfigure).toHaveBeenCalledWith(
      project,
      expect.objectContaining({ id: "route-telemetry" }),
    );

    fireEvent.click(
      screen.getByRole("button", {
        name: "Configure Clickstack destination",
      }),
    );
    expect(callbacks.onConfigureDestination).toHaveBeenCalledTimes(1);
    expect(callbacks.onConfigureDestination).toHaveBeenCalledWith(
      project,
      expect.objectContaining({ id: "destination-1" }),
    );
  });

  it("keeps separate destination nodes for different destination IDs", () => {
    render(
      <ExportMap
        exports={[
          exportRow({
            routeID: "route-a",
            dataSource: "product_telemetry",
            destinationID: "destination-1",
            destinationName: "Collector A",
          }),
          exportRow({
            routeID: "route-b",
            dataSource: "risk_findings",
            destinationID: "destination-2",
            destinationName: "Collector B",
          }),
        ]}
        {...callbacks}
      />,
    );

    expect(screen.getByText("Collector A")).toBeTruthy();
    expect(screen.getByText("Collector B")).toBeTruthy();
  });
});
