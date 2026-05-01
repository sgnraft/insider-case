package handlers

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/sgnraft/insider-case/internal/domain"
	"github.com/sgnraft/insider-case/internal/repository"
)

// TemplateHandler handles template CRUD.
type TemplateHandler struct {
	repo   *repository.Repository
	logger *slog.Logger
}

// NewTemplateHandler creates a new TemplateHandler.
func NewTemplateHandler(repo *repository.Repository, logger *slog.Logger) *TemplateHandler {
	return &TemplateHandler{repo: repo, logger: logger}
}

// Create handles POST /api/v1/templates.
func (h *TemplateHandler) Create(w http.ResponseWriter, r *http.Request) {
	var t domain.Template
	if err := json.NewDecoder(r.Body).Decode(&t); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if t.Name == "" || t.Channel == "" || t.Content == "" {
		writeError(w, http.StatusBadRequest, "name, channel, and content are required")
		return
	}
	if err := validateChannel(t.Channel); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := h.repo.CreateTemplate(r.Context(), &t); err != nil {
		h.logger.Error("create template failed", "err", err)
		writeError(w, http.StatusInternalServerError, "failed to create template")
		return
	}
	writeJSON(w, http.StatusCreated, t)
}

// GetByID handles GET /api/v1/templates/{id}.
func (h *TemplateHandler) GetByID(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	t, err := h.repo.GetTemplate(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "template not found")
		return
	}
	writeJSON(w, http.StatusOK, t)
}
