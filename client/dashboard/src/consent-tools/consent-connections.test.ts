import { readFileSync } from "node:fs";
import { runInNewContext } from "node:vm";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { waitFor, fireEvent } from "@testing-library/dom";

const script = readFileSync(
  "../../server/internal/mcp/consent_script.js",
  "utf8",
);
const fetchMock = vi.fn();
function reply(value: unknown, status = 200) {
  return {
    ok: status === 200,
    status,
    json: async () => ({ canManageConnections: true, ...(value as object) }),
  };
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

describe("identity chaining checks", () => {
  function chainedPage() {
    document.body.innerHTML = `
      <div data-service-connections data-action-url="/mcp/example/connect/remote-session">
        ${["client-a", "client-b"]
          .map(
            (id) => `
          <li data-remote-client="${id}">
            <input name="state" value="state-a"><input name="csrf_token" value="csrf-a">
            <span data-card-status data-identity-chained data-chain-check="pending">Managed by your identity provider</span>
            <button type="button" data-connect-fallback>Use a separate sign-in instead</button>
          </li>`,
          )
          .join("")}
      </div>`;
    const rejected = vi.fn<() => void>();
    document.addEventListener("gram:consent-chain-rejected", () => {
      rejected();
    });
    runInNewContext(script, {
      window,
      document,
      sessionStorage,
      URLSearchParams,
      CustomEvent,
      fetch: fetchMock,
    });
    return rejected;
  }
  function chainCheck(id: string) {
    return document.querySelector(
      `[data-remote-client="${id}"] [data-chain-check]`,
    )!;
  }

  afterEach(() => {
    document.documentElement.removeAttribute("data-consent-chain-rejected");
  });

  it("signals the island when one chained service is refused", async () => {
    fetchMock.mockImplementation(async (_url: string, init: RequestInit) => {
      const clientID = (init.body as URLSearchParams).get("client_id");
      return clientID === "client-a"
        ? reply({ status: "rejected", message: "Refused." })
        : reply({ status: "connected", message: "Connected" });
    });
    const rejected = chainedPage();
    await waitFor(() => {
      expect(chainCheck("client-a").getAttribute("data-chain-check")).toBe(
        "rejected",
      );
      expect(chainCheck("client-b").getAttribute("data-chain-check")).toBe(
        "connected",
      );
    });
    expect(
      document.documentElement.getAttribute("data-consent-chain-rejected"),
    ).toBe("true");
    expect(rejected).toHaveBeenCalled();
  });

  it("does not signal when chained services connect", async () => {
    fetchMock.mockResolvedValue(
      reply({ status: "connected", message: "Connected" }),
    );
    const rejected = chainedPage();
    await waitFor(() =>
      expect(chainCheck("client-b").getAttribute("data-chain-check")).toBe(
        "connected",
      ),
    );
    expect(
      document.documentElement.hasAttribute("data-consent-chain-rejected"),
    ).toBe(false);
    expect(rejected).not.toHaveBeenCalled();
  });
});

describe("existing agent connection renewal", () => {
  function renewalPage() {
    document.body.innerHTML = `
      <div data-delegation-form data-integrated-delegation data-action-url="/connect/remote-session">
        <input type="hidden" name="state" value="state-a">
        <input type="hidden" name="csrf_token" value="csrf-a">
        <input type="hidden" name="remote_session_id" value="session-a">
        <input type="hidden" name="grant_generation" value="8">
        <div data-delegation-header>
          <input type="checkbox" data-delegation-enabled checked>
          <button data-delegation-toggle aria-expanded="false"><span data-delegation-chevron></span></button>
          <span data-delegation-summary></span>
        </div>
        <div id="delegation-agent-choices" hidden>
          <input type="checkbox" name="agent_id" value="agent-a" data-agent-name="Alpha" data-binding-id="binding-a" checked>
          <input type="checkbox" name="agent_id" value="agent-b" data-agent-name="Beta" data-binding-id="binding-b" checked>
        </div>
        <p data-delegation-error hidden></p>
        <div data-delegation-fallback>Continue without updating agents</div>
      </div>
      <form data-approve-form>
        <input name="state" value="state-a">
        <button id="consent-approve-button" type="submit" data-consent-self-ready="true" disabled>Give access</button>
      </form>`;
    const form = document.querySelector<HTMLFormElement>("form")!;
    const submit = vi.spyOn(form, "requestSubmit").mockImplementation(() => {});
    runInNewContext(script, {
      window,
      document,
      sessionStorage,
      URLSearchParams,
      fetch: fetchMock,
    });
    return {
      form,
      submit,
      master: document.querySelector<HTMLInputElement>(
        "[data-delegation-enabled]",
      )!,
      agents: Array.from(
        document.querySelectorAll<HTMLInputElement>('input[name="agent_id"]'),
      ),
      approve: document.querySelector<HTMLButtonElement>(
        "#consent-approve-button",
      )!,
    };
  }

  it("selects and clears all renewals without hiding the section, and reflects partial selection", () => {
    const { master, agents } = renewalPage();
    expect(master.checked).toBe(true);
    fireEvent.click(master);
    expect(agents.every((agent) => !agent.checked)).toBe(true);
    expect(
      document.querySelector<HTMLElement>("[data-delegation-header]")!.hidden,
    ).toBe(false);
    expect(
      document.querySelector("[data-delegation-summary]")!.textContent,
    ).toBe("No agents selected");
    fireEvent.click(agents[0]!);
    expect(master.checked).toBe(false);
    expect(master.indeterminate).toBe(true);
    fireEvent.click(master);
    expect(agents.every((agent) => agent.checked)).toBe(true);
  });

  it("freezes the reviewed selections and sends each distinct binding with the exact grant before approval", async () => {
    let release!: () => void;
    const pending = new Promise<void>((resolve) => {
      release = resolve;
    });
    fetchMock.mockImplementationOnce(async () => {
      await pending;
      return reply({});
    });
    fetchMock.mockResolvedValue(reply({}));
    const { form, submit, master, agents, approve } = renewalPage();
    fireEvent.submit(form);
    expect(master.disabled).toBe(true);
    expect(agents.every((agent) => agent.disabled)).toBe(true);
    expect(approve.disabled).toBe(true);
    expect(submit).not.toHaveBeenCalled();
    release();
    await waitFor(() => expect(submit).toHaveBeenCalledOnce());
    const bodies = fetchMock.mock.calls.map((call) =>
      Object.fromEntries((call[1] as RequestInit).body as URLSearchParams),
    );
    expect(bodies).toEqual([
      {
        state: "state-a",
        csrf_token: "csrf-a",
        remote_session_id: "session-a",
        grant_generation: "8",
        action: "delegate_grant",
        agent_id: "agent-a",
        binding_id: "binding-a",
      },
      {
        state: "state-a",
        csrf_token: "csrf-a",
        remote_session_id: "session-a",
        grant_generation: "8",
        action: "delegate_grant",
        agent_id: "agent-b",
        binding_id: "binding-b",
      },
      {
        state: "state-a",
        csrf_token: "csrf-a",
        remote_session_id: "session-a",
        grant_generation: "8",
        action: "finish_delegation",
      },
    ]);
  });

  it("does not approve the client after a failed renewal and restores unfinished choices", async () => {
    fetchMock
      .mockResolvedValueOnce(reply({}))
      .mockResolvedValueOnce(reply({}, 409));
    const { form, submit, master, agents, approve } = renewalPage();
    fireEvent.submit(form);
    await waitFor(() =>
      expect(
        document.querySelector<HTMLElement>("[data-delegation-error]")!.hidden,
      ).toBe(false),
    );
    expect(submit).not.toHaveBeenCalled();
    expect(agents[0]!.disabled).toBe(true);
    expect(agents[1]!.disabled).toBe(false);
    expect(agents[1]!.checked).toBe(true);
    expect(master.disabled).toBe(false);
    expect(approve.disabled).toBe(false);
    expect(fetchMock).toHaveBeenCalledTimes(2);
  });

  it("finishes without renewing any agent after opting out", async () => {
    fetchMock.mockResolvedValue(reply({}));
    const { form, submit, master } = renewalPage();
    fireEvent.click(master);
    fireEvent.submit(form);
    await waitFor(() => expect(submit).toHaveBeenCalledOnce());
    expect(fetchMock).toHaveBeenCalledOnce();
    expect(
      (fetchMock.mock.calls[0]![1].body as URLSearchParams).get("action"),
    ).toBe("finish_delegation");
  });
});
