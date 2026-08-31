package metadata

import (
	"context"
	"log/slog"
	"strings"
	"sync"

	"github.com/JeremiahM37/librarr/internal/models"
)

// DiscoverService merges Google Books and Open Library search results for
// the browse/discover UI. Google Books results come first (better cover art
// and descriptions in practice); Open Library results fill in titles Google
// Books didn't return, deduplicated by normalized title so the same book
// doesn't appear twice just because both sources have it. Google Books
// requires no API key to function - if it's unreachable or rate-limited,
// results silently degrade to Open-Library-only rather than failing the
// whole request.
type DiscoverService struct {
	googleBooks *GoogleBooksClient
	openLibrary *Client
}

// NewDiscoverService creates a service wrapping both catalog clients.
func NewDiscoverService(googleBooks *GoogleBooksClient, openLibrary *Client) *DiscoverService {
	return &DiscoverService{googleBooks: googleBooks, openLibrary: openLibrary}
}

// Search queries both sources concurrently and returns a merged, deduped list.
func (s *DiscoverService) Search(ctx context.Context, query string, limit int) []models.DiscoverResult {
	var googleResults, olResults []models.DiscoverResult
	var wg sync.WaitGroup

	wg.Add(2)
	go func() {
		defer wg.Done()
		r, err := s.googleBooks.Search(ctx, query, limit)
		if err != nil {
			slog.Warn("discover: google books search failed", "error", err)
			return
		}
		googleResults = r
	}()
	go func() {
		defer wg.Done()
		r, err := s.openLibrary.SearchMulti(ctx, query, limit)
		if err != nil {
			slog.Warn("discover: open library search failed", "error", err)
			return
		}
		olResults = r
	}()
	wg.Wait()

	seen := make(map[string]bool, len(googleResults))
	merged := make([]models.DiscoverResult, 0, len(googleResults)+len(olResults))
	for _, r := range googleResults {
		if r.Title == "" {
			continue
		}
		seen[normalizeTitle(r.Title)] = true
		merged = append(merged, r)
	}
	for _, r := range olResults {
		key := normalizeTitle(r.Title)
		if key == "" || seen[key] {
			continue
		}
		seen[key] = true
		merged = append(merged, r)
	}

	if len(merged) > limit {
		merged = merged[:limit]
	}
	return merged
}

// BrowseGenre returns a ranked list of works for a subject/genre - what
// backs the Discover UI's browse rows when there's no search query yet.
// Open-Library-only (see BrowseSubject) rather than merged with Google
// Books, so the ranking stays meaningful instead of interleaving two
// differently-ordered lists.
func (s *DiscoverService) BrowseGenre(ctx context.Context, subject string, limit int) ([]models.DiscoverResult, error) {
	return s.openLibrary.BrowseSubject(ctx, subject, limit)
}

// GetDetail fetches full detail for one result by source+id, plus a short
// "more by this author" recommendation list. author is a hint from the
// caller (the summary result the frontend already had before opening the
// detail modal) - Open Library's work-detail endpoint doesn't resolve the
// author's name without a further lookup, so GetWork's own result usually
// has an empty Author; falling back to result.Author only helps the Google
// Books side, which does resolve it.
func (s *DiscoverService) GetDetail(ctx context.Context, source, id, author string) (*models.DiscoverResult, error) {
	var result *models.DiscoverResult
	var err error
	if source == "open_library" {
		result, err = s.openLibrary.GetWork(ctx, id)
	} else {
		result, err = s.googleBooks.GetVolume(ctx, id)
	}
	if err != nil || result == nil {
		return result, err
	}

	authorForRecs := strings.TrimSpace(author)
	if authorForRecs == "" {
		authorForRecs = result.Author
	}
	if authorForRecs != "" {
		result.Recommended = s.recommendedByAuthor(ctx, authorForRecs, result.Title)
	}
	return result, nil
}

// recommendedByAuthor returns a short "more by this author" list for the
// Discover detail modal, excluding the book currently being viewed. Reuses
// the same merged Google Books + Open Library search as the main Discover
// search box, rather than a separate recommendation source - neither
// catalog has a keyless "similar books" endpoint, but "more by this
// author" is data both already have and is a reasonable stand-in.
func (s *DiscoverService) recommendedByAuthor(ctx context.Context, author, excludeTitle string) []models.DiscoverResult {
	const maxRecommended = 6
	// Pad the request since the book being viewed is filtered back out.
	candidates := s.Search(ctx, author, maxRecommended+4)
	excludeKey := normalizeTitle(excludeTitle)

	recs := make([]models.DiscoverResult, 0, maxRecommended)
	for _, c := range candidates {
		if normalizeTitle(c.Title) == excludeKey {
			continue
		}
		recs = append(recs, c)
		if len(recs) >= maxRecommended {
			break
		}
	}
	return recs
}

func normalizeTitle(title string) string {
	return strings.ToLower(strings.TrimSpace(title))
}
