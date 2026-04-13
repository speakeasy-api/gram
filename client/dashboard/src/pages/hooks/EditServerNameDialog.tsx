import { Button } from "@/components/ui/button";
import { Dialog } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import type { ServerNameMappings } from "@/hooks/useServerNameMappings";
import type { ServerNameOverride } from "@gram/client/models/components";
import { Icon } from "@speakeasy-api/moonshine";
import { useCallback, useMemo, useState } from "react";

interface EditServerNameDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  /**
   * The raw server name that was clicked. This may be a display name if multiple
   * servers are grouped together.
   */
  serverName: string;
  /**
   * All overrides that have the same display name as this server.
   * If empty, this is an unmapped server.
   */
  groupedOverrides: ServerNameOverride[];
  /**
   * If provided, this is a raw server name that exists but has no override.
   * It should be included in the list of editable servers.
   */
  unmappedRawName: string | null;
  /**
   * Function to upsert a server name override
   */
  upsert: ServerNameMappings["upsert"];
  /**
   * Function to delete a server name override
   */
  remove: ServerNameMappings["remove"];
  /**
   * Whether an upsert operation is in progress
   */
  isUpserting: boolean;
  /**
   * Whether a delete operation is in progress
   */
  isDeleting: boolean;
}

export function EditServerNameDialog({
  open,
  onOpenChange,
  serverName,
  groupedOverrides,
  unmappedRawName,
  upsert,
  remove,
  isUpserting,
  isDeleting,
}: EditServerNameDialogProps) {
  // For single server, use one display name state
  // For grouped servers, use a map of raw name -> display name
  const [displayName, setDisplayName] = useState(
    groupedOverrides[0]?.displayName || serverName,
  );
  const [individualDisplayNames, setIndividualDisplayNames] = useState<
    Map<string, string>
  >(() => {
    const map = new Map(
      groupedOverrides.map((o) => [o.rawServerName, o.displayName]),
    );
    if (unmappedRawName) {
      map.set(unmappedRawName, unmappedRawName);
    }
    return map;
  });
  const [deletingIds, setDeletingIds] = useState<Set<string>>(new Set());

  const isProcessing = isUpserting || isDeleting;
  // Consider it grouped if we have multiple overrides OR if we have overrides plus an unmapped server
  const isGrouped =
    groupedOverrides.length > 1 ||
    (groupedOverrides.length === 1 && unmappedRawName);

  // Create a unified list of all servers to show (both overrides and unmapped)
  const allServers = useMemo(() => {
    const servers = [...groupedOverrides];
    if (unmappedRawName) {
      // Add the unmapped server as a pseudo-override
      servers.push({
        id: "", // Empty ID indicates this is unmapped
        rawServerName: unmappedRawName,
        displayName: unmappedRawName,
      } as ServerNameOverride);
    }
    return servers;
  }, [groupedOverrides, unmappedRawName]);

  const onUpsert = useCallback(
    async (rawServerName: string, displayName: string) => {
      await upsert.mutateAsync({
        request: {
          upsertRequestBody: {
            rawServerName: rawServerName,
            displayName: displayName,
          },
        },
      });
    },
    [upsert],
  );

  const onDelete = useCallback(
    async (overrideId: string) => {
      await remove.mutateAsync({
        request: { deleteRequestBody: { overrideId: overrideId } },
      });
    },
    [remove],
  );

  const handleSave = async () => {
    try {
      if (isGrouped) {
        // For grouped servers, save each individual display name
        const promises = allServers.map((server) => {
          const newDisplayName =
            individualDisplayNames.get(server.rawServerName) ||
            server.displayName;
          return onUpsert(server.rawServerName, newDisplayName);
        });
        await Promise.all(promises);
      } else if (groupedOverrides.length === 1) {
        // Single server - update its display name
        await onUpsert(groupedOverrides[0].rawServerName, displayName);
      } else {
        // No existing override - create new one
        await onUpsert(serverName, displayName);
      }
      onOpenChange(false);
    } catch (error) {
      console.error("Failed to save server name override:", error);
    }
  };

  const handleDelete = async (overrideId: string) => {
    try {
      setDeletingIds((prev) => new Set(prev).add(overrideId));
      await onDelete(overrideId);
      setDeletingIds((prev) => {
        const next = new Set(prev);
        next.delete(overrideId);
        return next;
      });

      // If we deleted the last override, close the dialog
      if (groupedOverrides.length === 1) {
        onOpenChange(false);
      }
    } catch (error) {
      console.error("Failed to delete server name override:", error);
      setDeletingIds((prev) => {
        const next = new Set(prev);
        next.delete(overrideId);
        return next;
      });
    }
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <Dialog.Content className="sm:max-w-[500px]">
        <Dialog.Header>
          <Dialog.Title>Edit Server Display Name</Dialog.Title>
          <Dialog.Description>
            {isGrouped
              ? "This display name groups multiple servers. You can rename the group or ungroup individual servers."
              : "Change how this server appears in the hooks UI. This is display-only and doesn't affect the actual server name."}
          </Dialog.Description>
        </Dialog.Header>

        <div className="space-y-4 py-4">
          {/* Show grouped servers if multiple */}
          {isGrouped && (
            <div className="space-y-3">
              {allServers.map((server) => (
                <div key={server.rawServerName} className="space-y-2">
                  <div className="flex items-center gap-2">
                    <Label className="text-muted-foreground flex-1 font-mono text-xs">
                      {server.rawServerName}
                    </Label>
                    {server.id && (
                      <Button
                        variant="ghost"
                        size="sm"
                        onClick={() => handleDelete(server.id)}
                        disabled={deletingIds.has(server.id) || isProcessing}
                        className="h-7 shrink-0 px-2"
                      >
                        {deletingIds.has(server.id) ? (
                          <Icon
                            name="loader-circle"
                            className="size-3 animate-spin"
                          />
                        ) : (
                          <Icon name="trash" className="size-3" />
                        )}
                        <span className="sr-only">Remove override</span>
                      </Button>
                    )}
                  </div>
                  <Input
                    value={
                      individualDisplayNames.get(server.rawServerName) ||
                      server.displayName
                    }
                    onChange={(value) => {
                      setIndividualDisplayNames((prev) => {
                        const next = new Map(prev);
                        next.set(server.rawServerName, value);
                        return next;
                      });
                    }}
                    placeholder="Display name"
                    disabled={isProcessing}
                    className="text-sm"
                  />
                </div>
              ))}
              <p className="text-muted-foreground text-xs">
                Change a server's display name to move it to a different group,
                or restore its original name to ungroup it. Click the trash icon
                to remove the override entirely.
              </p>
            </div>
          )}

          {/* Show single server editor if not grouped */}
          {!isGrouped && (
            <>
              {groupedOverrides.length > 0 && (
                <div className="space-y-2">
                  <Label htmlFor="raw-name" className="text-sm font-medium">
                    Original Server Name
                  </Label>
                  <Input
                    id="raw-name"
                    value={groupedOverrides[0].rawServerName}
                    readOnly
                    className="bg-muted font-mono text-sm"
                  />
                </div>
              )}

              {/* Display name input */}
              <div className="space-y-3">
                <Label htmlFor="display-name" className="text-sm font-medium">
                  Display Name
                </Label>
                <Input
                  id="display-name"
                  value={displayName}
                  onChange={setDisplayName}
                  placeholder="Enter a friendly name"
                  disabled={isProcessing}
                  autoFocus
                  onKeyDown={(e) => {
                    if (e.key === "Enter" && !isProcessing) {
                      handleSave();
                    }
                  }}
                />
                <p className="text-muted-foreground mt-3 text-xs">
                  This name will be shown in the UI instead of the raw server
                  name
                </p>
              </div>
            </>
          )}
        </div>

        <Dialog.Footer>
          <Button
            variant="outline"
            onClick={() => onOpenChange(false)}
            disabled={isProcessing}
          >
            Cancel
          </Button>
          {groupedOverrides.length > 0 && !isGrouped && (
            <Button
              variant="destructive"
              onClick={() => handleDelete(groupedOverrides[0].id)}
              disabled={isProcessing}
            >
              {isDeleting ? (
                <>
                  <Icon
                    name="loader-circle"
                    className="mr-2 size-4 animate-spin"
                  />
                  Removing...
                </>
              ) : (
                "Clear Mapping"
              )}
            </Button>
          )}
          <Button
            onClick={handleSave}
            disabled={
              isProcessing ||
              (isGrouped
                ? Array.from(individualDisplayNames.values()).some(
                    (name) => !name?.trim(),
                  )
                : !displayName.trim())
            }
          >
            {isUpserting ? (
              <>
                <Icon
                  name="loader-circle"
                  className="mr-2 size-4 animate-spin"
                />
                Saving...
              </>
            ) : (
              "Save"
            )}
          </Button>
        </Dialog.Footer>
      </Dialog.Content>
    </Dialog>
  );
}
