import asyncio
import os
from langchain import hub
from langchain_openai import ChatOpenAI
from langchain.agents import AgentExecutor, create_openai_functions_agent
from gram_ai.langchain import GramLangchain

key = os.getenv("GRAM_API_KEY")

gram = GramLangchain(api_key=key)

llm = ChatOpenAI(
    model="gpt-4",
    temperature=0,
    openai_api_key=os.getenv("GRAM_OPENAI_API_KEY")
)

tools = gram.tools(
    project="default",
    toolset="test",
    environment="default",
)

prompt = hub.pull("hwchase17/openai-functions-agent")

agent = create_openai_functions_agent(llm=llm, tools=tools, prompt=prompt)


agent_executor = AgentExecutor(agent=agent, tools=tools, verbose=False)

async def main():
    response = await agent_executor.ainvoke({
        "input": "Can you get me the speakeasy organization ryan-local"
    })
    print(response)

if __name__ == "__main__":
    asyncio.run(main())
