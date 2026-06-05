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
