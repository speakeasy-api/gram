import { useId, useRef, useState } from "react";
// oxlint-disable-next-line no-restricted-imports -- uploads must ignore completions after unmount
import { useEffect } from "react";
import { Button } from "@/components/ui/button";
import { adminUploadPlatformImage } from "@/lib/gramAdminClient";
import {
  readCatalogIcons,
  setCatalogIcon,
  type IconSlot,
} from "./registryIcons";
export type RegistryIconsProps = {
  value: string;
  disabled: boolean;
  onChange: (value: string) => void;
  onBusyChange: (busy: boolean) => void;
};
export function RegistryIcons({
  value,
  disabled,
  onChange,
  onBusyChange,
}: RegistryIconsProps) {
  const id = useId();
  const active = useRef(true);
  const uploading = useRef(false);
  const latest = useRef(value);
  latest.current = value;
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [filenames, setFilenames] = useState<
    Partial<Record<IconSlot, { src: string; name: string }>>
  >({});
  useEffect(() => {
    active.current = true;
    return () => {
      active.current = false;
    };
  }, []);
  const icons = readCatalogIcons(value);
  let repair = "";
  try {
    setCatalogIcon(value, "default", icons.default ?? null);
  } catch {
    repair = "Fix the JSON before editing icons.";
  }
  const locked = disabled || busy || !!repair;
  async function upload(slot: IconSlot, file: File) {
    if (locked || uploading.current) return;
    setError("");
    if (
      !file.name.toLowerCase().endsWith(".svg") ||
      (file.type !== "" && file.type !== "image/svg+xml")
    ) {
      setError("Choose an SVG file.");
      return;
    }
    uploading.current = true;
    setBusy(true);
    onBusyChange(true);
    try {
      const result = await adminUploadPlatformImage(file);
      if (!active.current) return;
      if (result.asset.contentType !== "image/svg+xml")
        throw new Error("The uploaded asset must be an SVG.");
      const src = result.url;
      if (!src || !safePreview(src))
        throw new Error("The upload did not return a valid public asset URL.");
      const publicURL = new URL(src);
      if (
        publicURL.hash ||
        publicURL.searchParams.size !== 1 ||
        publicURL.searchParams.get("id") !== result.asset.id
      )
        throw new Error("The upload did not return a valid public asset URL.");
      onChange(
        setCatalogIcon(latest.current, slot, {
          src,
          mimeType: "image/svg+xml",
          sizes: ["any"],
        }),
      );
      setFilenames((names) => ({ ...names, [slot]: { src, name: file.name } }));
    } catch (cause) {
      if (active.current)
        setError(
          cause instanceof Error
            ? cause.message
            : "Upload failed. Your existing icon has been retained.",
        );
    } finally {
      uploading.current = false;
      if (active.current) {
        setBusy(false);
        onBusyChange(false);
      }
    }
  }
  return (
    <section
      aria-label="Speakeasy-curated icons"
      className="space-y-3 rounded-md border p-3"
    >
      <div>
        <h3 className="text-sm font-medium">Icons</h3>
        <p className="text-muted-foreground text-xs">
          Speakeasy-curated icons. Upstream icons are unchanged.
        </p>
      </div>
      <div className="grid grid-cols-2 gap-2">
        {(
          [
            ["Light", icons.default],
            ["Dark", icons.dark ?? icons.default],
          ] as const
        ).map(([theme, icon]) => (
          <div
            key={theme}
            className={`flex h-20 items-center justify-center gap-3 rounded border p-2 ${theme === "Dark" ? "bg-zinc-950 text-white" : "bg-white text-zinc-950"}`}
          >
            {icon && safePreview(icon.src) ? (
              <img
                src={safePreview(icon.src)}
                alt={`${theme} preview`}
                className="size-10 object-contain"
              />
            ) : (
              <span className="text-xs">No logo</span>
            )}
            <div className="text-xs">
              {theme}
              {theme === "Dark" && !icons.dark && icons.default && (
                <div>Using primary</div>
              )}
            </div>
          </div>
        ))}
      </div>
      {(["default", "dark"] as const).map((slot) => {
        const icon = icons[slot];
        const name = slot === "default" ? "primary logo" : "dark-mode logo";
        return (
          <div
            key={slot}
            className="flex flex-wrap items-center gap-2 border-t pt-2"
          >
            {icon && safePreview(icon.src) && (
              <img
                src={safePreview(icon.src)}
                alt={`${name} thumbnail`}
                className="size-8 object-contain"
              />
            )}
            <div className="min-w-0 flex-1">
              <label htmlFor={`${id}-${slot}`} className="text-sm font-medium">
                {slot === "default"
                  ? "Primary logo"
                  : "Dark-mode logo (optional)"}
              </label>
              <p className="text-muted-foreground truncate text-xs">
                {icon
                  ? filenames[slot]?.src === icon.src
                    ? filenames[slot]?.name
                    : slot === "default"
                      ? "Primary SVG"
                      : "Dark-mode SVG"
                  : "No SVG uploaded"}
              </p>
            </div>
            <input
              id={`${id}-${slot}`}
              className="max-w-52 text-xs"
              type="file"
              accept=".svg,image/svg+xml"
              aria-label={`${icon ? "Replace" : "Upload"} ${name}`}
              disabled={locked}
              onChange={(event) => {
                const file = event.target.files?.[0];
                event.target.value = "";
                if (file) void upload(slot, file);
              }}
            />
            {icon && (
              <Button
                type="button"
                variant="ghost"
                size="sm"
                aria-label={`Remove ${name}`}
                disabled={locked}
                onClick={() => {
                  if (uploading.current) return;
                  setError("");
                  onChange(setCatalogIcon(value, slot, null));
                }}
              >
                Remove
              </Button>
            )}
            <p className="text-muted-foreground w-full text-xs">
              {slot === "default"
                ? "Used for both themes unless you add a dark-mode logo."
                : !icons.dark && icons.default
                  ? "Using primary logo as fallback."
                  : "Optional alternative for dark backgrounds."}
            </p>
          </div>
        );
      })}
      {busy && (
        <p role="status" className="text-xs">
          Uploading SVG…
        </p>
      )}
      {(repair || error) && (
        <p role="alert" className="text-destructive text-sm">
          {repair || error}
        </p>
      )}
    </section>
  );
}
// Never render data/blob URLs from the editable JSON as SVG previews.
function safePreview(src: string): string | undefined {
  try {
    const url = new URL(src);
    return (url.protocol === "https:" || url.protocol === "http:") &&
      !url.username &&
      !url.password &&
      url.pathname === "/rpc/assets.serveImage" &&
      url.searchParams.has("id")
      ? src
      : undefined;
  } catch {
    return undefined;
  }
}
