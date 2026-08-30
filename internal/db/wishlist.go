package db

import (
	"fmt"
	"time"

	"github.com/JeremiahM37/librarr/internal/models"
)

// --- Wishlist ---

// AddWishlistItem adds an item to the wishlist.
func (d *DB) AddWishlistItem(title, author, mediaType string) (int64, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	if mediaType == "" {
		mediaType = "ebook"
	}

	result, err := d.db.Exec(
		`INSERT INTO wishlist (title, author, media_type) VALUES (?, ?, ?)`,
		title, author, mediaType,
	)
	if err != nil {
		return 0, err
	}
	return result.LastInsertId()
}

// GetWishlist returns all wishlist items.
func (d *DB) GetWishlist() ([]models.WishlistItem, error) {
	rows, err := d.db.Query("SELECT id, title, author, media_type, added_at FROM wishlist ORDER BY added_at DESC")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []models.WishlistItem
	for rows.Next() {
		var item models.WishlistItem
		var ts float64
		if err := rows.Scan(&item.ID, &item.Title, &item.Author, &item.MediaType, &ts); err != nil {
			continue
		}
		item.AddedAt = time.Unix(int64(ts), 0)
		items = append(items, item)
	}
	return items, nil
}

// HasWishlistItemByTitle reports whether a wishlist entry already exists for
// a title (case-insensitive) - used to badge "watching" in the Discover UI,
// mirroring FindActiveRequestByTitle's role for the "requested" badge.
func (d *DB) HasWishlistItemByTitle(title string) (bool, error) {
	var n int
	err := d.db.QueryRow("SELECT COUNT(1) FROM wishlist WHERE LOWER(title) = LOWER(?)", title).Scan(&n)
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// DeleteWishlistItem removes a wishlist item by ID.
func (d *DB) DeleteWishlistItem(id int64) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	result, err := d.db.Exec("DELETE FROM wishlist WHERE id = ?", id)
	if err != nil {
		return err
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return fmt.Errorf("wishlist item not found")
	}
	return nil
}
