# Job scoring benchmark — ground truth philosophy

## Question the benchmark answers

**Should Jobifai surface this opportunity to the user?**

It does not ask whether the candidate is guaranteed to be hired or meets every preferred qualification.

## PASS (hard positive)

Label **pass** when a realistic applicant would **benefit from seeing the role** in their feed: strong fit, transferable fit, or adjacent fit where core duties overlap and no **mandatory** gate is clearly missing from visible profile facts.

## SKIP (hard negative)

Label **skip** when surfacing would likely waste user time: clear mismatch, mandatory qualification/licence/certification/location constraint visible in the posting that the profile does not satisfy, or seniority band is implausible.

## BORDERLINE (tracked separately)

Label **borderline** when reasonable recruiters could disagree, or mandatory requirements are **ambiguous from visible profile text alone**. Borderline cases are **excluded** from hard TP/TN/FP/FN/FNR/FPR. We still record model scores and whether the model would surface the job at the production threshold (default 7).

Examples:

- **Clinical Care Coordinator → Nurse Manager**: coordination skills align, but RN credentials and formal management authority are not visible → **borderline**.
- **Docker/Linux platform engineer → Kubernetes platform engineer** when the posting **explicitly requires** hands-on Kubernetes orchestration → **skip** (mandatory skill missing), not borderline.

## Adjudication

Human labels in `adjudication_2026-09-30.json` override prior synthetic expectations. Dataset version history is kept under `full/pre_adjudication_2026-09-30/`.
