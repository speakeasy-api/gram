import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Type } from "@/components/ui/type";
import { useAnnotations, type Annotation } from "@/hooks/useAnnotations";
import { MessageSquareIcon, PlusIcon } from "lucide-react";
import { useState } from "react";

/**
 * AnnotationsPanel — displays annotations list with create form for a corpus
 * file. Fetches data via useAnnotations hook.
 */
export function AnnotationsPanel({ filePath }: { filePath: string }) {
  const {
    data: annotations,
    create,
    isCreating,
    isReadOnly,
  } = useAnnotations(filePath);
  const [showForm, setShowForm] = useState(false);
  const [content, setContent] = useState("");

  if (!annotations) {
    return null;
  }

  const handleSubmit = () => {
    if (isReadOnly) return;
    if (!content.trim()) return;
    create(content.trim());
    setContent("");
    setShowForm(false);
  };

  return (
    <div className="border-t border-border">
      <div className="flex items-center gap-2 px-4 py-3">
        <MessageSquareIcon className="h-4 w-4 text-muted-foreground" />
        <Type small muted className="font-medium">
          Annotations ({annotations.length})
        </Type>
      </div>
      <div className="px-4 pb-3 space-y-3">
        {annotations.map((annotation) => (
          <AnnotationItem key={annotation.id} annotation={annotation} />
        ))}

        {showForm ? (
          <div className="space-y-2">
            <textarea
              value={content}
              onChange={(e) => setContent(e.target.value)}
              placeholder="Add a note..."
              className="w-full rounded-md border border-border bg-transparent px-3 py-2 text-sm focus:outline-none focus:border-ring"
              rows={3}
            />
            <div className="flex gap-2">
              <Button
                size="sm"
                onClick={handleSubmit}
                disabled={isCreating || isReadOnly || !content.trim()}
              >
                Submit
              </Button>
              <Button
                size="sm"
                variant="outline"
                onClick={() => {
                  setShowForm(false);
                  setContent("");
                }}
              >
                Cancel
              </Button>
            </div>
          </div>
        ) : (
          <Button
            size="sm"
            variant="outline"
            className="w-full"
            disabled={isReadOnly}
            onClick={() => setShowForm(true)}
          >
            <PlusIcon className="h-3.5 w-3.5 mr-1.5" />
            Add Annotation
          </Button>
        )}
      </div>
    </div>
  );
}

function AnnotationItem({ annotation }: { annotation: Annotation }) {
  return (
    <div className="rounded-md border border-border bg-muted/30 p-3 space-y-1.5">
      <div className="flex items-center gap-2">
        <Type small className="font-medium">
          {annotation.author}
        </Type>
        <Badge
          variant={annotation.authorType === "agent" ? "default" : "secondary"}
        >
          {annotation.authorType === "agent" ? "Agent" : "Human"}
        </Badge>
      </div>
      <Type small muted>
        {annotation.content}
      </Type>
    </div>
  );
}
