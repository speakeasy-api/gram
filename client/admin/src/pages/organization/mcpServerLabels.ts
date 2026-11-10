import type { AdminMcpServerHealthServer } from "@gram/admin-client/models/components/adminmcpserverhealthserver";

import type { AdminMcpServer, AdminMcpServerSource } from "@/lib/gramAdminApi";

// What the list and a server's health page call a server's source and
// visibility. Keyed on every value either endpoint can send, so a new one on
// either side fails the build here instead of rendering as a raw wire string.
export const SOURCE_LABELS: Record<
  AdminMcpServerSource | AdminMcpServerHealthServer["source"],
  string
> = {
  toolset: "Toolset",
  remote: "Remote",
  tunneled: "Tunneled",
  unproxied: "Unproxied",
  toolset_only: "Legacy toolset",
};

export const VISIBILITY_LABELS: Record<
  AdminMcpServer["visibility"] | AdminMcpServerHealthServer["visibility"],
  string
> = {
  public: "Public",
  private: "Private",
  disabled: "Disabled",
};
