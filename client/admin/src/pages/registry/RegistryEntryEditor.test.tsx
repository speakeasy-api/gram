import {
  act,
  fireEvent,
  screen,
  waitFor,
  cleanup,
} from "@testing-library/react";
import { QueryClient } from "@tanstack/react-query";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { createRootRoute, createRoute, Outlet } from "@tanstack/react-router";
import { RegistryEntryPage, NewRegistryEntryPage } from "./RegistryEntryPage";
import { registrySearch } from "./registrySearch";
import { renderRouteTree, renderWithApp } from "@/test/harness";
import { formatRegistryJson } from "@/lib/registryJsonEditor";
import { registryEntryQuery } from "@/lib/gramAdminClient";

vi.mock("./RegistryJsonEditor", () => ({
  default: (props: import("./RegistryJsonEditor").RegistryJsonEditorProps) => (
    <textarea
      aria-label="Record JSON"
      value={props.value}
      disabled={props.disabled}
      aria-invalid={props.invalid}
      aria-describedby={props.describedBy}
      data-server-issues={JSON.stringify(props.issues)}
      onChange={(event) => props.onChange(event.target.value)}
      onBlur={props.onBlur}
    />
  ),
}));

const mutations = vi.hoisted(() => ({
  save: vi.fn(),
  create: vi.fn(),
  visibility: vi.fn(),
  upload: vi.fn(),
}));
vi.mock("@gram/admin-client/react-query/adminUploadPlatformImage", () => ({
  buildAdminUploadPlatformImageMutation: () => ({
    mutationFn: mutations.upload,
  }),
}));
const okta = vi.hoisted(() => ({ candidates: [] as unknown[] }));
vi.mock("@gram/admin-client/react-query/adminSaveRegistryEntry", () => ({
  buildAdminSaveRegistryEntryMutation: () => ({
    mutationKey: ["save"],
    mutationFn: mutations.save,
  }),
}));
vi.mock("@gram/admin-client/react-query/adminCreateRegistryEntry", () => ({
  buildAdminCreateRegistryEntryMutation: () => ({
    mutationKey: ["create"],
    mutationFn: mutations.create,
  }),
}));
vi.mock(
  "@gram/admin-client/react-query/adminSetRegistryEntryPublished",
  () => ({
    buildAdminSetRegistryEntryPublishedMutation: () => ({
      mutationKey: ["visibility"],
      mutationFn: mutations.visibility,
    }),
  }),
);
vi.mock(
  "@gram/admin-client/react-query/adminGetRegistryOktaCandidates.core",
  () => ({
    buildAdminGetRegistryOktaCandidatesQuery: (
      _client: unknown,
      request: { id: string },
    ) => ({
      queryKey: ["okta-candidates", request.id],
      queryFn: async () => ({ candidates: okta.candidates }),
    }),
  }),
);
import { RegistryEntryEditor } from "./RegistryEntryEditor";
import { registryEntryNameKey } from "@/lib/registryEntryName";

const id = "00000000-0000-4000-8000-000000000001";
const raw =
  '{"server":{"name":"example.test/demo"},"_meta":{"n":9007199254740993}}';
const token = "2026-09-21T12:00:00.123456Z";
const entry = {
  id,
  dataJson: raw,
  updatedAt: token,
  createdAt: token,
  published: false,
  issues: [],
};
let client: QueryClient;
beforeEach(() => {
  vi.clearAllMocks();
  okta.candidates = [];
  client = new QueryClient({
    defaultOptions: { queries: { retry: false, staleTime: Infinity } },
  });
  client.setQueryData(registryEntryQuery(id).queryKey, entry);
});

afterEach(() => {
  cleanup();
  client.clear();
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});

async function mount() {
  return renderWithApp(
    <RegistryEntryEditor id={id} onClose={vi.fn<() => void>()} />,
    { queryClient: client },
  );
}

it("saves only explicitly, preserving raw large integers and the opaque token on unpublished rows", async () => {
  mutations.save.mockResolvedValue({ ...entry, updatedAt: token + "1" });
  await mount();
  await screen.findByLabelText("Record JSON");
  expect(mutations.save).not.toHaveBeenCalled();
  fireEvent.change(screen.getByLabelText("Record JSON"), {
    target: { value: raw },
  });
  fireEvent.click(screen.getByRole("button", { name: "Save" }));
  await waitFor(() => expect(mutations.save).toHaveBeenCalledTimes(1));
  expect(mutations.save.mock.calls[0]![0]).toEqual({
    request: { id, dataJson: raw, updatedAt: token },
  });
  expect(mutations.visibility).not.toHaveBeenCalled();
  await waitFor(() =>
    expect(
      (screen.getByRole("button", { name: "Save" }) as HTMLButtonElement)
        .disabled,
    ).toBe(true),
  );
  expect(
    (screen.getByLabelText("Record JSON") as HTMLTextAreaElement).value,
  ).toBe(formatRegistryJson(raw));
});

it("keeps dirty text and base token on refetch and disables visibility", async () => {
  mutations.save.mockResolvedValue(entry);
  await mount();
  fireEvent.change(await screen.findByLabelText("Record JSON"), {
    target: { value: raw + " " },
  });
  client.setQueryData(registryEntryQuery(id).queryKey, {
    ...entry,
    dataJson: "{}",
    updatedAt: "new-token",
  });
  expect(
    (screen.getByLabelText("Record JSON") as HTMLTextAreaElement).value,
  ).toBe(raw + " ");
  expect(
    (screen.getByRole("button", { name: "Republish" }) as HTMLButtonElement)
      .disabled,
  ).toBe(true);
  fireEvent.click(screen.getByRole("button", { name: "Save" }));
  await waitFor(() => expect(mutations.save).toHaveBeenCalled());
  expect(mutations.save.mock.calls[0]![0].request.updatedAt).toBe(token);
});

it("retains edits after conflict and only reloads after confirmation", async () => {
  mutations.save.mockRejectedValue(
    Object.assign(new Error("Stale entry"), { statusCode: 409 }),
  );
  await mount();
  fireEvent.change(await screen.findByLabelText("Record JSON"), {
    target: { value: raw + " " },
  });
  fireEvent.click(screen.getByRole("button", { name: "Save" }));
  await screen.findByRole("alert");
  expect(
    (screen.getByLabelText("Record JSON") as HTMLTextAreaElement).value,
  ).toBe(raw + " ");
  vi.stubGlobal("confirm", vi.fn().mockReturnValue(false));
  fireEvent.click(screen.getByRole("button", { name: "Reload" }));
  expect(window.confirm).toHaveBeenCalled();
  expect(
    (screen.getByLabelText("Record JSON") as HTMLTextAreaElement).value,
  ).toBe(raw + " ");
  expect(mutations.save).toHaveBeenCalledTimes(1);
});

const candidate = {
  oinName: "example_app",
  organizations: 1,
  signOnModes: ["SAML_2_0"],
  reason: "domain",
  integrator: false,
};

it("keeps the conflict explanation and disabled Save when an Okta name is added", async () => {
  okta.candidates = [candidate];
  mutations.save.mockRejectedValue(
    Object.assign(new Error("Stale entry"), { statusCode: 409 }),
  );
  await mount();
  fireEvent.change(await screen.findByLabelText("Record JSON"), {
    target: { value: raw + " " },
  });
  fireEvent.click(screen.getByRole("button", { name: "Save" }));
  await screen.findByRole("alert");
  fireEvent.click(
    await screen.findByRole("button", { name: "Add example_app" }),
  );
  expect(
    (screen.getByLabelText("Record JSON") as HTMLTextAreaElement).value,
  ).toContain('"example_app"');
  const alert = screen.getByRole("alert").textContent;
  expect(alert).toContain("Stale entry");
  expect(alert).toContain("Reload explicitly before retrying.");
  expect(
    (screen.getByRole("button", { name: "Save" }) as HTMLButtonElement)
      .disabled,
  ).toBe(true);
  expect(screen.getByRole("button", { name: "Reload" })).toBeTruthy();
  expect(mutations.save).toHaveBeenCalledTimes(1);
});

it("clears a non-conflict failure when an Okta name is added", async () => {
  okta.candidates = [candidate];
  mutations.save.mockRejectedValue(
    Object.assign(new Error("Request unavailable"), { statusCode: 503 }),
  );
  await mount();
  fireEvent.change(await screen.findByLabelText("Record JSON"), {
    target: { value: raw + " " },
  });
  fireEvent.click(screen.getByRole("button", { name: "Save" }));
  await screen.findByRole("alert");
  fireEvent.click(
    await screen.findByRole("button", { name: "Add example_app" }),
  );
  expect(
    (screen.getByLabelText("Record JSON") as HTMLTextAreaElement).value,
  ).toContain('"example_app"');
  expect(screen.queryByRole("alert")).toBeNull();
  expect(
    (screen.getByRole("button", { name: "Save" }) as HTMLButtonElement)
      .disabled,
  ).toBe(false);
  expect(screen.queryByRole("button", { name: "Reload" })).toBeNull();
});

it("confirms dirty cancel and performs no save", async () => {
  await mount();
  fireEvent.change(await screen.findByLabelText("Record JSON"), {
    target: { value: "{}" },
  });
  vi.stubGlobal("confirm", vi.fn().mockReturnValue(false));
  fireEvent.click(screen.getByRole("button", { name: "Cancel" }));
  expect(window.confirm).toHaveBeenCalled();
  expect(mutations.save).not.toHaveBeenCalled();
});

it("closes after one confirmed discard", async () => {
  const onClose = vi.fn<() => void>();
  await renderWithApp(<RegistryEntryEditor id={id} onClose={onClose} />, {
    queryClient: client,
  });
  fireEvent.change(await screen.findByLabelText("Record JSON"), {
    target: { value: "{}" },
  });
  vi.stubGlobal("confirm", vi.fn().mockReturnValue(true));
  fireEvent.click(screen.getByRole("button", { name: "Cancel" }));
  expect(window.confirm).toHaveBeenCalledTimes(1);
  expect(onClose).toHaveBeenCalledTimes(1);
});

it("titles the page with the entry name and records it for the crumb", async () => {
  client.removeQueries({ queryKey: registryEntryQuery(id).queryKey });
  vi.stubGlobal(
    "fetch",
    vi.fn().mockResolvedValue(
      new Response(
        JSON.stringify({
          id,
          data_json: raw,
          updated_at: token,
          created_at: token,
          published: false,
          issues: [],
        }),
        { status: 200, headers: { "Content-Type": "application/json" } },
      ),
    ),
  );
  await mount();
  expect(
    await screen.findByRole("heading", {
      name: "example.test/demo",
      level: 1,
    }),
  ).toBeTruthy();
  await waitFor(() =>
    expect(client.getQueryData(registryEntryNameKey(id))).toEqual({
      name: "example.test/demo",
    }),
  );
});

it.each([401, 422, 503])(
  "preserves dirty text and original token on %i errors",
  async (statusCode) => {
    mutations.save.mockRejectedValue(
      Object.assign(new Error("/server/name: rejected"), { statusCode }),
    );
    await mount();
    fireEvent.change(await screen.findByLabelText("Record JSON"), {
      target: { value: raw + " " },
    });
    fireEvent.click(screen.getByRole("button", { name: "Save" }));
    expect((await screen.findByRole("alert")).textContent).toContain(
      "/server/name",
    );
    expect(
      (screen.getByLabelText("Record JSON") as HTMLTextAreaElement).value,
    ).toBe(raw + " ");
    expect(mutations.save.mock.calls[0]![0].request.updatedAt).toBe(token);
  },
);
it("disables duplicate submission and close while pending", async () => {
  mutations.save.mockReturnValue(new Promise(() => {}));
  await mount();
  fireEvent.change(await screen.findByLabelText("Record JSON"), {
    target: { value: raw },
  });
  fireEvent.click(screen.getByRole("button", { name: "Save" }));
  fireEvent.click(screen.getByRole("button", { name: "Save" }));
  await waitFor(() => expect(mutations.save).toHaveBeenCalledTimes(1));
  expect(
    (screen.getByRole("button", { name: "Cancel" }) as HTMLButtonElement)
      .disabled,
  ).toBe(true);
});

it("delegates field validation on explicit Save", async () => {
  mutations.save.mockRejectedValue(
    Object.assign(new Error("/server/name: required"), { statusCode: 422 }),
  );
  await mount();
  const textarea = await screen.findByLabelText("Record JSON");
  fireEvent.change(textarea, { target: { value: "{}" } });
  expect(mutations.save).not.toHaveBeenCalled();
  fireEvent.click(screen.getByRole("button", { name: "Save" }));
  expect((await screen.findByRole("alert")).textContent).toContain(
    "/server/name",
  );
  expect((textarea as HTMLTextAreaElement).value).toBe("{}");
});

it("checks syntax on blur and before Save, not each keystroke", async () => {
  await mount();
  const textarea = await screen.findByLabelText("Record JSON");
  fireEvent.change(textarea, { target: { value: "{" } });
  expect(textarea.getAttribute("aria-invalid")).toBe("false");
  fireEvent.blur(textarea);
  expect(textarea.getAttribute("aria-invalid")).toBe("true");
  fireEvent.change(textarea, { target: { value: "[" } });
  fireEvent.click(screen.getByRole("button", { name: "Save" }));
  expect(mutations.save).not.toHaveBeenCalled();
  expect(textarea.getAttribute("aria-invalid")).toBe("true");
});

it("uses the successful response token for subsequent visibility changes", async () => {
  const latest = { ...entry, updatedAt: "2026-09-21T12:00:00.654321Z" };
  mutations.save.mockResolvedValue(latest);
  mutations.visibility.mockResolvedValue({ ...latest, published: true });
  await mount();
  fireEvent.change(await screen.findByLabelText("Record JSON"), {
    target: { value: raw },
  });
  fireEvent.click(screen.getByRole("button", { name: "Save" }));
  await waitFor(() =>
    expect(client.getQueryData(registryEntryQuery(id).queryKey)).toEqual(
      latest,
    ),
  );
  await waitFor(() =>
    expect(
      (screen.getByRole("button", { name: "Republish" }) as HTMLButtonElement)
        .disabled,
    ).toBe(false),
  );
  fireEvent.click(screen.getByRole("button", { name: "Republish" }));
  await waitFor(() => expect(mutations.visibility).toHaveBeenCalled());
  expect(mutations.visibility.mock.calls[0]![0].request).toEqual({
    id,
    updatedAt: latest.updatedAt,
    published: true,
  });
});

it("creates from unchanged raw text then saves rather than creating again", async () => {
  mutations.create.mockResolvedValue({ ...entry, published: true });
  mutations.save.mockResolvedValue({ ...entry, published: true });
  await renderWithApp(
    <RegistryEntryEditor id={null} onClose={vi.fn<() => void>()} />,
    { queryClient: client },
  );
  fireEvent.change(await screen.findByLabelText("Record JSON"), {
    target: { value: raw },
  });
  fireEvent.click(screen.getByRole("button", { name: "Save" }));
  await screen.findByRole("button", { name: "Unpublish" });
  await waitFor(() =>
    expect(
      (screen.getByRole("button", { name: "Save" }) as HTMLButtonElement)
        .disabled,
    ).toBe(true),
  );
  fireEvent.change(screen.getByLabelText("Record JSON"), {
    target: { value: raw },
  });
  expect(mutations.create.mock.calls[0]![0]).toEqual({
    request: { dataJson: raw },
  });
  await waitFor(() =>
    expect(
      (screen.getByRole("button", { name: "Save" }) as HTMLButtonElement)
        .disabled,
    ).toBe(false),
  );
  fireEvent.click(screen.getByRole("button", { name: "Save" }));
  await waitFor(() => expect(mutations.save).toHaveBeenCalled());
  expect(mutations.create).toHaveBeenCalledTimes(1);
});

it("reports a created entry only once it is saved and clean", async () => {
  mutations.create.mockResolvedValue({ ...entry, published: true });
  const onCreated = vi.fn<(id: string) => void>();
  await renderWithApp(
    <RegistryEntryEditor
      id={null}
      onClose={vi.fn<() => void>()}
      onCreated={onCreated}
    />,
    { queryClient: client },
  );
  expect(
    screen.getByRole("heading", { name: "New registry entry", level: 1 }),
  ).toBeTruthy();
  fireEvent.change(await screen.findByLabelText("Record JSON"), {
    target: { value: raw },
  });
  expect(onCreated).not.toHaveBeenCalled();
  fireEvent.click(screen.getByRole("button", { name: "Save" }));
  await waitFor(() => expect(onCreated).toHaveBeenCalledWith(id));
});

it("opens the created entry's page in place of the create page, keeping list filters", async () => {
  mutations.create.mockResolvedValue({ ...entry, published: true });
  const { router } = await renderWithApp(<NewRegistryEntryPage />, {
    queryClient: client,
    initialPath: "/registry/new?q=demo",
  });
  fireEvent.change(await screen.findByLabelText("Record JSON"), {
    target: { value: raw },
  });
  fireEvent.click(screen.getByRole("button", { name: "Save" }));
  await waitFor(() =>
    expect(router.state.location.pathname).toBe(`/registry/${id}`),
  );
  expect(router.state.location.search).toEqual({ q: "demo" });
  expect(router.history.length).toBe(1);
});

it("reloads only after confirmation and uses the freshly fetched token", async () => {
  mutations.save
    .mockRejectedValueOnce(
      Object.assign(new Error("Stale entry"), { statusCode: 409 }),
    )
    .mockResolvedValue(entry);
  await mount();
  fireEvent.change(await screen.findByLabelText("Record JSON"), {
    target: { value: raw + " " },
  });
  fireEvent.click(screen.getByRole("button", { name: "Save" }));
  await screen.findByRole("alert");
  vi.stubGlobal("confirm", vi.fn().mockReturnValue(true));
  const latestToken = "2026-09-21T12:00:00.654321Z";
  vi.stubGlobal(
    "fetch",
    vi.fn().mockResolvedValue(
      new Response(
        JSON.stringify({
          id,
          data_json: raw + "\n",
          updated_at: latestToken,
          created_at: token,
          published: false,
          issues: [],
        }),
        { status: 200, headers: { "Content-Type": "application/json" } },
      ),
    ),
  );
  fireEvent.click(screen.getByRole("button", { name: "Reload" }));
  await waitFor(() =>
    expect(
      (screen.getByLabelText("Record JSON") as HTMLTextAreaElement).value,
    ).toBe(formatRegistryJson(raw + "\n")),
  );
  expect(mutations.save).toHaveBeenCalledTimes(1);
  expect(
    (screen.getByRole("button", { name: "Save" }) as HTMLButtonElement)
      .disabled,
  ).toBe(true);
  fireEvent.change(screen.getByLabelText("Record JSON"), {
    target: { value: raw },
  });
  fireEvent.click(screen.getByRole("button", { name: "Save" }));
  await waitFor(() => expect(mutations.save).toHaveBeenCalledTimes(2));
  expect(mutations.save.mock.calls[1]![0].request.updatedAt).toBe(latestToken);
});

it("blocks navigation when dirty discard is declined", async () => {
  const { router } = await mount();
  fireEvent.change(await screen.findByLabelText("Record JSON"), {
    target: { value: raw + " " },
  });
  vi.stubGlobal("confirm", vi.fn().mockReturnValue(false));
  void router.navigate({ to: "/projects" });
  await waitFor(() => expect(window.confirm).toHaveBeenCalled());
  expect(router.state.location.pathname).toBe("/");
  expect(mutations.save).not.toHaveBeenCalled();
});

it("keeps historically invalid text repairable with visible issue pointers", async () => {
  client.setQueryData(registryEntryQuery(id).queryKey, {
    ...entry,
    dataJson: "{",
    issues: [{ path: "/server", message: "Missing server" }],
  });
  await mount();
  const textarea = await screen.findByLabelText("Record JSON");
  expect((textarea as HTMLTextAreaElement).value).toBe("{");
  expect(
    screen.getByText("Stored record /server: Missing server"),
  ).toBeTruthy();
  fireEvent.blur(textarea);
  expect(textarea.getAttribute("aria-invalid")).toBe("true");
  fireEvent.change(textarea, { target: { value: raw } });
  expect(textarea.getAttribute("aria-invalid")).toBe("false");
  expect(
    (screen.getByRole("button", { name: "Save" }) as HTMLButtonElement)
      .disabled,
  ).toBe(false);
});

it("saves synthetic extension JSON verbatim with syntax validation", async () => {
  const approved =
    '{ "server": { "name": "io.example/test", "version": "1" }, "extension": 9007199254740993 }';
  client.setQueryData(registryEntryQuery(id).queryKey, {
    ...entry,
    dataJson: approved,
  });
  mutations.save.mockResolvedValue({ ...entry, dataJson: approved });
  await mount();
  expect(
    ((await screen.findByLabelText("Record JSON")) as HTMLTextAreaElement)
      .value,
  ).toBe(formatRegistryJson(approved));
  fireEvent.change(screen.getByLabelText("Record JSON"), {
    target: { value: approved },
  });
  fireEvent.click(screen.getByRole("button", { name: "Save" }));
  await waitFor(() => expect(mutations.save).toHaveBeenCalled());
  expect(mutations.save.mock.calls[0]![0].request.dataJson).toBe(approved);
});

it("does not prompt to discard while existing detail is loading", async () => {
  client.removeQueries({ queryKey: registryEntryQuery(id).queryKey });
  vi.stubGlobal("fetch", vi.fn().mockReturnValue(new Promise(() => {})));
  vi.stubGlobal("confirm", vi.fn().mockReturnValue(false));
  const onClose = vi.fn<() => void>();
  const { router } = await renderWithApp(
    <RegistryEntryEditor id={id} onClose={onClose} />,
    { queryClient: client },
  );
  fireEvent.click(screen.getByRole("button", { name: "Cancel" }));
  expect(onClose).toHaveBeenCalled();
  expect(window.confirm).not.toHaveBeenCalled();
  const unload = new Event("beforeunload", { cancelable: true });
  window.dispatchEvent(unload);
  expect(unload.defaultPrevented).toBe(false);
  await router.navigate({ to: "/projects" });
  expect(router.state.location.pathname).toBe("/projects");
  expect(window.confirm).not.toHaveBeenCalled();
});

it("retries an initial detail failure without showing perpetual loading", async () => {
  client.removeQueries({ queryKey: registryEntryQuery(id).queryKey });
  const fetch = vi
    .fn()
    .mockResolvedValueOnce(new Response("unavailable", { status: 503 }));
  vi.stubGlobal("fetch", fetch);
  await mount();
  await screen.findByRole("alert");
  expect(screen.queryByText("Loading entry…")).toBeNull();
  fetch.mockResolvedValue(
    new Response(
      JSON.stringify({
        id,
        data_json: raw,
        updated_at: token,
        created_at: token,
        published: false,
        issues: [],
      }),
      { status: 200, headers: { "Content-Type": "application/json" } },
    ),
  );
  fireEvent.click(screen.getByRole("button", { name: "Retry" }));
  expect(
    ((await screen.findByLabelText("Record JSON")) as HTMLTextAreaElement)
      .value,
  ).toBe(formatRegistryJson(raw));
  expect(screen.queryByRole("button", { name: "Retry" })).toBeNull();
  expect(mutations.save).not.toHaveBeenCalled();
});

it("opens create without fetching or retrying the disabled detail query", async () => {
  const fetch = vi.fn();
  vi.stubGlobal("fetch", fetch);
  await renderWithApp(
    <RegistryEntryEditor id={null} onClose={vi.fn<() => void>()} />,
    { queryClient: client },
  );
  await screen.findByLabelText("Record JSON");
  expect(screen.queryByText("Loading entry…")).toBeNull();
  expect(screen.queryByRole("button", { name: "Retry" })).toBeNull();
  expect(fetch).not.toHaveBeenCalled();
  expect(mutations.create).not.toHaveBeenCalled();
});

it("maps server issues after Save and clears stale markers on edit", async () => {
  mutations.save.mockRejectedValue(
    Object.assign(
      new Error("/server/name: required; /server/version: invalid"),
      { statusCode: 422 },
    ),
  );
  await mount();
  const editor = await screen.findByLabelText("Record JSON");
  fireEvent.change(editor, { target: { value: raw } });
  fireEvent.click(screen.getByRole("button", { name: "Save" }));
  await screen.findByRole("alert");
  expect(JSON.parse(editor.getAttribute("data-server-issues")!)).toHaveLength(
    2,
  );
  expect(screen.getByRole("list").textContent).toContain(
    "/server/version: invalid",
  );
  fireEvent.change(editor, { target: { value: raw + " " } });
  expect(editor.getAttribute("data-server-issues")).toBe("[]");
  expect(screen.queryByRole("alert")).toBeNull();
});

it("shows the latest request failure after retrying a validation failure", async () => {
  mutations.save
    .mockRejectedValueOnce(
      Object.assign(new Error("/server/name: required"), { statusCode: 422 }),
    )
    .mockRejectedValueOnce(
      Object.assign(new Error("Latest request unavailable"), {
        statusCode: 500,
      }),
    );
  await mount();
  const editor = await screen.findByLabelText("Record JSON");
  fireEvent.change(editor, { target: { value: raw + " " } });
  fireEvent.click(screen.getByRole("button", { name: "Save" }));
  expect((await screen.findByRole("alert")).textContent).toContain(
    "/server/name: required",
  );
  await waitFor(() =>
    expect(
      (screen.getByRole("button", { name: "Save" }) as HTMLButtonElement)
        .disabled,
    ).toBe(false),
  );
  fireEvent.click(screen.getByRole("button", { name: "Save" }));
  await waitFor(() =>
    expect(screen.getByRole("alert").textContent).toContain(
      "Latest request unavailable",
    ),
  );
  expect(screen.getByRole("alert").textContent).not.toContain("required");
  expect(editor.getAttribute("data-server-issues")).toBe("[]");
  expect((editor as HTMLTextAreaElement).value).toBe(raw + " ");
  expect(mutations.save.mock.calls[1]![0].request.updatedAt).toBe(token);
});

it.each([
  raw,
  '{"unknown":{"integer":9007199254740993,"exponent":1e999}}',
  '{"padding":"' + "x".repeat(8 * 1024 * 1024 - 100) + '"}',
])(
  "pretty prints stored syntax-valid JSON without dirtying the baseline %#",
  async (dataJson) => {
    client.setQueryData(registryEntryQuery(id).queryKey, {
      ...entry,
      dataJson,
    });
    const confirm = vi.fn();
    vi.stubGlobal("confirm", confirm);
    await mount();
    const editor = await screen.findByLabelText("Record JSON");
    expect(
      (editor as HTMLTextAreaElement).value === formatRegistryJson(dataJson),
    ).toBe(true);
    expect(
      (screen.getByRole("button", { name: "Save" }) as HTMLButtonElement)
        .disabled,
    ).toBe(true);
    expect(
      (screen.getByRole("button", { name: "Republish" }) as HTMLButtonElement)
        .disabled,
    ).toBe(false);
    fireEvent.click(screen.getByRole("button", { name: "Cancel" }));
    expect(confirm).not.toHaveBeenCalled();
  },
);

it("waits for a stale cached entry's mount refetch before accepting its token", async () => {
  client.setDefaultOptions({ queries: { retry: false, staleTime: 0 } });
  let resolve!: (response: Response) => void;
  vi.stubGlobal(
    "fetch",
    vi.fn(
      () =>
        new Promise<Response>((done) => {
          resolve = done;
        }),
    ),
  );
  await mount();
  expect(screen.queryByLabelText("Record JSON")).toBeNull();
  await waitFor(() => expect(resolve).toBeTypeOf("function"));
  const freshToken = "2026-09-22T12:00:00.654321Z";
  resolve(
    new Response(
      JSON.stringify({
        id,
        data_json: raw,
        updated_at: freshToken,
        created_at: token,
        published: false,
        issues: [],
      }),
      { headers: { "Content-Type": "application/json" } },
    ),
  );
  mutations.save.mockResolvedValue(entry);
  fireEvent.change(await screen.findByLabelText("Record JSON"), {
    target: { value: raw + " " },
  });
  fireEvent.click(screen.getByRole("button", { name: "Save" }));
  await waitFor(() => expect(mutations.save).toHaveBeenCalled());
  expect(mutations.save.mock.calls[0]![0].request.updatedAt).toBe(freshToken);
});

it("lets a duplicate-name create conflict be corrected without reloading", async () => {
  mutations.create
    .mockRejectedValueOnce(
      Object.assign(new Error("Duplicate name"), { statusCode: 409 }),
    )
    .mockResolvedValue(entry);
  await renderWithApp(
    <RegistryEntryEditor id={null} onClose={vi.fn<() => void>()} />,
    {
      queryClient: client,
    },
  );
  fireEvent.change(await screen.findByLabelText("Record JSON"), {
    target: { value: raw },
  });
  fireEvent.click(screen.getByRole("button", { name: "Save" }));
  await screen.findByRole("alert");
  expect(screen.queryByRole("button", { name: "Reload" })).toBeNull();
  fireEvent.change(screen.getByLabelText("Record JSON"), {
    target: { value: raw.replace("demo", "another") },
  });
  fireEvent.click(screen.getByRole("button", { name: "Save" }));
  await waitFor(() => expect(mutations.create).toHaveBeenCalledTimes(2));
});

function pageRoutes() {
  const root = createRootRoute({ component: Outlet });
  const registry = createRoute({
    getParentRoute: () => root,
    path: "registry",
    validateSearch: registrySearch,
    component: Outlet,
  });
  return root.addChildren([
    registry.addChildren([
      createRoute({
        getParentRoute: () => registry,
        path: "/",
        component: () => <p>Registry list</p>,
      }),
      createRoute({
        getParentRoute: () => registry,
        path: "$entryId",
        component: RegistryEntryPage,
      }),
      createRoute({
        getParentRoute: () => registry,
        path: "new",
        component: NewRegistryEntryPage,
      }),
    ]),
  ]);
}

it("returns from the entry page after only one discard confirmation", async () => {
  const confirm = vi.fn().mockReturnValue(true);
  vi.stubGlobal("confirm", confirm);
  const { router } = await renderRouteTree(pageRoutes(), {
    initialPath: `/registry/${id}?q=demo&cursors=%5B%22page-two%22%5D`,
    queryClient: client,
  });
  fireEvent.change(await screen.findByLabelText("Record JSON"), {
    target: { value: raw + " " },
  });
  fireEvent.click(screen.getByRole("button", { name: "Cancel" }));
  await screen.findByText("Registry list");
  expect(confirm).toHaveBeenCalledTimes(1);
  expect(router.state.location.pathname).toBe("/registry");
  expect(router.state.location.search).toEqual({
    q: "demo",
    cursors: ["page-two"],
  });
});

it("replaces the new-entry page with the created entry", async () => {
  mutations.create.mockResolvedValue(entry);
  const { router } = await renderRouteTree(pageRoutes(), {
    initialPath: "/registry/new?q=demo",
    queryClient: client,
  });
  const length = router.history.length;
  fireEvent.change(await screen.findByLabelText("Record JSON"), {
    target: { value: raw },
  });
  fireEvent.click(screen.getByRole("button", { name: "Save" }));
  await waitFor(() =>
    expect(router.state.location.pathname).toBe(`/registry/${id}`),
  );
  expect(router.history.length).toBe(length);
  expect(router.state.location.search).toEqual({ q: "demo" });
});

it("locks JSON and Save during upload and saves icons with the same draft after retry", async () => {
  let resolve!: (value: unknown) => void;
  mutations.upload.mockImplementation(
    () =>
      new Promise((r) => {
        resolve = r;
      }),
  );
  mutations.save.mockRejectedValueOnce(new Error("Temporary save failure"));
  await mount();
  const editor = (await screen.findByLabelText(
    "Record JSON",
  )) as HTMLTextAreaElement;
  fireEvent.change(screen.getByLabelText("Upload primary logo file"), {
    target: {
      files: [new File(["<svg/>"], "logo.svg", { type: "image/svg+xml" })],
    },
  });
  expect(editor.disabled).toBe(true);
  expect(
    (screen.getByRole("button", { name: "Save" }) as HTMLButtonElement)
      .disabled,
  ).toBe(true);
  await act(async () =>
    resolve({
      url: "https://public.example.test/rpc/assets.serveImage?id=accepted",
      asset: { id: "accepted", contentType: "image/svg+xml" },
    }),
  );
  const draft = editor.value;
  expect(draft).toContain("9007199254740993");
  expect(draft).not.toContain("com.speakeasy.ai/icons");
  expect(screen.getByAltText("Light preview").getAttribute("src")).toContain(
    "id=accepted",
  );
  fireEvent.click(screen.getByRole("button", { name: "Save" }));
  await screen.findByText("Temporary save failure");
  expect(editor.value).toBe(draft);
  const fullDraft = mutations.save.mock.calls[0]![0].request.dataJson;
  expect(fullDraft).toContain("9007199254740993");
  expect(fullDraft).toContain("com.speakeasy.ai/icons");
  mutations.save.mockResolvedValueOnce({ ...entry, dataJson: fullDraft });
  fireEvent.click(screen.getByRole("button", { name: "Save" }));
  await waitFor(() => expect(mutations.save).toHaveBeenCalledTimes(2));
  expect(mutations.save.mock.calls[1]![0].request.dataJson).toBe(fullDraft);
});

it("cannot leave through Cancel while an icon upload is in flight", async () => {
  let resolve!: (value: unknown) => void;
  mutations.upload.mockImplementation(
    () =>
      new Promise((r) => {
        resolve = r;
      }),
  );
  const onClose = vi.fn<() => void>();
  await renderWithApp(<RegistryEntryEditor id={id} onClose={onClose} />, {
    queryClient: client,
  });
  await screen.findByLabelText("Record JSON");
  fireEvent.change(screen.getByLabelText("Upload primary logo file"), {
    target: {
      files: [new File(["<svg/>"], "logo.svg", { type: "image/svg+xml" })],
    },
  });
  const cancel = screen.getByRole("button", {
    name: "Cancel",
  }) as HTMLButtonElement;
  expect(cancel.disabled).toBe(true);
  fireEvent.click(cancel);
  expect(onClose).not.toHaveBeenCalled();
  await act(async () =>
    resolve({
      url: "https://public.example.test/rpc/assets.serveImage?id=accepted",
      asset: { id: "accepted", contentType: "image/svg+xml" },
    }),
  );
  expect(cancel.disabled).toBe(false);
});

it("retains icon draft on conflict and keeps Save locked until reload", async () => {
  mutations.upload.mockResolvedValue({
    url: "https://public.example.test/rpc/assets.serveImage?id=accepted",
    asset: { id: "accepted", contentType: "image/svg+xml" },
  });
  mutations.save.mockRejectedValue(
    Object.assign(new Error("Conflict"), { statusCode: 409 }),
  );
  await mount();
  const editor = (await screen.findByLabelText(
    "Record JSON",
  )) as HTMLTextAreaElement;
  fireEvent.change(screen.getByLabelText("Upload primary logo file"), {
    target: {
      files: [new File(["<svg/>"], "logo.svg", { type: "image/svg+xml" })],
    },
  });
  await screen.findByAltText("Light preview");
  const draft = editor.value;
  fireEvent.click(screen.getByRole("button", { name: "Save" }));
  await screen.findByRole("button", { name: "Reload" });
  expect(editor.value).toBe(draft);
  expect(
    (screen.getByRole("button", { name: "Save" }) as HTMLButtonElement)
      .disabled,
  ).toBe(true);
});

const managedRecord =
  '{"server":{"name":"example.test/demo"},"future":9007199254740993,"_meta":{"com.speakeasy.ai/icons":{"default":{"src":"https://public.example.test/rpc/assets.serveImage?id=existing","mimeType":"image/svg+xml","sizes":["any"]}},"com.speakeasy.ai/registry":{"published":false,"future":9007199254740995},"com.speakeasy.ai/catalog":{"name":"editable"}}}';

it("hides managed namespaces, keeps them when _meta is removed, and accepts a clean save", async () => {
  client.setQueryData(registryEntryQuery(id).queryKey, {
    ...entry,
    dataJson: managedRecord,
  });
  mutations.save.mockImplementation(async ({ request }) => ({
    ...entry,
    dataJson: request.dataJson,
  }));
  await mount();
  const editor = (await screen.findByLabelText(
    "Record JSON",
  )) as HTMLTextAreaElement;
  expect(editor.value).not.toContain("com.speakeasy.ai/icons");
  expect(editor.value).not.toContain("com.speakeasy.ai/registry");
  expect(editor.value).toContain("com.speakeasy.ai/catalog");
  expect(
    screen.getByText("Icons and publication metadata are managed separately."),
  ).toBeTruthy();
  fireEvent.change(editor, {
    target: {
      value:
        '{"server":{"name":"example.test/demo"},"future":9007199254740993}',
    },
  });
  fireEvent.click(screen.getByRole("button", { name: "Save" }));
  await waitFor(() => expect(mutations.save).toHaveBeenCalledOnce());
  const saved = mutations.save.mock.calls[0]![0].request.dataJson;
  expect(saved).toContain("id=existing");
  expect(saved).toContain("9007199254740993");
  expect(saved).toContain("9007199254740995");
  await waitFor(() =>
    expect(
      (screen.getByRole("button", { name: "Save" }) as HTMLButtonElement)
        .disabled,
    ).toBe(true),
  );
  expect(editor.value).not.toContain("com.speakeasy.ai/registry");
});

it.each(["com.speakeasy.ai/icons", "com.speakeasy.ai/registry"])(
  "blocks reintroduced %s with actionable feedback",
  async (key) => {
    await mount();
    const editor = await screen.findByLabelText("Record JSON");
    fireEvent.change(editor, {
      target: {
        value: `{"server":{"name":"example.test/demo"},"_meta":{"${key}":{}}}`,
      },
    });
    fireEvent.click(screen.getByRole("button", { name: "Save" }));
    expect((await screen.findByRole("alert")).textContent).toContain(
      `Remove ${key} from Record JSON.`,
    );
    expect(mutations.save).not.toHaveBeenCalled();
  },
);

it("removes an icon only through the shared explicit Save", async () => {
  client.setQueryData(registryEntryQuery(id).queryKey, {
    ...entry,
    dataJson: managedRecord,
  });
  mutations.save.mockImplementation(async ({ request }) => ({
    ...entry,
    dataJson: request.dataJson,
  }));
  await mount();
  fireEvent.click(
    await screen.findByRole("button", { name: "Remove primary logo" }),
  );
  expect(mutations.save).not.toHaveBeenCalled();
  fireEvent.click(screen.getByRole("button", { name: "Save" }));
  await waitFor(() => expect(mutations.save).toHaveBeenCalledOnce());
  const saved = mutations.save.mock.calls[0]![0].request.dataJson;
  expect(saved).not.toContain("id=existing");
  expect(saved).toContain("com.speakeasy.ai/registry");
  expect(saved).toContain("9007199254740995");
});

it("creates with an uploaded icon without exposing managed JSON", async () => {
  mutations.upload.mockResolvedValue({
    url: "https://public.example.test/rpc/assets.serveImage?id=new",
    asset: { id: "new", contentType: "image/svg+xml" },
  });
  mutations.create.mockImplementation(async ({ request }) => ({
    ...entry,
    dataJson: request.dataJson,
  }));
  await renderWithApp(
    <RegistryEntryEditor id={null} onClose={vi.fn<() => void>()} />,
    { queryClient: client },
  );
  const editor = (await screen.findByLabelText(
    "Record JSON",
  )) as HTMLTextAreaElement;
  fireEvent.change(editor, {
    target: {
      value: '{"server":{"name":"example.test/new"},"future":9007199254740993}',
    },
  });
  fireEvent.change(screen.getByLabelText("Upload primary logo file"), {
    target: {
      files: [new File(["<svg/>"], "logo.svg", { type: "image/svg+xml" })],
    },
  });
  await screen.findByAltText("Light preview");
  expect(editor.value).not.toContain("com.speakeasy.ai/icons");
  expect(mutations.create).not.toHaveBeenCalled();
  fireEvent.click(screen.getByRole("button", { name: "Save" }));
  await waitFor(() => expect(mutations.create).toHaveBeenCalledOnce());
  const saved = mutations.create.mock.calls[0]![0].request.dataJson;
  expect(saved).toContain("id=new");
  expect(saved).toContain("9007199254740993");
});
