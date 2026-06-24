// Shared chrome for the policy create/edit form (AGE-2704).
//
// The form is a single scrollable page: every section renders at once, with a
// sticky left section-nav rail (SectionNav) that scrolls to a section on click
// and scroll-spy-highlights the section currently in view. FormSection wraps one
// section, owns its anchor id, and renders the section heading.

import { cn } from "@/lib/utils";
import { useEffect, useState, type ReactNode } from "react";

/** One entry in the section-nav rail. `id` matches a `FormSection`'s id. */
export interface FormSectionDef {
  id: string;
  title: string;
  /** Optional inline marker after the title, e.g. "Required" / "Optional". */
  badge?: string;
}

/** Sticky left rail of section links. Clicking scrolls to the section; the link
 *  for the section currently in view is highlighted (scroll-spy). */
export function SectionNav({
  sections,
}: {
  sections: FormSectionDef[];
}): JSX.Element {
  const [activeId, setActiveId] = useState<string | null>(
    sections[0]?.id ?? null,
  );

  // Scroll-spy: highlight whichever section is nearest the top of the viewport.
  useEffect(() => {
    const elements = sections
      .map((s) => document.getElementById(s.id))
      .filter((el): el is HTMLElement => el != null);
    if (elements.length === 0) return;

    const observer = new IntersectionObserver(
      (entries) => {
        const visible = entries
          .filter((e) => e.isIntersecting)
          .sort((a, b) => a.boundingClientRect.top - b.boundingClientRect.top);
        if (visible[0]?.target.id) {
          setActiveId(visible[0].target.id);
        }
      },
      // Bias the active band toward the top of the viewport.
      { rootMargin: "-20% 0px -70% 0px", threshold: 0 },
    );
    for (const el of elements) observer.observe(el);
    return () => observer.disconnect();
  }, [sections]);

  const onJump = (id: string) => {
    document.getElementById(id)?.scrollIntoView({ behavior: "smooth" });
    setActiveId(id);
  };

  return (
    <nav className="flex flex-col gap-1" aria-label="Sections">
      {sections.map((section) => {
        const active = section.id === activeId;
        return (
          <button
            key={section.id}
            type="button"
            onClick={() => onJump(section.id)}
            className={cn(
              "flex items-center gap-2 rounded-md px-3 py-1.5 text-left text-sm transition-colors",
              active
                ? "bg-muted text-foreground font-medium"
                : "text-muted-foreground hover:text-foreground hover:bg-muted/50",
            )}
          >
            <span className="min-w-0 truncate">{section.title}</span>
            {section.badge && (
              <span className="text-muted-foreground text-xs font-normal">
                {section.badge}
              </span>
            )}
          </button>
        );
      })}
    </nav>
  );
}

/** Page layout: sticky section-nav rail + the stacked section column. Keeps the
 *  w-44 rail width / gap of the retired WizardShell. */
export function FormLayout({
  sections,
  children,
}: {
  sections: FormSectionDef[];
  children: ReactNode;
}): JSX.Element {
  return (
    <div className="flex gap-8">
      <div className="w-44 flex-shrink-0">
        <div className="sticky top-8">
          <SectionNav sections={sections} />
        </div>
      </div>
      <div className="min-w-0 flex-1 space-y-12">{children}</div>
    </div>
  );
}

/** One scroll-anchored section. The id is the scroll target used by SectionNav;
 *  scroll-margin keeps the heading clear of the top edge after a jump. */
export function FormSection({
  id,
  title,
  description,
  children,
}: {
  id: string;
  title: string;
  description: string;
  children: ReactNode;
}): JSX.Element {
  return (
    <section id={id} className="scroll-mt-8 space-y-6">
      <WizardStepHeading title={title} description={description} />
      {children}
    </section>
  );
}

export function WizardStepHeading({
  title,
  description,
}: {
  title: string;
  description: string;
}): JSX.Element {
  return (
    <div>
      <h3 className="text-base font-semibold">{title}</h3>
      <p className="text-muted-foreground text-sm">{description}</p>
    </div>
  );
}
