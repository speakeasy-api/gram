import type { ReactNode } from "react";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import type { ConfigureExportValues } from "./ConfigureExportSheet";
import DataExports from "./DataExports";

const state = vi.hoisted(() => ({
  dataSource: "product_telemetry",
  submittedSource: "product_telemetry",
  mutateAsync: vi.fn(),
}));
vi.mock("@/contexts/Auth", () => ({
  useOrganization: () => ({ id: "org-test", slug: "example" }),
}));
vi.mock("@gram/client/react-query/_context.js", () => ({
  useGramContext: () => ({}),
}));
vi.mock("@gram/client/react-query/listProjects.js", () => ({
  useListProjects: () => ({
    data: {
      projects: [{ id: "project-test", slug: "default", name: "Default" }],
    },
  }),
}));
vi.mock("@gram/client/react-query/listDataExportsForOrg.js", () => ({
  buildListDataExportsForOrgQuery: () => ({ queryKey: ["exports"] }),
}));
vi.mock("@tanstack/react-query", () => ({
  useQueryClient: () => ({}),
  useQuery: () => ({
    data: {
      routes: [
        {
          id: "route-test",
          projectId: "project-test",
          dataSource: state.dataSource,
          enabled: true,
        },
      ],
      destinations: [],
    },
  }),
}));
vi.mock("@gram/client/react-query/createDataExportDestination.js", () => ({
  useCreateDataExportDestinationMutation: () => ({
    mutateAsync: state.mutateAsync,
  }),
}));
vi.mock("@gram/client/react-query/createDataExportRoute.js", () => ({
  useCreateDataExportRouteMutation: () => ({ mutateAsync: state.mutateAsync }),
}));
vi.mock("@gram/client/react-query/updateDataExportRoute.js", () => ({
  useUpdateDataExportRouteMutation: () => ({ mutateAsync: state.mutateAsync }),
}));
vi.mock("@gram/client/react-query/updateDataExportDestination.js", () => ({
  useUpdateDataExportDestinationMutation: () => ({}),
}));
vi.mock("@gram/client/react-query/deleteDataExportRoute.js", () => ({
  useDeleteDataExportRouteMutation: () => ({}),
}));
vi.mock("@/components/page-templates", () => ({
  SettingsPage: ({ children }: { children: ReactNode }) => children,
}));
vi.mock("@/components/project-menu", () => ({ ProjectAvatar: () => null }));
vi.mock("@/components/require-scope", () => ({
  RequireScope: ({ children }: { children: ReactNode }) => children,
}));
vi.mock("./ConfigureExportSheet", () => ({
  ConfigureExportSheet: ({
    onSave,
  }: {
    onSave: (values: ConfigureExportValues) => Promise<void>;
  }) => (
    <button
      onClick={() =>
        void onSave({
          projectSlug: "default",
          dataSource:
            state.submittedSource as ConfigureExportValues["dataSource"],
          destinationId: "new",
          destinationName: "Example destination",
          endpointUrl: "https://collector.example.com",
          enabled: true,
          includeSensitiveData: false,
          headers: [],
        })
      }
    >
      Save export
    </button>
  ),
}));
beforeEach(() => {
  state.dataSource = "product_telemetry";
  state.submittedSource = "product_telemetry";
  state.mutateAsync.mockReset();
});
afterEach(cleanup);

it.each(["draft", "refreshed route"])(
  "does not create a destination when the %s has an unknown source",
  (source) => {
    const ui = () => (
      <MemoryRouter>
        <DataExports />
      </MemoryRouter>
    );
    const { rerender } = render(ui());
    fireEvent.click(
      screen.getByRole("button", {
        name: "Configure Product telemetry export",
      }),
    );
    if (source === "draft") state.submittedSource = "future_source";
    else state.dataSource = "future_source";
    rerender(ui());
    fireEvent.click(screen.getByRole("button", { name: "Save export" }));
    expect(state.mutateAsync).not.toHaveBeenCalled();
  },
);
