---
"dashboard": patch
---

Extend the brand-mesh surface treatment from the project home assistant
card to the full `/chat` landing page: the same neutral card-to-background
gradient with the brand rainbow breathing in from the top-right corner and
a film-grain wash. The decorative layers are now a shared `BrandMeshLayers`
component so the two surfaces can't drift apart, and the landing's scroll
container moved to an inner wrapper so the mesh and the back button stay
pinned while the content scrolls.
