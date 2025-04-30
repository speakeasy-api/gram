from dataclasses import dataclass
import functools
import json
from typing import Callable, Dict, Optional, Awaitable, Union

import httpx

from gram_ai import VERSION, GramAPI
from gram_ai.environments import get_server_url_by_key
from gram_ai.models.getinstanceresult import GetInstanceResult
from gram_ai.utils.retries import (
    BackoffStrategy,
    Retries,
    RetryConfig,
    retry_async,
    retry,
)


@dataclass
class GramFunctionCallingCall:
    tool_id: str
    project: str
    toolset: str
    environment: Optional[str] = None


@dataclass
class GramFunctionCallingTool:
    name: str
    description: str
    parameters: Dict
    execute: Callable[[str], str]
    aexecute: Callable[[str], Awaitable[str]]


class GramFunctionCalling:
    api_key: str
    server_url: str
    _cache: dict[tuple[str, str, Union[str, None]], list[GramFunctionCallingTool]] = {}

    def __init__(
        self,
        *,
        api_key: str,
    ):
        self.api_key = api_key
        self.server_url = get_server_url_by_key(api_key)
        self.client = GramAPI(server_url=self.server_url)

    async def _do_http_async(self, req: httpx.Request) -> httpx.Response:
        async with httpx.AsyncClient() as client:
            return await client.send(req)

    def _do_http_sync(self, req: httpx.Request) -> httpx.Response:
        with httpx.Client() as client:
            return client.send(req)

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
    ) -> list[GramFunctionCallingTool]:
        key = (project, toolset, environment)
        if key in self._cache:
            return self._cache[key]

        instance = self._fetch_tools(project, toolset, environment)

        tools: list[GramFunctionCallingTool] = []

        for tool in instance.tools:
            schema = json.loads(tool.schema_) if tool.schema_ else {}

            tool_call = GramFunctionCallingCall(tool.id, project, toolset, environment)

            func = self._create_sync_tool_function(tool_call)
            async_func = self._create_tool_function(tool_call)

            tools.append(
                GramFunctionCallingTool(
                    name=tool.name,
                    description=tool.description,
                    parameters=schema,
                    execute=func,
                    aexecute=async_func,
                )
            )

        self._cache[key] = tools
        return tools

    def _prepare_request(self, tool_call: GramFunctionCallingCall, inp: str):
        url = f"{self.server_url}/rpc/instances.invoke/tool"
        params = {"tool_id": tool_call.tool_id}
        if tool_call.environment:
            params["environment_slug"] = tool_call.environment

        json_input = json.loads(inp)

        return (
            url,
            params,
            {
                "gram-key": self.api_key,
                "gram-project": tool_call.project,
                "user-agent": f"gram-ai/openai-agents python {VERSION}",
                "content-type": "application/json",
            },
            json_input,
        )

    def _create_tool_function(self, tool_call: GramFunctionCallingCall):
        async def wrapper(inp: str):
            url, params, headers, data = self._prepare_request(tool_call, inp)
            req = httpx.Request(
                method="POST",
                url=url,
                params=params,
                headers=headers,
                json=data,
            )
            response = await retry_async(
                functools.partial(self._do_http_async, req), _retry_policy
            )
            response.raise_for_status()
            return response.text

        return wrapper

    def _create_sync_tool_function(self, tool_call: GramFunctionCallingCall):
        def wrapper(inp: str):
            url, params, headers, data = self._prepare_request(tool_call, inp)
            req = httpx.Request(
                method="POST",
                url=url,
                params=params,
                headers=headers,
                json=data,
            )
            response = retry(functools.partial(self._do_http_sync, req), _retry_policy)
            response.raise_for_status()
            return response.text

        return wrapper


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


__all__ = ["GramFunctionCalling"]
