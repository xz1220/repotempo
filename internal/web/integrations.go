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

func (h *Handler) agentSkillBundle(w http.ResponseWriter, r *http.Request) {
	data, err := integrations.SkillArchive()
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "application/gzip")
	w.Header().Set("Content-Disposition", `attachment; filename="repotempo-skill.tar.gz"`)
	http.ServeContent(w, r, "repotempo-skill.tar.gz", time.Time{}, bytes.NewReader(data))
}

func (h *Handler) agentSkillInstaller(w http.ResponseWriter, r *http.Request) {
	data, err := integrations.SkillInstaller()
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	http.ServeContent(w, r, "install-skill.sh", time.Time{}, bytes.NewReader(data))
}
