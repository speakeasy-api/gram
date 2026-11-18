package repo

// toolUsageFanOutLimit caps how many tool usage reads one request runs at once.
const toolUsageFanOutLimit = 3

// toolUsageReadSettings bounds the memory of one tool usage read. Each read
// groups every trace in the window by trace_id, which over a large project's
// 90 days outgrows the server's total memory limit when a page fires several
// at once. Past the spill threshold the GROUP BY and ORDER BY go to disk, and a
// read that still exceeds the cap fails alone instead of starving every other
// query on the server. It is a SETTINGS clause rather than context settings so
// it does not replace settings the caller put on the context.
const toolUsageReadSettings = ` SETTINGS max_memory_usage = 1610612736,
 max_bytes_before_external_group_by = 536870912,
 max_bytes_before_external_sort = 536870912`
