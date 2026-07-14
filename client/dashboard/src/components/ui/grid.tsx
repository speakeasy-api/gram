import {
  colSpanMapper,
  gapMapper,
  paddingMapper,
} from "@/components/ui/lib/responsiveMappers";
import { cn } from "@/lib/utils";
import { getResponsiveClasses } from "@/components/ui/lib/utils";
import {
  Columns,
  Gap,
  Padding,
  ResponsiveValue,
} from "@/components/ui/lib/types";
import { isValidElement, ReactElement } from "react";

interface GridProps {
  /**
   * The number of columns in the grid.
   * @default 1
   * @example Default
   * <Grid columns={2}>
   *   <Grid.Item>Item 1</Grid.Item>
   *   <Grid.Item>Item 2</Grid.Item>
   * </Grid>
   * @example With responsive columns
   * <Grid columns={{ xs: 1, md: 2 }}>
   *   <Grid.Item>Item 1</Grid.Item>
   *   <Grid.Item>Item 2</Grid.Item>
   * </Grid>
   */
  columns?: ResponsiveValue<Columns>;

  /**
   * The gap between the grid items.
   * @default 0
   * @example Default
   * <Grid gap={4}>
   *   <Grid.Item>Item 1</Grid.Item>
   *   <Grid.Item>Item 2</Grid.Item>
   * </Grid>
   * @example With responsive gap
   * <Grid gap={{ xs: 2, md: 4 }}>
   *   <Grid.Item>Item 1</Grid.Item>
   *   <Grid.Item>Item 2</Grid.Item>
   * </Grid>
   */
  gap?: ResponsiveValue<Gap>;

  /**
   * The Grid.Item children of the grid.
   * @example
   * <Grid>
   *   <Grid.Item>Item 1</Grid.Item>
   *   <Grid.Item>Item 2</Grid.Item>
   * </Grid>
   */
  children: Array<ReactElement<typeof GridItem>>;

  /**
   * If true, the grid will wrap its children.
   * @default false
   */
  wrap?: boolean;

  /**
   * Can be an object of responsive Padding values, or just Padding values.
   *
   * @example Simple Padding
   * padding: 10
   *
   * @example Responsive Padding
   * padding: { xs: 10, md: 20, lg: 30, xl: 40 }
   *
   * @example Padding per side
   * padding: { top: 10, right: 0, bottom: 10, left: 0 }
   *
   * @example Responsive Padding per side (just x and y axis)
   * padding: { xs: 0, md: 0, lg: 0, xl: { x: 10, y: 12 } }
   *
   * @example Responsive Padding per side with different values for each side
   * padding: { xs: 0, md: 0, lg: 0, xl: { top: 10, right: 0, bottom: 10, left: 0 } }
   */
  padding?: ResponsiveValue<Padding>;

  className?: string;
}

const columnsMapper = (columns: Columns) => `grid-cols-${columns}`;

const isValidGridChild = (child: ReactElement) =>
  isValidElement(child) && child.type === GridItem;

const Grid = ({
  children,
  columns = 1,
  gap = 0,
  wrap = true,
  padding = 0,
  className,
}: GridProps) => {
  const validGridChildren = children.filter(isValidGridChild);
  return (
    <div
      className={cn(
        "grid",
        getResponsiveClasses(columns, columnsMapper),
        getResponsiveClasses(gap, gapMapper),
        !wrap && "grid-flow-col",
        getResponsiveClasses(padding, paddingMapper),
        className,
      )}
    >
      {validGridChildren}
    </div>
  );
};

type PermittedHTMLAttributes = Omit<
  React.HTMLAttributes<HTMLDivElement>,
  "className"
>;

interface GridItemProps extends PermittedHTMLAttributes {
  children: React.ReactNode;
  colSpan?: ResponsiveValue<number>;
  padding?: ResponsiveValue<Padding>;
  className?: string;
}

const GridItem = ({
  children,
  colSpan,
  padding,
  className,
  ...props
}: GridItemProps) => {
  return (
    <div
      className={cn(
        "grid-item",
        colSpan && getResponsiveClasses(colSpan, colSpanMapper),
        padding && getResponsiveClasses(padding, paddingMapper),
        className,
      )}
      {...props}
    >
      {children}
    </div>
  );
};

const GridWithSubcomponents = Object.assign(Grid, {
  Item: GridItem,
});

export { GridWithSubcomponents as Grid };
