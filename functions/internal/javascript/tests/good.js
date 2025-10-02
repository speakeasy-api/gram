/**
 *
 * @param {string} name
 * @param {*} input
 */
export async function handleToolCall(name, input) {
  switch (name) {
    case "ping":
      return new Response(JSON.stringify({ response: "pong" }));
    case "get-weather":
      return new Response(
        JSON.stringify({ location: input.city, temperature: "20C" }),
        { status: 200, headers: { "Content-Type": "application/json" } }
      );
    case "list-products":
      return new Response(
        JSON.stringify({
          currentCursor: input.cursor,
          items: [
            { id: 1, name: "Product A", price: 10.0 },
            { id: 2, name: "Product B", price: 15.5 },
            { id: 3, name: "Product C", price: 7.25 },
          ],
        }),
        {
          status: 200,
          headers: {
            "Content-Type": "application/json",
            "X-Next-Cursor": "4",
          },
        }
      );
    case "proxy":
      return await fetch(input.url);
    case "create-charge":
      return createCharge(input);
    case "null-tool":
      return null;
    case "fail-tool":
      throw new Error("Intentional failure");
    default:
      return new Response(JSON.stringify({ error: "Unknown tool" }), {
        status: 400,
      });
  }
}

/**
 * @param {*} input
 */
function createCharge(input) {
  if (typeof input.amount !== "number" || input.amount <= 0) {
    return new Response(JSON.stringify({ error: "Invalid amount" }), {
      status: 400,
      headers: { "Content-Type": "application/json" },
    });
  } else {
    return new Response(
      JSON.stringify({ chargeId: "ch_12345", amount: input.amount }),
      { status: 200, headers: { "Content-Type": "application/json" } }
    );
  }
}
