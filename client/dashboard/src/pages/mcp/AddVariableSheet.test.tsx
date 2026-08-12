import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import type { ComponentProps } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { TooltipProvider } from "@/components/ui/Tooltip";
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

function deferred<T>() {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>((resolvePromise) => {
    resolve = resolvePromise;
  });
  return { promise, resolve };
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

  it("ignores an older file read after a newer import finishes", async () => {
    const firstRead = deferred<string>();
    const firstFile = new File([""], "first.env", { type: "text/plain" });
    vi.spyOn(firstFile, "text").mockReturnValue(firstRead.promise);
    renderSheet();

    fireEvent.change(screen.getByLabelText("Import .env file"), {
      target: { files: [firstFile] },
    });
    expect(
      screen.getByRole("button", { name: "Save" }).hasAttribute("disabled"),
    ).toBe(true);

    fireEvent.change(screen.getByLabelText("Import .env file"), {
      target: {
        files: [
          new File(["LATEST_KEY=latest-value"], "latest.env", {
            type: "text/plain",
          }),
        ],
      },
    });
    expect(await screen.findByDisplayValue("LATEST_KEY")).toBeTruthy();
    expect(
      screen.getByRole("button", { name: "Save" }).hasAttribute("disabled"),
    ).toBe(false);

    firstRead.resolve("STALE_KEY=stale-value");
    await firstRead.promise;

    await waitFor(() => {
      expect(screen.queryByDisplayValue("STALE_KEY")).toBeNull();
      expect(screen.getByDisplayValue("LATEST_KEY")).toBeTruthy();
    });
  });

  it("ignores a file read that finishes after the sheet closes", async () => {
    const fileRead = deferred<string>();
    const file = new File([""], ".env", { type: "text/plain" });
    vi.spyOn(file, "text").mockReturnValue(fileRead.promise);
    renderSheet();

    fireEvent.change(screen.getByLabelText("Import .env file"), {
      target: { files: [file] },
    });
    fireEvent.click(screen.getByRole("button", { name: "Close" }));

    fileRead.resolve("STALE_KEY=stale-value");
    await fileRead.promise;

    await waitFor(() => {
      expect(screen.queryByDisplayValue("STALE_KEY")).toBeNull();
      expect(
        (screen.getByPlaceholderText("CLIENT_KEY...") as HTMLInputElement)
          .value,
      ).toBe("");
    });
  });
});
