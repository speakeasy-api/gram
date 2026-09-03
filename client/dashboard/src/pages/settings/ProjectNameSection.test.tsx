import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import type { SessionInfoResponse } from "@gram/client/models/operations/sessioninfo.js";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const testState = vi.hoisted(() => ({
  mutate: vi.fn(),
  mutationError: null as Error | null,
  mutationIsError: false,
  mutationIsPending: false,
  mutationOptions: undefined as
    | {
        onSuccess?: (data: {
          project: { name: string; slug: string };
        }) => Promise<void>;
      }
    | undefined,
  organization: { refetch: vi.fn() },
  project: {
    id: "project-1",
    name: "Current project",
    slug: "current-project",
  },
  session: { session: "session-token" },
  setQueriesData: vi.fn(),
}));

const invalidateAllListProjects = vi.hoisted(() => vi.fn());

vi.mock("@/contexts/Auth", () => ({
  useOrganization: () => testState.organization,
  useProject: () => testState.project,
  useSession: () => testState.session,
}));

vi.mock("@/components/require-scope", () => ({
  RequireScope: ({
    children,
    resourceId,
  }: {
    children: React.ReactNode;
    resourceId?: string;
  }) => (
    <div data-testid="require-scope" data-resource-id={resourceId}>
      {children}
    </div>
  ),
}));

vi.mock("@gram/client/react-query/listProjects", () => ({
  invalidateAllListProjects,
}));

vi.mock("@gram/client/react-query/updateProject", () => ({
  useUpdateProjectMutation: (options: typeof testState.mutationOptions) => {
    testState.mutationOptions = options;
    return {
      error: testState.mutationError,
      isError: testState.mutationIsError,
      isPending: testState.mutationIsPending,
      mutate: testState.mutate,
      reset: vi.fn(),
    };
  },
}));

vi.mock("@tanstack/react-query", () => ({
  useQueryClient: () => ({ setQueriesData: testState.setQueriesData }),
}));

vi.mock("sonner", () => ({ toast: { success: vi.fn() } }));

import { ProjectNameSection } from "./ProjectNameSection";

beforeEach(() => {
  testState.mutate.mockReset();
  testState.mutationError = null;
  testState.mutationIsError = false;
  testState.mutationIsPending = false;
  testState.mutationOptions = undefined;
  testState.organization.refetch.mockReset();
  testState.setQueriesData.mockReset();
  testState.project = {
    id: "project-1",
    name: "Current project",
    slug: "current-project",
  };
  invalidateAllListProjects.mockReset();
});

afterEach(cleanup);

describe("ProjectNameSection", () => {
  it("trims the display name and tracks unsaved changes", async () => {
    render(<ProjectNameSection />);

    const nameInput = screen.getByLabelText("Display Name");
    const slugInput = screen.getByLabelText("Slug");
    const save = screen.getByRole("button", { name: "Save" });

    expect((slugInput as HTMLInputElement).disabled).toBe(true);
    expect((save as HTMLButtonElement).disabled).toBe(true);

    fireEvent.change(nameInput, { target: { value: "  Renamed project  " } });

    expect((save as HTMLButtonElement).disabled).toBe(false);
    fireEvent.click(save);

    await waitFor(() =>
      expect(testState.mutate).toHaveBeenCalledWith({
        request: {
          updateProjectForm: { name: "Renamed project" },
        },
        security: {
          projectSlugHeaderGramProject: "current-project",
          sessionHeaderGramSession: "session-token",
        },
      }),
    );

    testState.organization.refetch.mockResolvedValue(undefined);
    await act(async () => {
      await testState.mutationOptions?.onSuccess?.({
        project: { name: "Renamed project", slug: "current-project" },
      });
    });

    expect((nameInput as HTMLInputElement).value).toBe("Renamed project");
    expect((save as HTMLButtonElement).disabled).toBe(true);
    expect(testState.organization.refetch).toHaveBeenCalledTimes(1);
    expect(invalidateAllListProjects).toHaveBeenCalledTimes(1);
    expect(testState.setQueriesData).toHaveBeenCalledWith(
      { queryKey: ["@gram/client", "auth", "info"] },
      expect.any(Function),
    );

    const session: SessionInfoResponse = {
      headers: {},
      result: {
        activeOrganizationId: "organization-1",
        gramAccountType: "user",
        hasActiveSubscription: true,
        isAdmin: false,
        organizationOverride: false,
        organizations: [
          {
            id: "organization-1",
            name: "Organization one",
            slug: "organization-one",
            projects: [
              {
                id: "project-1",
                name: "Current project",
                slug: "current-project",
              },
              {
                id: "project-2",
                name: "Sibling project",
                slug: "sibling-project",
              },
            ],
          },
          {
            id: "organization-2",
            name: "Organization two",
            slug: "organization-two",
            projects: [
              {
                id: "project-3",
                name: "Unrelated project",
                slug: "unrelated-project",
              },
            ],
          },
        ],
        trial: null,
        userEmail: "developer@example.com",
        userId: "user-1",
        whitelisted: true,
      },
    };
    const updateSession = testState.setQueriesData.mock.calls[0]?.[1] as (
      cachedSession: SessionInfoResponse | undefined,
    ) => SessionInfoResponse | undefined;

    const updatedSession = updateSession(session);
    expect(updatedSession?.result.organizations[0]?.projects).toEqual([
      {
        id: "project-1",
        name: "Renamed project",
        slug: "current-project",
      },
      {
        id: "project-2",
        name: "Sibling project",
        slug: "sibling-project",
      },
    ]);
    expect(updatedSession?.result.organizations[0]?.projects[1]).toBe(
      session.result.organizations[0]?.projects[1],
    );
    expect(updatedSession?.result.organizations[1]).toEqual(
      session.result.organizations[1],
    );
  });

  it("shows update failures once at form level", () => {
    testState.mutationError = new Error("Project name already exists");
    testState.mutationIsError = true;

    render(<ProjectNameSection />);

    expect(screen.getAllByText("Project name already exists")).toHaveLength(1);
    expect(
      screen.getByLabelText("Display Name").getAttribute("aria-invalid"),
    ).toBe("false");
    expect(
      screen.getByTestId("require-scope").getAttribute("data-resource-id"),
    ).toBe("project-1");
  });

  it("resets the form when the project changes", () => {
    const { rerender } = render(<ProjectNameSection />);

    fireEvent.change(screen.getByLabelText("Display Name"), {
      target: { value: "Unsaved name" },
    });
    testState.project = {
      id: "project-2",
      name: "Another project",
      slug: "another-project",
    };
    rerender(<ProjectNameSection />);

    expect(
      (screen.getByLabelText("Display Name") as HTMLInputElement).value,
    ).toBe("Another project");
    expect((screen.getByRole("button") as HTMLButtonElement).disabled).toBe(
      true,
    );
  });

  it("does not enable save for a blank display name", () => {
    render(<ProjectNameSection />);

    fireEvent.change(screen.getByLabelText("Display Name"), {
      target: { value: "   " },
    });

    expect((screen.getByRole("button") as HTMLButtonElement).disabled).toBe(
      true,
    );
    expect(testState.mutate).not.toHaveBeenCalled();
  });
});
