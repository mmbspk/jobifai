package quota

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/user/jobifai/internal/domain"
)

func TestBurnCreditsFromRawMicro_IncludesMarkupAndFee(t *testing.T) {
	t.Parallel()
	def := domain.QuotaDefaults{CreditsPerUSD: 1000, ServiceMarkup: 0.5, PerCallFeeUSD: 0.002}
	raw := int64(20_000) // $0.02
	credits := BurnCreditsFromRawMicro(def, raw, true)
	assert.Greater(t, credits, int64(30))
}

func TestCreditsFromLoadedUSD_MinimumOne(t *testing.T) {
	t.Parallel()
	def := domain.QuotaDefaults{CreditsPerUSD: 1000}
	assert.Equal(t, int64(1), CreditsFromLoadedUSD(def, 1))
}
