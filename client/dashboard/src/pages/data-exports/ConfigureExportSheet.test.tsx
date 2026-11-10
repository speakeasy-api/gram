import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { DataExportRoute } from "@gram/client/models/components/dataexportroute.js";
import type { ProjectEntry } from "@gram/client/models/components/projectentry.js";
import {
  ConfigureExportSheet,
  type ConfigureExportValues,
  type OtelDataExportDestination,
} from "./ConfigureExportSheet";

vi.mock("@/components/require-scope", () => ({
  RequireScope: ({ children }: { children: React.ReactNode }) => children,
}));
vi.mock("@/components/project-menu", () => ({ ProjectAvatar: () => null }));

vi.mock("@/components/ui/Sheet", () => ({
  Sheet: ({ children }: { children: React.ReactNode }) => children,
  SheetContent: ({ children }: { children: React.ReactNode }) => (
    <div>{children}</div>
  ),
  SheetDescription: ({ children }: { children: React.ReactNode }) => (
    <p>{children}</p>
  ),
  SheetFooter: ({ children }: { children: React.ReactNode }) => (
    <footer>{children}</footer>
  ),
  SheetHeader: ({ children }: { children: React.ReactNode }) => (
    <header>{children}</header>
  ),
  SheetTitle: ({ children }: { children: React.ReactNode }) => (
    <h2>{children}</h2>
  ),
}));

const project = {
  id: "project-1",
  name: "Default",
  slug: "default",
} as ProjectEntry;

const destination = {
  createdAt: new Date(0),
  destinationType: "otel",
  id: "destination-1",
  name: "Collector",
  otel: { endpointUrl: "https://collector.example.com", headers: [] },
  projectId: "project-1",
  sensitiveData: "exclude",
  updatedAt: new Date(0),
} satisfies OtelDataExportDestination;

const sources: Array<{
  value: ConfigureExportValues["dataSource"];
  label: string;
  description: string;
}> = [
  {
    value: "mcp_connection_logs",
    label: "MCP connection logs",
    description: "Connections",
  },
  {
    value: "product_telemetry",
    label: "Product telemetry",
    description: "Telemetry",
  },
];

function route(
  dataSource: string,
  minSeverity?: DataExportRoute["minSeverity"],
): DataExportRoute {
  return {
    id: "route-1",
    projectId: "project-1",
    dataSource,
    enabled: true,
    otelDestinationId: "destination-1",
    minSeverity,
    createdAt: new Date(0),
    updatedAt: new Date(0),
  } as DataExportRoute;
}

function renderSheet(
  exportRoute: DataExportRoute,
  onSave: (values: ConfigureExportValues) => Promise<void>,
) {
  return render(
    <ConfigureExportSheet
      projects={[project]}
      project={project}
      dataSources={sources}
      destinations={[destination]}
      route={exportRoute}
      saving={false}
      loading={false}
      onClose={() => {}}
      onProjectChange={() => {}}
      onSave={onSave}
    />,
  );
}

describe("ConfigureExportSheet level", () => {
  afterEach(cleanup);

  it("shows the stored level for MCP connection logs and saves a new one", async () => {
    const onSave = vi.fn<(values: ConfigureExportValues) => Promise<void>>();
    onSave.mockResolvedValue(undefined);
    renderSheet(route("mcp_connection_logs", "warn"), onSave);

    expect(
      screen
        .getByRole("radio", { name: /^Failed and blocked connections/ })
        .getAttribute("aria-checked"),
    ).toBe("true");

    fireEvent.click(
      screen.getByRole("radio", {
        name: /^Failed, blocked and declined connections/,
      }),
    );
    fireEvent.click(screen.getByRole("button", { name: "Save export" }));

    await waitFor(() => expect(onSave).toHaveBeenCalledTimes(1));
    expect(onSave.mock.calls[0]?.[0].minSeverity).toBe("info");
  });

  it("defaults to failed connections when the route has no level", () => {
    renderSheet(route("mcp_connection_logs"), vi.fn());

    expect(
      screen
        .getByRole("radio", { name: /Failed connections \(recommended\)/ })
        .getAttribute("aria-checked"),
    ).toBe("true");
  });

  it("has no level for other data sources", () => {
    renderSheet(route("product_telemetry"), vi.fn());

    expect(screen.queryByRole("radio")).toBeNull();
  });
});
