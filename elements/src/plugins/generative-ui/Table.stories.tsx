import type { Meta, StoryObj } from '@storybook/react-vite'
import { Table } from './Table'
import { GenerativeUIWrapper } from './storybook-utils'

const meta: Meta<typeof Table> = {
  title: 'Generative UI/Table',
  component: Table,
  tags: ['autodocs'],
  parameters: {
    layout: 'centered',
  },
  decorators: [
    (Story) => (
      <GenerativeUIWrapper>
        <div className="w-[600px]">
          <Story />
        </div>
      </GenerativeUIWrapper>
    ),
  ],
}

export default meta
type Story = StoryObj<typeof Table>

export const Default: Story = {
  args: {
    headers: ['Name', 'Email', 'Role'],
    rows: [
      ['John Doe', 'john@example.com', 'Admin'],
      ['Jane Smith', 'jane@example.com', 'User'],
      ['Bob Wilson', 'bob@example.com', 'Editor'],
    ],
  },
}

export const WithNumbers: Story = {
  args: {
    headers: ['Product', 'Quantity', 'Price', 'Total'],
    rows: [
      ['Widget A', 10, '$25.00', '$250.00'],
      ['Widget B', 5, '$50.00', '$250.00'],
      ['Widget C', 20, '$10.00', '$200.00'],
    ],
  },
}

export const SingleColumn: Story = {
  args: {
    headers: ['Items'],
    rows: [['Apple'], ['Banana'], ['Cherry'], ['Date']],
  },
}

export const ManyColumns: Story = {
  args: {
    headers: ['ID', 'Name', 'Status', 'Created', 'Updated', 'Owner'],
    rows: [
      ['1', 'Project A', 'Active', '2024-01-01', '2024-01-15', 'Alice'],
      ['2', 'Project B', 'Pending', '2024-01-05', '2024-01-10', 'Bob'],
      ['3', 'Project C', 'Complete', '2023-12-01', '2024-01-20', 'Carol'],
    ],
  },
}

export const NoHeaders: Story = {
  args: {
    headers: [],
    rows: [
      ['Row 1, Col 1', 'Row 1, Col 2', 'Row 1, Col 3'],
      ['Row 2, Col 1', 'Row 2, Col 2', 'Row 2, Col 3'],
    ],
  },
}

export const EmptyTable: Story = {
  args: {
    headers: ['Name', 'Value'],
    rows: [],
  },
}
