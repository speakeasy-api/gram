import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const testState = vi.hoisted(() => ({
  mutate: vi.fn(),
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
  navigate: vi.fn(),
  setQueriesData: vi.fn(),
}));

const invalidateAllListProjects = vi.hoisted(() => vi.fn());

vi.mock("@/contexts/Auth", () => ({
  useOrganization: () => testState.organization,
  useProject: () => testState.project,
}));

vi.mock("@/contexts/Sdk", () => ({ useSlugs: () => ({ orgSlug: "acme" }) }));

vi.mock("react-router", () => ({ useNavigate: () => testState.navigate }));

vi.mock("@/components/require-scope", () => ({
  RequireScope: ({ children }: { children: React.ReactNode }) => children,
}));

vi.mock("@gram/client/react-query/listProjects", () => ({
  invalidateAllListProjects,
}));

vi.mock("@gram/client/react-query/updateProject", () => ({
  useUpdateProjectMutation: (options: typeof testState.mutationOptions) => {
    testState.mutationOptions = options;
    return {
      error: null,
      isError: false,
      isPending: false,
      mutate: testState.mutate,
      reset: vi.fn(),
    };
  },
}));

vi.mock("@tanstack/react-query", () => ({
  useQueryClient: () => ({ setQueriesData: testState.setQueriesData }),
}));

vi.mock("sonner", () => ({ toast: { error: vi.fn(), success: vi.fn() } }));

import { ProjectNameSection } from "./ProjectNameSection";

beforeEach(() => {
  testState.mutate.mockReset();
  testState.mutationOptions = undefined;
  testState.organization.refetch.mockReset();
  testState.navigate.mockReset();
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

    const inputs = screen.getAllByRole("textbox");
    const nameInput = inputs[0]!;
    const slugInput = inputs[1]!;
    const save = screen.getByRole("button", { name: "Save" });

    expect((save as HTMLButtonElement).disabled).toBe(true);

    fireEvent.change(nameInput, { target: { value: "  Renamed project  " } });

    expect((save as HTMLButtonElement).disabled).toBe(false);

    fireEvent.click(save);

    await waitFor(() =>
      expect(testState.mutate).toHaveBeenCalledWith({
        request: {
          updateProjectForm: {
            name: "Renamed project",
            slug: "current-project",
          },
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
    expect((slugInput as HTMLInputElement).value).toBe("current-project");
    expect((save as HTMLButtonElement).disabled).toBe(true);
    expect(testState.organization.refetch).toHaveBeenCalledTimes(1);
    expect(invalidateAllListProjects).toHaveBeenCalledTimes(1);
    expect(testState.navigate).not.toHaveBeenCalled();
    expect(testState.setQueriesData).not.toHaveBeenCalled();
  });

  it("warns before changing the slug", async () => {
    render(<ProjectNameSection />);

    fireEvent.change(screen.getAllByRole("textbox")[1]!, {
      target: { value: "renamed-project" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Save" }));

    expect(testState.mutate).not.toHaveBeenCalled();
    expect((await screen.findByRole("dialog")).textContent).toContain(
      "break saved project links",
    );

    const confirm = screen.getByRole("button", { name: "Change slug" });
    expect(confirm.className).toContain("bg-btn-destructive");
    fireEvent.click(confirm);

    expect(testState.mutate).toHaveBeenCalledWith({
      request: {
        updateProjectForm: {
          name: "Current project",
          slug: "renamed-project",
        },
      },
    });
  });

  it("shows slug requirements only after validation fails", async () => {
    render(<ProjectNameSection />);

    const message =
      "Use only lowercase letters, numbers, dashes, and underscores.";
    expect(screen.queryByText(message)).toBeNull();

    fireEvent.change(screen.getAllByRole("textbox")[1]!, {
      target: { value: "Invalid slug!" },
    });

    expect(await screen.findByText(message)).not.toBeNull();
    expect(
      (screen.getByRole("button", { name: "Save" }) as HTMLButtonElement)
        .disabled,
    ).toBe(true);
  });

  it("resets the form when the project changes", () => {
    const { rerender } = render(<ProjectNameSection />);

    fireEvent.change(screen.getAllByRole("textbox")[0]!, {
      target: { value: "Unsaved name" },
    });
    testState.project = {
      id: "project-2",
      name: "Another project",
      slug: "another-project",
    };
    rerender(<ProjectNameSection />);

    const inputs = screen.getAllByRole("textbox");
    expect((inputs[0] as HTMLInputElement).value).toBe("Another project");
    expect((inputs[1] as HTMLInputElement).value).toBe("another-project");
    expect((screen.getByRole("button") as HTMLButtonElement).disabled).toBe(
      true,
    );
  });

  it("navigates after a slug change when project refresh fails", async () => {
    render(<ProjectNameSection />);
    testState.organization.refetch.mockRejectedValue(
      new Error("refresh failed"),
    );

    await act(async () => {
      await testState.mutationOptions?.onSuccess?.({
        project: { name: "Renamed project", slug: "renamed-project" },
      });
    });

    expect(testState.setQueriesData).toHaveBeenCalledWith(
      { queryKey: ["@gram/client", "auth", "info"] },
      expect.any(Function),
    );
    expect(testState.navigate).toHaveBeenCalledWith(
      "/acme/projects/renamed-project/settings",
      { replace: true },
    );
  });

  it("does not enable save for a blank display name", () => {
    render(<ProjectNameSection />);

    fireEvent.change(screen.getAllByRole("textbox")[0]!, {
      target: { value: "   " },
    });

    expect((screen.getByRole("button") as HTMLButtonElement).disabled).toBe(
      true,
    );
    expect(testState.mutate).not.toHaveBeenCalled();
  });

  it("does not allow changing the default project slug", () => {
    testState.project = { ...testState.project, slug: "default" };

    render(<ProjectNameSection />);

    const slugInput = screen.getAllByRole("textbox")[1]!;
    expect((slugInput as HTMLInputElement).disabled).toBe(true);
  });
});
