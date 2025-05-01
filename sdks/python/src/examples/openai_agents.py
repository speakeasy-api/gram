import asyncio
import os
from agents import Agent, Runner, set_default_openai_key
from gram_ai.openai_agents import GramOpenAIAgents

key = os.getenv("GRAM_API_KEY")

gram = GramOpenAIAgents(
    api_key=key,
)

set_default_openai_key(os.getenv("GRAM_OPENAI_API_KEY"))

agent = Agent(
    name="Assistant",
    tools=gram.tools(
        project="default",
        toolset="speakeasy-admin",
        environment="default",
    ),
)


async def main():
    result = await Runner.run(
        agent,
        "Can you get me the speakeasy organization ryan-local.",
    )
    print(result.final_output)


if __name__ == "__main__":
    asyncio.run(main()) 