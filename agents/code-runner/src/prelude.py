# ruff: noqa: F821
# _gram_host is an external function resolved by Monty's suspension protocol.


class _GramTools:
    async def search(self, query="", server=None, limit=10, cursor=None):
        return await _gram_host(
            "search",
            {
                "query": query,
                "server": server,
                "limit": limit,
                "cursor": cursor,
            },
        )

    async def describe(self, path):
        return await _gram_host("describe", {"path": path})

    async def call(self, path, arguments):
        return await _gram_host("call", {"path": path, "arguments": arguments})

    async def servers(self, limit=10, cursor=None):
        return await _gram_host("servers", {"limit": limit, "cursor": cursor})


tools = _GramTools()
