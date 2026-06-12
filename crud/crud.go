package crud

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	"kickrss/db"
	"kickrss/models"

	"golang.org/x/net/html"
)

// --- Feeds ---

func ListFeeds() ([]models.Feed, error) {
	query := `
		SELECT f.id, f.title, f.url, f.site_url, f.etag, f.last_modified, f.last_fetched_at, f.seeded, f.enabled, f.need_classification,
		       COUNT(CASE WHEN e.is_read = 0 THEN 1 END) as unread_count
		FROM feeds f
		LEFT JOIN entries e ON e.feed_id = f.id
		GROUP BY f.id
		ORDER BY f.title ASC
	`
	rows, err := db.DB.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	feeds := []models.Feed{}
	for rows.Next() {
		var f models.Feed
		var siteURL, lastFetched, etag, lastMod sql.NullString
		err := rows.Scan(&f.ID, &f.Title, &f.URL, &siteURL, &etag, &lastMod, &lastFetched, &f.Seeded, &f.Enabled, &f.NeedClassification, &f.UnreadCount)
		if err != nil {
			return nil, err
		}
		f.SiteURL = siteURL.String
		f.LastFetchedAt = lastFetched.String
		f.Etag = etag.String
		f.LastModified = lastMod.String
		feeds = append(feeds, f)
	}
	return feeds, nil
}

func GetFeedByID(id int) (*models.Feed, error) {
	query := "SELECT id, title, url, site_url, etag, last_modified, last_fetched_at, seeded, enabled, need_classification FROM feeds WHERE id = ?"
	row := db.DB.QueryRow(query, id)

	var f models.Feed
	var siteURL, lastFetched, etag, lastMod sql.NullString
	err := row.Scan(&f.ID, &f.Title, &f.URL, &siteURL, &etag, &lastMod, &lastFetched, &f.Seeded, &f.Enabled, &f.NeedClassification)
	if err == sql.ErrNoRows {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	f.SiteURL = siteURL.String
	f.LastFetchedAt = lastFetched.String
	f.Etag = etag.String
	f.LastModified = lastMod.String
	return &f, nil
}

func GetFeedByURL(url string) (*models.Feed, error) {
	query := "SELECT id, title, url, site_url, etag, last_modified, last_fetched_at, seeded, enabled, need_classification FROM feeds WHERE url = ?"
	row := db.DB.QueryRow(query, url)

	var f models.Feed
	var siteURL, lastFetched, etag, lastMod sql.NullString
	err := row.Scan(&f.ID, &f.Title, &f.URL, &siteURL, &etag, &lastMod, &lastFetched, &f.Seeded, &f.Enabled, &f.NeedClassification)
	if err == sql.ErrNoRows {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	f.SiteURL = siteURL.String
	f.LastFetchedAt = lastFetched.String
	f.Etag = etag.String
	f.LastModified = lastMod.String
	return &f, nil
}

func AddFeed(url, title string) (int64, error) {
	query := "INSERT INTO feeds (title, url, site_url, seeded) VALUES (?, ?, '', 0)"
	res, err := db.DB.Exec(query, title, url)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func UpdateFeedTitle(id int, title string) error {
	_, err := db.DB.Exec("UPDATE feeds SET title = ? WHERE id = ?", title, id)
	return err
}

func UpdateFeedEnabled(id int, enabled bool) error {
	val := 0
	if enabled {
		val = 1
	}
	_, err := db.DB.Exec("UPDATE feeds SET enabled = ? WHERE id = ?", val, id)
	return err
}

func UpdateFeedNeedClassification(id int, needClassify bool) error {
	val := 0
	if needClassify {
		val = 1
	}
	_, err := db.DB.Exec("UPDATE feeds SET need_classification = ? WHERE id = ?", val, id)
	if err != nil {
		return err
	}
	if !needClassify {
		defaultCatID, err := GetDefaultCategory(id)
		if err != nil {
			return err
		}
		_, err = db.DB.Exec("UPDATE entries SET category_id = ?, classified_at = NULL WHERE feed_id = ?", defaultCatID, id)
		if err != nil {
			return err
		}
		_, err = db.DB.Exec("DELETE FROM categories WHERE feed_id = ? AND is_default = 0", id)
		if err != nil {
			return err
		}
		_, err = db.DB.Exec("UPDATE feeds SET seeded = 1 WHERE id = ?", id)
		if err != nil {
			return err
		}
	}
	return nil
}

func UpdateFeedFetchStatus(id int, etag, lastModified string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := db.DB.Exec("UPDATE feeds SET etag = ?, last_modified = ?, last_fetched_at = ? WHERE id = ?", etag, lastModified, now, id)
	return err
}

func DeleteFeed(id int) (bool, error) {
	res, err := db.DB.Exec("DELETE FROM feeds WHERE id = ?", id)
	if err != nil {
		return false, err
	}
	affected, err := res.RowsAffected()
	return affected > 0, err
}

// --- Categories ---

func GetCategoriesForFeed(feedID int) ([]models.Category, error) {
	query := `
		SELECT c.id, c.feed_id, c.name, c.is_default, c.created_at,
		       COUNT(CASE WHEN e.is_read = 0 THEN 1 END) as unread_count
		FROM categories c
		LEFT JOIN entries e ON e.category_id = c.id
		WHERE c.feed_id = ?
		GROUP BY c.id
		ORDER BY c.is_default DESC, c.name ASC
	`
	rows, err := db.DB.Query(query, feedID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	list := []models.Category{}
	for rows.Next() {
		var c models.Category
		var createdAt sql.NullString
		err := rows.Scan(&c.ID, &c.FeedID, &c.Name, &c.IsDefault, &createdAt, &c.UnreadCount)
		if err != nil {
			return nil, err
		}
		c.CreatedAt = createdAt.String
		list = append(list, c)
	}
	return list, nil
}

func GetDefaultCategory(feedID int) (int, error) {
	var id int
	err := db.DB.QueryRow("SELECT id FROM categories WHERE feed_id = ? AND is_default = 1", feedID).Scan(&id)
	if err == sql.ErrNoRows {
		// Create default category
		now := time.Now().UTC().Format(time.RFC3339)
		res, err := db.DB.Exec("INSERT INTO categories (feed_id, name, is_default, created_at) VALUES (?, '未归类', 1, ?)", feedID, now)
		if err != nil {
			return 0, err
		}
		lastID, err := res.LastInsertId()
		return int(lastID), err
	}
	return id, err
}

func SaveCategories(feedID int, categoryNames []string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	for _, name := range categoryNames {
		if name == "" || name == "未归类" {
			continue
		}
		// Insert ignore or replace
		_, _ = db.DB.Exec("INSERT OR IGNORE INTO categories (feed_id, name, is_default, created_at) VALUES (?, ?, 0, ?)", feedID, name, now)
	}
	return nil
}

func CleanEmptyCategories(feedID int) error {
	query := `
		SELECT c.id, c.name, COUNT(e.id) as entry_count
		FROM categories c
		LEFT JOIN entries e ON e.category_id = c.id
		WHERE c.feed_id = ? AND c.is_default = 0
		GROUP BY c.id
	`
	rows, err := db.DB.Query(query, feedID)
	if err != nil {
		return err
	}
	defer rows.Close()

	var emptyIDs []int
	for rows.Next() {
		var id int
		var name string
		var count int
		if err := rows.Scan(&id, &name, &count); err != nil {
			return err
		}
		if count == 0 {
			emptyIDs = append(emptyIDs, id)
		}
	}

	for _, id := range emptyIDs {
		_, _ = db.DB.Exec("DELETE FROM categories WHERE id = ?", id)
	}
	return nil
}

// --- Entries ---

func GetEntryByID(id int) (*models.Entry, error) {
	query := `
		SELECT e.id, e.feed_id, e.category_id, e.guid, e.title, e.url, e.author, e.published_at, e.fetched_at, e.raw_content,
		       e.attention, e.likely_no_text, e.fulltext_ready, e.is_read, e.read_at, e.classified_at, e.is_starred, e.starred_at,
		       f.title as feed_title, c.name as category_name
		FROM entries e
		JOIN feeds f ON f.id = e.feed_id
		LEFT JOIN categories c ON c.id = e.category_id
		WHERE e.id = ?
	`
	row := db.DB.QueryRow(query, id)

	var e models.Entry
	var catID sql.NullInt64
	var catName sql.NullString
	var author, readAt, classifiedAt, starredAt sql.NullString
	err := row.Scan(&e.ID, &e.FeedID, &catID, &e.Guid, &e.Title, &e.URL, &author, &e.PublishedAt, &e.FetchedAt, &e.RawContent,
		&e.Attention, &e.LikelyNoText, &e.FulltextReady, &e.IsRead, &readAt, &classifiedAt, &e.IsStarred, &starredAt,
		&e.FeedTitle, &catName)

	if err == sql.ErrNoRows {
		return nil, nil
	} else if err != nil {
		return nil, err
	}

	if catID.Valid {
		val := int(catID.Int64)
		e.CategoryID = &val
	}
	e.CategoryName = catName.String
	e.Author = author.String
	e.ReadAt = readAt.String
	e.ClassifiedAt = classifiedAt.String
	e.StarredAt = starredAt.String

	return &e, nil
}

func SaveEntries(feedID int, rawEntries []models.Entry, defaultCatID int) (int, error) {
	tx, err := db.DB.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	newCount := 0
	now := time.Now().UTC().Format(time.RFC3339)

	stmtCheck, err := tx.Prepare("SELECT id FROM entries WHERE feed_id = ? AND guid = ?")
	if err != nil {
		return 0, err
	}
	defer stmtCheck.Close()

	stmtInsert, err := tx.Prepare(`
		INSERT INTO entries (feed_id, category_id, guid, title, url, author, published_at, fetched_at, raw_content, attention, likely_no_text, fulltext_ready)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, 'skim', 0, 0)
	`)
	if err != nil {
		return 0, err
	}
	defer stmtInsert.Close()

	for _, re := range rawEntries {
		var existingID int
		errCheck := stmtCheck.QueryRow(feedID, re.Guid).Scan(&existingID)
		if errCheck == sql.ErrNoRows {
			// Determine if likely no text
			likelyNoText := 0
			if len(re.RawContent) < 200 {
				likelyNoText = 1
			}
			
			// If feed already has long text, mark fulltext_ready=1
			fulltextReady := 0
			if len(re.RawContent) > 800 {
				fulltextReady = 1
			}

			pubTime := re.PublishedAt
			if pubTime == "" {
				pubTime = now
			}

			_, err = stmtInsert.Exec(feedID, defaultCatID, re.Guid, re.Title, re.URL, re.Author, pubTime, now, re.RawContent, likelyNoText, fulltextReady)
			if err != nil {
				return 0, err
			}
			newCount++
		}
	}

	err = tx.Commit()
	return newCount, err
}

func GetUnclassifiedEntries(feedID int) ([]models.Entry, error) {
	query := `
		SELECT id, title, raw_content
		FROM entries
		WHERE feed_id = ? AND classified_at IS NULL
		ORDER BY published_at DESC
	`
	rows, err := db.DB.Query(query, feedID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	list := []models.Entry{}
	for rows.Next() {
		var e models.Entry
		err := rows.Scan(&e.ID, &e.Title, &e.RawContent)
		if err != nil {
			return nil, err
		}
		list = append(list, e)
	}
	return list, nil
}

func GetRecentUncategorizedEntries(feedID int, days int) ([]models.Entry, error) {
	query := `
		SELECT e.id, e.title
		FROM entries e
		JOIN categories c ON c.id = e.category_id
		WHERE e.feed_id = ? AND c.is_default = 1 AND e.fetched_at > datetime('now', ?)
		ORDER BY e.published_at DESC
	`
	rows, err := db.DB.Query(query, feedID, fmt.Sprintf("-%d days", days))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	list := []models.Entry{}
	for rows.Next() {
		var e models.Entry
		err := rows.Scan(&e.ID, &e.Title)
		if err != nil {
			return nil, err
		}
		list = append(list, e)
	}
	return list, nil
}

func UpdateEntryClassification(entryID int, categoryID int, attention string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := db.DB.Exec("UPDATE entries SET category_id = ?, attention = ?, classified_at = ? WHERE id = ?", categoryID, attention, now, entryID)
	return err
}

func ResetUncategorizedEntriesClassification() error {
	_, err := db.DB.Exec(`
		UPDATE entries
		SET classified_at = NULL
		WHERE category_id IN (SELECT id FROM categories WHERE is_default = 1)
	`)
	return err
}

func MoveEntriesToCategory(entryIDs []int, categoryID int) (int64, error) {
	if len(entryIDs) == 0 {
		return 0, nil
	}
	
	tx, err := db.DB.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	var moved int64 = 0
	for _, eid := range entryIDs {
		res, err := tx.Exec("UPDATE entries SET category_id = ? WHERE id = ?", categoryID, eid)
		if err != nil {
			return 0, err
		}
		affected, _ := res.RowsAffected()
		moved += affected
	}

	err = tx.Commit()
	return moved, err
}

func MarkEntriesAsRead(categoryID int) ([]int, error) {
	rows, err := db.DB.Query("SELECT id FROM entries WHERE category_id = ? AND is_read = 0", categoryID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	ids := []int{}
	for rows.Next() {
		var id int
		if err := rows.Scan(&id); err == nil {
			ids = append(ids, id)
		}
	}

	if len(ids) == 0 {
		return ids, nil
	}

	now := time.Now().UTC().Format(time.RFC3339)
	_, err = db.DB.Exec("UPDATE entries SET is_read = 1, read_at = ? WHERE category_id = ? AND is_read = 0", now, categoryID)
	return ids, err
}

func MarkFeedAsRead(feedID int) ([]int, error) {
	rows, err := db.DB.Query("SELECT id FROM entries WHERE feed_id = ? AND is_read = 0", feedID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	ids := []int{}
	for rows.Next() {
		var id int
		if err := rows.Scan(&id); err == nil {
			ids = append(ids, id)
		}
	}

	if len(ids) == 0 {
		return ids, nil
	}

	now := time.Now().UTC().Format(time.RFC3339)
	_, err = db.DB.Exec("UPDATE entries SET is_read = 1, read_at = ? WHERE feed_id = ? AND is_read = 0", now, feedID)
	return ids, err
}

func MarkAllAsRead() error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := db.DB.Exec("UPDATE entries SET is_read = 1, read_at = ? WHERE is_read = 0", now)
	return err
}

func SetEntryReadStatus(entryID int, isRead bool) error {
	val := 0
	var readAt sql.NullString
	if isRead {
		val = 1
		readAt.String = time.Now().UTC().Format(time.RFC3339)
		readAt.Valid = true
	}
	_, err := db.DB.Exec("UPDATE entries SET is_read = ?, read_at = ? WHERE id = ?", val, readAt, entryID)
	return err
}

func SetEntriesReadStatus(entryIDs []int, isRead bool) (int64, error) {
	if len(entryIDs) == 0 {
		return 0, nil
	}

	placeholders := make([]string, len(entryIDs))
	args := make([]interface{}, 0, len(entryIDs)+2)

	val := 0
	var readAt sql.NullString
	if isRead {
		val = 1
		readAt.String = time.Now().UTC().Format(time.RFC3339)
		readAt.Valid = true
	}

	args = append(args, val, readAt)
	for i, id := range entryIDs {
		placeholders[i] = "?"
		args = append(args, id)
	}

	query := fmt.Sprintf("UPDATE entries SET is_read = ?, read_at = ? WHERE id IN (%s)", strings.Join(placeholders, ","))
	res, err := db.DB.Exec(query, args...)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func SetEntryStarredStatus(entryID int, isStarred bool) error {
	val := 0
	var starredAt sql.NullString
	if isStarred {
		val = 1
		starredAt.String = time.Now().UTC().Format(time.RFC3339)
		starredAt.Valid = true
	}
	_, err := db.DB.Exec("UPDATE entries SET is_starred = ?, starred_at = ? WHERE id = ?", val, starredAt, entryID)
	return err
}

// --- Fulltext ---

func GetEntryFulltext(entryID int) (*models.Fulltext, error) {
	row := db.DB.QueryRow("SELECT entry_id, content, status, fetched_at, fetcher FROM fulltext WHERE entry_id = ?", entryID)
	var ft models.Fulltext
	err := row.Scan(&ft.EntryID, &ft.Content, &ft.Status, &ft.FetchedAt, &ft.Fetcher)
	if err == sql.ErrNoRows {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	return &ft, nil
}

func SaveFulltext(entryID int, content, status, fetcher string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := db.DB.Exec(`
		INSERT INTO fulltext (entry_id, content, status, fetched_at, fetcher)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(entry_id) DO UPDATE SET
			content = excluded.content,
			status = excluded.status,
			fetched_at = excluded.fetched_at,
			fetcher = excluded.fetcher
	`, entryID, content, status, now, fetcher)
	if err != nil {
		return err
	}
	
	// Also mark fulltext_ready in entries
	ready := 0
	if status == "ok" {
		ready = 1
	}
	_, err = db.DB.Exec("UPDATE entries SET fulltext_ready = ? WHERE id = ?", ready, entryID)
	return err
}

// --- Summaries ---

func GetEntrySummary(entryID int) (*models.Summary, error) {
	row := db.DB.QueryRow("SELECT entry_id, content, clickbait_note, model, created_at FROM summaries WHERE entry_id = ?", entryID)
	var s models.Summary
	var clickNote sql.NullString
	err := row.Scan(&s.EntryID, &s.Content, &clickNote, &s.Model, &s.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	s.ClickbaitNote = clickNote.String
	return &s, nil
}

func SaveSummary(entryID int, content, clickbaitNote, model string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	var clickNote sql.NullString
	if clickbaitNote != "" {
		clickNote.String = clickbaitNote
		clickNote.Valid = true
	}
	_, err := db.DB.Exec(`
		INSERT INTO summaries (entry_id, content, clickbait_note, model, created_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(entry_id) DO UPDATE SET
			content = excluded.content,
			clickbait_note = excluded.clickbait_note,
			model = excluded.model,
			created_at = excluded.created_at
	`, entryID, content, clickNote, model, now)
	return err
}

func DeleteSummary(entryID int) error {
	_, err := db.DB.Exec("DELETE FROM summaries WHERE entry_id = ?", entryID)
	return err
}

// --- Chat Messages ---

func GetChatMessages(entryID int) ([]models.ChatMessage, error) {
	rows, err := db.DB.Query("SELECT id, entry_id, role, content, created_at FROM chat_messages WHERE entry_id = ? ORDER BY id ASC", entryID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	list := []models.ChatMessage{}
	for rows.Next() {
		var cm models.ChatMessage
		err := rows.Scan(&cm.ID, &cm.EntryID, &cm.Role, &cm.Content, &cm.CreatedAt)
		if err != nil {
			return nil, err
		}
		list = append(list, cm)
	}
	return list, nil
}

func SaveChatMessage(entryID int, role, content string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := db.DB.Exec("INSERT INTO chat_messages (entry_id, role, content, created_at) VALUES (?, ?, ?, ?)", entryID, role, content, now)
	return err
}

func ClearChatMessages(entryID int) error {
	_, err := db.DB.Exec("DELETE FROM chat_messages WHERE entry_id = ?", entryID)
	return err
}

// --- Translations ---

func GetTranslation(entryID int, lang string) (*models.Translation, error) {
	row := db.DB.QueryRow("SELECT entry_id, content, lang, created_at FROM translations WHERE entry_id = ? AND lang = ?", entryID, lang)
	var t models.Translation
	err := row.Scan(&t.EntryID, &t.Content, &t.Lang, &t.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	return &t, nil
}

func SaveTranslation(entryID int, content, lang string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := db.DB.Exec(`
		INSERT INTO translations (entry_id, content, lang, created_at)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(entry_id) DO UPDATE SET
			content = excluded.content,
			lang = excluded.lang,
			created_at = excluded.created_at
	`, entryID, content, lang, now)
	return err
}

func GetParagraphTranslations(entryID int, lang string) ([]models.ParagraphTranslation, error) {
	rows, err := db.DB.Query("SELECT entry_id, para_index, lang, original_text, translated_text, created_at FROM paragraph_translations WHERE entry_id = ? AND lang = ? ORDER BY para_index ASC", entryID, lang)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	list := []models.ParagraphTranslation{}
	for rows.Next() {
		var pt models.ParagraphTranslation
		var createdAt sql.NullString
		err := rows.Scan(&pt.EntryID, &pt.ParaIndex, &pt.Lang, &pt.OriginalText, &pt.TranslatedText, &createdAt)
		if err != nil {
			return nil, err
		}
		pt.CreatedAt = createdAt.String
		list = append(list, pt)
	}
	return list, nil
}

func SaveParagraphTranslation(entryID, paraIndex int, lang, originalText, translatedText string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := db.DB.Exec(`
		INSERT INTO paragraph_translations (entry_id, para_index, lang, original_text, translated_text, created_at)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(entry_id, para_index, lang) DO UPDATE SET
			original_text = excluded.original_text,
			translated_text = excluded.translated_text,
			created_at = excluded.created_at
	`, entryID, paraIndex, lang, originalText, translatedText, now)
	return err
}

func DeleteTranslation(entryID int) error {
	_, err := db.DB.Exec("DELETE FROM translations WHERE entry_id = ?", entryID)
	return err
}

func DeleteParagraphTranslations(entryID int) error {
	_, err := db.DB.Exec("DELETE FROM paragraph_translations WHERE entry_id = ?", entryID)
	return err
}

func GetParagraphTranslation(entryID, paraIndex int, lang string) (*models.ParagraphTranslation, error) {
	row := db.DB.QueryRow("SELECT entry_id, para_index, lang, original_text, translated_text, created_at FROM paragraph_translations WHERE entry_id = ? AND para_index = ? AND lang = ?", entryID, paraIndex, lang)
	var pt models.ParagraphTranslation
	var createdAt sql.NullString
	err := row.Scan(&pt.EntryID, &pt.ParaIndex, &pt.Lang, &pt.OriginalText, &pt.TranslatedText, &createdAt)
	if err == sql.ErrNoRows {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	pt.CreatedAt = createdAt.String
	return &pt, nil
}

// --- Engagement ---

func SaveEngagement(entryID int, dwellMs int, scrolled float64, toBottom, original, fav int, manualBump string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	var bump sql.NullString
	if manualBump != "" {
		bump.String = manualBump
		bump.Valid = true
	}
	_, err := db.DB.Exec(`
		INSERT INTO engagement (entry_id, opened, active_dwell_ms, scrolled_pct, scrolled_to_bottom, opened_original, favorited, manual_bump, recorded_at)
		VALUES (?, 1, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(entry_id) DO UPDATE SET
			opened = 1,
			active_dwell_ms = active_dwell_ms + excluded.active_dwell_ms,
			scrolled_pct = MAX(scrolled_pct, excluded.scrolled_pct),
			scrolled_to_bottom = MAX(scrolled_to_bottom, excluded.scrolled_to_bottom),
			opened_original = MAX(opened_original, excluded.opened_original),
			favorited = MAX(favorited, excluded.favorited),
			manual_bump = COALESCE(excluded.manual_bump, manual_bump),
			recorded_at = excluded.recorded_at
	`, entryID, dwellMs, scrolled, toBottom, original, fav, bump, now)
	return err
}

// --- User Interest Profile ---

func GetLatestUserInterest() (*models.UserInterest, error) {
	row := db.DB.QueryRow("SELECT id, snapshot_date, total_articles, high_engagement, low_engagement, topics_json, prompt_text, generated_at FROM user_interests ORDER BY snapshot_date DESC LIMIT 1")
	var ui models.UserInterest
	err := row.Scan(&ui.ID, &ui.SnapshotDate, &ui.TotalArticles, &ui.HighEngagement, &ui.LowEngagement, &ui.TopicsJson, &ui.PromptText, &ui.GeneratedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	return &ui, nil
}

func CleanHTML(htmlContent string) string {
	if htmlContent == "" {
		return ""
	}
	if !strings.Contains(htmlContent, "<") && !strings.Contains(htmlContent, ">") {
		return htmlContent
	}

	// Wrap in a div to ensure a single root, and parse
	wrapped := "<div>" + htmlContent + "</div>"
	doc, err := html.Parse(strings.NewReader(wrapped))
	if err != nil {
		// Fallback to basic tag stripping if parsing fails
		re := regexp.MustCompile("<[^>]*>")
		cleaned := re.ReplaceAllString(htmlContent, "")
		cleaned = strings.ReplaceAll(cleaned, "&lt;", "<")
		cleaned = strings.ReplaceAll(cleaned, "&gt;", ">")
		cleaned = strings.ReplaceAll(cleaned, "&amp;", "&")
		cleaned = strings.ReplaceAll(cleaned, "&quot;", "\"")
		cleaned = strings.ReplaceAll(cleaned, "&nbsp;", " ")
		cleaned = strings.ReplaceAll(cleaned, "&#39;", "'")
		return strings.TrimSpace(cleaned)
	}

	// Find the div we wrapped it in
	var startNode *html.Node
	var findDiv func(*html.Node)
	findDiv = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "div" {
			startNode = n
			return
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			findDiv(c)
			if startNode != nil {
				return
			}
		}
	}
	findDiv(doc)

	if startNode == nil {
		startNode = doc
	}

	var convertNode func(*html.Node) string
	convertNode = func(n *html.Node) string {
		if n.Type == html.TextNode {
			return n.Data
		}

		if n.Type == html.ElementNode {
			var childrenBuf strings.Builder
			for c := n.FirstChild; c != nil; c = c.NextSibling {
				childrenBuf.WriteString(convertNode(c))
			}
			inner := childrenBuf.String()
			tag := strings.ToLower(n.Data)

			switch tag {
			case "p":
				return "\n\n" + strings.TrimSpace(inner) + "\n\n"
			case "br", "hr":
				return "\n"
			case "strong", "b":
				return "**" + strings.TrimSpace(inner) + "**"
			case "em", "i":
				return "*" + strings.TrimSpace(inner) + "*"
			case "a":
				href := ""
				for _, attr := range n.Attr {
					if attr.Key == "href" {
						href = attr.Val
						break
					}
				}
				trimmedInner := strings.TrimSpace(inner)
				if trimmedInner != "" {
					return "[" + trimmedInner + "](" + href + ")"
				}
				return href
			case "img":
				src := ""
				alt := ""
				for _, attr := range n.Attr {
					if attr.Key == "src" {
						src = attr.Val
					} else if attr.Key == "alt" || attr.Key == "title" {
						if alt == "" {
							alt = attr.Val
						}
					}
				}
				if alt == "" {
					alt = "image"
				}
				if src != "" {
					return "\n\n![" + alt + "](" + src + ")\n\n"
				}
				return ""
			case "h1", "h2", "h3", "h4", "h5", "h6":
				level := 1
				if len(tag) == 2 {
					level = int(tag[1] - '0')
				}
				hashes := strings.Repeat("#", level)
				return "\n\n" + hashes + " " + strings.TrimSpace(inner) + "\n\n"
			case "li":
				return "\n* " + strings.TrimSpace(inner)
			case "ul", "ol":
				return "\n" + inner + "\n"
			case "blockquote":
				return "\n\n> " + strings.TrimSpace(inner) + "\n\n"
			case "code", "pre":
				return "`" + strings.TrimSpace(inner) + "`"
			default:
				return inner
			}
		}
		return ""
	}

	md := convertNode(startNode)

	// Clean up multiple newlines (similar to re.sub(r'\n{3,}', '\n\n', md))
	reNewlines := regexp.MustCompile(`\n{3,}`)
	md = reNewlines.ReplaceAllString(md, "\n\n")

	return strings.TrimSpace(md)
}

// --- Topic Details Structs ---

type TopicItem struct {
	Topic    string `json:"topic"`
	EntryIDs []int  `json:"entry_ids"`
}

type TopicsJSON struct {
	HighInterest      []TopicItem `json:"high_interest"`
	LowInterest       []TopicItem `json:"low_interest"`
	ConcentrationNote *string     `json:"concentration_note"`
}

type TopicStats struct {
	ArticleCount  int `json:"article_count"`
	FavoriteCount int `json:"favorite_count"`
	OriginalCount int `json:"original_count"`
}

type TopicArticle struct {
	EntryID int      `json:"entry_id"`
	Title   string   `json:"title"`
	Source  string   `json:"source"`
	Badges  []string `json:"badges"`
}

type TopicDetail struct {
	Topic       string         `json:"topic"`
	Stats       TopicStats     `json:"stats"`
	WeeklyTrend []int          `json:"weekly_trend"`
	Articles    []TopicArticle `json:"articles"`
}

func GetTopicDetail(topicName string) (*TopicDetail, error) {
	ui, err := GetLatestUserInterest()
	if err != nil {
		return nil, err
	}
	if ui == nil {
		return nil, nil
	}

	var topics TopicsJSON
	if err := json.Unmarshal([]byte(ui.TopicsJson), &topics); err != nil {
		return nil, err
	}

	var targetItem *TopicItem
	for i := range topics.HighInterest {
		if topics.HighInterest[i].Topic == topicName {
			targetItem = &topics.HighInterest[i]
			break
		}
	}
	if targetItem == nil {
		for i := range topics.LowInterest {
			if topics.LowInterest[i].Topic == topicName {
				targetItem = &topics.LowInterest[i]
				break
			}
		}
	}

	if targetItem == nil {
		return nil, nil
	}

	if len(targetItem.EntryIDs) == 0 {
		return &TopicDetail{
			Topic:       topicName,
			Stats:       TopicStats{ArticleCount: 0, FavoriteCount: 0, OriginalCount: 0},
			WeeklyTrend: make([]int, 12),
			Articles:    []TopicArticle{},
		}, nil
	}

	// Build query with placeholders
	placeholders := make([]string, len(targetItem.EntryIDs))
	args := make([]interface{}, len(targetItem.EntryIDs))
	for i, id := range targetItem.EntryIDs {
		placeholders[i] = "?"
		args[i] = id
	}

	query := fmt.Sprintf(`
		SELECT e.id, e.title, f.title as source, e.is_starred, e.published_at,
		       COALESCE(g.opened_original, 0), COALESCE(g.active_dwell_ms, 0)
		FROM entries e
		JOIN feeds f ON e.feed_id = f.id
		LEFT JOIN engagement g ON g.entry_id = e.id
		WHERE e.id IN (%s)
	`, strings.Join(placeholders, ","))

	rows, err := db.DB.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	articles := []TopicArticle{}
	favoriteCount := 0
	originalCount := 0
	weeklyTrend := make([]int, 12)
	now := time.Now()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)

	for rows.Next() {
		var entryID int
		var title, source, publishedAt string
		var isStarred, openedOriginal, activeDwellMs int

		if err := rows.Scan(&entryID, &title, &source, &isStarred, &publishedAt, &openedOriginal, &activeDwellMs); err != nil {
			return nil, err
		}

		badges := []string{}
		if isStarred > 0 {
			badges = append(badges, "favorited")
			favoriteCount++
		}
		if openedOriginal > 0 {
			badges = append(badges, "opened_original")
			originalCount++
		}

		articles = append(articles, TopicArticle{
			EntryID: entryID,
			Title:   title,
			Source:  source,
			Badges:  badges,
		})

		if publishedAt != "" {
			pubDateStr := publishedAt
			if len(publishedAt) > 10 {
				pubDateStr = publishedAt[:10]
			}
			pubDate, err := time.Parse("2006-01-02", pubDateStr)
			if err == nil {
				dayDiff := int(today.Sub(pubDate).Hours() / 24)
				if dayDiff < 0 {
					dayDiff = 0
				}
				weekIndex := 11 - (dayDiff / 7)
				if weekIndex >= 0 && weekIndex < 12 {
					weeklyTrend[weekIndex]++
				}
			}
		}
	}

	return &TopicDetail{
		Topic: topicName,
		Stats: TopicStats{
			ArticleCount:  len(articles),
			FavoriteCount: favoriteCount,
			OriginalCount: originalCount,
		},
		WeeklyTrend: weeklyTrend,
		Articles:    articles,
	}, nil
}

func scanEntries(rows *sql.Rows) ([]models.Entry, error) {
	entries := []models.Entry{}
	for rows.Next() {
		var e models.Entry
		var catID sql.NullInt64
		var catName sql.NullString
		var author, readAt, classifiedAt, starredAt sql.NullString

		err := rows.Scan(&e.ID, &e.FeedID, &catID, &e.Guid, &e.Title, &e.URL, &author, &e.PublishedAt, &e.FetchedAt, &e.RawContent,
			&e.Attention, &e.LikelyNoText, &e.FulltextReady, &e.IsRead, &readAt, &classifiedAt, &e.IsStarred, &starredAt,
			&e.FeedTitle, &catName)
		if err != nil {
			return nil, err
		}

		if catID.Valid {
			val := int(catID.Int64)
			e.CategoryID = &val
		}
		e.CategoryName = catName.String
		e.Author = author.String
		e.ReadAt = readAt.String
		e.ClassifiedAt = classifiedAt.String
		e.StarredAt = starredAt.String

		entries = append(entries, e)
	}
	return entries, nil
}

func GetEntriesForCategory(categoryID int, unreadOnly bool, limit, offset int) ([]models.Entry, error) {
	var whereClause string
	if unreadOnly {
		whereClause = "e.category_id = ? AND e.is_read = 0"
	} else {
		whereClause = "e.category_id = ?"
	}

	query := fmt.Sprintf(`
		SELECT e.id, e.feed_id, e.category_id, e.guid, e.title, e.url, e.author, e.published_at, e.fetched_at, e.raw_content,
		       e.attention, e.likely_no_text, e.fulltext_ready, e.is_read, e.read_at, e.classified_at, e.is_starred, e.starred_at,
		       f.title as feed_title, c.name as category_name
		FROM entries e
		JOIN feeds f ON f.id = e.feed_id
		LEFT JOIN categories c ON c.id = e.category_id
		WHERE %s
		ORDER BY e.published_at DESC, e.id DESC
		LIMIT ? OFFSET ?
	`, whereClause)

	rows, err := db.DB.Query(query, categoryID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return scanEntries(rows)
}

func GetEntriesForFeed(feedID int, unreadOnly bool, limit, offset int) ([]models.Entry, error) {
	var whereClause string
	if unreadOnly {
		whereClause = "e.feed_id = ? AND e.is_read = 0"
	} else {
		whereClause = "e.feed_id = ?"
	}

	query := fmt.Sprintf(`
		SELECT e.id, e.feed_id, e.category_id, e.guid, e.title, e.url, e.author, e.published_at, e.fetched_at, e.raw_content,
		       e.attention, e.likely_no_text, e.fulltext_ready, e.is_read, e.read_at, e.classified_at, e.is_starred, e.starred_at,
		       f.title as feed_title, c.name as category_name
		FROM entries e
		JOIN feeds f ON f.id = e.feed_id
		LEFT JOIN categories c ON c.id = e.category_id
		WHERE %s
		ORDER BY e.published_at DESC, e.id DESC
		LIMIT ? OFFSET ?
	`, whereClause)

	rows, err := db.DB.Query(query, feedID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return scanEntries(rows)
}

func UpdateEntryAttention(entryID int, attention string) error {
	_, err := db.DB.Exec("UPDATE entries SET attention = ? WHERE id = ?", attention, entryID)
	return err
}

// --- Token Usage ---

func RecordTokenUsage(promptTokens, completionTokens, totalTokens int) {
	dateStr := time.Now().Local().Format("2006-01-02")
	_, _ = db.DB.Exec(`
		INSERT INTO token_usage (date, prompt_tokens, completion_tokens, total_tokens)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(date) DO UPDATE SET
			prompt_tokens = prompt_tokens + excluded.prompt_tokens,
			completion_tokens = completion_tokens + excluded.completion_tokens,
			total_tokens = total_tokens + excluded.total_tokens
	`, dateStr, promptTokens, completionTokens, totalTokens)
}

type TokenStats struct {
	Date             string `json:"date"`
	PromptTokens     int    `json:"prompt_tokens"`
	CompletionTokens int    `json:"completion_tokens"`
	TotalTokens      int    `json:"total_tokens"`
}

func GetDailyTokenStats() (TokenStats, error) {
	dateStr := time.Now().Local().Format("2006-01-02")
	var stats TokenStats
	stats.Date = dateStr
	err := db.DB.QueryRow("SELECT prompt_tokens, completion_tokens, total_tokens FROM token_usage WHERE date = ?", dateStr).
		Scan(&stats.PromptTokens, &stats.CompletionTokens, &stats.TotalTokens)
	if err == sql.ErrNoRows {
		return stats, nil
	}
	return stats, err
}
