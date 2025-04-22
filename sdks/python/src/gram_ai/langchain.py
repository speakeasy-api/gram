from dataclasses import dataclass
import functools
import json
from typing import Any, Optional, Union

import httpx

from langchain_core.tools import (
    StructuredTool,
    Tool,
)

from gram_ai import VERSION, GramAPI
from gram_ai.models.getinstanceresult import GetInstanceResult
from gram_ai.utils.retries import BackoffStrategy, Retries, RetryConfig, retry_async, retry


@dataclass
class GramLangchainCall:
    tool_id: str
    project: str
    toolset: str
    environment: Optional[str] = None


class GramLangchain:
    api_key: str
    _cache: dict[tuple[str, str, Union[str, None]], list[Tool]] = {}

    def __init__(
        self,
        *,
        api_key: str,
    ):
        self.api_key = api_key
        self.client = GramAPI(server_url="http://localhost:8080")

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
            StructuredTool(
                name=tool.name,
                description=tool.description,
                args_schema=json.loads(tool.schema_) if tool.schema_ else {},
                coroutine=self._create_tool_function(
                    GramLangchainCall(tool.id, project, toolset, environment)
                ),
                func=self._create_sync_tool_function(
                    GramLangchainCall(tool.id, project, toolset, environment)
                ),
            )
            for tool in instance.tools
        ]

        self._cache[key] = result

        return result

    def _prepare_request(self, tool_call: GramLangchainCall, **kwargs):
        url = "http://localhost:8080/rpc/instances.invoke/tool"
        params = {"tool_id": tool_call.tool_id}
        if tool_call.environment:
            params["environment_slug"] = tool_call.environment

        return url, params, {
            "gram-key": self.api_key,
            "gram-project": tool_call.project,
            "user-agent": f"gram-ai/openai-agents python {VERSION}",
            "content-type": "application/json",
        }, kwargs

    def _create_tool_function(self, tool_call: GramLangchainCall):
        async def wrapper(**kwargs):
            url, params, headers, data = self._prepare_request(tool_call, **kwargs)
            async with httpx.AsyncClient() as client:
                response = await client.post(
                    url=url,
                    params=params,
                    headers=headers,
                    json=data,
                )
                response = await retry_async(
                    functools.partial(client.send, response.request),
                    _retry_policy
                )
                response.raise_for_status()
                return response.text
        return wrapper

    def _create_sync_tool_function(self, tool_call: GramLangchainCall):
        def wrapper(**kwargs):
            url, params, headers, data = self._prepare_request(tool_call, **kwargs)
            with httpx.Client() as client:
                response = client.post(
                    url=url,
                    params=params,
                    headers=headers,
                    json=data,
                )
                response = retry(
                    functools.partial(client.send, response.request),
                    _retry_policy
                )
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


__all__ = ["GramLangchain"]
