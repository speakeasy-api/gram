import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import {
  ConfigureDestinationSheet,
  type ConfigureDestinationValues,
} from "./ConfigureDestinationSheet";
import type { OtelDataExportDestination } from "./ConfigureExportSheet";

vi.mock("@/components/require-scope", () => ({
  RequireScope: ({ children }: { children: React.ReactNode }) => children,
}));

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

const destination = {
  createdAt: new Date(0),
  destinationType: "otel",
  id: "destination-1",
  name: "Clickstack",
  otel: {
    endpointUrl: "https://clickstack.example.com",
    headers: [{ name: "Authorization", hasValue: true }],
  },
  projectId: "project-1",
  sensitiveData: "exclude",
  updatedAt: new Date(0),
} satisfies OtelDataExportDestination;

describe("ConfigureDestinationSheet", () => {
  it("prefills and saves an existing destination without exposing stored header values", async () => {
    const onClose = vi.fn<() => void>();
    const onSave =
      vi.fn<(values: ConfigureDestinationValues) => Promise<void>>();
    onSave.mockResolvedValue(undefined);

    render(
      <ConfigureDestinationSheet
        destination={destination}
        saving={false}
        onClose={onClose}
        onSave={onSave}
      />,
    );

    expect(screen.getByDisplayValue("Clickstack")).toBeTruthy();
    expect(
      screen.getByDisplayValue("https://clickstack.example.com"),
    ).toBeTruthy();
    expect(screen.getByDisplayValue("Authorization")).toBeTruthy();
    expect(screen.queryByDisplayValue(/Bearer|token/i)).toBeNull();

    fireEvent.change(screen.getByLabelText("Destination name"), {
      target: { value: "Primary collector" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Save destination" }));

    await waitFor(() => expect(onSave).toHaveBeenCalledTimes(1));
    expect(onSave).toHaveBeenCalledWith({
      destinationName: "Primary collector",
      endpointUrl: "https://clickstack.example.com",
      includeSensitiveData: false,
      headers: [
        {
          rowID: "existing-0-Authorization",
          name: "Authorization",
          storedName: "Authorization",
          hasStoredValue: true,
          value: "",
        },
      ],
    });
  });
});
