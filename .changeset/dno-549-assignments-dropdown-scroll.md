---
"dashboard": patch
---

Fix the plugin assignments dropdown not scrolling when it contains many users/roles. The multi-select popover is portaled inside the modal assignments sheet, so make its popover modal to restore mouse-wheel scrolling of the options list.
