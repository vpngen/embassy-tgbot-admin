package handler

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/vpngen/embassy-tgbot-admin/internal/auth"
	"github.com/vpngen/embassy-tgbot-admin/internal/model"
	"github.com/vpngen/embassy-tgbot-admin/internal/storage"
)

// Handler holds dependencies for HTTP handlers.
type Handler struct {
	store      *storage.FileStore
	auth       *auth.Auth
	botWebhook string // URL to notify the bot on changes (optional)
}

// New creates a new Handler.
func New(store *storage.FileStore, auth *auth.Auth, botWebhook string) *Handler {
	return &Handler{store: store, auth: auth, botWebhook: botWebhook}
}

// RegisterRoutes sets up the HTTP routes on the given mux.
// Protected routes require a valid JWT Bearer token.
func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	// Public: login endpoint (Basic Auth → JWT)
	mux.HandleFunc("POST /api/login", h.login)

	// Protected API routes (require JWT or API key)
	protected := http.NewServeMux()
	protected.HandleFunc("GET /api/flows", h.listFlows)
	protected.HandleFunc("GET /api/flows/{id}", h.getFlow)
	protected.HandleFunc("PUT /api/flows/{id}", h.updateFlow)
	protected.HandleFunc("DELETE /api/flows/{id}", h.deleteFlow)
	protected.HandleFunc("POST /api/flows/publish", h.publishFlows)
	protected.HandleFunc("GET /api/flows/export", h.exportFlows)
	protected.HandleFunc("PATCH /api/flows/{id}/stages/{stageId}/buttons/reorder", h.reorderButtons)

	protected.HandleFunc("GET /api/decisions", h.getDecisions)
	protected.HandleFunc("PUT /api/decisions", h.updateDecisions)

	protected.HandleFunc("GET /api/ministry", h.getMinistryMessages)
	protected.HandleFunc("PUT /api/ministry", h.updateMinistryMessages)

	mux.Handle("/api/", h.auth.Middleware(protected))
}

// parseLang extracts and validates the lang query parameter.
// Returns "" for Russian (default), "en" for English.
func parseLang(r *http.Request) string {
	l := r.URL.Query().Get("lang")
	if l == "en" {
		return "en"
	}
	return ""
}

func (h *Handler) listFlows(w http.ResponseWriter, r *http.Request) {
	items, err := h.store.List()
	if err != nil {
		httpError(w, http.StatusInternalServerError, "failed to list flows: %s", err)
		return
	}

	if items == nil {
		items = []model.FlowListItem{}
	}

	jsonResponse(w, http.StatusOK, items)
}

func (h *Handler) getFlow(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		httpError(w, http.StatusBadRequest, "missing flow id")
		return
	}

	lang := parseLang(r)

	flow, err := h.store.GetLang(id, lang)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			httpError(w, http.StatusNotFound, "flow %q not found", id)
			return
		}
		httpError(w, http.StatusInternalServerError, "failed to get flow: %s", err)
		return
	}

	jsonResponse(w, http.StatusOK, flow)
}

func (h *Handler) updateFlow(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		httpError(w, http.StatusBadRequest, "missing flow id")
		return
	}

	lang := parseLang(r)

	var update model.FlowUpdate
	if err := json.NewDecoder(r.Body).Decode(&update); err != nil {
		httpError(w, http.StatusBadRequest, "invalid json: %s", err)
		return
	}

	if len(update.Stages) == 0 {
		httpError(w, http.StatusBadRequest, "flow must have at least one stage")
		return
	}

	// Validate stages
	stageIDs := make(map[string]bool)
	for _, s := range update.Stages {
		if s.ID == "" {
			httpError(w, http.StatusBadRequest, "every stage must have an id")
			return
		}
		if stageIDs[s.ID] {
			httpError(w, http.StatusBadRequest, "duplicate stage id: %s", s.ID)
			return
		}
		stageIDs[s.ID] = true
	}

	// Validate cross-references
	for _, s := range update.Stages {
		if s.OnSuccess != "" && !stageIDs[s.OnSuccess] {
			httpError(w, http.StatusBadRequest, "stage %q references unknown on_success: %q", s.ID, s.OnSuccess)
			return
		}
		if s.OnFailure != "" && !stageIDs[s.OnFailure] {
			httpError(w, http.StatusBadRequest, "stage %q references unknown on_failure: %q", s.ID, s.OnFailure)
			return
		}
		// for _, btn := range s.Buttons {
		// 	if btn.Action == "goto" && !stageIDs[btn.Target] {
		// 		httpError(w, http.StatusBadRequest, "stage %q button %q references unknown target: %q", s.ID, btn.Label, btn.Target)
		// 		return
		// 	}
		// }
	}

	flow := &model.Flow{
		ID:     id,
		Name:   update.Name,
		Stages: update.Stages,
	}
	if flow.Name == "" {
		flow.Name = id
	}

	if err := h.store.SaveLang(flow, lang); err != nil {
		httpError(w, http.StatusInternalServerError, "failed to save flow: %s", err)
		return
	}

	jsonResponse(w, http.StatusOK, flow)
}

func (h *Handler) deleteFlow(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		httpError(w, http.StatusBadRequest, "missing flow id")
		return
	}

	if err := h.store.Delete(id); err != nil {
		if strings.Contains(err.Error(), "not found") {
			httpError(w, http.StatusNotFound, "flow %q not found", id)
			return
		}
		httpError(w, http.StatusInternalServerError, "failed to delete flow: %s", err)
		return
	}

	jsonResponse(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func (h *Handler) publishFlows(w http.ResponseWriter, r *http.Request) {
	// Notify the bot to reload flow data.
	if h.botWebhook == "" {
		jsonResponse(w, http.StatusOK, map[string]any{
			"status":  "published",
			"message": "no webhook configured",
		})
		return
	}

	resp, err := http.Post(h.botWebhook, "application/json", nil) //nolint:gosec
	if err != nil {
		httpError(w, http.StatusBadGateway, "failed to notify bot: %s", err)
		return
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		httpError(w, http.StatusBadGateway, "bot responded with status %d", resp.StatusCode)
		return
	}

	jsonResponse(w, http.StatusOK, map[string]any{
		"status": "published",
	})
}

// exportFlows bundles every file in the flows directory into a zip and serves it as a download.
func (h *Handler) exportFlows(w http.ResponseWriter, r *http.Request) {
	var buf bytes.Buffer
	if err := h.store.WriteZip(&buf); err != nil {
		httpError(w, http.StatusInternalServerError, "failed to build archive: %s", err)
		return
	}

	filename := fmt.Sprintf("flows-%s.zip", time.Now().UTC().Format("20060102-150405"))

	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
	w.Header().Set("Content-Length", strconv.Itoa(buf.Len()))
	w.WriteHeader(http.StatusOK)
	w.Write(buf.Bytes()) //nolint:errcheck
}

func (h *Handler) reorderButtons(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	stageID := r.PathValue("stageId")
	if id == "" || stageID == "" {
		httpError(w, http.StatusBadRequest, "missing flow or stage id")
		return
	}

	var body struct {
		From int `json:"from"`
		To   int `json:"to"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		httpError(w, http.StatusBadRequest, "invalid json: %s", err)
		return
	}

	// Apply to both language files; the default (ru) result is returned.
	var result *model.Stage
	for _, lang := range []string{"", "en"} {
		flow, err := h.store.GetLang(id, lang)
		if err != nil {
			if strings.Contains(err.Error(), "not found") {
				continue
			}
			httpError(w, http.StatusInternalServerError, "failed to get flow: %s", err)
			return
		}

		stageIdx := -1
		for i, s := range flow.Stages {
			if s.ID == stageID {
				stageIdx = i
				break
			}
		}
		if stageIdx == -1 {
			httpError(w, http.StatusNotFound, "stage %q not found in flow %q", stageID, id)
			return
		}

		btns := flow.Stages[stageIdx].Buttons
		if body.From < 0 || body.From >= len(btns) || body.To < 0 || body.To >= len(btns) {
			httpError(w, http.StatusBadRequest, "button index out of range")
			return
		}

		moved := btns[body.From]
		rest := append(btns[:body.From:body.From], btns[body.From+1:]...)
		reordered := make([]model.Button, 0, len(btns))
		reordered = append(reordered, rest[:body.To]...)
		reordered = append(reordered, moved)
		reordered = append(reordered, rest[body.To:]...)
		flow.Stages[stageIdx].Buttons = reordered

		if err := h.store.SaveLang(flow, lang); err != nil {
			httpError(w, http.StatusInternalServerError, "failed to save flow: %s", err)
			return
		}

		if lang == "" {
			s := flow.Stages[stageIdx]
			result = &s
		}
	}

	if result == nil {
		httpError(w, http.StatusNotFound, "flow %q not found", id)
		return
	}

	jsonResponse(w, http.StatusOK, result)
}

func jsonResponse(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(data); err != nil {
		log.Printf("json encode error: %s", err)
	}
}

func httpError(w http.ResponseWriter, status int, format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	log.Printf("[error] %s", msg)
	jsonResponse(w, status, map[string]string{"error": msg})
}

func (h *Handler) getDecisions(w http.ResponseWriter, r *http.Request) {
	lang := parseLang(r)

	cfg, err := h.store.GetDecisionsLang(lang)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			httpError(w, http.StatusNotFound, "decisions not found")
			return
		}
		httpError(w, http.StatusInternalServerError, "failed to get decisions: %s", err)
		return
	}

	jsonResponse(w, http.StatusOK, cfg)
}

func (h *Handler) updateDecisions(w http.ResponseWriter, r *http.Request) {
	lang := parseLang(r)

	var update model.DecisionUpdate
	if err := json.NewDecoder(r.Body).Decode(&update); err != nil {
		httpError(w, http.StatusBadRequest, "invalid json: %s", err)
		return
	}

	if len(update.Decisions) == 0 {
		httpError(w, http.StatusBadRequest, "decisions list must not be empty")
		return
	}

	// Validate unique codes
	seen := make(map[int]bool)
	for _, d := range update.Decisions {
		if d.Key == "" {
			httpError(w, http.StatusBadRequest, "every decision must have a key")
			return
		}
		if seen[d.Code] {
			httpError(w, http.StatusBadRequest, "duplicate decision code: %d", d.Code)
			return
		}
		seen[d.Code] = true
	}

	cfg := &model.DecisionConfig{
		Decisions:           update.Decisions,
		SupportLinkTemplate: update.SupportLinkTemplate,
	}

	// Keep existing support_link_template if not provided.
	if cfg.SupportLinkTemplate == "" {
		if existing, err := h.store.GetDecisionsLang(lang); err == nil {
			cfg.SupportLinkTemplate = existing.SupportLinkTemplate
		}
	}

	if err := h.store.SaveDecisionsLang(cfg, lang); err != nil {
		httpError(w, http.StatusInternalServerError, "failed to save decisions: %s", err)
		return
	}

	jsonResponse(w, http.StatusOK, cfg)
}

func (h *Handler) login(w http.ResponseWriter, r *http.Request) {
	username, password, ok := r.BasicAuth()
	if !ok {
		w.Header().Set("WWW-Authenticate", `Basic realm="admin"`)
		httpError(w, http.StatusUnauthorized, "missing credentials")
		return
	}

	if !h.auth.Validate(username, password) {
		httpError(w, http.StatusUnauthorized, "invalid credentials")
		return
	}

	token, err := h.auth.IssueToken(username)
	if err != nil {
		httpError(w, http.StatusInternalServerError, "failed to issue token: %s", err)
		return
	}

	jsonResponse(w, http.StatusOK, map[string]string{
		"token":    token,
		"username": username,
	})
}

func (h *Handler) getMinistryMessages(w http.ResponseWriter, r *http.Request) {
	lang := parseLang(r)

	cfg, err := h.store.GetMinistryMessages(lang)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			httpError(w, http.StatusNotFound, "ministry messages not found")
			return
		}
		httpError(w, http.StatusInternalServerError, "failed to get ministry messages: %s", err)
		return
	}

	jsonResponse(w, http.StatusOK, cfg)
}

func (h *Handler) updateMinistryMessages(w http.ResponseWriter, r *http.Request) {
	lang := parseLang(r)

	var update model.MinistryMessagesUpdate
	if err := json.NewDecoder(r.Body).Decode(&update); err != nil {
		httpError(w, http.StatusBadRequest, "invalid json: %s", err)
		return
	}

	if len(update.Messages) == 0 && len(update.Downloads) == 0 {
		httpError(w, http.StatusBadRequest, "messages or downloads must not be empty")
		return
	}

	// Merge with existing data so a partial update doesn't erase the other field.
	cfg := &model.MinistryMessages{
		Messages:  update.Messages,
		Downloads: update.Downloads,
	}

	if existing, err := h.store.GetMinistryMessages(lang); err == nil {
		if cfg.Messages == nil {
			cfg.Messages = existing.Messages
		}
		if cfg.Downloads == nil {
			cfg.Downloads = existing.Downloads
		}
	}

	if err := h.store.SaveMinistryMessages(cfg, lang); err != nil {
		httpError(w, http.StatusInternalServerError, "failed to save ministry messages: %s", err)
		return
	}

	jsonResponse(w, http.StatusOK, cfg)
}
