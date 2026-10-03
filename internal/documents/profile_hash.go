package documents

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"

	"github.com/user/jobifai/internal/domain"
)

func ProfileSnapshotHash(p *domain.ResumeProfile) string {
	if p == nil {
		return ""
	}
	b, err := json.Marshal(p)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
