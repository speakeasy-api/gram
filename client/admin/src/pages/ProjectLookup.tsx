import { useState, type JSX } from "react";
import { useNavigate } from "@tanstack/react-router";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";

export function ProjectLookup(): JSX.Element {
  const [idOrSlug, setIdOrSlug] = useState("");
  const navigate = useNavigate();

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault();
    const trimmed = idOrSlug.trim();
    if (!trimmed) return;
    void navigate({ to: "/projects/$idOrSlug", params: { idOrSlug: trimmed } });
  };

  return (
    <div className="space-y-6">
      <section>
        <span className="text-muted-foreground text-sm">
          Look up a Gram project by its UUID or slug.
        </span>

        <form onSubmit={handleSubmit} className="mt-3 flex items-center gap-2">
          <Input
            placeholder="Project UUID or slug"
            value={idOrSlug}
            onChange={(e) => setIdOrSlug(e.target.value)}
            className="w-96 px-2 py-1.5"
          />
          <Button
            type="submit"
            variant="default"
            size="xs"
            disabled={!idOrSlug.trim()}
          >
            Lookup
          </Button>
        </form>
      </section>
    </div>
  );
}
