import { readFileSync } from "node:fs";
import { runInNewContext } from "node:vm";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { waitFor, fireEvent } from "@testing-library/dom";

const script = readFileSync(
  "../../server/internal/mcp/consent_script.js",
  "utf8",
);
const fetchMock = vi.fn();
function page(withProvider = true, initialize = true) {
  document.body.innerHTML = `
    <input type="radio" data-agent-mode name="consent_actor" value="self" checked data-subject-display="person@example.com">
    <input type="radio" data-agent-mode name="consent_actor" value="agent" disabled>
    <input type="hidden" data-agent-self name="agent_id" value="" form="approve">
    <div data-agent-picker hidden>
      <input type="search" data-agent-search>
      <label data-agent-option><input type="radio" data-agent-select name="agent_id" value="agent-a" data-subject-display="Alpha Helper" form="approve" disabled></label>
      <label data-agent-option><input type="radio" data-agent-select name="agent_id" value="agent-b" data-subject-display="Beta Helper" form="approve" disabled></label>
      <p data-agent-empty hidden>No agents match your search.</p>
    </div>
    <p data-agent-setup hidden>Missing an agent?</p>
    <div data-agent-self-only>Tool access</div>
    <span data-consent-subject-display></span>
    <div data-agent-policy hidden><span data-agent-policy-name></span></div>
    <form id="approve" data-approve-form><input name="state" value="state-a"><input name="csrf_token" value="csrf-a">
      <button type="submit" data-self-label="Give access" data-agent-label="Authorize agent" data-consent-self-ready="true">Give access</button>
    </form>
    <div data-service-connections data-action-url="/x/mcp/example/connect/remote-session">
      <li data-remote-client="client-a" data-remote-display="Example provider">
        <span>Example provider</span><span data-account-identity>Connected</span>
        <div data-agent-access hidden></div>
      </li>
    </div>`;
  if (!withProvider)
    document.querySelector("[data-service-connections]")!.remove();
  if (initialize)
    runInNewContext(script, {
      window,
      document,
      sessionStorage,
      URLSearchParams,
      fetch: fetchMock,
    });
  return document.querySelector<HTMLButtonElement>("form button")!;
}
function mode(value: "self" | "agent") {
  const input = document.querySelector<HTMLInputElement>(
    `input[data-agent-mode][value="${value}"]`,
  )!;
  input.checked = true;
  fireEvent.change(input);
}
function select(id: string) {
  mode(id ? "agent" : "self");
  if (!id) return;
  const input = document.querySelector<HTMLInputElement>(
    `input[value="${id}"]`,
  )!;
  input.checked = true;
  fireEvent.change(input);
}
function reply(value: unknown, status = 200) {
  return { ok: status === 200, status, json: async () => value };
}
beforeEach(() => {
  sessionStorage.clear();
  vi.stubGlobal("fetch", fetchMock);
  fetchMock.mockReset();
});
afterEach(() => {
  document.body.replaceChildren();
  vi.unstubAllGlobals();
});

describe("consent agent connections", () => {
  it("shows the agent setup hint only in Agent mode", () => {
    page(false);
    const hint = document.querySelector<HTMLElement>("[data-agent-setup]")!;
    expect(hint.hidden).toBe(true);
    mode("agent");
    expect(hint.hidden).toBe(false);
    mode("self");
    expect(hint.hidden).toBe(true);
  });

  it("keeps Agent mode unavailable until its script initializes", () => {
    page(false, false);
    const agentMode = document.querySelector<HTMLInputElement>(
      'input[data-agent-mode][value="agent"]',
    )!;
    expect(agentMode.disabled).toBe(true);
    agentMode.click();
    expect(agentMode.checked).toBe(false);
    page(false);
    expect(
      document.querySelector<HTMLInputElement>(
        'input[data-agent-mode][value="agent"]',
      )!.disabled,
    ).toBe(false);
  });

  it.each([false, true])(
    "reuses the canonical combined account label (attached=%s)",
    async (attached) => {
      const account = {
        ID: "owned-session",
        RemoteSessionClientID: "client-a",
        UpstreamDisplayName: "Example account",
        UpstreamEmail: "account@example.com",
      };
      fetchMock.mockResolvedValue(
        reply({
          candidates: [account],
          bindings: attached
            ? [
                {
                  ID: "binding-a",
                  RemoteSessionClientID: "client-a",
                  RemoteSessionID: account.ID,
                  RemoteSession: account,
                },
              ]
            : [],
        }),
      );
      page();
      document
        .querySelector("[data-remote-client]")!
        .setAttribute(
          "data-connected-as",
          "Example account · account@example.com",
        );
      select("agent-a");
      await waitFor(() =>
        expect(
          document.querySelector("[data-agent-access]")!.textContent,
        ).toContain(attached ? "Available to agent" : "Use this account"),
      );
      expect(document.querySelector("select")).toBeNull();
      expect(
        document.querySelector("[data-agent-access]")!.textContent,
      ).not.toContain("Example account");
    },
  );

  it("derives availability from bindings and reuses a selected owned connection through consent actions", async () => {
    let attached = false;
    fetchMock.mockImplementation(async (_url, init) => {
      const body = init.body as URLSearchParams;
      expect(body.get("state")).toBe("state-a");
      expect(body.get("csrf_token")).toBe("csrf-a");
      expect(body.get("agent_id")).toBe("agent-a");
      if (body.get("action") === "agent_attach") {
        expect(body.get("remote_session_id")).toBe("owned-session");
        attached = true;
        return reply({});
      }
      return reply({
        candidates: [
          {
            ID: "owned-session",
            RemoteSessionClientID: "client-a",
            Scopes: ["read"],
          },
        ],
        bindings: attached
          ? [
              {
                ID: "binding-a",
                RemoteSessionClientID: "client-a",
                RemoteSessionID: "owned-session",
                RemoteSession: { ID: "owned-session" },
              },
            ]
          : [],
      });
    });
    const button = page();
    select("agent-a");
    expect(button.disabled).toBe(true);
    await waitFor(() =>
      expect(document.body.textContent).toContain("Use this account"),
    );
    expect(button.disabled).toBe(true);
    const attach = [...document.querySelectorAll("button")].find(
      (el) => el.textContent === "Use this account",
    )!;
    fireEvent.click(attach);
    await waitFor(() => expect(button.disabled).toBe(false));
    expect(button.value).toBe("approve_agent");
  });

  it("loads canonical pages without starting provider OAuth", async () => {
    fetchMock.mockImplementation(async (_url, init) => {
      const body = init.body as URLSearchParams;
      expect(body.get("action")).toBe("agent_connections");
      return body.get("cursor") === "next-page"
        ? reply({
            candidates: [
              { ID: "later-owned", RemoteSessionClientID: "client-a" },
            ],
            bindings: [],
          })
        : reply({ candidates: [], bindings: [], nextCursor: "next-page" });
    });
    page();
    select("agent-a");
    await waitFor(() =>
      expect(document.querySelector("option")?.value).toBe("later-owned"),
    );
    expect(fetchMock).toHaveBeenCalledTimes(2);
  });

  it("restores agent selection after the upstream OAuth round trip", async () => {
    sessionStorage.setItem("gram-consent-agent-v1:state-a", "agent-a");
    fetchMock.mockResolvedValue(reply({ candidates: [], bindings: [] }));
    const button = page();
    expect(
      document.querySelector<HTMLInputElement>('input[value="agent-a"]')!
        .checked,
    ).toBe(true);
    expect(button.disabled).toBe(true);
    await waitFor(() =>
      expect(document.body.textContent).toContain("No account is eligible"),
    );
    expect(
      document.querySelector<HTMLElement>("[data-agent-access]")!.hidden,
    ).toBe(false);
  });

  it("does not enable a newer selection from a stale list response", async () => {
    let resolveA!: (value: unknown) => void;
    fetchMock.mockImplementation(async (_url, init) => {
      const body = init.body as URLSearchParams;
      if (body.get("agent_id") === "agent-a")
        return new Promise((resolve) => {
          resolveA = resolve;
        });
      return reply({ candidates: [], bindings: [] });
    });
    const button = page();
    select("agent-a");
    select("agent-b");
    resolveA(
      reply({
        candidates: [{ ID: "owned-session" }],
        bindings: [
          {
            RemoteSessionClientID: "client-a",
            RemoteSessionID: "owned-session",
            RemoteSession: { ID: "owned-session" },
          },
        ],
      }),
    );
    await waitFor(() =>
      expect(document.body.textContent).toContain("No account is eligible"),
    );
    expect(button.disabled).toBe(true);
  });

  it("ignores in-flight agent access when switching to self and back to empty Agent mode", async () => {
    let resolve!: (value: unknown) => void;
    fetchMock.mockImplementation(
      () =>
        new Promise((done) => {
          resolve = done;
        }),
    );
    const button = page();
    select("agent-a");
    mode("self");
    expect(button.disabled).toBe(false);
    mode("agent");
    resolve(
      reply({
        candidates: [],
        bindings: [
          {
            RemoteSessionClientID: "client-a",
            RemoteSessionID: "session-a",
            RemoteSession: { ID: "session-a" },
          },
        ],
      }),
    );
    await waitFor(() => expect(fetchMock).toHaveResolved());
    expect(button.disabled).toBe(true);
    expect(
      document.querySelector<HTMLInputElement>('input[value="agent-a"]')!
        .checked,
    ).toBe(false);
    expect(fireEvent.submit(document.querySelector("form")!)).toBe(false);
  });

  it("keeps approval disabled when the consent human cannot authorize the agent", async () => {
    fetchMock.mockResolvedValue(reply({}, 401));
    const button = page();
    select("agent-a");
    await waitFor(() =>
      expect(
        document.querySelector<HTMLElement>("[data-agent-access-unavailable]")!
          .hidden,
      ).toBe(false),
    );
    expect(button.disabled).toBe(true);
    expect(document.body.textContent).toContain(
      "not available for authorization",
    );
    expect(document.body.textContent).not.toContain("Sign in to Gram");
  });
});

describe("canonical consent account identity", () => {
  it("shows stored upstream identity, but keeps unavailable bindings detach-only", async () => {
    let unavailable = false;
    fetchMock.mockImplementation(async () => {
      const session = {
        ID: "existing-session",
        RemoteSessionClientID: "client-a",
        UpstreamDisplayName: "Existing upstream account",
        UpstreamEmail: "existing@example.com",
        Scopes: ["read"],
      };
      return reply({
        candidates: [session],
        bindings: [
          {
            ID: "binding-a",
            RemoteSessionID: session.ID,
            RemoteSessionClientID: "client-a",
            ...(unavailable ? {} : { RemoteSession: session }),
          },
        ],
      });
    });
    const button = page();
    select("agent-a");
    await waitFor(() =>
      expect(document.body.textContent).toContain(
        "Existing upstream account · existing@example.com",
      ),
    );
    unavailable = true;
    select("agent-b");
    await waitFor(() =>
      expect(document.body.textContent).toContain("Agent access unavailable"),
    );
    expect(button.disabled).toBe(true);
    expect(document.body.textContent).not.toContain("existing@example.com");
    expect(document.body.textContent).toContain("Remove agent access");
    expect(document.body.textContent).not.toContain("Use this account");
  });
});

describe("unified provider card", () => {
  it("shows the connected account once and keeps attachment explicit", async () => {
    const account = {
      ID: "owned-session",
      RemoteSessionClientID: "client-a",
      UpstreamEmail: "account@example.com",
    };
    let attached = false;
    fetchMock.mockImplementation(async (_url, init) => {
      if ((init.body as URLSearchParams).get("action") === "agent_attach") {
        attached = true;
        return reply({});
      }
      return reply({
        candidates: [account],
        bindings: attached
          ? [
              {
                ID: "binding",
                RemoteSessionClientID: "client-a",
                RemoteSessionID: account.ID,
                RemoteSession: account,
              },
            ]
          : [],
      });
    });
    const button = page();
    const provider = document.querySelector<HTMLElement>(
      "[data-remote-client]",
    )!;
    provider.dataset.connectedAs = account.UpstreamEmail;
    provider.querySelector("[data-account-identity]")!.textContent =
      "Connected as " + account.UpstreamEmail;
    select("agent-a");
    await waitFor(() =>
      expect(provider.textContent).toContain("Use this account"),
    );
    expect(button.disabled).toBe(true);
    expect(provider.querySelector("select")).toBeNull();
    expect(provider.textContent!.match(/account@example.com/g)).toHaveLength(1);
    fireEvent.click(provider.querySelector("button")!);
    await waitFor(() =>
      expect(provider.textContent).toContain("Available to agent"),
    );
    expect(button.disabled).toBe(false);
    expect(provider.textContent!.match(/account@example.com/g)).toHaveLength(1);
    expect(document.querySelector("[data-agent-connections]")).toBeNull();
    select("");
    expect(
      provider.querySelector<HTMLElement>("[data-agent-access]")!.hidden,
    ).toBe(true);
    expect(provider.textContent).toContain("Connected as account@example.com");
  });

  it("keeps the connected account intact when agent permission lookup fails", async () => {
    fetchMock.mockResolvedValue(reply({}, 500));
    const button = page();
    document.querySelector("[data-account-identity]")!.textContent =
      "Connected as account@example.com";
    select("agent-a");
    await waitFor(() =>
      expect(document.body.textContent).toContain(
        "Could not verify agent access",
      ),
    );
    const provider = document.querySelector("[data-remote-client]")!;
    expect(provider.textContent).toContain("Connected as account@example.com");
    expect(provider.textContent).toContain(
      "Connection state is unknown. Retry to reload the current state.",
    );
    expect(provider.querySelector("[role=alert]")).not.toBeNull();
    expect(button.disabled).toBe(true);
  });

  it("retains multiple compatible accounts as an explicit choice", async () => {
    fetchMock.mockResolvedValue(
      reply({
        candidates: [
          {
            ID: "first",
            RemoteSessionClientID: "client-a",
            UpstreamEmail: "first@example.com",
          },
          {
            ID: "second",
            RemoteSessionClientID: "client-a",
            UpstreamEmail: "second@example.com",
          },
        ],
        bindings: [],
      }),
    );
    page();
    select("agent-a");
    await waitFor(() =>
      expect(
        document.querySelectorAll("[data-remote-client] option"),
      ).toHaveLength(2),
    );
  });

  it("allows agents with no upstream providers without a discovery panel", () => {
    page(false);
    select("agent-a");
    expect(
      document.querySelector<HTMLButtonElement>("form button")!.disabled,
    ).toBe(false);
    expect(fetchMock).not.toHaveBeenCalled();
  });
});

describe("consent actor picker", () => {
  function element(selector: string) {
    return document.querySelector<HTMLElement>(selector)!;
  }
  function formData() {
    return new FormData(document.querySelector("form")!);
  }
  function search(value: string) {
    fireEvent.input(element("[data-agent-search]"), { target: { value } });
  }

  it("defaults to Myself and prevents empty Agent mode from submitting as self", () => {
    const button = page(false);
    expect(element("[data-agent-picker]").hidden).toBe(true);
    expect(button.disabled).toBe(false);
    expect(formData().getAll("agent_id")).toEqual([""]);
    mode("agent");
    expect(element("[data-agent-picker]").hidden).toBe(false);
    expect(element("[data-agent-self-only]").hidden).toBe(true);
    expect(element("[data-agent-policy]").hidden).toBe(true);
    expect(button.disabled).toBe(true);
    expect(button.value).toBe("approve_agent");
    // The tool-access island must not overwrite the Agent-mode gate.
    expect(button.dataset.agentSelected).toBe("true");
    expect(formData().has("agent_id")).toBe(false);
    expect(document.querySelector("form")!.checkValidity()).toBe(false);
    expect(fireEvent.submit(element("form"))).toBe(false);
    // Even accidental re-enabling cannot turn an empty choice into self consent.
    button.disabled = false;
    expect(fireEvent.submit(element("form"))).toBe(false);
    select("agent-a");
    expect(button.disabled).toBe(false);
    expect(formData().getAll("agent_id")).toEqual(["agent-a"]);
    expect(element("[data-agent-policy]").hidden).toBe(false);
    expect(element("[data-consent-subject-display]").textContent).toBe(
      "Alpha Helper",
    );
    mode("self");
    expect(button.value).toBe("approve");
    expect(button.disabled).toBe(false);
    expect(element("[data-agent-self-only]").hidden).toBe(false);
    expect(element("[data-agent-picker]").hidden).toBe(true);
    expect(element("[data-consent-subject-display]").textContent).toBe(
      "person@example.com",
    );
    expect(formData().getAll("agent_id")).toEqual([""]);
    expect(document.querySelector("form")!.checkValidity()).toBe(true);
    button.dataset.consentSelfReady = "false";
    mode("agent");
    mode("self");
    expect(button.disabled).toBe(true);
  });

  it("filters names case-insensitively and clears empty results without changing selection", () => {
    page(false);
    select("agent-a");
    const options = document.querySelectorAll<HTMLElement>(
      "[data-agent-option]",
    );
    search("  bEtA  ");
    expect(options[0]!.hidden).toBe(true);
    expect(options[1]!.hidden).toBe(false);
    expect(formData().get("agent_id")).toBe("agent-a");
    search("agent-a"); // IDs are not search terms.
    expect(element("[data-agent-empty]").hidden).toBe(false);
    expect(Array.from(options).every((option) => option.hidden)).toBe(true);
    search("");
    expect(element("[data-agent-empty]").hidden).toBe(true);
    expect(Array.from(options).every((option) => !option.hidden)).toBe(true);
  });

  it("ignores unavailable saved agents and restores available agents in Agent mode", () => {
    sessionStorage.setItem("gram-consent-agent-v1:state-a", "removed-agent");
    page(false);
    expect(element("[data-agent-picker]").hidden).toBe(true);
    sessionStorage.setItem("gram-consent-agent-v1:state-a", "agent-b");
    page(false);
    expect(element("[data-agent-picker]").hidden).toBe(false);
    expect(formData().getAll("agent_id")).toEqual(["agent-b"]);
  });
});
