package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"

	"github.com/JeremiahM37/librarr/internal/netutil"
	"github.com/JeremiahM37/librarr/internal/webhook"
)

// webhookTypes is the set of channel types the UI and API accept.
var webhookTypes = map[string]bool{
	"discord": true, "generic": true, "ntfy": true, "pushover": true,
}

// validateWebhookConfig fills in defaults and checks the fields each
// channel type actually needs. Pushover has no URL at all (a fixed API
// endpoint, see webhook.pushoverAPI) - everything else is reached by URL,
// so the requirement flips depending on cfg.Type rather than always
// demanding a URL.
func validateWebhookConfig(cfg *webhook.Config) error {
	if cfg.Type == "" {
		cfg.Type = "generic"
	}
	if !webhookTypes[cfg.Type] {
		return fmt.Errorf("unknown webhook type %q", cfg.Type)
	}
	if cfg.Events == "" {
		cfg.Events = "*"
	}
	if cfg.Name == "" {
		cfg.Name = cfg.Type + " webhook"
	}

	if cfg.Type == "pushover" {
		if cfg.Token == "" || cfg.UserKey == "" {
			return fmt.Errorf("Pushover requires both an application token and a user key")
		}
		return nil
	}

	if cfg.URL == "" {
		return fmt.Errorf("URL is required")
	}
	if err := netutil.ValidateOutboundURL(cfg.URL); err != nil {
		return err
	}
	return nil
}

// handleGetWebhooks returns all configured webhooks.
func (s *Server) handleGetWebhooks(w http.ResponseWriter, r *http.Request) {
	configs, err := s.db.GetWebhookConfigs()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{
			"success": false, "error": "Failed to load webhooks",
		})
		return
	}
	if configs == nil {
		configs = []webhook.Config{}
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success":  true,
		"webhooks": configs,
	})
}

// handleCreateWebhook adds a new webhook configuration.
func (s *Server) handleCreateWebhook(w http.ResponseWriter, r *http.Request) {
	var cfg webhook.Config
	if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{
			"success": false, "error": "Invalid JSON",
		})
		return
	}

	if err := validateWebhookConfig(&cfg); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{
			"success": false, "error": err.Error(),
		})
		return
	}

	id, err := s.db.CreateWebhookConfig(&cfg)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{
			"success": false, "error": "Failed to create webhook",
		})
		return
	}

	cfg.ID = id

	// Refresh sender configs.
	s.refreshWebhookSender()

	writeJSON(w, http.StatusCreated, map[string]interface{}{
		"success": true,
		"webhook": cfg,
	})
}

// handleUpdateWebhook edits an existing webhook configuration (including
// just toggling Enabled, the most common edit).
func (s *Server) handleUpdateWebhook(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{
			"success": false, "error": "Invalid webhook ID",
		})
		return
	}

	var cfg webhook.Config
	if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{
			"success": false, "error": "Invalid JSON",
		})
		return
	}
	cfg.ID = id

	if err := validateWebhookConfig(&cfg); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{
			"success": false, "error": err.Error(),
		})
		return
	}

	if err := s.db.UpdateWebhookConfig(&cfg); err != nil {
		writeJSON(w, http.StatusNotFound, map[string]interface{}{
			"success": false, "error": "Webhook not found",
		})
		return
	}

	s.refreshWebhookSender()

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"webhook": cfg,
	})
}

// handleDeleteWebhook removes a webhook config by ID.
func (s *Server) handleDeleteWebhook(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{
			"success": false, "error": "Invalid webhook ID",
		})
		return
	}

	if err := s.db.DeleteWebhookConfig(id); err != nil {
		writeJSON(w, http.StatusNotFound, map[string]interface{}{
			"success": false, "error": "Webhook not found",
		})
		return
	}

	s.refreshWebhookSender()

	writeJSON(w, http.StatusOK, map[string]interface{}{"success": true})
}

// handleTestWebhook sends a test notification using the same fields a real
// config would have (URL and/or Token/UserKey depending on Type), so a test
// exercises exactly what saving would.
func (s *Server) handleTestWebhook(w http.ResponseWriter, r *http.Request) {
	var cfg webhook.Config
	if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{
			"success": false, "error": "Invalid JSON",
		})
		return
	}

	if err := validateWebhookConfig(&cfg); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{
			"success": false, "error": err.Error(),
		})
		return
	}

	if err := s.webhookSender.Test(cfg); err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]interface{}{
			"success": false, "error": "Webhook delivery failed",
		})
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"success": true, "message": "Test sent"})
}

// refreshWebhookSender reloads webhook configs from DB into the sender.
func (s *Server) refreshWebhookSender() {
	if s.webhookSender == nil {
		return
	}
	configs, err := s.db.GetWebhookConfigs()
	if err != nil {
		return
	}
	s.webhookSender.SetConfigs(configs)
}
