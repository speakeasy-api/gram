#!/usr/bin/env node

import { randomUUID } from "crypto";
import { readFileSync } from "fs";

// Database connection
import { Pool } from "pg";

const pool = new Pool({
  host: "127.0.0.1",
  port: 5439,
  user: "gram",
  password: "gram",
  database: "gram",
});

// Sample messages with hidden secrets
const messagesWithSecrets = [
  "Hey, I'm setting up the deployment. Here's the config: AWS_ACCESS_KEY_ID=AKIAIOSFODNN7EXAMPLE and AWS_SECRET_ACCESS_KEY=wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
  "Can you help me debug this? The API key is sk-1234567890abcdef1234567890abcdef1234567890abcdef",
  "I'm connecting to the database with postgres://username:password123@localhost:5432/mydb",
  "The JWT secret is: jwt_secret_key_abc123xyz789",
  "GitHub personal access token: ghp_1234567890abcdefghijklmnopqrstuvwxyz123",
  "Here's the Stripe secret key: sk_test_51234567890abcdefghijklmnopqrstuvwxyz",
  "Docker registry login: docker login -u myuser -p mypassword123 registry.example.com",
  "The private key is: -----BEGIN RSA PRIVATE KEY-----\nMIIEowIBAAKCAQEA1234567890abcdef...",
  "SSH key: ssh-rsa AAAAB3NzaC1yc2EAAAADAQABAAABAQC1234567890 user@hostname",
  "Firebase config: apiKey: AIzaSyC1234567890abcdefghijklmnopqrstuvwxyz",
];

const normalMessages = [
  "How do I implement authentication in React?",
  "Can you help me optimize this SQL query?",
  "What's the best practice for error handling in Node.js?",
  "I'm getting a CORS error, any ideas?",
  "How do I deploy to Kubernetes?",
  "Can you review my API design?",
  "What's the difference between REST and GraphQL?",
  "How do I handle file uploads securely?",
  "Best practices for Docker containerization?",
  "How to implement caching with Redis?",
  "What's the recommended folder structure for a React app?",
  "How do I optimize database queries?",
  "Can you explain microservices architecture?",
  "What's the best way to handle user sessions?",
  "How do I implement rate limiting?",
  "Best practices for API versioning?",
  "How to handle background jobs in Node.js?",
  "What's the difference between OAuth and JWT?",
  "How do I implement real-time features with WebSockets?",
  "Can you help me design a scalable database schema?",
  "How to implement proper logging and monitoring?",
  "What's the best approach for testing APIs?",
  "How do I handle database migrations?",
  "Can you explain the CAP theorem?",
  "How to implement search functionality?",
  "What's the best way to handle configuration management?",
  "How do I optimize React performance?",
  "Can you help me understand async/await?",
  "How to implement proper validation?",
  "What's the difference between SQL and NoSQL?",
];

// Helper to get random element from array
function getRandomElement<T>(arr: T[]): T {
  return arr[Math.floor(Math.random() * arr.length)];
}

// Helper to generate realistic chat conversation
function generateChatMessages(count: number) {
  const messages = [];
  const projectId = "019d916d-b437-7710-ba0f-eb2eb1eb6e32"; // From seed data
  const organizationId = "550e8400-e29b-41d4-a716-446655440000";
  const userId = "dev-user-1";

  // Mix of secrets and normal messages (about 15% with secrets)
  const secretsToInclude = Math.floor(count * 0.15);
  const normalToInclude = count - secretsToInclude;

  // Add messages with secrets
  for (let i = 0; i < secretsToInclude; i++) {
    messages.push({
      id: randomUUID(),
      chatId: randomUUID(),
      projectId,
      organizationId,
      role: "user",
      content: getRandomElement(messagesWithSecrets),
      userId,
      externalUserId: userId,
      createdAt: new Date(Date.now() - Math.random() * 7 * 24 * 60 * 60 * 1000), // Random time in last week
    });
  }

  // Add normal messages
  for (let i = 0; i < normalToInclude; i++) {
    messages.push({
      id: randomUUID(),
      chatId: randomUUID(),
      projectId,
      organizationId,
      role: Math.random() > 0.5 ? "user" : "assistant",
      content: getRandomElement(normalMessages),
      userId,
      externalUserId: userId,
      createdAt: new Date(Date.now() - Math.random() * 7 * 24 * 60 * 60 * 1000),
    });
  }

  return messages.sort((a, b) => a.createdAt.getTime() - b.createdAt.getTime());
}

async function seedChatMessages() {
  try {
    console.log("🚀 Starting to seed chat messages with secrets...");

    const messages = generateChatMessages(200); // Generate 200 messages

    console.log(
      `📝 Generated ${messages.length} messages (${messages.filter((m) => messagesWithSecrets.some((s) => m.content === s)).length} with potential secrets)`,
    );

    // Insert messages into database
    const client = await pool.connect();

    try {
      await client.query("BEGIN");

      for (const message of messages) {
        await client.query(
          `
          INSERT INTO chat_messages (
            id, chat_id, project_id, organization_id, role, content,
            user_id, external_user_id, created_at, updated_at,
            message_id, tool_call_id, finish_reason, tool_calls,
            prompt_tokens, completion_tokens, total_tokens,
            origin, user_agent, ip_address, source
          ) VALUES (
            $1, $2, $3, $4, $5, $6, $7, $8, $9, $10,
            $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, $21
          )
        `,
          [
            message.id,
            message.chatId,
            message.projectId,
            message.organizationId,
            message.role,
            message.content,
            message.userId,
            message.externalUserId,
            message.createdAt,
            message.createdAt,
            randomUUID(), // message_id
            "", // tool_call_id
            message.role === "assistant" ? "stop" : null, // finish_reason
            "[]", // tool_calls
            Math.floor(Math.random() * 1000) + 100, // prompt_tokens
            Math.floor(Math.random() * 500) + 50, // completion_tokens
            Math.floor(Math.random() * 1500) + 150, // total_tokens
            "api", // origin
            "test-seeder/1.0", // user_agent
            "127.0.0.1", // ip_address
            "api", // source
          ],
        );
      }

      await client.query("COMMIT");
      console.log(`✅ Successfully inserted ${messages.length} chat messages`);
    } catch (error) {
      await client.query("ROLLBACK");
      throw error;
    } finally {
      client.release();
    }
  } catch (error) {
    console.error("❌ Error seeding chat messages:", error);
    process.exit(1);
  } finally {
    await pool.end();
  }
}

// Run the seeder
seedChatMessages().then(() => {
  console.log("🎉 Chat message seeding completed!");
});
