import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";

import { XaaConfirmBar } from "./XaaConfirmBar";

afterEach(cleanup);

describe("XaaConfirmBar", () => {
  it("blocks confirmation until a valid audience is entered", () => {
    const onConfirm =
      vi.fn<(audience: string, app: string | undefined) => void>();
    render(
      <XaaConfirmBar
        selectedCount={3}
        applications={[]}
        onConfirm={onConfirm}
        pending={false}
        error={undefined}
      />,
    );

    const button = screen.getByRole("button", {
      name: "Confirm 3 servers",
    });
    expect(button.hasAttribute("disabled")).toBe(true);

    const input = screen.getByLabelText("Issuer URL");
    fireEvent.change(input, { target: { value: "http://auth.example.com" } });
    expect(input.getAttribute("aria-invalid")).toBe("true");
    expect(button.hasAttribute("disabled")).toBe(true);
    expect(screen.getByRole("alert").textContent).toContain(
      "Enter the HTTPS Issuer URL",
    );
    fireEvent.keyDown(input, { key: "Enter" });
    expect(onConfirm).not.toHaveBeenCalled();

    fireEvent.change(input, { target: { value: "https://auth.example.com" } });
    expect(input.getAttribute("aria-invalid")).toBe("false");
    expect(button.hasAttribute("disabled")).toBe(false);

    fireEvent.click(button);
    expect(onConfirm).toHaveBeenCalledWith(
      "https://auth.example.com",
      undefined,
    );
  });

  it("shows the API error inline while keeping the form", () => {
    render(
      <XaaConfirmBar
        selectedCount={1}
        applications={[{ id: "0oa1", label: "Linear" }]}
        onConfirm={vi.fn<() => void>()}
        pending={false}
        error={{ statusCode: 412, message: "the connection is not verified" }}
      />,
    );

    expect(screen.getByText("Confirm 1 selected server")).toBeTruthy();
    expect(screen.getByText("Cannot continue")).toBeTruthy();
    expect(screen.getByText("the connection is not verified")).toBeTruthy();
    expect(
      screen.getByRole("button", { name: "Confirm 1 server" }),
    ).toBeTruthy();
  });
});

it("guards Enter while pending and freezes both inputs", () => {
  const onConfirm =
    vi.fn<(audience: string, app: string | undefined) => void>();
  const props = {
    selectedCount: 1,
    applications: [],
    onConfirm,
    pending: false,
    error: undefined,
  };
  const { rerender } = render(<XaaConfirmBar {...props} />);
  const input = screen.getByLabelText("Issuer URL");
  fireEvent.change(input, { target: { value: "https://auth.example.com" } });
  fireEvent.keyDown(input, { key: "Enter" });
  expect(onConfirm).toHaveBeenCalledTimes(1);
  rerender(<XaaConfirmBar {...props} pending />);
  expect(input.hasAttribute("disabled")).toBe(true);
  expect(screen.getByRole("combobox").hasAttribute("disabled")).toBe(true);
  fireEvent.keyDown(input, { key: "Enter" });
  expect(onConfirm).toHaveBeenCalledTimes(1);
  rerender(<XaaConfirmBar {...props} selectedCount={0} />);
  fireEvent.keyDown(input, { key: "Enter" });
  expect(onConfirm).toHaveBeenCalledTimes(1);
});

it("blocks a removed app instance until the administrator chooses again", () => {
  const onConfirm =
    vi.fn<(audience: string, app: string | undefined) => void>();
  const props = {
    selectedCount: 1,
    applications: [{ id: "app", label: "Resource app" }],
    onConfirm,
    pending: false,
    error: undefined,
  };
  const { rerender } = render(<XaaConfirmBar {...props} />);
  fireEvent.change(screen.getByLabelText("Issuer URL"), {
    target: { value: "https://issuer.example.com" },
  });
  fireEvent.keyDown(screen.getByRole("combobox"), { key: "ArrowDown" });
  fireEvent.click(screen.getByRole("option", { name: "Resource app" }));
  rerender(<XaaConfirmBar {...props} applications={[]} />);
  expect(screen.getByRole("alert").textContent).toContain(
    "no longer available",
  );
  expect(
    screen
      .getByRole("button", { name: "Confirm 1 server" })
      .hasAttribute("disabled"),
  ).toBe(true);
  fireEvent.keyDown(screen.getByLabelText("Issuer URL"), {
    key: "Enter",
  });
  expect(onConfirm).not.toHaveBeenCalled();
  fireEvent.keyDown(screen.getByRole("combobox"), { key: "ArrowDown" });
  fireEvent.click(screen.getByRole("option", { name: "Not recorded" }));
  fireEvent.click(screen.getByRole("button", { name: "Confirm 1 server" }));
  expect(onConfirm).toHaveBeenCalledWith(
    "https://issuer.example.com",
    undefined,
  );
});

describe("guided confirmation", () => {
  const props = {
    selectedCount: 1,
    applicationsUrl: "https://example-admin.okta.com/admin/apps/active",
    applications: [{ id: "app", label: "Resource app" }],
    pending: false,
    error: undefined,
    review: {
      serverName: "Example server",
      confirmed: false,
      connectionsUrl: "https://example.okta.com/connections",
      createUrl: "https://example.okta.com/create",
    },
    initialValues: {
      audience: "https://issuer.example.com/oauth2/resource",
      oktaApplicationId: "app",
    },
  };

  it("guides reuse before creation and confirms a recovered explicit instance", () => {
    const onConfirm =
      vi.fn<(audience: string, app: string | undefined) => void>();
    render(<XaaConfirmBar {...props} onConfirm={onConfirm} />);
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
      ["Okta Applications", props.applicationsUrl],
      ["Open Okta connections", props.review.connectionsUrl],
      ["Create a connection", props.review.createUrl],
    ]) {
      const link = screen.getByRole("link", { name });
      expect(link.getAttribute("href")).toBe(href);
      expect(link.getAttribute("target")).toBe("_blank");
      expect(link.getAttribute("rel")).toContain("noopener");
    }
    expect(
      (screen.getByLabelText("Issuer URL") as HTMLInputElement).value,
    ).toBe(props.initialValues.audience);
    fireEvent.click(screen.getByRole("button", { name: "Confirm setup" }));
    expect(onConfirm).toHaveBeenCalledWith(props.initialValues.audience, "app");
  });

  it("prefills once and saves edits without resetting when props change", () => {
    const onConfirm =
      vi.fn<(audience: string, app: string | undefined) => void>();
    const review = { ...props.review, confirmed: true };
    const { rerender } = render(
      <XaaConfirmBar {...props} review={review} onConfirm={onConfirm} />,
    );
    expect(screen.getByText("Review confirmation")).toBeTruthy();
    fireEvent.change(screen.getByLabelText("Issuer URL"), {
      target: { value: "https://issuer.example.com/edited" },
    });
    rerender(
      <XaaConfirmBar
        {...props}
        review={review}
        onConfirm={onConfirm}
        initialValues={{ audience: "https://other.example.com" }}
      />,
    );
    fireEvent.click(screen.getByRole("button", { name: "Save confirmation" }));
    expect(onConfirm).toHaveBeenCalledWith(
      "https://issuer.example.com/edited",
      "app",
    );
  });

  it.each([true, false])(
    "retains a recorded app when snapshot includes it: %s",
    (available) => {
      const onConfirm =
        vi.fn<(audience: string, app: string | undefined) => void>();
      render(
        <XaaConfirmBar
          {...props}
          applications={available ? props.applications : []}
          review={{ ...props.review, confirmed: true }}
          onConfirm={onConfirm}
        />,
      );
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
      fireEvent.click(
        screen.getByRole("button", { name: "Save confirmation" }),
      );
      expect(onConfirm).toHaveBeenCalledWith(
        props.initialValues.audience,
        undefined,
      );
    },
  );

  it("surfaces unavailable recovery instances and allows explicit omission", () => {
    const onConfirm =
      vi.fn<(audience: string, app: string | undefined) => void>();
    render(
      <XaaConfirmBar {...props} applications={[]} onConfirm={onConfirm} />,
    );
    expect(screen.getByRole("combobox").textContent).toContain(
      "Unavailable application",
    );
    expect(screen.getByRole("alert").textContent).toContain(
      "no longer available",
    );
    expect(
      screen
        .getByRole("button", { name: "Confirm setup" })
        .hasAttribute("disabled"),
    ).toBe(true);
    fireEvent.keyDown(screen.getByLabelText("Issuer URL"), {
      key: "Enter",
    });
    expect(onConfirm).not.toHaveBeenCalled();
    fireEvent.keyDown(screen.getByRole("combobox"), { key: "ArrowDown" });
    fireEvent.click(screen.getByRole("option", { name: "Not recorded" }));
    fireEvent.click(screen.getByRole("button", { name: "Confirm setup" }));
    expect(onConfirm).toHaveBeenCalledWith(
      props.initialValues.audience,
      undefined,
    );
  });

  it("allows cancellation but guards cancellation and submission while pending", () => {
    const onConfirm =
      vi.fn<(audience: string, app: string | undefined) => void>();
    const onCancel = vi.fn<() => void>();
    const { rerender } = render(
      <XaaConfirmBar {...props} onConfirm={onConfirm} onCancel={onCancel} />,
    );
    fireEvent.click(screen.getByRole("button", { name: "Cancel" }));
    expect(onCancel).toHaveBeenCalledTimes(1);
    expect(onConfirm).not.toHaveBeenCalled();
    rerender(
      <XaaConfirmBar
        {...props}
        onConfirm={onConfirm}
        onCancel={onCancel}
        pending
      />,
    );
    expect(
      screen.getByRole("button", { name: "Cancel" }).hasAttribute("disabled"),
    ).toBe(true);
    expect(screen.getByLabelText("Issuer URL").hasAttribute("disabled")).toBe(
      true,
    );
    expect(screen.getByRole("combobox").hasAttribute("disabled")).toBe(true);
    fireEvent.click(screen.getByRole("button", { name: "Cancel" }));
    fireEvent.click(screen.getByRole("button", { name: "Recording..." }));
    fireEvent.keyDown(screen.getByLabelText("Issuer URL"), {
      key: "Enter",
    });
    expect(onCancel).toHaveBeenCalledTimes(1);
    expect(onConfirm).not.toHaveBeenCalled();
  });

  it("allows confirmation without an optional app or Okta links", () => {
    const onConfirm =
      vi.fn<(audience: string, app: string | undefined) => void>();
    render(
      <XaaConfirmBar
        {...props}
        applicationsUrl={undefined}
        initialValues={{ audience: props.initialValues.audience }}
        review={{ serverName: "Example server", confirmed: true }}
        onConfirm={onConfirm}
      />,
    );
    expect(screen.queryByRole("link")).toBeNull();
    expect(screen.getByRole("combobox").textContent).toContain("Not recorded");
    fireEvent.click(screen.getByRole("button", { name: "Save confirmation" }));
    expect(onConfirm).toHaveBeenCalledWith(
      props.initialValues.audience,
      undefined,
    );
  });
});
