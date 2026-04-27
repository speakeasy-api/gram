import { createServer } from "node:http";

const port = Number(process.env.PORT || 8788);
const host = process.env.HOST || "127.0.0.1";
const expectedApiKey = process.env.EXPECTED_API_KEY || "";
const publicBaseUrl =
  process.env.PUBLIC_BASE_URL || "https://gram-tiago.ngrok.app";
const openApiServerUrl =
  process.env.OPENAPI_SERVER_URL || `${publicBaseUrl.replace(/\/$/, "")}/v1`;

const monthlyLimit = 1000;
const monthlyRemaining = 999;
const intervalLimit = 10;
const intervalRemaining = 9;

const sampleOrg = {
  id: "31aebaba-f790-42bb-af8c-e338ddcc4e14",
  type: "organizations",
  attributes: {
    name: "Cummings Inc-95",
    slug: "cummings-inc-95",
  },
};

const sampleOwner = {
  id: "de0f55a0-ffe3-4c87-ac7d-3d29302e591b",
  type: "people",
  attributes: {
    role: "team_member",
    created_at: 1738594630,
    updated_at: 1738594631,
    timezone: "Europe/Paris",
    first_name: "Keven-11",
    last_name: "Prohaska-11",
    email: "11-cyrus@stanton.info",
    avatar_link: null,
  },
};

const sampleEvent = {
  id: "34304943-e1d2-42f6-bca4-95db94434fd3",
  type: "events",
  attributes: {
    title: "cousin-nicky6",
    slug: "perspiciatis-enim",
    registration_link:
      "https://app.livestorm.co/p/34304943-e1d2-42f6-bca4-95db94434fd3",
    estimated_duration: 30,
    registration_page_enabled: true,
    everyone_can_speak: false,
    description: "Unde earum ut. Soluta voluptatibus ullam.",
    status: "draft",
    light_registration_page_enabled: true,
    recording_enabled: true,
    recording_public: true,
    show_in_company_page: false,
    chat_enabled: true,
    polls_enabled: true,
    questions_enabled: true,
    language: "en",
    published_at: 0,
    scheduling_status: "draft",
    created_at: 1738594631,
    updated_at: 1738594631,
    owner: sampleOwner,
    sessions_count: 1,
    fields: [],
  },
};

const sampleSession = {
  id: "18f9b8b2-020f-4277-83b4-2383149df70e",
  type: "sessions",
  attributes: {
    event_id: sampleEvent.id,
    status: "draft",
    timezone: "Europe/Paris",
    room_link:
      "https://app.livestorm.co/p/34304943-e1d2-42f6-bca4-95db94434fd3/live?s=18f9b8b2-020f-4277-83b4-2383149df70e",
    name: "Beta Quadrant",
    attendees_count: 0,
    duration: null,
    estimated_started_at: 1768953600,
    started_at: 0,
    ended_at: 0,
    canceled_at: 0,
    created_at: 1738594771,
    updated_at: 1738594771,
    registrants_count: 1,
    breakout_room_parent_session_id: null,
  },
};

const samplePerson = {
  id: "3020e801-93e2-4db3-ad97-8dfd486ebbad",
  type: "people",
  attributes: {
    role: "participant",
    created_at: 1738594704,
    updated_at: 1738594704,
    timezone: null,
    first_name: "Jean-Baptiste",
    last_name: "Smith",
    email: "jean-baptiste@mysite.co",
    avatar_link: null,
    registrant_detail: {
      event_id: sampleEvent.id,
      session_id: sampleSession.id,
      created_at: 1738594704,
      updated_at: 1738594704,
      fields: [
        {
          id: "email",
          type: "text",
          value: "jean-baptiste@mysite.co",
          required: null,
        },
        {
          id: "first_name",
          type: "text",
          value: "Jean-Baptiste",
          required: null,
        },
        { id: "last_name", type: "text", value: "Smith", required: null },
      ],
      referrer: "referrer",
      utm_source: "utm_source",
      utm_medium: "utm_medium",
      utm_term: "utm_term",
      utm_content: "utm_content",
      utm_campaign: "utm_campaign",
      browser_version: null,
      browser_name: null,
      os_name: null,
      os_version: null,
      screen_height: null,
      screen_width: null,
      ip_city: null,
      ip_country_code: null,
      ip_country_name: null,
      password_key: "c06b70735df5d60c88ef0f",
      connection_link:
        "https://app.livestorm.co/p/34304943-e1d2-42f6-bca4-95db94434fd3/live?email=jean-baptiste%40mysite.co&key=c06b70735df5d60c88ef0f&s=18f9b8b2-020f-4277-83b4-2383149df70e",
      attended: false,
      attendance_rate: null,
      attendance_duration: 0,
      has_viewed_replay: false,
      registration_type: "API",
      is_highlighted: false,
      is_guest_speaker: false,
      session_role: null,
    },
    messages_count: 0,
    questions_count: 0,
    votes_count: 0,
    up_votes_count: 0,
    replay_view_detail: null,
  },
};

function getBaseUrl(req) {
  const forwardedProto = req.headers["x-forwarded-proto"];
  const proto =
    typeof forwardedProto === "string" && forwardedProto.length > 0
      ? forwardedProto
      : "http";
  const hostHeader = req.headers.host || `${host}:${port}`;
  return `${proto}://${hostHeader}`;
}

function makeServerUrl(baseUrl) {
  if (!openApiServerUrl) return "";
  return openApiServerUrl;
}

function json(res, statusCode, body, headers = {}) {
  const payload = JSON.stringify(body, null, 2);
  res.writeHead(statusCode, {
    "content-type": "application/json; charset=utf-8",
    "content-length": Buffer.byteLength(payload),
    ...headers,
  });
  res.end(payload);
}

function text(
  res,
  statusCode,
  body,
  contentType = "text/plain; charset=utf-8",
) {
  res.writeHead(statusCode, {
    "content-type": contentType,
    "content-length": Buffer.byteLength(body),
  });
  res.end(body);
}

function noContent(res, statusCode, headers = {}) {
  res.writeHead(statusCode, headers);
  res.end();
}

function getAuthToken(req) {
  const value = req.headers.authorization;
  return typeof value === "string" ? value : "";
}

function mask(value) {
  if (!value) return "";
  if (value.length <= 8) return "*".repeat(value.length);
  return `${value.slice(0, 4)}...${value.slice(-4)}`;
}

function meta(recordCount) {
  return {
    current_page: 0,
    previous_page: null,
    next_page: null,
    record_count: recordCount,
    page_count: recordCount > 0 ? 1 : 0,
    items_per_page: recordCount,
  };
}

function rateLimitHeaders() {
  return {
    "RateLimit-Monthly-Remaining": String(monthlyRemaining),
    "RateLimit-Monthly-Limit": String(monthlyLimit),
    "RateLimit-Interval-Remaining": String(intervalRemaining),
    "RateLimit-Interval-Limit": String(intervalLimit),
  };
}

function errorResponse(res, statusCode, title, detail, req) {
  const token = getAuthToken(req);
  return json(
    res,
    statusCode,
    {
      errors: [
        {
          title,
          detail,
          code: title.toLowerCase().replaceAll(" ", "_"),
          status: String(statusCode),
        },
      ],
      debug: {
        headerName: "Authorization",
        receivedValue: token,
        maskedValue: mask(token),
        expectedConfigured: expectedApiKey.length > 0,
        matchesExpected:
          expectedApiKey.length > 0 ? token === expectedApiKey : false,
      },
    },
    rateLimitHeaders(),
  );
}

function requireAuth(req, res) {
  const token = getAuthToken(req);
  if (!token) {
    errorResponse(
      res,
      401,
      "Authentication failed",
      "Missing Authorization header.",
      req,
    );
    return false;
  }

  if (expectedApiKey && token !== expectedApiKey) {
    errorResponse(
      res,
      401,
      "Authentication failed",
      "Authorization header did not match EXPECTED_API_KEY.",
      req,
    );
    return false;
  }

  return true;
}

function buildOpenApiDocument(baseUrl) {
  const document = {
    openapi: "3.0.3",
    info: {
      title: "Livestorm - Public API",
      description:
        "Welcome to Livestorm's Public API documentation.\n\nThe Livestorm API is organized around REST principles.\n\nIn addition, all request and response bodies, including errors, are encoded in JSON format.\n",
      termsOfService: "https://livestorm.co/terms/",
      contact: {
        name: "Contact",
        url: "https://support.livestorm.co",
        email: "help@livestorm.co",
      },
      version: "1",
    },
    components: {
      securitySchemes: {
        api_key: {
          type: "apiKey",
          name: "Authorization",
          in: "header",
        },
        oauth2: {
          type: "oauth2",
          flows: {
            authorizationCode: {
              authorizationUrl: `${baseUrl}/oauth/authorize`,
              tokenUrl: `${baseUrl}/oauth/token`,
              scopes: {},
            },
          },
        },
      },
      schemas: {
        BaseResponse: {
          type: "object",
          properties: {
            id: { type: "string", description: "ID" },
            type: { type: "string", description: "Type" },
          },
        },
        Meta: {
          type: "object",
          properties: {
            record_count: {
              type: "integer",
              description: "Total Record Count",
            },
            page_count: { type: "integer", description: "Page Count" },
            items_per_page: { type: "integer", description: "Items per page" },
          },
        },
        Errors: {
          type: "object",
          properties: {
            errors: { $ref: "#/components/schemas/ErrorsDetail" },
          },
        },
        ErrorsDetail: {
          type: "array",
          items: {
            type: "object",
            properties: {
              title: { type: "string" },
              detail: { type: "string" },
              code: { type: "string" },
              status: { type: "string" },
            },
          },
        },
      },
    },
    tags: [
      { name: "Ping", description: "Test Authorization" },
      {
        name: "Identity",
        description: "Retrieve the currently authenticated user",
      },
      { name: "Events" },
      { name: "Sessions" },
      { name: "People", description: "Participant and Team Member" },
    ],
    paths: {
      "/ping": {
        get: {
          summary: "Test authentication",
          tags: ["Ping"],
          description:
            "Test whether your API token or OAuth2 access token works.",
          security: [{ api_key: [] }, { oauth2: [] }],
          responses: {
            200: {
              description: "Authentication success",
              content: {},
            },
            401: {
              description: "Authentication failed",
              content: {
                "application/json": {
                  schema: { $ref: "#/components/schemas/Errors" },
                },
              },
            },
          },
        },
      },
      "/me": {
        get: {
          summary: "Get current user",
          tags: ["Identity"],
          description:
            "Get the currently connected user (only supported with OAuth2) and organization.",
          security: [{ api_key: [] }, { oauth2: [] }],
          responses: {
            200: { description: "Get detail with API key authentication" },
            401: { description: "Authentication failed" },
          },
        },
      },
      "/organization": {
        get: {
          summary: "Get current organization",
          tags: ["Identity"],
          description: "Get the currently connected workspace organization.",
          security: [{ api_key: [] }, { oauth2: [] }],
          responses: {
            200: { description: "Get detail with API key authentication" },
            401: { description: "Authentication failed" },
          },
        },
      },
      "/events": {
        get: {
          summary: "List events",
          tags: ["Events"],
          description: "List the events of your workspace.",
          security: [{ api_key: [] }, { oauth2: [] }],
          parameters: [
            {
              name: "page[number]",
              in: "query",
              schema: { type: "string" },
              description: "Page index to be returned",
            },
            {
              name: "page[size]",
              in: "query",
              schema: { type: "string" },
              description: "Number of record to be returned by page",
            },
            {
              name: "filter[title]",
              in: "query",
              schema: { type: "string" },
              description: "Filter Events by title",
            },
            {
              name: "include",
              in: "query",
              schema: {
                type: "array",
                items: { type: "string", enum: ["sessions"] },
              },
              description: "Include Related Data",
            },
          ],
          responses: {
            200: { description: "Fetch List" },
            401: { description: "Authentication failed" },
          },
        },
      },
      "/events/{id}": {
        get: {
          summary: "Get an event",
          tags: ["Events"],
          description: "Retrieve a single event.",
          security: [{ api_key: [] }, { oauth2: [] }],
          parameters: [
            {
              name: "id",
              in: "path",
              schema: { type: "string" },
              description: "Event ID",
              required: true,
            },
          ],
          responses: {
            200: { description: "Get detail" },
            401: { description: "Authentication failed" },
            404: { description: "Not found" },
          },
        },
      },
      "/sessions": {
        get: {
          summary: "List Sessions",
          tags: ["Sessions"],
          description: "List all your event sessions.",
          security: [{ api_key: [] }, { oauth2: [] }],
          parameters: [
            {
              name: "page[number]",
              in: "query",
              schema: { type: "string" },
              description: "Page index to be returned",
            },
            {
              name: "page[size]",
              in: "query",
              schema: { type: "string" },
              description: "Number of record to be returned by page",
            },
            {
              name: "filter[status]",
              in: "query",
              schema: { type: "string" },
              description:
                "Filter Sessions by status : 'upcoming', 'live', 'on_demand', 'past', 'past_not_started', 'canceled', 'draft'",
            },
          ],
          responses: {
            200: { description: "Get detail" },
            401: { description: "Authentication failed" },
          },
        },
      },
      "/sessions/{id}": {
        get: {
          summary: "Get a Session",
          tags: ["Sessions"],
          description: "Retrieve a single session.",
          security: [{ api_key: [] }, { oauth2: [] }],
          parameters: [
            {
              name: "id",
              in: "path",
              schema: { type: "string" },
              description: "Session ID",
              required: true,
            },
          ],
          responses: {
            200: { description: "Get detail" },
            401: { description: "Authentication failed" },
            404: { description: "Not found" },
          },
        },
      },
      "/sessions/{id}/people": {
        get: {
          summary: "List the people from a session",
          tags: ["Sessions"],
          description: "List all the participants of a session.",
          security: [{ api_key: [] }, { oauth2: [] }],
          parameters: [
            {
              name: "id",
              in: "path",
              schema: { type: "string" },
              description: "Session ID",
              required: true,
            },
            {
              name: "filter[email]",
              in: "query",
              schema: { type: "string" },
              description: "Filter People by their email (exact match)",
            },
          ],
          responses: {
            200: { description: "Fetch List" },
            401: { description: "Authentication failed" },
            404: { description: "Not found" },
          },
        },
      },
    },
  };

  const serverUrl = makeServerUrl(baseUrl);
  if (serverUrl) {
    document.servers = [{ url: serverUrl }];
  }

  return document;
}

function toYaml(document) {
  return `${JSON.stringify(document, null, 2)}\n`;
}

function sendPing(res) {
  noContent(res, 200, rateLimitHeaders());
}

function sendMe(res) {
  json(res, 200, { data: sampleOrg }, rateLimitHeaders());
}

function sendOrganization(res) {
  json(res, 200, { data: sampleOrg }, rateLimitHeaders());
}

function sendEvents(res) {
  json(res, 200, { data: [sampleEvent], meta: meta(1) }, rateLimitHeaders());
}

function sendEvent(res, id) {
  if (id !== sampleEvent.id) {
    return errorResponse(res, 404, "Not found", "Event Not found", {
      headers: {},
    });
  }

  return json(res, 200, { data: sampleEvent }, rateLimitHeaders());
}

function sendSessions(res) {
  json(res, 200, { data: [sampleSession], meta: meta(1) }, rateLimitHeaders());
}

function sendSession(res, id) {
  if (id !== sampleSession.id) {
    return errorResponse(res, 404, "Not found", "Session Not found", {
      headers: {},
    });
  }

  return json(res, 200, { data: sampleSession }, rateLimitHeaders());
}

function sendSessionPeople(res, id, url) {
  if (id !== sampleSession.id) {
    return errorResponse(res, 404, "Not found", "Session Not found", {
      headers: {},
    });
  }

  const emailFilter = url.searchParams.get("filter[email]");
  const data =
    emailFilter && emailFilter !== samplePerson.attributes.email
      ? []
      : [samplePerson];

  return json(res, 200, { data, meta: meta(data.length) }, rateLimitHeaders());
}

const server = createServer((req, res) => {
  const baseUrl = getBaseUrl(req);
  const url = new URL(req.url || "/", baseUrl);

  if (req.method === "GET" && url.pathname === "/openapi.json") {
    return json(res, 200, buildOpenApiDocument(baseUrl));
  }

  if (req.method === "GET" && url.pathname === "/openapi.yaml") {
    return text(
      res,
      200,
      toYaml(buildOpenApiDocument(baseUrl)),
      "application/yaml; charset=utf-8",
    );
  }

  if (req.method === "GET" && url.pathname === "/__debug/headers") {
    return json(res, 200, req.headers);
  }

  if (!url.pathname.startsWith("/v1/")) {
    return json(res, 404, { error: "not_found", path: url.pathname });
  }

  if (!requireAuth(req, res)) return;

  if (req.method === "GET" && url.pathname === "/v1/ping") {
    return sendPing(res);
  }

  if (req.method === "GET" && url.pathname === "/v1/me") {
    return sendMe(res);
  }

  if (req.method === "GET" && url.pathname === "/v1/organization") {
    return sendOrganization(res);
  }

  if (req.method === "GET" && url.pathname === "/v1/events") {
    return sendEvents(res);
  }

  if (req.method === "GET" && url.pathname === `/v1/events/${sampleEvent.id}`) {
    return sendEvent(res, sampleEvent.id);
  }

  if (req.method === "GET" && url.pathname === "/v1/sessions") {
    return sendSessions(res);
  }

  if (
    req.method === "GET" &&
    url.pathname === `/v1/sessions/${sampleSession.id}`
  ) {
    return sendSession(res, sampleSession.id);
  }

  if (
    req.method === "GET" &&
    url.pathname === `/v1/sessions/${sampleSession.id}/people`
  ) {
    return sendSessionPeople(res, sampleSession.id, url);
  }

  return errorResponse(res, 404, "Not found", "Resource not found", req);
});

server.listen(port, host, () => {
  console.error(
    `Livestorm-style auth repro server listening on http://${host}:${port}\n` +
      `OpenAPI JSON: http://${host}:${port}/openapi.json\n` +
      `OpenAPI YAML: http://${host}:${port}/openapi.yaml\n` +
      `Protected base: http://${host}:${port}/v1\n` +
      `Debug headers endpoint: http://${host}:${port}/__debug/headers`,
  );
});
