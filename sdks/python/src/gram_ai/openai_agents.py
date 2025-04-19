import functools
import json
from typing import Any, List, Optional

import httpx
from agents import FunctionTool, Tool, RunContextWrapper

from gram_ai import VERSION, GramAPI
from gram_ai.models.getinstanceresult import GetInstanceResult
from gram_ai.utils.retries import BackoffStrategy, Retries, RetryConfig, retry_async


class GramOpenAIAgents:
    def __init__(
        self,
        *,
        api_key: str,
        project: str,
        toolset: str,
        environment: Optional[str] = None,
    ):
        self.api_key = api_key
        self.project = project
        self.toolset = toolset
        self.environment = environment

        self.client = GramAPI(server_url="http://localhost:8080")
        self.instance = self.refetch()

    def refetch(self) -> GetInstanceResult:
        self.instance = self.client.instances.get_by_slug(
            security={
                "option2": {
                    "apikey_header_gram_key": self.api_key,
                    "project_slug_header_gram_project": self.project,
                }
            },
            toolset_slug=self.toolset,
            environment_slug=self.environment,
        )
        return self.instance

    @property
    def tools(self) -> List[Tool]:
        return [
            FunctionTool(
                name=tool.name,
                description=tool.description,
                params_json_schema=json.loads(tool.schema_) if tool.schema_ else {},
                strict_json_schema=False,
                on_invoke_tool=functools.partial(self.invoke_tool, tool.id),
            )
            for tool in self.instance.tools
        ]

    async def invoke_tool(
        self, tool_id: str, _ctx: RunContextWrapper[Any], data: str
    ) -> str:
        url = "http://localhost:8080/rpc/instances.invoke/tool"
        params = {"tool_id": tool_id}
        if self.environment:
            params["environment_slug"] = self.environment

        req = httpx.Request(
            "POST",
            url=url,
            params=params,
            headers={
                "gram-key": self.api_key,
                "gram-project": self.project,
                "user-agent": f"@gram-ai/for/openai-agents python {VERSION}",
                "content-type": "application/json",
            },
            content=data,
        )

        response = await retry_async(
            functools.partial(self._do_http, req), _retry_policy
        )

        response.raise_for_status()

        return response.text

    async def _do_http(self, req: httpx.Request) -> httpx.Response:
        async with httpx.AsyncClient() as client:
            return await client.send(req)


_retry_policy = Retries(
    config=RetryConfig(
        strategy="backoff",
        backoff=BackoffStrategy(
            initial_interval=500,
            max_interval=60000,
            exponent=1.5,
            max_elapsed_time=3600000,
        ),
        retry_connection_errors=True,
    ),
    status_codes=["429", "5XX"],
)
