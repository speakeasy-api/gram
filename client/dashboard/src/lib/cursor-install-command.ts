/**
 * Install a trusted Speakeasy ZIP from ~/Downloads. Requires Bash, unzip and Python 3
 * on macOS/Linux (not native PowerShell). Validation does not execute hooks or
 * prove Cursor discovery. Only the setup wizard's known slug template is allowed;
 * it must be resolved before running the command.
 */
export function getCursorInstallCommand({
  pluginName,
  archiveName,
  requireHooks = false,
}: {
  pluginName: string;
  archiveName: string;
  requireHooks?: boolean;
}): string {
  if (
    !/^[a-z0-9]+(?:-[a-z0-9]+)*$/.test(pluginName) &&
    pluginName !== "{{GRAM_CURSOR_PLUGIN_NAME}}"
  ) {
    throw new Error("A resolved, lowercase plugin slug is required");
  }
  if (!/^[a-zA-Z0-9][a-zA-Z0-9._ ()-]*\.zip$/.test(archiveName)) {
    throw new Error("A ZIP filename, not a path, is required");
  }

  // All interpolated values are allowlisted above. Quoted heredocs prevent the
  // caller's shell from expanding HOME or interpreting the embedded Python.
  return `# macOS/Linux: requires bash, unzip, and python3. Use a freshly downloaded ZIP.
bash <<'GRAM_CURSOR_INSTALL'
set -euo pipefail
plugin='${pluginName}'
[[ "$plugin" =~ ^[a-z0-9]+(-[a-z0-9]+)*$ ]] || { echo "Resolve the Cursor plugin name before installing." >&2; exit 1; }
for tool in unzip python3; do
  command -v "$tool" >/dev/null || { echo "Required command not found: $tool" >&2; exit 1; }
done
: "\${HOME:?HOME must be set}"
archive="$HOME/Downloads/"'${archiveName}'
[[ -f "$archive" ]] || { echo "Download the ZIP to $archive first." >&2; exit 1; }
parent="$HOME/.cursor/plugins/local"
destination="$parent/$plugin"
mkdir -p "$parent"
lock="$parent/.$plugin.install-lock"
mkdir "$lock" || { echo "Another install may be running; inspect $lock before retrying." >&2; exit 1; }
stage=''
committed=0
cleanup() {
  status=$?
  trap - EXIT HUP INT TERM
  if [[ -n "$stage" && -e "$stage/previous" && "$committed" = 0 ]]; then
    if [[ -e "$destination" || -L "$destination" ]] || ! mv "$stage/previous" "$destination"; then
      echo "Restore the previous installation from $stage/previous; backup retained." >&2
      rmdir "$lock"
      exit 1
    fi
  fi
  if [[ -n "$stage" ]]; then rm -rf "$stage"; fi
  rmdir "$lock"
  exit "$status"
}
trap cleanup EXIT
trap 'exit 129' HUP
trap 'exit 130' INT
trap 'exit 143' TERM
umask 077
stage=$(mktemp -d "$parent/.$plugin.install.XXXXXX")
cp "$archive" "$stage/archive.zip"
unzip -tq "$stage/archive.zip"
python3 - "$stage/archive.zip" "$plugin" '${requireHooks ? "yes" : "no"}' <<'GRAM_CURSOR_VALIDATE'
import json, stat, sys, zipfile

def check(condition, message):
    if not condition:
        sys.exit("Invalid Cursor ZIP: " + message)

with zipfile.ZipFile(sys.argv[1]) as archive:
    seen = set()
    for entry in archive.infolist():
        name = entry.filename.rstrip("/")
        check(name and not name.startswith("/") and chr(92) not in name
              and all(part not in ("", ".", "..") for part in name.split("/")),
              "unsafe archive path")
        check(name not in seen, "duplicate archive path")
        seen.add(name)
        kind = stat.S_IFMT(entry.external_attr >> 16)
        check(kind in (0, stat.S_IFREG, stat.S_IFDIR), "links or special files are not allowed")
    try:
        manifest = json.loads(archive.read(".cursor-plugin/plugin.json"))
        check(isinstance(manifest, dict) and manifest.get("name") == sys.argv[2]
              and isinstance(manifest.get("version"), str) and manifest["version"].strip(),
              "plugin manifest name/version mismatch")
        if sys.argv[3] == "yes" or "hooks/hooks.json" in seen:
            hooks = json.loads(archive.read("hooks/hooks.json"))
            check(isinstance(hooks, dict) and type(hooks.get("version")) is int
                  and hooks["version"] == 1 and isinstance(hooks.get("hooks"), dict)
                  and bool(hooks["hooks"]), "invalid hooks manifest")
            for commands in hooks["hooks"].values():
                check(isinstance(commands, list) and bool(commands), "invalid hook commands")
                for command in commands:
                    check(isinstance(command, dict) and isinstance(command.get("command"), str)
                          and bool(command["command"].strip()), "invalid hook command")
    except (KeyError, ValueError, UnicodeError):
        sys.exit("Invalid Cursor ZIP: missing or malformed plugin/hooks manifest")
GRAM_CURSOR_VALIDATE
mkdir "$stage/plugin"
unzip -q "$stage/archive.zip" -d "$stage/plugin"
[[ -f "$stage/plugin/.cursor-plugin/plugin.json" ]]
${
  requireHooks
    ? `for required in hooks/hooks.json hooks/bootstrap.sh speakeasy.json; do
  [[ -f "$stage/plugin/$required" ]] || { echo "Invalid Cursor ZIP: missing $required" >&2; exit 1; }
done
`
    : ""
}# Do not follow an existing symlink or overwrite a non-directory.
if [[ -L "$destination" || ( -e "$destination" && ! -d "$destination" ) ]]; then
  echo "Refusing to replace a symlink or non-directory: $destination" >&2
  exit 1
fi
if [[ -d "$destination" ]]; then mv "$destination" "$stage/previous"; fi
mv "$stage/plugin" "$destination"
committed=1
echo "Installed $plugin. In Cursor, run Developer: Reload Window and check Customize."
GRAM_CURSOR_INSTALL`;
}
