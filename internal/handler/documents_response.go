package handler

import (
	"errors"
	"net/http"

	"github.com/user/jobifai/internal/quota"
)

func writeDocumentVersionCreated(w http.ResponseWriter, versionID string, err error) {
	if err != nil && versionID != "" {
		writeJSON(w, http.StatusCreated, map[string]string{
			"content_version_id": versionID,
			"render_status":      "failed",
			"message":            err.Error(),
		})
		return
	}
	if err != nil {
		if errors.Is(err, quota.ErrExceeded) {
			writeJSON(w, http.StatusPaymentRequired, map[string]string{"message": "insufficient credits"})
			return
		}
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"message": err.Error()})
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{
		"content_version_id": versionID,
		"render_status":      "ok",
	})
}
