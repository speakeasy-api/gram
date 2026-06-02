# Problem Statement

Currently risk policies have no audience, meaning we have no way to link which policies should apply to which users or roles.

We have an initial lay of the land here: /Users/tzmendes/.claude/plans/ok-i-ve-got-more-expressive-manatee.md

We have decided that we are going with the approach of using RBAC for this.

# Key design decisions

- For now we are only concerned with positive permissions (this policy applies to X). We do not care about bypass yet (this policy does not apply to Y).
- Zanzibar is our north star. Whatever we name our scopes should be inspired by it.
- We ARE NOT using selectors as a mean to store policy business logic. Selectors just link to a policy ID.
- When a policy applies to a role, it means that that policy will be further evaluated. If the policy does NOT apply to a role, then it wont.
- Even though the system we're using underneath is RBAC, we won't be displaying policy setting rules in our current RBAC screen - that's going to be an option in the policy page (for example, when setting a policy, we will have an audience section where you can say role:engineer). UI changes are out of scope for now.
- There should be an option to apply a policy to everyone

# Logical flow

- We receive a hook from an agent
- We check if the hook is tied to a user and if that user belongs to Gram. If so, we load the users' grants.
- If the user is authed and has grants, we fetch applicable policies that apply to the users grants
- For each policy that should be applied we list policies and evaluate them as usual, given by the policy ID being applied

Flow continues as usual.

# What we need

- A solid plan and changes required
- Think about caveats, problems, things to watch out for
- A plan on how to split work into small, safe, reviewable PRs
- Write this in a separate plan file, keep this file as is (pristine)
- Think about a plan as you would an RFC
- Think about potential refactoring needs on existing code to make things cleaner
- Clean code, best design practices in mind
