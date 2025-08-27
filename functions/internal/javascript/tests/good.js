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
    case "create-charge":
      return createCharge(input);
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
  if (input.amount <= 0) {
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
