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
	// NextCheck is derived (LastChecked + CheckIntervalDays), not stored.
	// Zero when the author has never been checked yet - the scheduler picks
	// those up on its next pass regardless of interval.
	NextCheck time.Time `json:"next_check,omitzero"`
}

// AuthorRelease is one entry in a monitored author's release history - a
// new book the author monitor found, recorded in addition to the
// notification/webhook fired at the time so the Calendar tab has more than
// just "most recent find per author" to show.
type AuthorRelease struct {
	ID         int64     `json:"id"`
	AuthorID   int64     `json:"author_id"`
	AuthorName string    `json:"author_name"`
	Title      string    `json:"title"`
	Year       int       `json:"year"`
	FoundAt    time.Time `json:"found_at"`
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
			a.NextCheck = a.LastChecked.Add(time.Duration(a.CheckIntervalDays) * 24 * time.Hour)
		}
		authors = append(authors, a)
	}
	return authors, nil
}

// GetMonitoredAuthorByID looks up a single monitored author, used by the
// "Check now" action to re-check one author on demand.
func (d *DB) GetMonitoredAuthorByID(id int64) (*MonitoredAuthor, error) {
	var a MonitoredAuthor
	var lastChecked float64
	err := d.db.QueryRow(
		"SELECT id, name, last_checked, last_book_found, check_interval_days FROM monitored_authors WHERE id = ?", id,
	).Scan(&a.ID, &a.Name, &lastChecked, &a.LastBookFound, &a.CheckIntervalDays)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("monitored author not found")
	}
	if err != nil {
		return nil, err
	}
	if lastChecked > 0 {
		a.LastChecked = time.Unix(int64(lastChecked), 0)
		a.NextCheck = a.LastChecked.Add(time.Duration(a.CheckIntervalDays) * 24 * time.Hour)
	}
	return &a, nil
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

// --- Author Releases (Calendar feed) ---

// RecordAuthorRelease appends one entry to a monitored author's release
// history. Called alongside the existing notification/webhook when the
// author monitor finds a new book, so the Calendar tab has a timeline to
// show rather than just "most recent find" per author.
func (d *DB) RecordAuthorRelease(authorID int64, authorName, title string, year int) (int64, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	result, err := d.db.Exec(
		"INSERT INTO author_releases (author_id, author_name, title, year, found_at) VALUES (?, ?, ?, ?, ?)",
		authorID, authorName, title, year, float64(time.Now().Unix()),
	)
	if err != nil {
		return 0, err
	}
	return result.LastInsertId()
}

// GetAuthorReleases returns the most recent author releases across all
// monitored authors, newest first, for the Calendar tab.
func (d *DB) GetAuthorReleases(limit int) ([]AuthorRelease, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := d.db.Query(
		"SELECT id, author_id, author_name, title, year, found_at FROM author_releases ORDER BY found_at DESC LIMIT ?",
		limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var releases []AuthorRelease
	for rows.Next() {
		var r AuthorRelease
		var foundAt float64
		if err := rows.Scan(&r.ID, &r.AuthorID, &r.AuthorName, &r.Title, &r.Year, &foundAt); err != nil {
			continue
		}
		r.FoundAt = time.Unix(int64(foundAt), 0)
		releases = append(releases, r)
	}
	return releases, nil
}

// GetDBPath returns the file path to the database.
