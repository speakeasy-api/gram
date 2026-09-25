import { fireEvent, screen, waitFor, cleanup } from "@testing-library/react";
import { QueryClient } from "@tanstack/react-query";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { renderWithApp } from "@/test/harness";
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
}));
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
import { RegistryEntrySheet } from "./RegistryEntrySheet";

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
    <RegistryEntrySheet
      id={id}
      open
      onOpenChange={vi.fn<(open: boolean) => void>()}
    />,
    { queryClient: client },
  );
}

it("saves only explicitly, preserving raw large integers and the opaque token on unpublished rows", async () => {
  mutations.save.mockResolvedValue({ ...entry, updatedAt: token + "1" });
  await mount();
  await screen.findByLabelText("Record JSON");
  expect(mutations.save).not.toHaveBeenCalled();
  fireEvent.click(screen.getByRole("button", { name: "Save" }));
  await waitFor(() => expect(mutations.save).toHaveBeenCalledTimes(1));
  expect(mutations.save.mock.calls[0]![0]).toEqual({
    request: { id, dataJson: raw, updatedAt: token },
  });
  expect(mutations.visibility).not.toHaveBeenCalled();
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
  fireEvent.click(await screen.findByRole("button", { name: "Save" }));
  fireEvent.click(screen.getByRole("button", { name: "Save" }));
  await waitFor(() => expect(mutations.save).toHaveBeenCalledTimes(1));
  expect(
    (screen.getByRole("button", { name: "Cancel" }) as HTMLButtonElement)
      .disabled,
  ).toBe(true);
});

it("never fetches a schema and delegates field validation on explicit Save", async () => {
  const fetch = vi.fn().mockRejectedValue(new Error("Unexpected schema fetch"));
  vi.stubGlobal("fetch", fetch);
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
  expect(fetch).not.toHaveBeenCalled();
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
  fireEvent.click(await screen.findByRole("button", { name: "Save" }));
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
    <RegistryEntrySheet
      id={null}
      open
      onOpenChange={vi.fn<(open: boolean) => void>()}
    />,
    { queryClient: client },
  );
  fireEvent.change(await screen.findByLabelText("Record JSON"), {
    target: { value: raw },
  });
  fireEvent.click(screen.getByRole("button", { name: "Save" }));
  await screen.findByRole("button", { name: "Unpublish" });
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
    ).toBe(raw + "\n"),
  );
  expect(mutations.save).toHaveBeenCalledTimes(1);
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
  ).toBe(approved);
  fireEvent.click(screen.getByRole("button", { name: "Save" }));
  await waitFor(() => expect(mutations.save).toHaveBeenCalled());
  expect(mutations.save.mock.calls[0]![0].request.dataJson).toBe(approved);
});

it("does not prompt to discard while existing detail is loading", async () => {
  client.removeQueries({ queryKey: registryEntryQuery(id).queryKey });
  vi.stubGlobal("fetch", vi.fn().mockReturnValue(new Promise(() => {})));
  vi.stubGlobal("confirm", vi.fn().mockReturnValue(false));
  const onOpenChange = vi.fn<(open: boolean) => void>();
  const { router } = await renderWithApp(
    <RegistryEntrySheet id={id} open onOpenChange={onOpenChange} />,
    { queryClient: client },
  );
  fireEvent.click(screen.getByRole("button", { name: "Cancel" }));
  expect(onOpenChange).toHaveBeenCalledWith(false);
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
  ).toBe(raw);
  expect(screen.queryByRole("button", { name: "Retry" })).toBeNull();
  expect(mutations.save).not.toHaveBeenCalled();
});

it("opens create without fetching or retrying the disabled detail query", async () => {
  const fetch = vi.fn();
  vi.stubGlobal("fetch", fetch);
  await renderWithApp(
    <RegistryEntrySheet id={null} open onOpenChange={vi.fn<() => void>()} />,
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
