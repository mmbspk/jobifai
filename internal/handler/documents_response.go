package handler

import "net/http"

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
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"message": err.Error()})
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{
		"content_version_id": versionID,
		"render_status":      "ok",
	})
}
