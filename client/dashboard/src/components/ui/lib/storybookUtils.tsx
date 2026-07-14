import { Grid } from "@/components/ui/grid";

/**
 * Create a list of sample children for testing and rendering in storybook
 */
export function createSampleChildren(
  count: number,
  attachTestIds?: boolean,
): React.JSX.Element[] {
  return Array.from({ length: count }).map((_, index) => {
    return (
      <div
        key={index}
        className="flex min-w-[100px] items-center justify-center rounded-md bg-muted p-4 text-center text-body"
        data-testid={attachTestIds ? `sample-child-${index + 1}` : undefined}
      >
        {index + 1}
      </div>
    );
  });
}

export function createSampleGridChildren(count: number): React.JSX.Element[] {
  return createSampleChildren(count).map((child) => (
    <Grid.Item key={child.key} {...child.props}>
      {child}
    </Grid.Item>
  ));
}
