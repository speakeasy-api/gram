import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import {
  ConfigureExportSheet,
  type OtelDataExportDestination,
} from "./ConfigureExportSheet";

vi.mock("@/components/project-menu", () => ({
  ProjectAvatar: () => <span aria-hidden="true" />,
}));

vi.mock("@/components/require-scope", () => ({
  RequireScope: ({ children }: { children: React.ReactNode }) => children,
}));

vi.mock("@gram/client/react-query/testDataExportRoute.js", () => ({
  useTestDataExportRouteMutation: () => ({
    mutate: vi.fn(),
    data: undefined,
    error: undefined,
    isPending: false,
    isError: false,
  }),
}));

afterEach(() => {
  cleanup();
});

type SheetProps = Parameters<typeof ConfigureExportSheet>[0];

const project = {
  id: "project-1",
  slug: "default",
  name: "Default",
} as SheetProps["project"];
const timestamp = new Date(0);
const destination = {
  createdAt: timestamp,
  id: "destination-1",
  projectId: project.id,
  destinationType: "otel",
  name: "Datadog",
  sensitiveData: "exclude",
  otel: { endpointUrl: "https://otlp.example.com", headers: [] },
  updatedAt: timestamp,
} as OtelDataExportDestination;

function renderSheet(dataSource: string, otelDestinationId?: string) {
  render(
    <ConfigureExportSheet
      projects={[project]}
      project={project}
      dataSources={[
        {
          value: "mcp_connection_logs",
          label: "MCP connection logs",
          description: "",
        },
        {
          value: "product_telemetry",
          label: "Product telemetry",
          description: "",
        },
      ]}
      destinations={[destination]}
      route={{
        createdAt: timestamp,
        id: "route-1",
        projectId: project.id,
        dataSource: dataSource as NonNullable<
          SheetProps["route"]
        >["dataSource"],
        enabled: true,
        otelDestinationId,
        updatedAt: timestamp,
      }}
      saving={false}
      loading={false}
      onClose={() => {}}
      onProjectChange={() => {}}
      onSave={async () => {}}
    />,
  );
}

describe("ConfigureExportSheet test event", () => {
  it("offers a test event for a saved MCP connection logs export", () => {
    renderSheet("mcp_connection_logs", destination.id);
    expect(
      screen.getByRole("button", { name: "Send test event" }),
    ).toBeTruthy();
  });

  it("does not offer one for other data", () => {
    renderSheet("product_telemetry", destination.id);
    expect(
      screen.queryByRole("button", { name: "Send test event" }),
    ).toBeNull();
  });

  it("does not offer one without a destination", () => {
    renderSheet("mcp_connection_logs");
    expect(
      screen.queryByRole("button", { name: "Send test event" }),
    ).toBeNull();
  });
});
