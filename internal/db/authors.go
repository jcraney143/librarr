package db

import (
	"database/sql"
	"fmt"
	"time"
)

// --- Monitored Authors ---

// MonitoredAuthor represents a monitored author record.
type MonitoredAuthor struct {
	ID                int64     `json:"id"`
	Name              string    `json:"name"`
	LastChecked       time.Time `json:"last_checked"`
	LastBookFound     string    `json:"last_book_found"`
	CheckIntervalDays int       `json:"check_interval_days"`
}

// AddMonitoredAuthor adds an author to watch for new releases. Idempotent
// on name (case-insensitive): calling it again for an author already being
// monitored returns the existing row instead of creating a duplicate that
// the scheduler would then poll and notify on twice. This matters more now
// that the Discover UI's "Watch this author" button can call it repeatedly
// for the same author across separate book cards, not just the occasional
// manual add it was originally written for.
func (d *DB) AddMonitoredAuthor(name string, intervalDays int) (int64, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	var existingID int64
	err := d.db.QueryRow("SELECT id FROM monitored_authors WHERE LOWER(name) = LOWER(?)", name).Scan(&existingID)
	if err == nil {
		return existingID, nil
	}
	if err != sql.ErrNoRows {
		return 0, err
	}

	result, err := d.db.Exec(
		"INSERT INTO monitored_authors (name, check_interval_days) VALUES (?, ?)",
		name, intervalDays,
	)
	if err != nil {
		return 0, err
	}
	return result.LastInsertId()
}

func (d *DB) GetMonitoredAuthors() ([]MonitoredAuthor, error) {
	rows, err := d.db.Query("SELECT id, name, last_checked, last_book_found, check_interval_days FROM monitored_authors ORDER BY name")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var authors []MonitoredAuthor
	for rows.Next() {
		var a MonitoredAuthor
		var lastChecked float64
		if err := rows.Scan(&a.ID, &a.Name, &lastChecked, &a.LastBookFound, &a.CheckIntervalDays); err != nil {
			continue
		}
		if lastChecked > 0 {
			a.LastChecked = time.Unix(int64(lastChecked), 0)
		}
		authors = append(authors, a)
	}
	return authors, nil
}

func (d *DB) DeleteMonitoredAuthor(id int64) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	result, err := d.db.Exec("DELETE FROM monitored_authors WHERE id = ?", id)
	if err != nil {
		return err
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return fmt.Errorf("monitored author not found")
	}
	return nil
}

func (d *DB) UpdateMonitoredAuthorCheck(id int64, lastBookFound string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	_, err := d.db.Exec(
		"UPDATE monitored_authors SET last_checked = ?, last_book_found = ? WHERE id = ?",
		float64(time.Now().Unix()), lastBookFound, id,
	)
	return err
}

// GetDBPath returns the file path to the database.
