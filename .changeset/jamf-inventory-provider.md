---
"server": minor
---

Add the Jamf Pro inventory-source provider to the device integrations framework. Organizations connect a Jamf Cloud tenant with an instance URL and least-privilege API Client credentials (an API Role with only "Read Computers"); the provider authenticates via the OAuth client-credentials grant with the token cached until expiry, pulls the computer inventory in stably ordered, section-filtered pages, and maps each device's serial, hostname, OS, assigned-user email, and last check-in into the managed-device store — preserving the full vendor record. Credential rejections, including tokens expiring mid-pull, classify as auth errors feeding the scheduler's auto-pause streak, and every API request carries the unique User-Agent header the Jamf Technology Partner Program requires.
