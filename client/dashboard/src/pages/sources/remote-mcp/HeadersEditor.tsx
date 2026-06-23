import { RequireScope } from "@/components/require-scope";
import { Checkbox } from "@/components/ui/checkbox";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Type } from "@/components/ui/type";
import { Button, Stack } from "@speakeasy-api/moonshine";
import { AlertCircle, Plus, Trash2 } from "lucide-react";
import type { Dispatch, SetStateAction } from "react";
import { emptyHeaderRow, headerRowError, type HeaderRow } from "./headers";

// Shared editor for remote MCP proxy headers, used by both the create form and
// the source Settings tab. Each header is either a static value or a pass-through
// of an inbound request header. Secret values are encrypted server-side.

export function HeadersEditor({
  rows,
  setRows,
}: {
  rows: HeaderRow[];
  setRows: Dispatch<SetStateAction<HeaderRow[]>>;
}): JSX.Element {
  const patchRow = (key: string, patch: Partial<HeaderRow>) =>
    setRows((rs) => rs.map((r) => (r.key === key ? { ...r, ...patch } : r)));
  const removeRow = (key: string) =>
    setRows((rs) => rs.filter((r) => r.key !== key));
  const addRow = () => setRows((rs) => [...rs, emptyHeaderRow()]);

  return (
    <Stack gap={4}>
      {rows.length === 0 && (
        <Type muted small>
          No headers configured.
        </Type>
      )}
      {rows.map((row) => {
        const error = headerRowError(row);
        return (
          <div key={row.key} className="rounded-md border p-4">
            <Stack gap={3}>
              <Stack direction="horizontal" gap={2} align="center">
                <div className="flex-1">
                  <Label
                    htmlFor={`${row.key}-name`}
                    className="mb-1 block text-xs"
                  >
                    Header name
                  </Label>
                  <Input
                    id={`${row.key}-name`}
                    value={row.name}
                    onChange={(value) => patchRow(row.key, { name: value })}
                    placeholder="X-Gram-Tunnel-Id"
                  />
                </div>
                <RequireScope scope="mcp:write" level="component">
                  <Button
                    variant="tertiary"
                    size="sm"
                    className="mt-5"
                    onClick={() => removeRow(row.key)}
                  >
                    <Button.LeftIcon>
                      <Trash2 className="size-4" />
                    </Button.LeftIcon>
                    <Button.Text>Remove</Button.Text>
                  </Button>
                </RequireScope>
              </Stack>

              {row.mode === "static" ? (
                <div>
                  <Label
                    htmlFor={`${row.key}-value`}
                    className="mb-1 block text-xs"
                  >
                    Value
                  </Label>
                  <Input
                    id={`${row.key}-value`}
                    value={row.value}
                    onChange={(value) =>
                      patchRow(row.key, { value, valueDirty: true })
                    }
                    placeholder={
                      row.existingSecret && !row.valueDirty
                        ? "•••••• (unchanged — type to replace)"
                        : "demo-tunnel"
                    }
                  />
                </div>
              ) : (
                <div>
                  <Label
                    htmlFor={`${row.key}-passthrough`}
                    className="mb-1 block text-xs"
                  >
                    Pass through inbound request header
                  </Label>
                  <Input
                    id={`${row.key}-passthrough`}
                    value={row.valueFromRequestHeader}
                    onChange={(value) =>
                      patchRow(row.key, { valueFromRequestHeader: value })
                    }
                    placeholder="X-Inbound-Header-Name"
                  />
                </div>
              )}

              <Stack direction="horizontal" gap={6} align="center">
                <label className="flex items-center gap-2 text-sm">
                  <Checkbox
                    checked={row.mode === "passthrough"}
                    onCheckedChange={(checked) =>
                      patchRow(row.key, {
                        mode: checked ? "passthrough" : "static",
                        isSecret: checked ? false : row.isSecret,
                      })
                    }
                  />
                  Pass through
                </label>
                <label className="flex items-center gap-2 text-sm">
                  <Checkbox
                    checked={row.isRequired}
                    onCheckedChange={(checked) =>
                      patchRow(row.key, { isRequired: Boolean(checked) })
                    }
                  />
                  Required
                </label>
                <label className="flex items-center gap-2 text-sm">
                  <Checkbox
                    checked={row.isSecret}
                    disabled={row.mode === "passthrough"}
                    onCheckedChange={(checked) =>
                      patchRow(row.key, {
                        isSecret: Boolean(checked),
                        valueDirty: true,
                      })
                    }
                  />
                  Secret
                </label>
              </Stack>

              {error && (
                <div
                  role="alert"
                  className="text-destructive flex items-center gap-1.5 text-xs"
                >
                  <AlertCircle className="h-3.5 w-3.5 shrink-0" />
                  <span>{error}</span>
                </div>
              )}
            </Stack>
          </div>
        );
      })}

      <div>
        <RequireScope scope="mcp:write" level="component">
          <Button variant="secondary" onClick={addRow}>
            <Button.LeftIcon>
              <Plus className="size-4" />
            </Button.LeftIcon>
            <Button.Text>Add header</Button.Text>
          </Button>
        </RequireScope>
      </div>
    </Stack>
  );
}
