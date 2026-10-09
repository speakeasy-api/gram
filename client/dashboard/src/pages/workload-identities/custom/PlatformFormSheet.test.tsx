import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import type { RegisterIssuerValues } from "../formValues";
import { PlatformFormSheet } from "./PlatformFormSheet";

vi.mock("@gram/client/react-query/workloadCustomFlows.js", async () => {
  const { customFlows } = await import("./customFlowsFixture");
  return {
    useWorkloadCustomFlows: () => ({
      data: customFlows,
      isPending: false,
      isError: false,
    }),
  };
});

afterEach(cleanup);

const STORED: RegisterIssuerValues = {
  name: "Example CI",
  description: "Deploy jobs",
  issuer: "https://identity.example.com",
  jwksUri: "https://identity.example.com/jwks",
  tags: ["ci"],
};

function renderSheet(initial?: RegisterIssuerValues) {
  const onSubmit =
    vi.fn<
      (
        values: RegisterIssuerValues,
        baseline: RegisterIssuerValues | undefined,
      ) => void
    >();
  render(
    <PlatformFormSheet
      open
      onOpenChange={() => {}}
      onSubmit={onSubmit}
      isPending={false}
      initial={initial}
    />,
  );
  return { onSubmit };
}

function field(label: string): HTMLInputElement {
  return screen.getByLabelText(label);
}

function button(name: string): HTMLButtonElement {
  return screen.getByRole("button", { name });
}

it("registers a platform once its values pass the checks", () => {
  const { onSubmit } = renderSheet();

  expect(screen.getByText("Register new access")).toBeTruthy();
  expect(button("Register").disabled).toBe(true);

  fireEvent.change(field("Name"), { target: { value: "Example CI" } });
  fireEvent.change(field("Issuer"), {
    target: { value: "http://identity.example.com" },
  });
  fireEvent.change(field("JWKS URI"), {
    target: { value: "https://identity.example.com/jwks" },
  });

  const problem = document.getElementById("workload-issuer-url-error");
  expect(problem).not.toBeNull();
  expect(problem!.textContent?.trim()).toBeTruthy();
  expect(field("Issuer").getAttribute("aria-invalid")).toBe("true");
  expect(button("Register").disabled).toBe(true);

  fireEvent.change(field("Issuer"), {
    target: { value: "https://identity.example.com" },
  });
  expect(document.getElementById("workload-issuer-url-error")).toBeNull();

  fireEvent.click(button("Register"));
  expect(onSubmit).toHaveBeenCalledWith(
    {
      name: "Example CI",
      description: "",
      issuer: "https://identity.example.com",
      jwksUri: "https://identity.example.com/jwks",
      tags: [],
    },
    undefined,
  );
});

it("refuses a name past the server's limit", () => {
  renderSheet();

  fireEvent.change(field("Name"), { target: { value: "x".repeat(101) } });

  expect(
    document.getElementById("workload-issuer-name-error")?.textContent,
  ).toBe("At most 100 characters.");
});

it("shows the issuer URL read-only when editing", () => {
  renderSheet(STORED);

  expect(screen.getByText("Edit platform")).toBeTruthy();
  expect(field("Issuer").readOnly).toBe(true);
  expect(field("Issuer").value).toBe("https://identity.example.com");
  expect(field("Name").readOnly).toBe(false);
});

it("saves nothing until a field changes", () => {
  const { onSubmit } = renderSheet(STORED);

  expect(button("Save changes").disabled).toBe(true);

  fireEvent.change(field("Name"), { target: { value: "Renamed CI" } });
  expect(button("Save changes").disabled).toBe(false);

  fireEvent.click(button("Save changes"));
  expect(onSubmit).toHaveBeenCalledWith(
    { ...STORED, name: "Renamed CI" },
    STORED,
  );
});
