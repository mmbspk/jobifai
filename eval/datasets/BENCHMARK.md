# Eval dataset confidence notes

Smoke datasets (`smoke/`) are small, representative samples for cheap regression checks.

Full datasets (`full/`) repeat a fixed set of semantic scenario templates to reach target row counts. **Raw row count must not be treated as independent sample size** for production recommendation confidence.

Before a full benchmark or production policy promotion, either:

- generate genuinely varied cases per scenario template, or
- report effective sample size / confidence using **unique scenario tags** or normalized semantic inputs (see `dataset.BuildQualityReport`).

Until then, use full-run metrics for directional comparison only, not high-confidence deployment decisions.
