package web

import (
	"bytes"
	"net/http"
	"time"

	"github.com/xz1220/repotempo/integrations"
)

func (h *Handler) agentBundle(w http.ResponseWriter, r *http.Request) {
	data, err := integrations.AgentArchive()
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", `attachment; filename="repotempo-agent.zip"`)
	http.ServeContent(w, r, "repotempo-agent.zip", time.Time{}, bytes.NewReader(data))
}
