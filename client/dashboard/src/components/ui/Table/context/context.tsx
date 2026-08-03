import { createContext, useContext } from "react";

type TableContextValue = {
  depth: number;
  expandedRowKeys: Set<string | number>;
  toggleExpanded: (rowKey: string | number) => void;
};

export const TableContext = createContext<TableContextValue>({
  depth: 0,
  expandedRowKeys: new Set(),
  toggleExpanded: () => {},
});

export function useTable(): TableContextValue {
  const context = useContext(TableContext);

  return context;
}
