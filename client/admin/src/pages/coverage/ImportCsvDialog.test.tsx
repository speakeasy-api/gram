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

  it("keeps invalid uploads and save failures in the dialog without reporting success", async () => {
    const onImport = vi
      .fn()
      .mockRejectedValue(
        new Error("Support matrix changed. Reload before saving."),
      );
    render(
      <ImportCsvDialog
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
