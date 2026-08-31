package db

import (
	"fmt"

	"github.com/JeremiahM37/librarr/internal/webhook"
)

// --- Webhook Configs ---

// GetWebhookConfigs returns all webhook configurations.
func (d *DB) GetWebhookConfigs() ([]webhook.Config, error) {
	rows, err := d.db.Query("SELECT id, name, url, type, enabled, events, token, user_key FROM webhook_configs ORDER BY id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var configs []webhook.Config
	for rows.Next() {
		var c webhook.Config
		var enabled int
		if err := rows.Scan(&c.ID, &c.Name, &c.URL, &c.Type, &enabled, &c.Events, &c.Token, &c.UserKey); err != nil {
			continue
		}
		c.Enabled = enabled == 1
		configs = append(configs, c)
	}
	return configs, nil
}

// CreateWebhookConfig inserts a new webhook config.
func (d *DB) CreateWebhookConfig(c *webhook.Config) (int64, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	enabledInt := 0
	if c.Enabled {
		enabledInt = 1
	}

	result, err := d.db.Exec(
		`INSERT INTO webhook_configs (name, url, type, enabled, events, token, user_key) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		c.Name, c.URL, c.Type, enabledInt, c.Events, c.Token, c.UserKey,
	)
	if err != nil {
		return 0, err
	}
	return result.LastInsertId()
}

// UpdateWebhookConfig updates an existing webhook config in place (id must
// already be set on c).
func (d *DB) UpdateWebhookConfig(c *webhook.Config) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	enabledInt := 0
	if c.Enabled {
		enabledInt = 1
	}

	result, err := d.db.Exec(
		`UPDATE webhook_configs SET name = ?, url = ?, type = ?, enabled = ?, events = ?, token = ?, user_key = ? WHERE id = ?`,
		c.Name, c.URL, c.Type, enabledInt, c.Events, c.Token, c.UserKey, c.ID,
	)
	if err != nil {
		return err
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return fmt.Errorf("webhook not found")
	}
	return nil
}

// DeleteWebhookConfig removes a webhook config by ID.
func (d *DB) DeleteWebhookConfig(id int64) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	result, err := d.db.Exec("DELETE FROM webhook_configs WHERE id = ?", id)
	if err != nil {
		return err
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return fmt.Errorf("webhook not found")
	}
	return nil
}
