import { Block, BlockInner } from "@/components/block";
import { RequireScope } from "@/components/require-scope";
import { Checkbox } from "@/components/ui/checkbox";
import { Type } from "@/components/ui/type";
import {
  usePublishing,
  type PublishingTarget,
} from "@/pages/mcp/usePublishing";
import { Button, Stack } from "@speakeasy-api/moonshine";
import { PageSection } from "./MCPDetails";

export function MCPPublishingSection({
  target,
  canPublish,
  disabledMessage,
}: {
  target: PublishingTarget;
  canPublish: boolean;
  disabledMessage: string;
}): React.JSX.Element {
  const {
    collections,
    effectiveSelected,
    hasChanges,
    isSaving,
    isLoading,
    toggleCollection,
    handleSave,
    handleDiscard,
  } = usePublishing(target);

  return (
    // Publishing attaches the server to an org-level collection, which the
    // collections service authorizes as org:admin (see AttachServer /
    // DetachServer). Gate the controls to match so non-admins don't see
    // actions that would 403.
    <RequireScope scope="org:admin" level="component">
      <PageSection
        heading="Publishing"
        description="Publish this server to collections so it can be discovered and installed by others in your organization."
      >
        <Block label="Collections" className="p-0">
          <BlockInner>
            {!canPublish ? (
              <Type muted small>
                {disabledMessage}
              </Type>
            ) : isLoading ? (
              <Type muted small>
                Loading collections...
              </Type>
            ) : collections.length === 0 ? (
              <Type muted small>
                No collections available.
              </Type>
            ) : (
              <Stack direction="vertical" gap={2}>
                {collections.map((collection) => (
                  <label
                    key={collection.id}
                    className="flex cursor-pointer items-center gap-3"
                  >
                    <Checkbox
                      checked={effectiveSelected.has(collection.id)}
                      disabled={isSaving}
                      onCheckedChange={() => toggleCollection(collection.id)}
                    />
                    <Stack direction="vertical" gap={0}>
                      <Type small className="font-medium">
                        {collection.name}
                      </Type>
                      {collection.description && (
                        <Type muted small>
                          {collection.description}
                        </Type>
                      )}
                    </Stack>
                  </label>
                ))}
              </Stack>
            )}
          </BlockInner>
          {hasChanges && (
            <BlockInner>
              <Stack direction="horizontal" gap={2}>
                <Button size="sm" disabled={isSaving} onClick={handleSave}>
                  <Button.Text>{isSaving ? "Saving..." : "Save"}</Button.Text>
                </Button>
                <Button
                  size="sm"
                  variant="secondary"
                  disabled={isSaving}
                  onClick={handleDiscard}
                >
                  <Button.Text>Discard</Button.Text>
                </Button>
              </Stack>
            </BlockInner>
          )}
        </Block>
      </PageSection>
    </RequireScope>
  );
}
