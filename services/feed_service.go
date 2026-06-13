package services

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	neturl "net/url"
	"strings"
	"sync"
	"time"

	"kickrss/config"
	"kickrss/crud"
	"kickrss/db"
	"kickrss/models"

	"github.com/go-shiori/go-readability"
	"github.com/mmcdole/gofeed"
)

// --- Web Scraper (Readability) ---

func FetchAndExtractFulltext(url string) (string, string, string) {
	minChars := config.GlobalConfig.Fulltext.MinTextChars
	if minChars == 0 {
		minChars = 200
	}

	log.Printf("[Extractor] Extracting fulltext via readability for URL: %s", url)

	// Try 1: Direct Fetch and Go-Readability extraction
	content, err := extractDirect(url)
	if err == nil && len(content) >= minChars {
		log.Printf("[Extractor] Successfully extracted fulltext (%d chars) via direct fetch", len(content))
		return content, "ok", "trafilatura" // Keep string compatible with python schema
	}
	if err != nil {
		log.Printf("[Extractor] Direct fetch failed for %s: %s", url, err)
	}

	// Try 2: Fallback to JS Rendering Service if configured
	renderingURL := config.GlobalConfig.Fulltext.RenderingServiceURL
	if renderingURL != "" {
		log.Printf("[Extractor] Falling back to rendering service for URL: %s -> %s", url, renderingURL)
		content, err = extractWithRenderingService(url, renderingURL)
		if err == nil && len(content) >= minChars {
			log.Printf("[Extractor] Successfully extracted fulltext (%d chars) via rendering service", len(content))
			return content, "ok", "rendering_service"
		}
		if err != nil {
			log.Printf("[Extractor] Rendering service failed for %s: %s", url, err)
		}
	}

	return "", "fetch_failed", "trafilatura"
}

func extractDirect(url string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTP status %d", resp.StatusCode)
	}

	parsedURL, errURL := neturl.Parse(url)
	if errURL != nil {
		parsedURL, _ = neturl.Parse("http://localhost")
	}
	article, err := readability.FromReader(resp.Body, parsedURL)
	if err != nil {
		return "", err
	}

	return crud.CleanHTML(article.Content), nil
}

func extractWithRenderingService(url, serviceURL string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	payloadMap := map[string]string{"url": url}
	payloadBytes, _ := json.Marshal(payloadMap)

	req, err := http.NewRequestWithContext(ctx, "POST", serviceURL, bytes.NewBuffer(payloadBytes))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("Rendering service returned HTTP %d", resp.StatusCode)
	}

	parsedURL, errURL := neturl.Parse(url)
	if errURL != nil {
		parsedURL, _ = neturl.Parse("http://localhost")
	}
	article, err := readability.FromReader(resp.Body, parsedURL)
	if err != nil {
		return "", err
	}

	return crud.CleanHTML(article.Content), nil
}

// --- Feed Ingesting (GoFeed) ---

type FetchResult struct {
	FeedTitle    string
	SiteURL      string
	Etag         string
	LastModified string
	Entries      []models.Entry
	NotModified  bool
}

func FetchFeed(url string, etag, lastModified string) (*FetchResult, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; KickRSS/1.0;)")
	if etag != "" {
		req.Header.Set("If-None-Match", etag)
	}
	if lastModified != "" {
		req.Header.Set("If-Modified-Since", lastModified)
	}

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotModified {
		return &FetchResult{NotModified: true}, nil
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP status %d", resp.StatusCode)
	}

	newEtag := resp.Header.Get("ETag")
	newLastMod := resp.Header.Get("Last-Modified")

	fp := gofeed.NewParser()
	feed, err := fp.Parse(resp.Body)
	if err != nil {
		return nil, err
	}

	var entries []models.Entry
	for _, item := range feed.Items {
		guid := item.GUID
		if guid == "" {
			guid = item.Link
		}
		if guid == "" {
			guid = item.Title
		}

		pubDate := ""
		if item.PublishedParsed != nil {
			pubDate = item.PublishedParsed.UTC().Format(time.RFC3339)
		} else if item.UpdatedParsed != nil {
			pubDate = item.UpdatedParsed.UTC().Format(time.RFC3339)
		}

		content := item.Content
		if content == "" {
			content = item.Description
		}

		author := ""
		if item.Author != nil {
			author = item.Author.Name
		} else if len(item.Authors) > 0 {
			author = item.Authors[0].Name
		}

		entries = append(entries, models.Entry{
			Guid:        guid,
			Title:       item.Title,
			URL:         item.Link,
			Author:      author,
			PublishedAt: pubDate,
			RawContent:  content,
		})
	}

	siteURL := feed.Link
	if siteURL == "" {
		siteURL = feed.FeedLink
	}

	return &FetchResult{
		FeedTitle:    feed.Title,
		SiteURL:      siteURL,
		Etag:         newEtag,
		LastModified: newLastMod,
		Entries:      entries,
	}, nil
}

// --- OPML Import/Export ---

type OpmlOutline struct {
	Text    string        `xml:"text,attr"`
	Title   string        `xml:"title,attr"`
	Type    string        `xml:"type,attr"`
	XMLURL  string        `xml:"xmlUrl,attr"`
	HTMLURL string        `xml:"htmlUrl,attr"`
	Outline []OpmlOutline `xml:"outline"`
}

type OpmlXML struct {
	XMLName xml.Name      `xml:"opml"`
	Version string        `xml:"version,attr"`
	Title   string        `xml:"head>title"`
	Outline []OpmlOutline `xml:"body>outline"`
}

func ImportOPML(r io.Reader) ([]models.Feed, error) {
	var opmlData OpmlXML
	decoder := xml.NewDecoder(r)
	if err := decoder.Decode(&opmlData); err != nil {
		return nil, err
	}

	var flatOutlines []OpmlOutline
	var traverse func(outlines []OpmlOutline)
	traverse = func(outlines []OpmlOutline) {
		for _, o := range outlines {
			if o.XMLURL != "" {
				flatOutlines = append(flatOutlines, o)
			}
			if len(o.Outline) > 0 {
				traverse(o.Outline)
			}
		}
	}
	traverse(opmlData.Outline)

	var addedFeeds []models.Feed
	for _, o := range flatOutlines {
		url := strings.TrimSpace(o.XMLURL)
		if url == "" {
			continue
		}

		// Check if exists
		existing, err := crud.GetFeedByURL(url)
		if err == nil && existing != nil {
			addedFeeds = append(addedFeeds, *existing)
			continue
		}

		title := o.Title
		if title == "" {
			title = o.Text
		}
		if title == "" {
			title = url
		}

		id, err := crud.AddFeed(url, title)
		if err == nil {
			f, _ := crud.GetFeedByID(int(id))
			if f != nil {
				addedFeeds = append(addedFeeds, *f)
			}
		}
	}

	return addedFeeds, nil
}

func ExportOPML(feeds []models.Feed) (string, error) {
	var outlines []OpmlOutline
	for _, f := range feeds {
		outlines = append(outlines, OpmlOutline{
			Text:    f.Title,
			Title:   f.Title,
			Type:    "rss",
			XMLURL:  f.URL,
			HTMLURL: f.SiteURL,
		})
	}

	opmlData := OpmlXML{
		Version: "1.0",
		Title:   "KickRSS Subscriptions",
		Outline: outlines,
	}

	output, err := xml.MarshalIndent(opmlData, "", "  ")
	if err != nil {
		return "", err
	}

	xmlHeader := `<?xml version="1.0" encoding="UTF-8"?>` + "\n"
	return xmlHeader + string(output), nil
}

// --- Asynchronous Pipelines ---

func AsyncColdStartFeed(feedID int) {
	log.Printf("[FeedService] Starting async cold start for feed %d", feedID)
	feed, err := crud.GetFeedByID(feedID)
	if err != nil || feed == nil {
		log.Printf("[FeedService] Feed %d not found for cold start", feedID)
		return
	}

	result, err := FetchFeed(feed.URL, "", "")
	if err != nil {
		log.Printf("[FeedService] Cold start fetch failed for feed %d: %s", feedID, err)
		return
	}

	var titles []string
	for _, e := range result.Entries {
		if e.Title != "" {
			titles = append(titles, e.Title)
		}
	}

	log.Printf("[FeedService] Seeding categories for feed %d with %d articles", feedID, len(titles))
	seedCategories := []string{}
	if len(titles) > 0 && feed.NeedClassification == 1 {
		seedCategories, err = GenerateSeedCategories(titles)
		if err != nil {
			log.Printf("[FeedService] Seed category generation failed for feed %d: %s", feedID, err)
		}
	}

	tx, err := db.DB.Begin()
	if err != nil {
		log.Printf("[FeedService] Failed to begin tx for cold start feed %d: %s", feedID, err)
		return
	}
	defer tx.Rollback()

	if len(seedCategories) > 0 {
		now := time.Now().UTC().Format(time.RFC3339)
		for _, name := range seedCategories {
			if name == "" || name == "未归类" {
				continue
			}
			_, _ = tx.Exec("INSERT OR IGNORE INTO categories (feed_id, name, is_default, created_at) VALUES (?, ?, 0, ?)", feedID, name, now)
		}
	}

	// Update seeded flag
	seededVal := 0
	if len(seedCategories) > 0 || feed.NeedClassification == 0 {
		seededVal = 1
	}

	title := result.FeedTitle
	if title == "" {
		title = feed.Title
	}
	siteURL := result.SiteURL
	if siteURL == "" {
		siteURL = feed.SiteURL
	}

	_, err = tx.Exec("UPDATE feeds SET title = ?, site_url = ?, seeded = ? WHERE id = ?", title, siteURL, seededVal, feedID)
	if err != nil {
		log.Printf("[FeedService] Failed to update feed during cold start: %s", err)
		return
	}

	// Save entries
	var defaultCatID int
	err = tx.QueryRow("SELECT id FROM categories WHERE feed_id = ? AND is_default = 1", feedID).Scan(&defaultCatID)
	if err == sql.ErrNoRows {
		now := time.Now().UTC().Format(time.RFC3339)
		res, _ := tx.Exec("INSERT INTO categories (feed_id, name, is_default, created_at) VALUES (?, '未归类', 1, ?)", feedID, now)
		lastID, _ := res.LastInsertId()
		defaultCatID = int(lastID)
	}

	newCount := 0
	nowStr := time.Now().UTC().Format(time.RFC3339)

	stmtCheck, _ := tx.Prepare("SELECT id FROM entries WHERE feed_id = ? AND guid = ?")
	stmtInsert, _ := tx.Prepare(`
		INSERT INTO entries (feed_id, category_id, guid, title, url, author, published_at, fetched_at, raw_content, attention, likely_no_text, fulltext_ready)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, 'skim', ?, ?)
	`)

	for _, re := range result.Entries {
		var existingID int
		errCheck := stmtCheck.QueryRow(feedID, re.Guid).Scan(&existingID)
		if errCheck == sql.ErrNoRows {
			likelyNoText := 0
			if len(re.RawContent) < 200 {
				likelyNoText = 1
			}
			fulltextReady := 0
			if len(re.RawContent) > 800 {
				fulltextReady = 1
			}
			pubTime := re.PublishedAt
			if pubTime == "" {
				pubTime = nowStr
			}

			_, _ = stmtInsert.Exec(feedID, defaultCatID, re.Guid, re.Title, re.URL, re.Author, pubTime, nowStr, re.RawContent, likelyNoText, fulltextReady)
			newCount++
		}
	}
	stmtCheck.Close()
	stmtInsert.Close()

	// Update fetch status
	nowFetch := time.Now().UTC().Format(time.RFC3339)
	_, _ = tx.Exec("UPDATE feeds SET etag = ?, last_modified = ?, last_fetched_at = ? WHERE id = ?", result.Etag, result.LastModified, nowFetch, feedID)

	if err = tx.Commit(); err != nil {
		log.Printf("[FeedService] Cold start transaction commit failed: %s", err)
		return
	}

	log.Printf("[FeedService] Cold start completed. Saved %d entries.", newCount)

	if newCount > 0 {
		// Enqueue classification
		ClassifyFeedEntries(feedID)
	}
}

func SyncResetFeedCategoriesStep1(feedID int) (bool, error) {
	log.Printf("[FeedService] Starting reset categories step 1 for feed %d", feedID)
	feed, err := crud.GetFeedByID(feedID)
	if err != nil || feed == nil {
		return false, errors.New("feed not found")
	}

	defaultCatID, err := crud.GetDefaultCategory(feedID)
	if err != nil {
		return false, err
	}

	tx, err := db.DB.Begin()
	if err != nil {
		return false, err
	}
	defer tx.Rollback()

	// Delete custom categories
	_, err = tx.Exec("DELETE FROM categories WHERE feed_id = ? AND is_default = 0", feedID)
	if err != nil {
		return false, err
	}

	// Move entries back to default category and clear classified_at
	_, err = tx.Exec("UPDATE entries SET category_id = ?, classified_at = NULL WHERE feed_id = ?", defaultCatID, feedID)
	if err != nil {
		return false, err
	}

	// Retrieve titles to seed
	rows, err := tx.Query("SELECT title FROM entries WHERE feed_id = ? ORDER BY published_at DESC LIMIT 100", feedID)
	if err != nil {
		return false, err
	}
	defer rows.Close()

	var titles []string
	for rows.Next() {
		var title string
		if err := rows.Scan(&title); err == nil && title != "" {
			titles = append(titles, title)
		}
	}

	if err = tx.Commit(); err != nil {
		return false, err
	}

	seedCategories := []string{}
	if len(titles) > 0 && feed.NeedClassification == 1 {
		seedCategories, err = GenerateSeedCategories(titles)
		if err != nil {
			log.Printf("[FeedService] Reset categories seeding failed for feed %d: %s", feedID, err)
		}
	}

	tx2, err := db.DB.Begin()
	if err != nil {
		return false, err
	}
	defer tx2.Rollback()

	if len(seedCategories) > 0 {
		now := time.Now().UTC().Format(time.RFC3339)
		for _, name := range seedCategories {
			_, _ = tx2.Exec("INSERT OR IGNORE INTO categories (feed_id, name, is_default, created_at) VALUES (?, ?, 0, ?)", feedID, name, now)
		}
	}

	seededVal := 0
	if len(seedCategories) > 0 || feed.NeedClassification == 0 {
		seededVal = 1
	}

	_, err = tx2.Exec("UPDATE feeds SET seeded = ? WHERE id = ?", seededVal, feedID)
	if err != nil {
		return false, err
	}

	err = tx2.Commit()
	return seededVal == 1, err
}

func AsyncResetFeedCategories(feedID int) {
	log.Printf("[FeedService] Starting async reset categories for feed %d", feedID)
	_, err := SyncResetFeedCategoriesStep1(feedID)
	if err != nil {
		log.Printf("[FeedService] Reset categories step 1 failed: %s", err)
		return
	}
	ClassifyFeedEntries(feedID)
}

func ClassifyFeedEntries(feedID int) {
	log.Printf("[Classifier] Starting classification for feed %d", feedID)

	feed, err := crud.GetFeedByID(feedID)
	if err != nil || feed == nil || feed.NeedClassification == 0 {
		log.Printf("[Classifier] Feed %d classification skipped (not found or disabled)", feedID)
		return
	}

	categories, err := crud.GetCategoriesForFeed(feedID)
	if err != nil || len(categories) == 0 {
		log.Printf("[Classifier] No categories found for feed %d", feedID)
		return
	}

	var defaultCatID int
	categoryMap := make(map[string]int)
	var allowedNames []string

	for _, cat := range categories {
		categoryMap[strings.ToLower(cat.Name)] = cat.ID
		allowedNames = append(allowedNames, cat.Name)
		if cat.IsDefault == 1 {
			defaultCatID = cat.ID
		}
	}

	unclassified, err := crud.GetUnclassifiedEntries(feedID)
	if err != nil || len(unclassified) == 0 {
		log.Printf("[Classifier] No unclassified entries for feed %d", feedID)
		return
	}

	log.Printf("[Classifier] Found %d unclassified entries for feed %d", len(unclassified), feedID)

	batchSize := 25
	var wg sync.WaitGroup
	sem := make(chan struct{}, 3) // Throttling concurrency to max 3 parallel LLM requests

	for i := 0; i < len(unclassified); i += batchSize {
		end := i + batchSize
		if end > len(unclassified) {
			end = len(unclassified)
		}
		batch := unclassified[i:end]

		wg.Add(1)
		go func(b []models.Entry, startIndex, endIndex int) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			var batchEntries []map[string]interface{}
			for _, e := range b {
				batchEntries = append(batchEntries, map[string]interface{}{
					"id":      e.ID,
					"title":   e.Title,
					"summary": e.RawContent,
				})
			}

			log.Printf("[Classifier] Classifying batch of %d entries (index %d to %d)", len(batchEntries), startIndex, endIndex)

			// Get user reading preferences prompt if enabled
			interestPrompt := ""
			if config.GlobalConfig.InterestProfileEnabled {
				latestInterest, err := crud.GetLatestUserInterest()
				if err == nil && latestInterest != nil {
					interestPrompt = latestInterest.PromptText
				}
			}

			results, err := ClassifyEntriesBatch(allowedNames, batchEntries, interestPrompt)
			if err != nil {
				log.Printf("[Classifier] Classification batch call failed: %s", err)
			}

			resultsMap := make(map[int]ClassificationResult)
			for _, r := range results {
				var intID int
				switch v := r.ID.(type) {
				case float64:
					intID = int(v)
				case int:
					intID = v
				case int64:
					intID = int(v)
				}
				resultsMap[intID] = r
			}

			for _, item := range batchEntries {
				entryID := item["id"].(int)
				res, found := resultsMap[entryID]

				catID := defaultCatID
				attention := "skim"

				if found {
					if res.Category != "" {
						if id, ok := categoryMap[strings.ToLower(res.Category)]; ok {
							catID = id
						}
					}
					if res.Attention == "read" || res.Attention == "skim" || res.Attention == "glance" {
						attention = res.Attention
					}
				}

				// Update in DB
				_ = crud.UpdateEntryClassification(entryID, catID, attention)
			}
		}(batch, i, end)
	}
	wg.Wait()

	log.Printf("[Classifier] Completed classification for feed %d", feedID)

	// Pregenerate summaries in background if enabled
	if config.GlobalConfig.AI.Pregenerate {
		go PregenerateSummariesForFeed(feedID)
	}
}

func PregenerateSummariesForFeed(feedID int) {
	log.Printf("[SummaryPre] Checking summaries to pregenerate for feed %d", feedID)

	query := `
		SELECT e.id, e.title, e.url, ft.content
		FROM entries e
		JOIN fulltext ft ON ft.entry_id = e.id
		LEFT JOIN summaries s ON s.entry_id = e.id
		WHERE e.feed_id = ? AND e.attention = 'read' AND e.fulltext_ready = 1
		  AND ft.status = 'ok' AND s.entry_id IS NULL
	`
	rows, err := db.DB.Query(query, feedID)
	if err != nil {
		log.Printf("[SummaryPre] Query failed: %s", err)
		return
	}
	defer rows.Close()

	type job struct {
		ID    int
		Title string
		URL   string
		Text  string
	}
	var jobs []job
	for rows.Next() {
		var j job
		if err := rows.Scan(&j.ID, &j.Title, &j.URL, &j.Text); err == nil {
			jobs = append(jobs, j)
		}
	}

	if len(jobs) == 0 {
		return
	}

	log.Printf("[SummaryPre] Pregenerating summaries for %d entries in feed %d", len(jobs), feedID)

	model := config.GlobalConfig.AI.Default.Model
	for _, j := range jobs {
		cleanLen := EstimateCleanTextLength(j.Text)
		targetChars := cleanLen / 10
		if targetChars < 100 {
			targetChars = 100
		}
		if targetChars > 900 {
			targetChars = 900
		}

		rawSummary, err := GenerateSummarySync(j.Title, j.URL, j.Text, targetChars, config.GlobalConfig.AI.SummaryLanguage)
		if err != nil {
			log.Printf("[SummaryPre] Failed to generate summary for entry %d: %s", j.ID, err)
			continue
		}

		summaryText, clickbait := ParseAISummaryResponse(rawSummary)
		err = crud.SaveSummary(j.ID, summaryText, clickbait, model)
		if err != nil {
			log.Printf("[SummaryPre] Failed to save summary for entry %d: %s", j.ID, err)
		} else {
			log.Printf("[SummaryPre] Pregenerated summary for entry %d", j.ID)
		}
	}
}

func GetEntryFulltext(entryID int) (map[string]interface{}, error) {
	entry, err := crud.GetEntryByID(entryID)
	if err != nil {
		return nil, err
	}
	if entry == nil {
		return nil, errors.New("entry not found")
	}

	hasSummary := false
	summaryRow, _ := crud.GetEntrySummary(entryID)
	if summaryRow != nil && strings.TrimSpace(summaryRow.Content) != "" {
		hasSummary = true
	}

	ftRow, _ := crud.GetEntryFulltext(entryID)
	if ftRow != nil && strings.TrimSpace(ftRow.Content) != "" {
		cleanLen := EstimateCleanTextLength(ftRow.Content)
		return map[string]interface{}{
			"content":          ftRow.Content,
			"status":           ftRow.Status,
			"has_summary":      hasSummary,
			"clean_char_count": cleanLen,
		}, nil
	}

	var content, status, fetcher string
	if entry.FulltextReady == 1 && strings.TrimSpace(entry.RawContent) != "" {
		content = crud.CleanHTML(entry.RawContent)
		status = "ok"
		fetcher = "feed"
	} else {
		content, status, fetcher = FetchAndExtractFulltext(entry.URL)
	}

	_ = crud.SaveFulltext(entryID, content, status, fetcher)

	cleanLen := EstimateCleanTextLength(content)
	return map[string]interface{}{
		"content":          content,
		"status":           status,
		"has_summary":      hasSummary,
		"clean_char_count": cleanLen,
	}, nil
}

func EnsureFeedSeeded(feedID int) error {
	feed, err := crud.GetFeedByID(feedID)
	if err != nil || feed == nil {
		return err
	}
	if feed.NeedClassification == 0 {
		if feed.Seeded == 0 {
			_, err := db.DB.Exec("UPDATE feeds SET seeded = 1 WHERE id = ?", feedID)
			return err
		}
		return nil
	}
	if feed.Seeded == 1 {
		return nil
	}

	rows, err := db.DB.Query("SELECT title FROM entries WHERE feed_id = ? ORDER BY published_at DESC LIMIT 100", feedID)
	if err != nil {
		return err
	}
	defer rows.Close()

	var titles []string
	for rows.Next() {
		var title string
		if err := rows.Scan(&title); err == nil && title != "" {
			titles = append(titles, title)
		}
	}

	if len(titles) == 0 {
		return nil
	}

	log.Printf("[FeedService] Feed %d is not seeded. Attempting to generate seed categories with %d articles.", feedID, len(titles))
	seedCategories, err := GenerateSeedCategories(titles)
	if err != nil {
		return err
	}

	if len(seedCategories) > 0 {
		log.Printf("[FeedService] Successfully generated seed categories for feed %d: %v", feedID, seedCategories)
		tx, errTx := db.DB.Begin()
		if errTx != nil {
			return errTx
		}
		defer tx.Rollback()

		now := time.Now().UTC().Format(time.RFC3339)
		for _, name := range seedCategories {
			_, _ = tx.Exec("INSERT OR IGNORE INTO categories (feed_id, name, is_default, created_at) VALUES (?, ?, 0, ?)", feedID, name, now)
		}

		defaultCatID := 0
		_ = tx.QueryRow("SELECT id FROM categories WHERE feed_id = ? AND is_default = 1", feedID).Scan(&defaultCatID)

		_, _ = tx.Exec("UPDATE entries SET category_id = ?, classified_at = NULL WHERE feed_id = ? AND category_id = ?", defaultCatID, feedID, defaultCatID)
		_, _ = tx.Exec("UPDATE feeds SET seeded = 1 WHERE id = ?", feedID)
		tx.Commit()
	} else {
		log.Printf("[FeedService] AI generated empty seed categories for feed %d (possibly LLM not configured). Will retry later.", feedID)
	}

	return nil
}

