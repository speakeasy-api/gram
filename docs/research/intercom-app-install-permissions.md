# Intercom app installation and teammate permissions

Research date: 2026-09-10. Sources: official Intercom Help and developer documentation, retrieved directly.

## Where an administrator changes access

1. In the workspace receiving the app, open **Settings > Workspace > Teammates**.
2. Hover over the installing teammate and click **Edit** (the help article also describes clicking the teammate's name).
3. Under **Apps and integrations**, enable **Can install, configure and delete apps**. Click **Save changes**. Intercom says saved teammate-permission changes take effect immediately, without signing out. [1]

The person making this change must have **Can manage teammates, seats and permissions** on their own account. That permission grants access to the Teammates settings, including Roles, and the ability to edit teammate permissions. Teammates **cannot change their own permissions**; another teammate with the management permission must do it. An informal “admin” title alone is not the documented requirement. Test workspaces inherit teammate permissions from the main workspace, so make changes in the main workspace. [1]

Intercom explicitly says that without **Can install, configure and delete apps**, teammates can visit the App Store but must request installation from an admin in a dropdown. This makes the permission a relevant first check for installation trouble. It does **not** establish that a missing **Authorize Access** button on a particular OAuth page is caused by that permission. [1]

## Plan limits and role-based access

- **Custom roles are only available on the Expert plan**, according to the current custom-roles help article. The reviewed teammate-permissions article does not state an Expert-only restriction on the individual installation permission; do not infer that an upgrade is needed merely to authorize an app. [1][2]
- To create a reusable role: **Settings > Workspace > Teammates > Roles > New role**, select permissions, and save. To edit one: open the **Roles** tab, click the role's **gear icon**, adjust permissions, then **Apply changes** and confirm. Changes apply immediately to **all teammates with that role**. You cannot edit your own role or permissions. Prefer changing only the intended teammate unless a broader role change is deliberate. [2]

## Workspace permission versus developer app permissions

| Control                                    | Location / purpose                                                                                                                                                                                                                                                                                                                                 |
| ------------------------------------------ | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| **Can install, configure and delete apps** | Workspace teammate permission under **Apps and integrations**; controls whether a person may install/manage apps. [1]                                                                                                                                                                                                                              |
| **Can access developer hub**               | Separate workspace teammate permission under **Apps and integrations**; controls Developer Hub access, not the same installation permission. [1]                                                                                                                                                                                                   |
| App OAuth **Permissions** / scopes         | The app's **Authentication** page in **Developer Hub**; the developer selects which account data/actions the application requests, such as **Read conversations** or **Write conversations**. These scopes are presented to the user during OAuth consent. Changing them does not grant the installing teammate workspace installation rights. [3] |

Public apps accessing other workspaces must use OAuth, whether installed through the App Store or the provider's website. Private apps accessing the developer's own workspace can use an access token. Do not substitute a manually shared access token for third-party OAuth; Intercom explicitly warns against giving access tokens to third parties. [4][5]

## Missing Authorize Access: what is and is not established

The OAuth guide documents presenting requested permissions, user approval, and redirection back to the app. It does not document a missing **Authorize Access** button as a diagnostic signature of insufficient teammate permissions. The reviewed sources therefore support a **permission check**, not a confirmed root cause. [1][3]

Recommended verification (inference from the documented controls): confirm the signed-in teammate and intended workspace, ask another authorized teammate to inspect the installation permission there, save any approved change, and retry the OAuth flow. If it is already enabled or the symptom persists, capture the non-sensitive on-page error and escalate rather than repeatedly broadening permissions or changing app scopes. Do not grant full administrator permissions merely as a workaround.

## Primary sources

1. [Teammate permissions: how to control workspace access](https://www.intercom.com/help/en/articles/176-teammate-permissions-how-to-control-workspace-access) — settings navigation, exact permission names, who can edit, self-edit restriction, immediate effect, test-workspace inheritance.
2. [Manage permissions effortlessly with custom roles](https://www.intercom.com/help/en/articles/3659500-manage-permissions-effortlessly-with-custom-roles) — Expert-plan limit, role navigation, role-wide effects.
3. [Setting up OAuth](https://developers.intercom.com/docs/build-an-integration/learn-more/authentication/setting-up-oauth) — app Authentication configuration, requested permissions/scopes, approval flow.
4. [Installing & Uninstalling Apps](https://developers.intercom.com/docs/build-an-integration/learn-more/authentication/installing-uninstalling-apps) — public/private installation and OAuth requirements.
5. [Authentication](https://developers.intercom.com/docs/build-an-integration/learn-more/authentication) — private access tokens versus public OAuth; warning against third-party token sharing.
