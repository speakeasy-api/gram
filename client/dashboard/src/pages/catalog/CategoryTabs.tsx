import { cn } from "@/lib/utils";
import { Flame } from "lucide-react";
import type { ReactNode } from "react";

export type Category = "all" | "popular";

interface CategoryTabsProps {
  value: Category;
  onChange: (category: Category) => void;
  counts?: Record<Category, number>;
}

interface CategoryConfig {
  value: Category;
  label: string;
  icon: ReactNode;
  description: string;
}

const CATEGORIES: CategoryConfig[] = [
  {
    value: "all",
    label: "All",
    icon: null,
    description: "Show all servers",
  },
  {
    value: "popular",
    label: "Popular",
    icon: <Flame className="w-3.5 h-3.5" />,
    description: "Most used servers",
  },
];

/**
 * Horizontal category tabs for quick server filtering.
 * Each tab represents a predefined filter category.
 */
export function CategoryTabs({ value, onChange, counts }: CategoryTabsProps) {
  return (
    <div className="flex flex-wrap gap-2">
      {CATEGORIES.map((category) => {
        const isActive = value === category.value;
        const count = counts?.[category.value];

        return (
          <button
            key={category.value}
            onClick={() => onChange(category.value)}
            className={cn(
              "inline-flex items-center gap-1.5 px-3 py-1.5 rounded-lg border text-sm font-medium transition-all",
              "hover:border-primary/50 hover:bg-primary/5",
              "focus:outline-none focus-visible:ring-2 focus-visible:ring-ring",
              isActive
                ? "border-primary bg-primary/10 text-primary"
                : "border-border bg-background text-muted-foreground hover:text-foreground",
            )}
            title={category.description}
          >
            {category.icon}
            <span>{category.label}</span>
            {count !== undefined && (
              <span
                className={cn(
                  "ml-1 px-1.5 py-0.5 rounded text-xs",
                  isActive
                    ? "bg-primary/20 text-primary"
                    : "bg-muted text-muted-foreground",
                )}
              >
                {count}
              </span>
            )}
          </button>
        );
      })}
    </div>
  );
}

export { CATEGORIES };
