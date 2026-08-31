package db

import (
	"strconv"
	"strings"

	"github.com/JeremiahM37/librarr/internal/models"
)

// DuplicateGroup is a set of library items that are probably duplicates of
// each other, along with why they were grouped. Mirrors the manual sqlite3
// queries in docs/library-duplicate-report.md - this is those same signals
// exposed as a real endpoint instead of something only reachable with
// direct DB access.
type DuplicateGroup struct {
	Reason string               `json:"reason"` // same_path, same_content, same_title_author
	Items  []models.LibraryItem `json:"items"`
}

// GetDuplicateGroups finds library items that are likely duplicates.
// same_path and same_content are treated as reliable (an identical
// destination path, or an identical content hash within the same media
// type and format); same_title_author is listed separately and last since
// a format difference (EPUB vs MOBI) there is often a legitimate second
// edition, not a duplicate - the caller decides what to do with it, this
// just surfaces the groups.
func (d *DB) GetDuplicateGroups() ([]DuplicateGroup, error) {
	var groups []DuplicateGroup
	seen := make(map[string]bool)

	queries := []struct {
		reason string
		sql    string
	}{
		{
			reason: "same_path",
			sql: `SELECT group_concat(id) FROM library_items
			      GROUP BY lower(replace(replace(trim(file_path), char(92), '/'), '//', '/'))
			      HAVING COUNT(*) > 1 AND trim(file_path) <> ''`,
		},
		{
			reason: "same_content",
			sql: `SELECT group_concat(id) FROM library_items
			      WHERE content_hash <> ''
			      GROUP BY content_hash, media_type, file_format
			      HAVING COUNT(*) > 1`,
		},
		{
			reason: "same_title_author",
			sql: `SELECT group_concat(id) FROM library_items
			      GROUP BY lower(trim(title)), lower(trim(author)), media_type, lower(trim(file_format))
			      HAVING COUNT(*) > 1 AND trim(title) <> ''`,
		},
	}

	for _, q := range queries {
		idGroups, err := d.duplicateIDGroups(q.sql)
		if err != nil {
			return nil, err
		}
		for _, ids := range idGroups {
			key := idGroupKey(ids)
			if seen[key] {
				continue // identical item set already reported by a higher-confidence query
			}
			seen[key] = true

			items, err := d.itemsByIDs(ids)
			if err != nil {
				return nil, err
			}
			if len(items) < 2 {
				continue // a member was deleted between the GROUP BY and this lookup
			}
			groups = append(groups, DuplicateGroup{Reason: q.reason, Items: items})
		}
	}

	return groups, nil
}

// duplicateIDGroups runs a "SELECT group_concat(id) ... GROUP BY ... HAVING
// COUNT(*) > 1" query and parses each row's comma-separated id list.
func (d *DB) duplicateIDGroups(query string) ([][]int64, error) {
	rows, err := d.db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var groups [][]int64
	for rows.Next() {
		var idList string
		if err := rows.Scan(&idList); err != nil {
			continue
		}
		var ids []int64
		for _, s := range strings.Split(idList, ",") {
			if id, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64); err == nil {
				ids = append(ids, id)
			}
		}
		if len(ids) > 1 {
			groups = append(groups, ids)
		}
	}
	return groups, nil
}

// itemsByIDs fetches full library item rows for a set of IDs, in the order
// they were found by the grouping query isn't preserved (added_at DESC
// instead) since these are shown together as a group either way.
func (d *DB) itemsByIDs(ids []int64) ([]models.LibraryItem, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	placeholders := make([]string, len(ids))
	args := make([]interface{}, len(ids))
	for i, id := range ids {
		placeholders[i] = "?"
		args[i] = id
	}
	query := "SELECT " + libraryItemColumns + " FROM library_items WHERE id IN (" +
		strings.Join(placeholders, ",") + ") ORDER BY added_at DESC"

	rows, err := d.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanLibraryItems(rows)
}

// idGroupKey turns a set of ids into a stable dedup key, independent of order.
func idGroupKey(ids []int64) string {
	sorted := make([]int64, len(ids))
	copy(sorted, ids)
	for i := 1; i < len(sorted); i++ {
		for j := i; j > 0 && sorted[j-1] > sorted[j]; j-- {
			sorted[j-1], sorted[j] = sorted[j], sorted[j-1]
		}
	}
	parts := make([]string, len(sorted))
	for i, id := range sorted {
		parts[i] = strconv.FormatInt(id, 10)
	}
	return strings.Join(parts, ",")
}
