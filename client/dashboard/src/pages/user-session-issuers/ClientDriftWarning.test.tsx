import type { UserSessionIssuerRemoteSessionClientBindings } from "@gram/client/models/components/usersessionissuerremotesessionclientbindings.js";
import {
  cleanup,
  fireEvent,
  render,
  screen,
  within,
} from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { ClientDriftWarning } from "./ClientDriftWarning";

const bindings = vi.hoisted(() => ({
  data: undefined as UserSessionIssuerRemoteSessionClientBindings | undefined,
}));

vi.mock(
  "@gram/client/react-query/organizationUserSessionIssuerRemoteSessionClientBindings.js",
  () => ({
    useOrganizationUserSessionIssuerRemoteSessionClientBindings: () => ({
      data: bindings.data,
    }),
  }),
);

afterEach(() => {
  cleanup();
  bindings.data = undefined;
});

function client(id: string) {
  return {
    id,
    clientId: `${id}-cid`,
    remoteSessionIssuerId: `${id}-as`,
    remoteSessionIssuerName: id,
    remoteSessionIssuerUrl: `https://${id}.example.com`,
  };
}

function uses(serverName: string, id: string) {
  return {
    mcpServerId: `${serverName}-id`,
    mcpServerName: serverName,
    projectId: "project-1",
    projectName: "Default",
    remoteSessionClient: client(id),
  };
}

it("stays hidden while the comparison loads", () => {
  render(<ClientDriftWarning issuerId="usi-1" />);
  expect(screen.queryByRole("button")).toBeNull();
});

it("stays hidden when bound and used clients match", () => {
  bindings.data = {
    boundClients: [client("github")],
    serverClients: [uses("GitHub", "github")],
  };
  render(<ClientDriftWarning issuerId="usi-1" />);
  expect(screen.queryByRole("button")).toBeNull();
});

it("shows each mismatch when the warning is opened", () => {
  bindings.data = {
    boundClients: [client("github"), client("unused")],
    serverClients: [uses("GitHub", "github"), uses("Linear", "stray")],
  };
  render(<ClientDriftWarning issuerId="usi-1" />);

  fireEvent.click(
    screen.getByRole("button", { name: "2 clients don't match" }),
  );
  const dialog = screen.getByRole("dialog");
  expect(
    within(dialog).getByText(
      /1 used by its servers isn't bound here, and 1 bound here isn't used by any of its servers/,
    ),
  ).toBeTruthy();

  const rows = within(dialog).getAllByRole("row").slice(1);
  expect(rows.map((row) => row.textContent)).toEqual([
    expect.stringMatching(/^stray.*Not bound.*Linear/),
    expect.stringMatching(/^unused.*Bound.*No server uses it/),
    expect.stringMatching(/^github.*Bound.*GitHub/),
  ]);
});
