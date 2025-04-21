<!-- Start SDK Example Usage [usage] -->
```python
# Synchronous Example
import gram_ai
from gram_ai import GramAPI


with GramAPI() as gram_api:

    res = gram_api.instances.get_by_slug(security=gram_ai.GetInstanceSecurity(
        option1=gram_ai.GetInstanceSecurityOption1(
            project_slug_header_gram_project="<YOUR_API_KEY_HERE>",
            session_header_gram_session="<YOUR_API_KEY_HERE>",
        ),
    ), toolset_slug="<value>")

    # Handle response
    print(res)
```

</br>

The same SDK client can also be used to make asychronous requests by importing asyncio.
```python
# Asynchronous Example
import asyncio
import gram_ai
from gram_ai import GramAPI

async def main():

    async with GramAPI() as gram_api:

        res = await gram_api.instances.get_by_slug_async(security=gram_ai.GetInstanceSecurity(
            option1=gram_ai.GetInstanceSecurityOption1(
                project_slug_header_gram_project="<YOUR_API_KEY_HERE>",
                session_header_gram_session="<YOUR_API_KEY_HERE>",
            ),
        ), toolset_slug="<value>")

        # Handle response
        print(res)

asyncio.run(main())
```
<!-- End SDK Example Usage [usage] -->