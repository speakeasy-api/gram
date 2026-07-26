import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import type { ComponentProps } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { TooltipProvider } from "@/components/ui/tooltip";
import { AddVariableSheet } from "./AddVariableSheet";

afterEach(cleanup);

type OnAddVariables = ComponentProps<typeof AddVariableSheet>["onAddVariables"];

function renderSheet(onAddVariables: OnAddVariables = () => {}) {
  render(
    <TooltipProvider>
      <AddVariableSheet
        open
        onOpenChange={() => {}}
        attachedEnvironment={null}
        availableEnvVarsFromAttached={[]}
        onAddVariables={onAddVariables}
        onLoadFromEnvironment={() => {}}
      />
    </TooltipProvider>,
  );
}

describe("AddVariableSheet", () => {
  it("imports variables from a selected dotenv file", async () => {
    const onAddVariables = vi.fn<OnAddVariables>();
    renderSheet(onAddVariables);

    const file = new File(
      ["# credentials\nAPI_KEY=secret-value\nBASE_URL=https://example.test"],
      ".env",
      { type: "text/plain" },
    );
    fireEvent.change(screen.getByLabelText("Import .env file"), {
      target: { files: [file] },
    });

    await waitFor(() => {
      expect(screen.getByDisplayValue("API_KEY")).toBeTruthy();
      expect(screen.getByDisplayValue("BASE_URL")).toBeTruthy();
    });

    fireEvent.click(screen.getByRole("button", { name: "Save" }));

    expect(onAddVariables).toHaveBeenCalledWith([
      {
        key: "API_KEY",
        value: "secret-value",
        state: "system",
        isSecret: true,
      },
      {
        key: "BASE_URL",
        value: "https://example.test",
        state: "system",
        isSecret: true,
      },
    ]);
  });

  it("reports files without dotenv assignments", async () => {
    renderSheet();

    fireEvent.change(screen.getByLabelText("Import .env file"), {
      target: {
        files: [new File(["API_KEY"], ".env", { type: "text/plain" })],
      },
    });

    expect(
      await screen.findByText(
        "No valid environment variable assignments found.",
      ),
    ).toBeTruthy();
  });
});
