---
"dashboard": patch
---

Two refinements to the PAYG rate adjustment in the TUM contract price estimator: a cheaper-than-committed PAYG outcome is only attributed to the adjustment when it is a discount — under an uplift the modelling-error warning ("check the platform fee against the baseline") is kept, since an uplift only raises PAYG above list rates; and adjustments at or past -100% are now ignored (list rates) at every layer instead of pricing PAYG as free.
