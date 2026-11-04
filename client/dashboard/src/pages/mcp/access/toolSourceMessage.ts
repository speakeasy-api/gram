import type { ToolSource } from "@/pages/access/useServerTools";

/** Explain why a tool catalogue is unavailable without confusing it with a dynamic server. */
export function toolSourceUnavailableMessage(source?: ToolSource): string {
  if (source?.status === "needs-connect") {
    return source.connect
      ? "Connect to this server to load its tools."
      : "Connect isn't available for this server. Review its authentication settings.";
  }
  if (source?.status === "needs-write") {
    return "Someone with write access to this server must load its tools before they can be selected here.";
  }
  return "Tools are resolved when the server is called";
}
