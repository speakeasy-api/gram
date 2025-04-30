from dataclasses import dataclass
import functools
import json
from typing import Any, Optional, Union

import httpx
from agents import FunctionTool, Tool, RunContextWrapper

from gram_ai import VERSION, GramAPI
from gram_ai.environments import get_server_url_by_key
from gram_ai.models.getinstanceresult import GetInstanceResult
from gram_ai.utils.retries import BackoffStrategy, Retries, RetryConfig, retry_async


@dataclass
class GramOpenAIAgentsCall:
    tool_id: str
    project: str
    toolset: str
    environment: Optional[str] = None


class GramOpenAIAgents:
    api_key: str
    server_url: str
    _cache: dict[tuple[str, str, Union[str, None]], list[Tool]] = {}

    def __init__(
        self,
        *,
        api_key: str,
    ):
        self.api_key = api_key
        self.server_url = get_server_url_by_key(api_key)
        self.client = GramAPI(server_url=self.server_url)

    def _fetch_tools(
        self,
        project: str,
        toolset: str,
        environment: Optional[str] = None,
    ) -> GetInstanceResult:
        return self.client.instances.get_by_slug(
            security={
                "option2": {
                    "apikey_header_gram_key": self.api_key,
                    "project_slug_header_gram_project": project,
                }
            },
            toolset_slug=toolset,
            environment_slug=environment,
        )

    def tools(
        self,
        project: str,
        toolset: str,
        environment: Optional[str] = None,
    ) -> list[Tool]:
        key = (project, toolset, environment)
        if key in self._cache:
            return self._cache[key]

        instance = self._fetch_tools(project, toolset, environment)

        result: list[Tool] = [
            FunctionTool(
                name=tool.name,
                description=tool.description,
                params_json_schema=json.loads(tool.schema_) if tool.schema_ else {},
                strict_json_schema=False,
                on_invoke_tool=functools.partial(
                    self._invoke_tool,
                    GramOpenAIAgentsCall(tool.id, project, toolset, environment),
                ),
            )
            for tool in instance.tools
        ]

        self._cache[key] = result

        return result

    async def _invoke_tool(
        self, tool_call: GramOpenAIAgentsCall, _ctx: RunContextWrapper[Any], data: str
    ) -> str:
        url = f"{self.server_url}/rpc/instances.invoke/tool"
        params = {"tool_id": tool_call.tool_id}
        if tool_call.environment:
            params["environment_slug"] = tool_call.environment

        req = httpx.Request(
            "POST",
            url=url,
            params=params,
            headers={
                "gram-key": self.api_key,
                "gram-project": tool_call.project,
                "user-agent": f"gram-ai/openai-agents python {VERSION}",
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


__all__ = ["GramOpenAIAgents"]
