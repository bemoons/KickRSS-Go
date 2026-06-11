package services

import (
	"database/sql"
	"encoding/json"
	"log"
	"strings"
	"time"

	"kickrss/config"
	"kickrss/crud"
	"kickrss/db"
)

func RunMaintenanceForFeed(feedID int) ([]map[string]interface{}, error) {
	promoteThreshold := config.GlobalConfig.Classify.PromoteThreshold
	if promoteThreshold == 0 {
		promoteThreshold = 5
	}
	log.Printf("[Maintenance] Running maintenance for feed %d (threshold=%d)", feedID, promoteThreshold)

	feed, err := crud.GetFeedByID(feedID)
	if err != nil || feed == nil || feed.Enabled == 0 || feed.NeedClassification == 0 {
		return nil, nil
	}

	recentUncategorized, err := crud.GetRecentUncategorizedEntries(feedID, 14)
	if err != nil || len(recentUncategorized) == 0 {
		log.Printf("[Maintenance] No recent uncategorized entries for feed %d", feedID)
		return nil, nil
	}

	log.Printf("[Maintenance] Found %d recent uncategorized entries for feed %d", len(recentUncategorized), feedID)

	var entriesList []map[string]interface{}
	allowedIDs := make(map[int]bool)
	for _, row := range recentUncategorized {
		entriesList = append(entriesList, map[string]interface{}{
			"id":    row.ID,
			"title": row.Title,
		})
		allowedIDs[row.ID] = true
	}

	promotions, err := IdentifyPromotableTopics(entriesList, promoteThreshold)
	if err != nil {
		log.Printf("[Maintenance] AI identified no topics for promotion for feed %d: %s", feedID, err)
		return nil, err
	}

	var results []map[string]interface{}

	for _, promo := range promotions {
		categoryName := strings.TrimSpace(promo.CategoryName)
		if categoryName == "" || categoryName == "未归类" || len(promo.EntryIDs) == 0 {
			continue
		}

		// Filter valid IDs belonging to recent list
		var validIDs []int
		for _, eid := range promo.EntryIDs {
			var intID int
			switch v := eid.(type) {
			case float64:
				intID = int(v)
			case int:
				intID = v
			case int64:
				intID = int(v)
			}
			if allowedIDs[intID] {
				validIDs = append(validIDs, intID)
			}
		}

		if len(validIDs) < promoteThreshold {
			log.Printf("[Maintenance] Skipping promotion of '%s' because valid entry count (%d) is below threshold (%d)", categoryName, len(validIDs), promoteThreshold)
			continue
		}

		log.Printf("[Maintenance] Promoting category '%s' with %d entries for feed %d", categoryName, len(validIDs), feedID)

		// Save to DB and move entries
		var categoryID int
		err := db.DB.QueryRow("SELECT id FROM categories WHERE feed_id = ? AND name = ?", feedID, categoryName).Scan(&categoryID)
		if err == sql.ErrNoRows {
			now := time.Now().UTC().Format(time.RFC3339)
			res, errInsert := db.DB.Exec("INSERT INTO categories (feed_id, name, is_default, created_at) VALUES (?, ?, 0, ?)", feedID, categoryName, now)
			if errInsert != nil {
				log.Printf("[Maintenance] Failed to insert category '%s': %s", categoryName, errInsert)
				continue
			}
			lastID, _ := res.LastInsertId()
			categoryID = int(lastID)
		}

		moved, errMove := crud.MoveEntriesToCategory(validIDs, categoryID)
		if errMove != nil {
			log.Printf("[Maintenance] Failed to move entries to category: %s", errMove)
			continue
		}

		results = append(results, map[string]interface{}{
			"category_name": categoryName,
			"category_id":   categoryID,
			"moved_count":   moved,
		})
	}

	// Clean up duplicate and empty categories
	if err := MergeDuplicateCategories(feedID); err != nil {
		log.Printf("[Maintenance] Failed to merge duplicate categories for feed %d: %s", feedID, err)
	}
	if err := crud.CleanEmptyCategories(feedID); err != nil {
		log.Printf("[Maintenance] Failed to clean empty categories for feed %d: %s", feedID, err)
	}

	return results, nil
}

func MergeDuplicateCategories(feedID int) error {
	log.Printf("[Maintenance] Checking for duplicate categories to merge for feed %d", feedID)

	rows, err := db.DB.Query("SELECT id, name FROM categories WHERE feed_id = ? AND is_default = 0", feedID)
	if err != nil {
		return err
	}
	defer rows.Close()

	catMap := make(map[string]int)
	var categoryNames []string
	for rows.Next() {
		var id int
		var name string
		if err := rows.Scan(&id, &name); err == nil {
			catMap[name] = id
			categoryNames = append(categoryNames, name)
		}
	}

	if len(categoryNames) < 2 {
		return nil
	}

	merges, err := IdentifyDuplicateCategories(categoryNames)
	if err != nil {
		return err
	}

	for _, m := range merges {
		sourceName := strings.TrimSpace(m.Source)
		targetName := strings.TrimSpace(m.Target)

		sourceID, ok1 := catMap[sourceName]
		targetID, ok2 := catMap[targetName]

		if ok1 && ok2 && sourceID != targetID {
			log.Printf("[Maintenance] Merging category '%s' (id=%d) into '%s' (id=%d) for feed %d", sourceName, sourceID, targetName, targetID, feedID)
			
			tx, errTx := db.DB.Begin()
			if errTx != nil {
				return errTx
			}
			// 1. Move entries
			_, errTx = tx.Exec("UPDATE entries SET category_id = ? WHERE category_id = ?", targetID, sourceID)
			if errTx != nil {
				tx.Rollback()
				return errTx
			}
			// 2. Delete source category
			_, errTx = tx.Exec("DELETE FROM categories WHERE id = ?", sourceID)
			if errTx != nil {
				tx.Rollback()
				return errTx
			}
			tx.Commit()

			// Update map
			delete(catMap, sourceName)
		}
	}

	return nil
}

func BuildUserInterestProfile() error {
	if !config.GlobalConfig.InterestProfileEnabled {
		log.Printf("[Maintenance] Reading profile function is disabled. Skipping LLM interest profile builder.")
		return nil
	}

	log.Printf("[Maintenance] Building user interest profile...")

	query := `
		SELECT
			e.id AS entry_id,
			e.title,
			e.attention AS ai_attention,
			e.published_at,
			f.title AS feed_name,
			g.active_dwell_ms,
			g.scrolled_pct,
			g.scrolled_to_bottom,
			g.opened_original,
			g.favorited,
			g.manual_bump
		FROM entries e
		JOIN engagement g ON g.entry_id = e.id
		JOIN feeds f ON f.id = e.feed_id
		WHERE e.fetched_at > datetime('now', '-30 days')
	`
	rows, err := db.DB.Query(query)
	if err != nil {
		return err
	}
	defer rows.Close()

	var engagementList []map[string]interface{}
	for rows.Next() {
		var entryID, activeDwellMs, scrolledToBottom, openedOriginal, favorited int
		var title, aiAttention, publishedAt, feedName string
		var scrolledPct float64
		var manualBump sql.NullString

		if err := rows.Scan(&entryID, &title, &aiAttention, &publishedAt, &feedName, &activeDwellMs, &scrolledPct, &scrolledToBottom, &openedOriginal, &favorited, &manualBump); err != nil {
			log.Printf("[Maintenance] Failed to scan engagement row: %s", err)
			continue
		}

		bumpVal := ""
		if manualBump.Valid {
			bumpVal = manualBump.String
		}

		engagementList = append(engagementList, map[string]interface{}{
			"entry_id":           entryID,
			"title":              title,
			"ai_attention":       aiAttention,
			"published_at":       publishedAt,
			"feed_name":          feedName,
			"active_dwell_ms":    activeDwellMs,
			"scrolled_pct":       scrolledPct,
			"scrolled_to_bottom": scrolledToBottom,
			"opened_original":    openedOriginal,
			"favorited":          favorited,
			"manual_bump":        bumpVal,
		})
	}

	if len(engagementList) < 15 {
		log.Printf("[Maintenance] Not enough engagement data (%d articles < 15). Skipping LLM interest profile builder.", len(engagementList))
		return nil
	}

	snapshot, err := AggregateUserInterestsSnapshot(engagementList)
	if err != nil {
		return err
	}

	now := time.Now().UTC().Format(time.RFC3339)
	todayStr := time.Now().Format("2006-01-02")

	// Calculate high/low engagement metrics for stats
	highEng := 0
	lowEng := 0
	for _, eng := range engagementList {
		dwell := eng["active_dwell_ms"].(int)
		scrolled := eng["scrolled_pct"].(float64)
		opened := eng["opened_original"].(int)
		fav := eng["favorited"].(int)

		if dwell > 45000 || scrolled > 0.8 || opened == 1 || fav == 1 {
			highEng++
		} else if dwell < 10000 && scrolled < 0.3 {
			lowEng++
		}
	}

	topicsMap := map[string]interface{}{
		"high_interest":      snapshot.HighInterest,
		"low_interest":       snapshot.LowInterest,
		"concentration_note": snapshot.ConcentrationNote,
	}
	topicsBytes, _ := json.Marshal(topicsMap)

	_, err = db.DB.Exec(`
		INSERT INTO user_interests (snapshot_date, total_articles, high_engagement, low_engagement, topics_json, prompt_text, generated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(snapshot_date) DO UPDATE SET
			total_articles = excluded.total_articles,
			high_engagement = excluded.high_engagement,
			low_engagement = excluded.low_engagement,
			topics_json = excluded.topics_json,
			prompt_text = excluded.prompt_text,
			generated_at = excluded.generated_at
	`, todayStr, len(engagementList), highEng, lowEng, string(topicsBytes), snapshot.AttentionGuide, now)

	if err != nil {
		return err
	}

	log.Println("[Maintenance] Successfully updated user interest profile snapshot in database.")
	return nil
}

func RunAllFeedsMaintenance() (map[int][]map[string]interface{}, error) {
	log.Println("[Maintenance] Running daily maintenance job for all feeds")
	
	// 1. Reset classification for all uncategorized entries
	if err := ResetUncategorizedEntriesClassification(); err != nil {
		log.Printf("[Maintenance] Failed to reset uncategorized entries: %s", err)
	}

	feeds, err := crud.ListFeeds()
	if err != nil {
		return nil, err
	}

	// 2. Re-classify those entries first so they map to existing drawers if possible
	for _, feed := range feeds {
		if feed.Enabled == 1 && feed.NeedClassification == 1 {
			// Ensure the feed is seeded first (if LLM wasn't configured previously)
			if feed.Seeded == 0 {
				if errSeed := EnsureFeedSeeded(feed.ID); errSeed != nil {
					log.Printf("[Maintenance] Auto-seeding failed for feed %d: %s", feed.ID, errSeed)
				}
			}
			ClassifyFeedEntries(feed.ID)
		}
	}

	// 3. Perform topic promotion, merging and cleaning
	report := make(map[int][]map[string]interface{})
	for _, feed := range feeds {
		if feed.Enabled == 0 || feed.NeedClassification == 0 {
			continue
		}
		res, err := RunMaintenanceForFeed(feed.ID)
		if err != nil {
			log.Printf("[Maintenance] Maintenance job failed for feed %d: %s", feed.ID, err)
			continue
		}
		if len(res) > 0 {
			report[feed.ID] = res
		}
	}

	// 4. Generate user interest profile
	if err := BuildUserInterestProfile(); err != nil {
		log.Printf("[Maintenance] Failed to build user interest profile: %s", err)
	}

	// 5. Clean up old user interests snapshot records (older than 90 days)
	_, err = db.DB.Exec("DELETE FROM user_interests WHERE snapshot_date < date('now', '-90 days')")
	if err != nil {
		log.Printf("[Maintenance] Failed to clean up old snapshots: %s", err)
	} else {
		log.Println("[Maintenance] Successfully cleaned up old user interests snapshot records.")
	}

	return report, nil
}

// Reset classification for all uncategorized entries back to NULL so classifier can re-run
func ResetUncategorizedEntriesClassification() error {
	_, err := db.DB.Exec(`
		UPDATE entries
		SET classified_at = NULL
		WHERE category_id IN (SELECT id FROM categories WHERE is_default = 1)
	`)
	return err
}
