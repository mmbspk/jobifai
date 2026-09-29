package llm

import (
	"fmt"

	"github.com/user/jobifai/internal/pricing"
)

// ErrPolicyCostExceeded is returned when an approved policy max-cost ceiling would be exceeded.
var ErrPolicyCostExceeded = fmt.Errorf("llm policy max cost exceeded")

type costCeiling struct {
	catalog *pricing.Catalog
	maxUSD  float64
}

func (c *Client) WithCostCeiling(catalog *pricing.Catalog, maxUSD float64) *Client {
	if catalog == nil || maxUSD <= 0 {
		return c
	}
	nc := *c
	nc.costCeiling = &costCeiling{catalog: catalog, maxUSD: maxUSD}
	return &nc
}

func (c *Client) checkVisionCostCeiling(imageBytes []byte, promptChars, maxOutputTokens int) error {
	estIn := conservativeVisionInputTokens(len(imageBytes), promptChars)
	return c.checkCostCeilingTokens(estIn, int64(maxOutputTokens))
}

func conservativeVisionInputTokens(imageLen, promptChars int) int64 {
	// Conservative ceiling: image bytes do not map 1:1 to text tokens.
	est := int64(imageLen/4 + promptChars/4)
	if est < 1500 {
		est = 1500
	}
	return est
}

func (c *Client) checkCostCeiling(inputChars, maxOutputTokens int) error {
	estIn := int64(inputChars / 4)
	if estIn < 1 {
		estIn = 1
	}
	return c.checkCostCeilingTokens(estIn, int64(maxOutputTokens))
}

func (c *Client) checkCostCeilingTokens(estIn, maxOutputTokens int64) error {
	if c.costCeiling == nil {
		return nil
	}
	estOut := maxOutputTokens
	if estOut < 1 {
		estOut = 512
	}
	res, err := c.costCeiling.catalog.Resolve(c.cfg.Model, true)
	if err != nil || res.UsedFallback || !res.Known {
		if c.billing.EvalPrice != nil {
			if er, e2 := c.billing.EvalPrice(c.cfg.Model); e2 == nil {
				res = er
				err = nil
			}
		}
	}
	if err != nil {
		return err
	}
	micro := pricing.RawCostMicroUSD(res.Record, pricing.TokenUsage{
		InputTokens:  estIn,
		OutputTokens: estOut,
	})
	maxMicro := int64(c.costCeiling.maxUSD * 1_000_000)
	if micro > maxMicro {
		return fmt.Errorf("%w: estimated $%.4f exceeds ceiling $%.4f", ErrPolicyCostExceeded, float64(micro)/1_000_000, c.costCeiling.maxUSD)
	}
	return nil
}
