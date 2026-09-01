import { render, screen, fireEvent } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { PublicMcpWarningDialog } from "./public-mcp-warning-dialog";

const { useListToolSchemaStaticValues } = vi.hoisted(() => ({
  useListToolSchemaStaticValues: vi.fn(),
}));

vi.mock("@gram/client/react-query/listToolSchemaStaticValues.js", () => ({
  useListToolSchemaStaticValues,
}));

vi.mock("@/contexts/Sdk", () => ({
  useProjectSlugForRequests: () => "test-project",
}));

// moonshine's bundle imports lucide-react/dynamicIconImports which can't be
// resolved in the test environment (no package exports map). Mock the whole
// package so Button renders as a plain <button>.
vi.mock("@/components/ui/Button", () => ({
  Button: ({
    children,
    onClick,
    disabled,
  }: {
    children: React.ReactNode;
    onClick?: () => void;
    disabled?: boolean;
    variant?: string;
  }) => (
    <button onClick={onClick} disabled={disabled}>
      {children}
    </button>
  ),
}));

const defaultProps = {
  isOpen: true,
  onClose: vi.fn(),
  onConfirm: vi.fn(),
  toolsetSlug: "public-server",
  environmentSlug: "production",
  variableNames: ["STRIPE_API_KEY", "DATABASE_URL"],
};

describe("PublicMcpWarningDialog", () => {
  beforeEach(() => {
    useListToolSchemaStaticValues.mockReturnValue({
      isPending: false,
      isFetching: false,
      isError: false,
      isSuccess: true,
      data: {
        tools: [
          {
            toolUrn: "tools:weather",
            toolName: "get_weather",
            values: [
              {
                schemaPath: "/properties/api_key",
                keyword: "example",
                valueJson: '"example-value"',
              },
              {
                schemaPath: "",
                keyword: "const",
                valueJson: "null",
              },
              {
                schemaPath: "/properties/large_id",
                keyword: "const",
                valueJson: "9007199254740993",
              },
            ],
          },
        ],
      },
      refetch: vi.fn(),
    });
  });

  it("renders the warning, static values, and environment variables", () => {
    render(<PublicMcpWarningDialog {...defaultProps} />);

    expect(screen.getByText("Review public server values")).toBeTruthy();
    expect(screen.getByText(/We recommend you review/)).toBeTruthy();
    expect(screen.getByText(/from the Default Environment/)).toBeTruthy();
    expect(screen.getByText("STRIPE_API_KEY")).toBeTruthy();
    expect(screen.getByText("DATABASE_URL")).toBeTruthy();

    fireEvent.click(screen.getByRole("button", { name: /get_weather/ }));
    expect(screen.getByText("example")).toBeTruthy();
    expect(screen.getByText("/properties/api_key")).toBeTruthy();
    expect(screen.getByText('"example-value"')).toBeTruthy();
    expect(screen.getByText("(root)")).toBeTruthy();
    expect(screen.getByText("null")).toBeTruthy();
    expect(screen.getByText("9007199254740993")).toBeTruthy();

    expect(useListToolSchemaStaticValues).toHaveBeenCalledWith(
      { slug: "public-server", gramProject: "test-project" },
      undefined,
      expect.objectContaining({
        enabled: true,
        retry: false,
        throwOnError: false,
      }),
    );

    const link = screen.getByRole("link", {
      name: /Review in "Default Environment"/,
    });
    expect(link.getAttribute("href")).toBe("/environments/production");
    expect(link.getAttribute("target")).toBe("_blank");
  });

  it("fires onConfirm when the destructive action is clicked", () => {
    const onConfirm = vi.fn();
    render(
      <PublicMcpWarningDialog
        {...defaultProps}
        onConfirm={() => void onConfirm()}
      />,
    );
    fireEvent.click(screen.getByRole("button", { name: "Make public" }));
    expect(onConfirm).toHaveBeenCalledTimes(1);
  });

  it("blocks publishing until static values load", () => {
    useListToolSchemaStaticValues.mockReturnValue({
      isPending: true,
      isFetching: true,
      isError: false,
      isSuccess: false,
      data: undefined,
      refetch: vi.fn(),
    });

    render(<PublicMcpWarningDialog {...defaultProps} />);

    expect(
      (screen.getByRole("button", { name: "Make public" }) as HTMLButtonElement)
        .disabled,
    ).toBe(true);
  });

  it("fires onClose when Cancel is clicked", () => {
    const onClose = vi.fn();
    render(
      <PublicMcpWarningDialog
        {...defaultProps}
        onClose={() => void onClose()}
      />,
    );
    fireEvent.click(screen.getByRole("button", { name: "Cancel" }));
    expect(onClose).toHaveBeenCalledTimes(1);
  });
});
