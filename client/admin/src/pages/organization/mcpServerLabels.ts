// What the list and a server's health page call a server's source and
// visibility. Keyed on the wire value, which the two endpoints share.
export const SOURCE_LABELS: Record<string, string> = {
  toolset: "Toolset",
  remote: "Remote",
  tunneled: "Tunneled",
  unproxied: "Unproxied",
  toolset_only: "Legacy toolset",
};

export const VISIBILITY_LABELS: Record<string, string> = {
  public: "Public",
  private: "Private",
  disabled: "Disabled",
};
