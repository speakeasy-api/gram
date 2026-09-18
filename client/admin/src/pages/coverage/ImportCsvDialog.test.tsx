import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { ImportCsvDialog } from "./ImportCsvDialog";
import { importCsvHeader } from "./importCsv";

const catalog = {
  methods: [{ id: "device", name: "Device Agent" }],
  products: [{ id: "claude-code-cli", name: "Claude Code CLI" }],
  capabilities: [{ id: "session", name: "Session tracking" }],
};
const validCsv = `${importCsvHeader}\nreference,device,,session,supported,via hooks,false,,`;
afterEach(cleanup);

async function upload(text: string) {
  const file = new File([text], "matrix.csv", { type: "text/csv" });
  Object.defineProperty(file, "text", { value: () => Promise.resolve(text) });
  fireEvent.change(screen.getByLabelText("2. Upload the formatted CSV"), {
    target: { files: [file] },
  });
  await waitFor(() => expect(screen.queryByText("Reading CSV…")).toBeNull());
}

describe("ImportCsvDialog", () => {
  it("validates the file and waits for a successful save before closing", async () => {
    let complete: (() => void) | undefined;
    const onImport = vi.fn(
      () =>
        new Promise<void>((resolve) => {
          complete = resolve;
        }),
    );
    render(
      <ImportCsvDialog
        revision="revision-1"
        catalog={catalog}
        draft={{ mappings: {}, references: {} }}
        onImport={onImport}
      />,
    );
    fireEvent.click(screen.getByRole("button", { name: "Import CSV" }));
    expect(
      (
        screen.getByLabelText(
          "1. Format with your agent",
        ) as HTMLTextAreaElement
      ).value,
    ).toContain(importCsvHeader);
    await upload(validCsv);
    expect(screen.getByRole("status").textContent).toContain(
      "1 reference claims",
    );
    expect(onImport).not.toHaveBeenCalled();
    fireEvent.click(
      within(screen.getByRole("dialog")).getByRole("button", {
        name: "Import CSV",
      }),
    );
    expect(onImport).toHaveBeenCalledWith({
      mappings: {},
      references: {
        device: {
          session: { status: "supported", note: "via hooks", verify: false },
        },
      },
    });
    expect(
      screen
        .getByRole("button", { name: "Importing…" })
        .hasAttribute("disabled"),
    ).toBe(true);
    complete?.();
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
  });

  it("rejects a small import when the merged matrix exceeds the save limit", async () => {
    const onImport = vi.fn();
    const references = Object.fromEntries(
      Array.from({ length: 105 }, (_, index) => [
        `method-${index}`,
        {
          session: {
            status: "supported" as const,
            note: "a".repeat(10000),
            verify: false,
          },
        },
      ]),
    );
    render(
      <ImportCsvDialog
        revision="revision-1"
        catalog={catalog}
        draft={{ mappings: {}, references }}
        onImport={onImport}
      />,
    );
    fireEvent.click(screen.getByRole("button", { name: "Import CSV" }));
    await upload(validCsv);
    expect(screen.getByRole("alert").textContent).toContain("1 MB save limit");
    expect(screen.queryByText(/Ready to import/)).toBeNull();
    expect(
      within(screen.getByRole("dialog"))
        .getByRole("button", { name: "Import CSV" })
        .hasAttribute("disabled"),
    ).toBe(true);
    expect(onImport).not.toHaveBeenCalled();
  });

  it("keeps invalid uploads and save failures in the dialog without reporting success", async () => {
    const onImport = vi
      .fn()
      .mockRejectedValue(
        new Error("Support matrix changed. Reload before saving."),
      );
    render(
      <ImportCsvDialog
        revision="revision-1"
        catalog={catalog}
        draft={{ mappings: {}, references: {} }}
        onImport={onImport}
      />,
    );
    fireEvent.click(screen.getByRole("button", { name: "Import CSV" }));
    await upload("invalid,header");
    expect(screen.getByRole("alert").textContent).toContain("Expected header");
    expect(
      within(screen.getByRole("dialog"))
        .getByRole("button", { name: "Import CSV" })
        .hasAttribute("disabled"),
    ).toBe(true);
    expect(onImport).not.toHaveBeenCalled();
    await upload(validCsv);
    fireEvent.click(
      within(screen.getByRole("dialog")).getByRole("button", {
        name: "Import CSV",
      }),
    );
    await waitFor(() =>
      expect(screen.getByRole("alert").textContent).toContain(
        "Support matrix changed",
      ),
    );
    expect(screen.getByRole("dialog")).toBeTruthy();
  });
});
