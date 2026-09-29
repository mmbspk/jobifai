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

func (c *Client) checkCostCeiling(inputChars, maxOutputTokens int) error {
	if c.costCeiling == nil {
		return nil
	}
	estIn := int64(inputChars / 4)
	if estIn < 1 {
		estIn = 1
	}
	estOut := int64(maxOutputTokens)
	if estOut < 1 {
		estOut = 512
	}
	res, err := c.costCeiling.catalog.Resolve(c.cfg.Model, true)
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
