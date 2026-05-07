---
"server": patch
---

add prompt-injection detection to risk analysis. layered heuristic engine — instruction override, role hijack, system-prompt leak, jailbreak personas, delimiter injection, encoded payload, tool abuse — runs in both the batch analyzer and the realtime hook path; findings carry rule_id and confidence into risk_results. deberta classifier descoped for a follow-up; the detector ships heuristics-only
