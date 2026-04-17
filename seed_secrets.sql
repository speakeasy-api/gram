-- Insert chat messages with secrets for testing risk analysis

INSERT INTO chat_messages (
    id, chat_id, project_id, role, content,
    user_id, external_user_id, created_at,
    message_id, tool_call_id, finish_reason, tool_calls,
    prompt_tokens, completion_tokens, total_tokens
) VALUES
-- Messages with AWS credentials
(
    generate_uuidv7(), generate_uuidv7(), '019d916d-b437-7710-ba0f-eb2eb1eb6e32',
    'user', 'Hey, I''m setting up the deployment. Here''s the config: AWS_ACCESS_KEY_ID=AKIAIOSFODNN7EXAMPLE and AWS_SECRET_ACCESS_KEY=wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY',
    'dev-user-1', 'dev-user-1', NOW() - INTERVAL '1 hour',
    generate_uuidv7(), '', NULL, '[]', 150, 75, 225
),
-- Message with OpenAI API key
(
    generate_uuidv7(), generate_uuidv7(), '019d916d-b437-7710-ba0f-eb2eb1eb6e32', '550e8400-e29b-41d4-a716-446655440000',
    'user', 'Can you help me debug this? The API key is sk-1234567890abcdef1234567890abcdef1234567890abcdef',
    'dev-user-1', 'dev-user-1', NOW() - INTERVAL '2 hours', NOW() - INTERVAL '2 hours',
    generate_uuidv7(), '', NULL, '[]', 120, 60, 180, 'api', 'test-seeder/1.0', '127.0.0.1', 'api'
),
-- Message with database URL
(
    generate_uuidv7(), generate_uuidv7(), '019d916d-b437-7710-ba0f-eb2eb1eb6e32', '550e8400-e29b-41d4-a716-446655440000',
    'user', 'I''m connecting to the database with postgres://username:password123@localhost:5432/mydb',
    'dev-user-1', 'dev-user-1', NOW() - INTERVAL '3 hours', NOW() - INTERVAL '3 hours',
    generate_uuidv7(), '', NULL, '[]', 100, 50, 150, 'api', 'test-seeder/1.0', '127.0.0.1', 'api'
),
-- Message with JWT secret
(
    generate_uuidv7(), generate_uuidv7(), '019d916d-b437-7710-ba0f-eb2eb1eb6e32', '550e8400-e29b-41d4-a716-446655440000',
    'user', 'The JWT secret is: jwt_secret_key_abc123xyz789',
    'dev-user-1', 'dev-user-1', NOW() - INTERVAL '4 hours', NOW() - INTERVAL '4 hours',
    generate_uuidv7(), '', NULL, '[]', 80, 40, 120, 'api', 'test-seeder/1.0', '127.0.0.1', 'api'
),
-- Message with GitHub token
(
    generate_uuidv7(), generate_uuidv7(), '019d916d-b437-7710-ba0f-eb2eb1eb6e32', '550e8400-e29b-41d4-a716-446655440000',
    'user', 'GitHub personal access token: ghp_1234567890abcdefghijklmnopqrstuvwxyz123',
    'dev-user-1', 'dev-user-1', NOW() - INTERVAL '5 hours', NOW() - INTERVAL '5 hours',
    generate_uuidv7(), '', NULL, '[]', 90, 45, 135, 'api', 'test-seeder/1.0', '127.0.0.1', 'api'
),
-- Message with Stripe secret
(
    generate_uuidv7(), generate_uuidv7(), '019d916d-b437-7710-ba0f-eb2eb1eb6e32', '550e8400-e29b-41d4-a716-446655440000',
    'user', 'Here''s the Stripe secret key: sk_test_51234567890abcdefghijklmnopqrstuvwxyz',
    'dev-user-1', 'dev-user-1', NOW() - INTERVAL '6 hours', NOW() - INTERVAL '6 hours',
    generate_uuidv7(), '', NULL, '[]', 110, 55, 165, 'api', 'test-seeder/1.0', '127.0.0.1', 'api'
),
-- Message with private key
(
    generate_uuidv7(), generate_uuidv7(), '019d916d-b437-7710-ba0f-eb2eb1eb6e32', '550e8400-e29b-41d4-a716-446655440000',
    'user', 'The private key is: -----BEGIN RSA PRIVATE KEY-----
MIIEowIBAAKCAQEA1234567890abcdef...',
    'dev-user-1', 'dev-user-1', NOW() - INTERVAL '7 hours', NOW() - INTERVAL '7 hours',
    generate_uuidv7(), '', NULL, '[]', 200, 100, 300, 'api', 'test-seeder/1.0', '127.0.0.1', 'api'
),
-- Message with SSH key
(
    generate_uuidv7(), generate_uuidv7(), '019d916d-b437-7710-ba0f-eb2eb1eb6e32', '550e8400-e29b-41d4-a716-446655440000',
    'user', 'SSH key: ssh-rsa AAAAB3NzaC1yc2EAAAADAQABAAABAQC1234567890 user@hostname',
    'dev-user-1', 'dev-user-1', NOW() - INTERVAL '8 hours', NOW() - INTERVAL '8 hours',
    generate_uuidv7(), '', NULL, '[]', 130, 65, 195, 'api', 'test-seeder/1.0', '127.0.0.1', 'api'
),
-- Message with Firebase API key
(
    generate_uuidv7(), generate_uuidv7(), '019d916d-b437-7710-ba0f-eb2eb1eb6e32', '550e8400-e29b-41d4-a716-446655440000',
    'user', 'Firebase config: apiKey: AIzaSyC1234567890abcdefghijklmnopqrstuvwxyz',
    'dev-user-1', 'dev-user-1', NOW() - INTERVAL '9 hours', NOW() - INTERVAL '9 hours',
    generate_uuidv7(), '', NULL, '[]', 140, 70, 210, 'api', 'test-seeder/1.0', '127.0.0.1', 'api'
),
-- Message with Docker credentials
(
    generate_uuidv7(), generate_uuidv7(), '019d916d-b437-7710-ba0f-eb2eb1eb6e32', '550e8400-e29b-41d4-a716-446655440000',
    'user', 'Docker registry login: docker login -u myuser -p mypassword123 registry.example.com',
    'dev-user-1', 'dev-user-1', NOW() - INTERVAL '10 hours', NOW() - INTERVAL '10 hours',
    generate_uuidv7(), '', NULL, '[]', 160, 80, 240, 'api', 'test-seeder/1.0', '127.0.0.1', 'api'
);

-- Add some normal messages too
INSERT INTO chat_messages (
    id, chat_id, project_id, role, content,
    user_id, external_user_id, created_at,
    message_id, tool_call_id, finish_reason, tool_calls,
    prompt_tokens, completion_tokens, total_tokens
) VALUES
(
    generate_uuidv7(), generate_uuidv7(), '019d916d-b437-7710-ba0f-eb2eb1eb6e32', '550e8400-e29b-41d4-a716-446655440000',
    'user', 'How do I implement authentication in React?',
    'dev-user-1', 'dev-user-1', NOW() - INTERVAL '30 minutes', NOW() - INTERVAL '30 minutes',
    generate_uuidv7(), '', NULL, '[]', 95, 48, 143, 'api', 'test-seeder/1.0', '127.0.0.1', 'api'
),
(
    generate_uuidv7(), generate_uuidv7(), '019d916d-b437-7710-ba0f-eb2eb1eb6e32', '550e8400-e29b-41d4-a716-446655440000',
    'assistant', 'To implement authentication in React, you can use libraries like Auth0, Firebase Auth, or build your own with JWT tokens. Here''s a basic approach...',
    'dev-user-1', 'dev-user-1', NOW() - INTERVAL '25 minutes', NOW() - INTERVAL '25 minutes',
    generate_uuidv7(), '', 'stop', '[]', 0, 250, 250, 'api', 'test-seeder/1.0', '127.0.0.1', 'api'
),
(
    generate_uuidv7(), generate_uuidv7(), '019d916d-b437-7710-ba0f-eb2eb1eb6e32', '550e8400-e29b-41d4-a716-446655440000',
    'user', 'Can you help me optimize this SQL query?',
    'dev-user-1', 'dev-user-1', NOW() - INTERVAL '20 minutes', NOW() - INTERVAL '20 minutes',
    generate_uuidv7(), '', NULL, '[]', 85, 43, 128, 'api', 'test-seeder/1.0', '127.0.0.1', 'api'
),
(
    generate_uuidv7(), generate_uuidv7(), '019d916d-b437-7710-ba0f-eb2eb1eb6e32', '550e8400-e29b-41d4-a716-446655440000',
    'user', 'What''s the best practice for error handling in Node.js?',
    'dev-user-1', 'dev-user-1', NOW() - INTERVAL '15 minutes', NOW() - INTERVAL '15 minutes',
    generate_uuidv7(), '', NULL, '[]', 105, 53, 158, 'api', 'test-seeder/1.0', '127.0.0.1', 'api'
),
(
    generate_uuidv7(), generate_uuidv7(), '019d916d-b437-7710-ba0f-eb2eb1eb6e32', '550e8400-e29b-41d4-a716-446655440000',
    'user', 'I''m getting a CORS error, any ideas?',
    'dev-user-1', 'dev-user-1', NOW() - INTERVAL '10 minutes', NOW() - INTERVAL '10 minutes',
    generate_uuidv7(), '', NULL, '[]', 75, 38, 113, 'api', 'test-seeder/1.0', '127.0.0.1', 'api'
);