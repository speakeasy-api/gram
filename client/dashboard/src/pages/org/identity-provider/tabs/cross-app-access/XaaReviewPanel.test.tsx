import type { ComponentProps } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";

import type { XaaConfirmHandler } from "./XaaConfirmFields";
import { XaaReviewPanel } from "./XaaReviewPanel";

type PanelProps = ComponentProps<typeof XaaReviewPanel>;

const defaults = {
  serverName: "Example server",
  confirmed: false,
  connectionsUrl: "https://example.okta.com/connections",
  createUrl: "https://example.okta.com/create",
  applicationsUrl: "https://example-admin.okta.com/admin/apps/active",
  applications: [{ id: "app", label: "Resource app" }],
  initialValues: {
    audience: "https://issuer.example.com/oauth2/resource",
    oktaApplicationId: "app",
  },
  pending: false,
};

function renderPanel(overrides: Partial<PanelProps> = {}) {
  const onConfirm = vi.fn<XaaConfirmHandler>();
  const onCancel = vi.fn<() => void>();
  const props: PanelProps = { ...defaults, onConfirm, onCancel, ...overrides };
  const view = render(<XaaReviewPanel {...props} />);
  return {
    onConfirm,
    onCancel,
    update: (next: Partial<PanelProps>) =>
      view.rerender(<XaaReviewPanel {...props} {...next} />),
  };
}

const issuerInput = () => screen.getByLabelText<HTMLInputElement>("Issuer URL");
const button = (name: string) => screen.getByRole("button", { name });

afterEach(cleanup);

describe("XaaReviewPanel", () => {
  it("guides reuse before creation and confirms a recovered explicit instance", () => {
    const { onConfirm } = renderPanel();
    expect(
      screen.getByRole("region", { name: "Review setup for Example server" }),
    ).toBeTruthy();
    expect(screen.getByText("Confirm this connection")).toBeTruthy();
    expect(screen.getByText(/it does not change or verify Okta/)).toBeTruthy();
    expect(
      screen.getByText(/Copy Issuer URL, not the separate Audience\/tenant ID/),
    ).toBeTruthy();
    expect(
      screen.getByText(/Check for an existing agent connection/).textContent,
    ).toContain("create a connection only if it is missing");
    for (const [name, href] of [
      ["Okta Applications", defaults.applicationsUrl],
      ["Open Okta connections", defaults.connectionsUrl],
      ["Create a connection", defaults.createUrl],
    ]) {
      const link = screen.getByRole("link", { name });
      expect(link.getAttribute("href")).toBe(href);
      expect(link.getAttribute("target")).toBe("_blank");
      expect(link.getAttribute("rel")).toContain("noopener");
    }
    expect(issuerInput().value).toBe(defaults.initialValues.audience);
    fireEvent.click(button("Confirm setup"));
    expect(onConfirm).toHaveBeenCalledWith(
      defaults.initialValues.audience,
      "app",
    );
  });

  it("prefills once and saves edits without resetting when props change", () => {
    const { onConfirm, update } = renderPanel({ confirmed: true });
    expect(screen.getByText("Review confirmation")).toBeTruthy();
    fireEvent.change(issuerInput(), {
      target: { value: "https://issuer.example.com/edited" },
    });
    update({ initialValues: { audience: "https://other.example.com" } });
    fireEvent.click(button("Save confirmation"));
    expect(onConfirm).toHaveBeenCalledWith(
      "https://issuer.example.com/edited",
      "app",
    );
  });

  it.each([true, false])(
    "retains a recorded app when snapshot includes it: %s",
    (available) => {
      const { onConfirm } = renderPanel({
        confirmed: true,
        applications: available ? defaults.applications : [],
      });
      expect(screen.getByText(/retains the current app/)).toBeTruthy();
      if (available) {
        fireEvent.keyDown(screen.getByRole("combobox"), { key: "ArrowDown" });
        expect(
          screen.queryByRole("option", { name: "Not recorded" }),
        ).toBeNull();
        fireEvent.click(
          screen.getByRole("option", { name: "Keep recorded app" }),
        );
      } else {
        expect(screen.getByRole("combobox").textContent).toContain(
          "Keep recorded app",
        );
        expect(screen.queryByRole("alert")).toBeNull();
      }
      fireEvent.click(button("Save confirmation"));
      expect(onConfirm).toHaveBeenCalledWith(
        defaults.initialValues.audience,
        undefined,
      );
    },
  );

  it("surfaces unavailable recovery instances and allows explicit omission", () => {
    const { onConfirm } = renderPanel({ applications: [] });
    expect(screen.getByRole("combobox").textContent).toContain(
      "Unavailable application",
    );
    expect(screen.getByRole("alert").textContent).toContain(
      "no longer available",
    );
    expect(button("Confirm setup").hasAttribute("disabled")).toBe(true);
    fireEvent.keyDown(issuerInput(), { key: "Enter" });
    expect(onConfirm).not.toHaveBeenCalled();
    fireEvent.keyDown(screen.getByRole("combobox"), { key: "ArrowDown" });
    fireEvent.click(screen.getByRole("option", { name: "Not recorded" }));
    fireEvent.click(button("Confirm setup"));
    expect(onConfirm).toHaveBeenCalledWith(
      defaults.initialValues.audience,
      undefined,
    );
  });

  it("allows cancellation but guards cancellation and submission while pending", () => {
    const { onConfirm, onCancel, update } = renderPanel();
    fireEvent.click(button("Cancel"));
    expect(onCancel).toHaveBeenCalledTimes(1);
    expect(onConfirm).not.toHaveBeenCalled();
    update({ pending: true });
    expect(button("Cancel").hasAttribute("disabled")).toBe(true);
    expect(issuerInput().hasAttribute("disabled")).toBe(true);
    expect(screen.getByRole("combobox").hasAttribute("disabled")).toBe(true);
    fireEvent.click(button("Cancel"));
    fireEvent.click(button("Recording..."));
    fireEvent.keyDown(issuerInput(), { key: "Enter" });
    expect(onCancel).toHaveBeenCalledTimes(1);
    expect(onConfirm).not.toHaveBeenCalled();
  });

  it("allows confirmation without an optional app or Okta links", () => {
    const { onConfirm } = renderPanel({
      confirmed: true,
      applicationsUrl: undefined,
      connectionsUrl: undefined,
      createUrl: undefined,
      initialValues: { audience: defaults.initialValues.audience },
    });
    expect(screen.queryByRole("link")).toBeNull();
    expect(screen.getByRole("combobox").textContent).toContain("Not recorded");
    fireEvent.click(button("Save confirmation"));
    expect(onConfirm).toHaveBeenCalledWith(
      defaults.initialValues.audience,
      undefined,
    );
  });
});
