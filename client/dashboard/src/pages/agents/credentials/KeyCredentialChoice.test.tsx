import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { KeyCredentialChoice } from "./KeyCredentialChoice";

const mocks = vi.hoisted(() => ({ choices: [] as unknown[], failed: false }));
vi.mock("@gram/client/react-query/mcpServerAuthClientChoices.js", () => ({
  useMcpServerAuthClientChoices: () =>
    mocks.failed
      ? { isError: true, isSuccess: false, data: undefined }
      : { isError: false, isSuccess: true, data: { choices: mocks.choices } },
}));

afterEach(() => {
  cleanup();
  mocks.failed = false;
});

describe("KeyCredentialChoice", () => {
  it("asks nothing of a server without shared credentials", () => {
    mocks.choices = [];
    const { container } = render(
      <KeyCredentialChoice
        serverId="s"
        serverName="GitHub"
        value={undefined}
        onChange={() => {}}
      />,
    );
    expect(container.innerHTML).toBe("");
  });

  it("offers a server's only shared credential as an explicit pick", () => {
    mocks.choices = [
      { id: "a", name: "Token", clientType: "static_credential" },
    ];
    const onChange = vi.fn();
    render(
      <KeyCredentialChoice
        serverId="s"
        serverName="GitHub"
        value={undefined}
        onChange={(id) => {
          onChange(id);
        }}
      />,
    );
    expect(onChange).not.toHaveBeenCalled();
    fireEvent.click(
      screen.getByRole("combobox", {
        name: "Credential the key uses on GitHub",
      }),
    );
    fireEvent.click(
      screen.getByRole("option", { name: /Token · Static credential/ }),
    );
    expect(onChange).toHaveBeenCalledWith("a");
  });

  it("asks which credential to use when a server offers several", () => {
    mocks.choices = [
      { id: "a", name: "Engineering token", clientType: "static_credential" },
      { id: "b", name: "Marketing", clientType: "service_account" },
    ];
    const onChange = vi.fn();
    render(
      <KeyCredentialChoice
        serverId="s"
        serverName="GitHub"
        value={undefined}
        onChange={(id) => {
          onChange(id);
        }}
      />,
    );
    fireEvent.click(
      screen.getByRole("combobox", {
        name: "Credential the key uses on GitHub",
      }),
    );
    fireEvent.click(
      screen.getByRole("option", { name: /Marketing · Service account/ }),
    );
    expect(onChange).toHaveBeenCalledWith("b");
  });

  it("says so when the server's credentials cannot be loaded", () => {
    mocks.failed = true;
    render(
      <KeyCredentialChoice
        serverId="s"
        serverName="GitHub"
        value={undefined}
        onChange={() => undefined}
      />,
    );
    expect(
      screen.getByText(/The credentials GitHub offers could not be loaded/),
    ).toBeTruthy();
  });

  it("clears a pick the server no longer offers and keeps one it does", () => {
    mocks.choices = [
      { id: "a", name: "Engineering token", clientType: "static_credential" },
      { id: "b", name: "Marketing", clientType: "service_account" },
    ];
    const onChange = vi.fn();
    const { rerender } = render(
      <KeyCredentialChoice
        serverId="s"
        serverName="GitHub"
        value="a"
        onChange={(id) => {
          onChange(id);
        }}
      />,
    );
    expect(onChange).not.toHaveBeenCalled();
    rerender(
      <KeyCredentialChoice
        serverId="s"
        serverName="GitHub"
        value="gone"
        onChange={(id) => {
          onChange(id);
        }}
      />,
    );
    expect(onChange).toHaveBeenCalledWith("");
  });
});
