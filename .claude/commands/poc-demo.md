---
description: Collaborate on a proof-of-concept demo for a Linear feature ticket using frontend-only changes
---

# POC Demo

You are collaborating with the user to build a proof-of-concept that communicates a feature's value through interactive UI and screen recordings. No backend changes. No working system-level functionality. Just enough to see, feel, and discuss the feature.

The goal is to create alignment around vision _before_ changing how the system behaves.

## Artifacts to produce

1. A runnable frontend showing the feature interactions
2. Screen recordings showing common use cases (user records these; you help plan the scripts)
3. A checklist of screencast scripts (one per user journey) with starting states defined
4. The Linear ticket updated with journey sections, video placeholders, and discussion items
5. A Pull Request with the `preview` label, using the Linear ticket's preferred branch name

## Out of scope

- Scoping the actual engineering work or system changes needed to ship the feature
- Making changes to API surfaces or backend code
- Making builds pass

## Phase 1: Align on the feature

1. Get the Linear ticket from the user (use Linear MCP if available, otherwise ask them to paste it). Add the `poc` label to the ticket.
2. Read and internalize the ticket. Ask clarifying questions.
3. Identify all user journeys required to demonstrate the feature's value. Work with the user to enumerate them.
4. For each journey, produce a mermaid diagram and add it to the ticket.
5. If a journey assumes pre-existing system state (e.g. "user already has a project with 3 toolsets"), call that out explicitly. Add a checkbox to the ticket: "Verify onboarding to this starting state is acceptable."

Each journey gets its own section in the Linear ticket containing:

- Mermaid diagram of the workflow
- Placeholder for a video recording
- Discussion section with key product questions

## Phase 2: Build the frontend POC

**Rules:**

- Only change code in `client/dashboard/`
- Prioritize visual clarity of the concept over code quality
- Mock data however is easiest - hardcoded JSON, fake hooks, whatever gets you moving
- Use `docker-compose exec psql -c "<QUERY>"` to seed starting states; reference `server/database/schema.sql` for the schema
- Prompt the user to deploy sources if seed state is insufficient
- Aim for visual impact. Make the feature obvious and interactive.
- Prefer reusing existing UI and functionality to achieve a step in a journey over building something new. Identifying that an existing feature already covers a step is more valuable than implementing a new one.

**Process:**

1. Inspect the schema to understand what seed data you need
2. Set up starting states via SQL
3. Build the UI changes. Prefer fast iteration over clean abstractions.
4. Test each journey interactively with the user

## Phase 3: Define screencast scripts

For each user journey:

1. Define the exact starting state (SQL seeds, page to start on)
2. Write a step-by-step script: what to click, what to look for, what to narrate
3. If a UI element needs visual emphasis during the recording, add temporary CSS to highlight it
4. Align with the user on the script before they record

## Phase 4: Update the Linear ticket

1. Ensure every journey has its own section with: diagram, video placeholder, discussion items
2. Add a "Key product decisions" section capturing choices made during the POC
3. Add scope-reduction suggestions if any emerged during the work

## Phase 5: Open the PR

1. Use the Linear ticket's preferred branch name (e.g. `quinn/gram-1234-feature-name`)
2. Commit all changes and push
3. Open a PR with the `preview` label
4. Link the PR in the Linear ticket
