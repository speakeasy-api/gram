import { Page } from "@/components/page-layout";
import { Input } from "@/components/ui/input";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { SearchIcon } from "lucide-react";
import { useState } from "react";

export default function Logs() {
  const [searchQuery, setSearchQuery] = useState("");
  const [toolTypeFilter, setToolTypeFilter] = useState<string>("");
  const [serverNameFilter, setServerNameFilter] = useState<string>("");
  const [statusFilter, setStatusFilter] = useState<string>("");

  return (
    <Page>
      <Page.Header>
        <Page.Header.Title>Logs</Page.Header.Title>
      </Page.Header>
      <Page.Body>
        <Page.Section>
          <Page.Section.Body>
            <div className="flex flex-col gap-4">
              {/* Search and Filters Row */}
              <div className="flex items-center justify-between gap-4">
                {/* Search Input */}
                <div className="relative flex-1 max-w-xs">
                  <SearchIcon className="absolute left-3 top-1/2 -translate-y-1/2 w-4 h-4 text-muted-foreground" />
                  <Input
                    placeholder="Search"
                    value={searchQuery}
                    onChange={(e) => setSearchQuery(e.target.value)}
                    className="pl-9"
                  />
                </div>

                {/* Filters */}
                <div className="flex items-center gap-2">
                  <Select value={toolTypeFilter} onValueChange={setToolTypeFilter}>
                    <SelectTrigger className="w-[180px]">
                      <SelectValue placeholder="Tool Type" />
                    </SelectTrigger>
                    <SelectContent>
                      <SelectItem value="all">All Types</SelectItem>
                      {/* Add more tool type options here */}
                    </SelectContent>
                  </Select>

                  <Select
                    value={serverNameFilter}
                    onValueChange={setServerNameFilter}
                  >
                    <SelectTrigger className="w-[180px]">
                      <SelectValue placeholder="Server Name" />
                    </SelectTrigger>
                    <SelectContent>
                      <SelectItem value="all">All Servers</SelectItem>
                      {/* Add more server name options here */}
                    </SelectContent>
                  </Select>

                  <Select value={statusFilter} onValueChange={setStatusFilter}>
                    <SelectTrigger className="w-[180px]">
                      <SelectValue placeholder="Status" />
                    </SelectTrigger>
                    <SelectContent>
                      <SelectItem value="all">All Statuses</SelectItem>
                      {/* Add more status options here */}
                    </SelectContent>
                  </Select>
                </div>
              </div>

              {/* Table */}
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>TIMESTAMP</TableHead>
                    <TableHead>SERVER_NAME</TableHead>
                    <TableHead>TOOL_NAME</TableHead>
                    <TableHead>STRING</TableHead>
                    <TableHead>CLIENT</TableHead>
                    <TableHead>DURATION</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {/* Placeholder for table rows - to be implemented later */}
                  <TableRow>
                    <TableCell colSpan={6} className="text-center py-8 text-muted-foreground">
                      No logs to display
                    </TableCell>
                  </TableRow>
                </TableBody>
              </Table>
            </div>
          </Page.Section.Body>
        </Page.Section>
      </Page.Body>
    </Page>
  );
}