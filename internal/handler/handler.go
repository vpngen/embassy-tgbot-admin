package handler

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"

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

	// Protected API routes
	protected := http.NewServeMux()
	protected.HandleFunc("GET /api/flows", h.listFlows)
	protected.HandleFunc("GET /api/flows/{id}", h.getFlow)
	protected.HandleFunc("PUT /api/flows/{id}", h.updateFlow)
	protected.HandleFunc("DELETE /api/flows/{id}", h.deleteFlow)
	protected.HandleFunc("POST /api/flows/publish", h.publishFlows)

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
		for _, btn := range s.Buttons {
			if btn.Action == "goto" && !stageIDs[btn.Target] {
				httpError(w, http.StatusBadRequest, "stage %q button %q references unknown target: %q", s.ID, btn.Label, btn.Target)
				return
			}
		}
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
	if h.botWebhook == "" {
		jsonResponse(w, http.StatusOK, map[string]string{
			"status":  "saved",
			"message": "no bot webhook configured — bot must be restarted to pick up changes",
		})
		return
	}

	resp, err := http.Post(h.botWebhook, "application/json", nil) //nolint:gosec // URL comes from server config, not user input
	if err != nil {
		httpError(w, http.StatusBadGateway, "failed to notify bot: %s", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		httpError(w, http.StatusBadGateway, "bot responded with status %d", resp.StatusCode)
		return
	}

	jsonResponse(w, http.StatusOK, map[string]string{"status": "published"})
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

	// Keep existing support_link_template if not provided
	if cfg.SupportLinkTemplate == "" {
		existing, err := h.store.GetDecisionsLang(lang)
		if err == nil {
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

	if len(update.Messages) == 0 {
		httpError(w, http.StatusBadRequest, "messages must not be empty")
		return
	}

	cfg := &model.MinistryMessages{
		Messages: update.Messages,
	}

	if err := h.store.SaveMinistryMessages(cfg, lang); err != nil {
		httpError(w, http.StatusInternalServerError, "failed to save ministry messages: %s", err)
		return
	}

	jsonResponse(w, http.StatusOK, cfg)
}
