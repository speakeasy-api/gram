---
"server": minor
---

Propagate assistant runtime image upgrades to existing fly.io machines: on the next admission, an idle machine running an older runtime image is recycled in place to the latest version. Mid-turn admissions are left alone so a future idle window picks up the upgrade.
