import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
} from "@testing-library/react";
import { useState } from "react";
import { afterEach, expect, it, vi } from "vitest";
import { RegistryIcons } from "./RegistryIcons.tsx";
import { setCatalogIcon } from "./registryIcons";
const upload = vi.hoisted(() => vi.fn());
vi.mock("@/lib/gramAdminClient", () => ({ adminUploadPlatformImage: upload }));
afterEach(() => {
  cleanup();
  vi.resetAllMocks();
});
const primary = {
  src: `${window.location.origin}/rpc/assets.serveImage?id=primary`,
  mimeType: "image/svg+xml" as const,
  sizes: ["any"] as ["any"],
};
const draft = setCatalogIcon("{}", "default", primary);
function Harness({
  initial = draft,
  disabled = false,
}: {
  initial?: string;
  disabled?: boolean;
}) {
  const [value, onChange] = useState(initial);
  return (
    <>
      <RegistryIcons
        value={value}
        onChange={onChange}
        disabled={disabled}
        onBusyChange={vi.fn<(busy: boolean) => void>()}
      />
      <textarea
        aria-label="draft"
        value={value}
        onChange={(e) => onChange(e.target.value)}
      />
    </>
  );
}
function select(name = "Upload primary logo") {
  fireEvent.change(screen.getByLabelText(`${name} file`), {
    target: {
      files: [new File(["<svg/>"], "logo.svg", { type: "image/svg+xml" })],
    },
  });
}
it("shows primary fallback, both logos, dark-only and empty previews from the draft", () => {
  render(<Harness />);
  expect(screen.getByAltText("Light preview").getAttribute("src")).toBe(
    primary.src,
  );
  expect(screen.getByText("Using primary")).toBeTruthy();
  fireEvent.change(screen.getByLabelText("draft"), {
    target: {
      value: setCatalogIcon(draft, "dark", {
        ...primary,
        src: primary.src + "-dark",
      }),
    },
  });
  expect(screen.queryByText("Using primary")).toBeNull();
  fireEvent.click(screen.getByRole("button", { name: "Remove primary logo" }));
  expect(screen.queryByAltText("Light preview")).toBeNull();
  expect(screen.getByAltText("Dark preview")).toBeTruthy();
  fireEvent.click(
    screen.getByRole("button", { name: "Remove dark-mode logo" }),
  );
  expect(screen.queryAllByRole("img")).toHaveLength(0);
});
it("uploads only accepted assets and locks concurrent actions", async () => {
  let resolve!: (value: unknown) => void;
  upload.mockImplementation(
    () =>
      new Promise((r) => {
        resolve = r;
      }),
  );
  render(<Harness initial="{}" />);
  select();
  expect(
    (screen.getByLabelText("Upload dark-mode logo file") as HTMLInputElement)
      .disabled,
  ).toBe(true);
  expect(screen.queryAllByRole("img")).toHaveLength(0);
  await act(async () =>
    resolve({
      url: "https://public.example.test/rpc/assets.serveImage?id=accepted",
      asset: { id: "accepted", contentType: "image/svg+xml" },
    }),
  );
  expect(screen.getByAltText("Light preview").getAttribute("src")).toBe(
    "https://public.example.test/rpc/assets.serveImage?id=accepted",
  );
  expect(screen.getByText("logo.svg")).toBeTruthy();
});
it("preserves prior draft on rejection and ignores late completion after close", async () => {
  upload.mockRejectedValueOnce(new Error("Re-export a static, flat-path SVG"));
  const view = render(<Harness />);
  select("Replace primary logo");
  await screen.findByText("Re-export a static, flat-path SVG");
  expect(screen.getByAltText("Light preview").getAttribute("src")).toBe(
    primary.src,
  );
  view.unmount();
  let resolve!: (value: unknown) => void;
  upload.mockImplementation(
    () =>
      new Promise((r) => {
        resolve = r;
      }),
  );
  const onChange = vi.fn<(value: string) => void>();
  const next = render(
    <RegistryIcons
      value="{}"
      disabled={false}
      onChange={onChange}
      onBusyChange={vi.fn<(busy: boolean) => void>()}
    />,
  );
  select();
  next.unmount();
  render(<Harness initial="{}" />);
  await act(async () =>
    resolve({
      url: "https://public.example.test/rpc/assets.serveImage?id=late",
      asset: { id: "late", contentType: "image/svg+xml" },
    }),
  );
  expect(onChange).not.toHaveBeenCalled();
  expect((screen.getByLabelText("draft") as HTMLTextAreaElement).value).toBe(
    "{}",
  );
  expect(screen.queryAllByRole("img")).toHaveLength(0);
});
it("disables writes for invalid JSON or parent busy state", () => {
  const view = render(<Harness disabled />);
  expect(
    (screen.getByLabelText("Replace primary logo file") as HTMLInputElement)
      .disabled,
  ).toBe(true);
  view.unmount();
  render(<Harness initial="{" />);
  expect(screen.getByText("Fix the JSON before editing icons.")).toBeTruthy();
  expect(
    (screen.getByLabelText("Upload primary logo file") as HTMLInputElement)
      .disabled,
  ).toBe(true);
});
it.each([
  'data:image/svg+xml,<svg onload="alert(1)"/>',
  "blob:http://localhost/untrusted",
  "http://public.example.test/rpc/assets.serveImage?id=insecure",
])("never previews unsafe URL %s from JSON", (src) => {
  render(
    <Harness
      initial={setCatalogIcon("{}", "default", {
        ...primary,
        src,
      })}
    />,
  );
  expect(screen.queryAllByRole("img")).toHaveLength(0);
});

it.each([
  undefined,
  "",
  "https://public.example.test/other?id=accepted",
  "data:image/svg+xml,<svg/>",
  "https://public.example.test/rpc/assets.serveImage?id=different",
])(
  "rejects missing or invalid upload URL %s without changing the draft",
  async (url) => {
    upload.mockResolvedValue({
      url,
      asset: { id: "accepted", contentType: "image/svg+xml" },
    });
    render(<Harness />);
    select("Replace primary logo");
    await screen.findByText(
      "The upload did not return a valid public asset URL.",
    );
    expect(screen.getByAltText("Light preview").getAttribute("src")).toBe(
      primary.src,
    );
  },
);

it("opens hidden file pickers from the styled upload and replace buttons", () => {
  render(<Harness />);
  for (const name of ["Replace primary logo", "Upload dark-mode logo"]) {
    const input = screen.getByLabelText(`${name} file`) as HTMLInputElement;
    expect(input.hidden).toBe(true);
    const click = vi.spyOn(input, "click").mockImplementation(() => {});
    fireEvent.click(screen.getByRole("button", { name }));
    expect(click).toHaveBeenCalledOnce();
    click.mockRestore();
  }
});

it.each([
  ["logo.png", "image/png"],
  ["logo.svg", "image/png"],
])("rejects non-SVG file %s (%s) before upload", (name, type) => {
  render(<Harness />);
  fireEvent.change(screen.getByLabelText("Replace primary logo file"), {
    target: { files: [new File(["bytes"], name, { type })] },
  });
  expect(screen.getByRole("alert").textContent).toBe("Choose an SVG file.");
  expect(upload).not.toHaveBeenCalled();
  expect((screen.getByLabelText("draft") as HTMLTextAreaElement).value).toBe(
    draft,
  );
});
it("rejects non-SVG upload responses without changing the draft", async () => {
  upload.mockResolvedValue({
    url: primary.src,
    asset: { id: "primary", contentType: "image/png" },
  });
  render(<Harness />);
  select("Replace primary logo");
  await screen.findByText("The uploaded asset must be an SVG.");
  expect((screen.getByLabelText("draft") as HTMLTextAreaElement).value).toBe(
    draft,
  );
});
it("discloses persistent public uploads before choosing a file", () => {
  render(<Harness />);
  expect(
    screen.getByText(/Uploads are public immediately/).textContent,
  ).toContain("cancel");
});
it.each(["localhost", "127.0.0.1", "[::1]"])(
  "allows loopback HTTP previews on %s",
  (host) => {
    render(
      <Harness
        initial={setCatalogIcon("{}", "default", {
          ...primary,
          src: `http://${host}/rpc/assets.serveImage?id=local`,
        })}
      />,
    );
    expect(screen.getByAltText("Light preview")).toBeTruthy();
  },
);
