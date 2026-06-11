package routers

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"kickrss/config"
	"kickrss/crud"
	"kickrss/db"
	"kickrss/models"
	"kickrss/scheduler"
	"kickrss/services"

	"github.com/gin-gonic/gin"
)

func SetupRouter() *gin.Engine {
	r := gin.Default()

	// CORS middleware if needed (optional since frontend is served in-place)
	r.Use(func(c *gin.Context) {
		c.Writer.Header().Set("Access-Control-Allow-Origin", "*")
		c.Writer.Header().Set("Access-Control-Allow-Credentials", "true")
		c.Writer.Header().Set("Access-Control-Allow-Headers", "Content-Type, Content-Length, Accept-Encoding, X-CSRF-Token, Authorization, accept, origin, Cache-Control, X-Requested-With")
		c.Writer.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS, GET, PUT, DELETE")

		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}
		c.Next()
	})

	// Static files serving via SPA fallback routing
	r.NoRoute(func(c *gin.Context) {
		path := c.Request.URL.Path
		filePath := filepath.Join("static", path)
		if fileInfo, err := os.Stat(filePath); err == nil && !fileInfo.IsDir() {
			c.File(filePath)
			return
		}
		c.File("static/index.html")
	})

	// Settings Routes
	r.GET("/settings", getSettings)
	r.PUT("/settings", updateSettings)
	r.POST("/settings/test-llm", testLLMConnection)
	r.GET("/settings/token-stats", getTokenStats)

	// Feeds Routes
	r.GET("/feeds", listFeeds)
	r.POST("/feeds", addFeed)
	r.PUT("/feeds/:feed_id", updateFeed)
	r.DELETE("/feeds/:feed_id", deleteFeed)
	r.GET("/feeds/:feed_id/categories", getFeedCategories)
	r.POST("/feeds/:feed_id/reset-categories", resetSingleFeedCategories)
	r.POST("/feeds/reset-categories", resetAllFeedsCategories)
	r.GET("/feeds/:feed_id/entries", getFeedEntries)
	r.POST("/feeds/:feed_id/read", readFeed)

	// Categories Routes
	r.GET("/categories/:category_id/entries", getCategoryEntries)
	r.POST("/categories/:category_id/read", readCategory)

	// Entries Routes
	r.GET("/entries/unread", getUnreadEntries)
	r.GET("/entries/starred", getStarredEntries)
	r.GET("/entries/starred/count", getStarredEntriesCount)
	r.GET("/entries/notes", getNotesEntries)
	r.GET("/entries/notes/count", getNotesEntriesCount)
	r.POST("/entries/read", markMultipleEntriesRead)
	r.POST("/entries/unread", markMultipleEntriesUnread)
	r.GET("/entries/:entry_id", getEntryDetails)
	r.GET("/entries/:entry_id/fulltext", getEntryFulltext)
	r.POST("/entries/:entry_id/read", setEntryReadStatus)
	r.POST("/entries/:entry_id/star", setEntryStarredStatus)
	r.POST("/entries/:entry_id/unstar", setEntryUnstarredStatus)
	r.GET("/entries/:entry_id/summary", getEntrySummary)
	r.POST("/entries/:entry_id/chat", chatWithEntry)
	r.POST("/entries/:entry_id/chat/clear", clearEntryChat)
	r.DELETE("/entries/:entry_id/chat", clearEntryChat)
	r.POST("/entries/:entry_id/unread", setEntryUnreadStatus)
	r.POST("/entries/:entry_id/attention", updateEntryAttention)
	r.GET("/entries/:entry_id/translate", getEntryTranslation)
	r.POST("/entries/:entry_id/translate_paragraph", translateEntryParagraph)
	r.GET("/entries/:entry_id/chat", getEntryChatHistory)
	r.POST("/entries/:entry_id/engagement", recordEngagement)
	r.POST("/entries/:entry_id/favorite", toggleFavorite)

	// OPML Routes
	r.POST("/import/opml", importOPML)
	r.GET("/export/opml", exportOPML)

	// Maintenance & Refresh
	r.POST("/maintenance", triggerMaintenance)
	r.POST("/refresh", triggerRefresh)

	// Profile & Health Routes
	r.GET("/profile/interests", getInterestProfile)
	r.GET("/profile/topic-detail", getTopicDetail)
	r.GET("/healthz", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	return r
}

// --- Handlers ---

func getSettings(c *gin.Context) {
	c.JSON(http.StatusOK, services.GetSettings())
}

type SettingsUpdateBody struct {
	FetchIntervalMinutes   *int    `json:"fetch_interval_minutes"`
	MinTextChars           *int    `json:"min_text_chars"`
	PromoteThreshold       *int    `json:"promote_threshold"`
	AIBaseURL              *string `json:"ai_base_url"`
	AIAPIKey               *string `json:"ai_api_key"`
	AIModel                *string `json:"ai_model"`
	AIPregenerate          *bool   `json:"ai_pregenerate"`
	AIStream               *bool   `json:"ai_stream"`
	AIAutoSummary          *bool   `json:"ai_auto_summary"`
	AISummaryLength        *string `json:"ai_summary_length"`
	AISummaryLang          *string `json:"ai_summary_lang"`
	SystemLang             *string `json:"system_lang"`
	ChatBaseURL            *string `json:"chat_base_url"`
	ChatAPIKey             *string `json:"chat_api_key"`
	ChatModel              *string `json:"chat_model"`
	ChatMaxTokens          *int    `json:"chat_max_tokens"`
	InterestProfileEnabled *bool   `json:"interest_profile_enabled"`
}

func updateSettings(c *gin.Context) {
	var body SettingsUpdateBody
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": err.Error()})
		return
	}

	services.UpdateSettings(
		body.FetchIntervalMinutes,
		body.MinTextChars,
		body.PromoteThreshold,
		body.AIBaseURL,
		body.AIAPIKey,
		body.AIModel,
		body.AIPregenerate,
		body.AIStream,
		body.AIAutoSummary,
		body.AISummaryLength,
		body.AISummaryLang,
		body.SystemLang,
		body.ChatBaseURL,
		body.ChatAPIKey,
		body.ChatModel,
		body.ChatMaxTokens,
		body.InterestProfileEnabled,
	)

	if body.FetchIntervalMinutes != nil {
		scheduler.RescheduleRefreshJob(*body.FetchIntervalMinutes)
	}

	c.JSON(http.StatusOK, services.GetSettings())
}

func listFeeds(c *gin.Context) {
	feeds, err := crud.ListFeeds()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": err.Error()})
		return
	}
	c.JSON(http.StatusOK, feeds)
}

type AddFeedBody struct {
	URL string `json:"url" binding:"required"`
}

func addFeed(c *gin.Context) {
	var body AddFeedBody
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": err.Error()})
		return
	}

	url := strings.TrimSpace(body.URL)
	if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "Invalid feed URL scheme (must be http or https)"})
		return
	}

	existing, err := crud.GetFeedByURL(url)
	if err == nil && existing != nil {
		c.JSON(http.StatusOK, existing)
		return
	}

	id, err := crud.AddFeed(url, url)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": err.Error()})
		return
	}

	feed, _ := crud.GetFeedByID(int(id))

	// Run cold start in background
	go services.AsyncColdStartFeed(int(id))

	c.JSON(http.StatusOK, feed)
}

type UpdateFeedBody struct {
	Title              *string `json:"title"`
	Enabled            *bool   `json:"enabled"`
	NeedClassification *bool   `json:"need_classification"`
}

func updateFeed(c *gin.Context) {
	feedIDStr := c.Param("feed_id")
	feedID, err := strconv.Atoi(feedIDStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "Invalid feed_id"})
		return
	}

	var body UpdateFeedBody
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": err.Error()})
		return
	}

	feed, err := crud.GetFeedByID(feedID)
	if err != nil || feed == nil {
		c.JSON(http.StatusNotFound, gin.H{"detail": "Feed not found"})
		return
	}

	if body.Title != nil {
		_ = crud.UpdateFeedTitle(feedID, strings.TrimSpace(*body.Title))
	}
	if body.Enabled != nil {
		_ = crud.UpdateFeedEnabled(feedID, *body.Enabled)
	}
	if body.NeedClassification != nil {
		_ = crud.UpdateFeedNeedClassification(feedID, *body.NeedClassification)
	}

	updated, _ := crud.GetFeedByID(feedID)
	c.JSON(http.StatusOK, updated)
}

func deleteFeed(c *gin.Context) {
	feedIDStr := c.Param("feed_id")
	feedID, err := strconv.Atoi(feedIDStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "Invalid feed_id"})
		return
	}

	deleted, err := crud.DeleteFeed(feedID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": err.Error()})
		return
	}

	if !deleted {
		c.JSON(http.StatusNotFound, gin.H{"detail": "Feed not found"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func getFeedCategories(c *gin.Context) {
	feedIDStr := c.Param("feed_id")
	feedID, err := strconv.Atoi(feedIDStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "Invalid feed_id"})
		return
	}

	categories, err := crud.GetCategoriesForFeed(feedID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": err.Error()})
		return
	}

	c.JSON(http.StatusOK, categories)
}

func resetSingleFeedCategories(c *gin.Context) {
	feedIDStr := c.Param("feed_id")
	feedID, err := strconv.Atoi(feedIDStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "Invalid feed_id"})
		return
	}

	feed, err := crud.GetFeedByID(feedID)
	if err != nil || feed == nil {
		c.JSON(http.StatusNotFound, gin.H{"detail": "Feed not found"})
		return
	}

	// Trigger seeding synchronously in step 1, classification in background
	_, err = services.SyncResetFeedCategoriesStep1(feedID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": err.Error()})
		return
	}

	go services.ClassifyFeedEntries(feedID)

	c.JSON(http.StatusOK, gin.H{
		"ok":      true,
		"message": fmt.Sprintf("Category reset and seeding completed for feed %d. Classification enqueued in background.", feedID),
	})
}

func resetAllFeedsCategories(c *gin.Context) {
	feeds, err := crud.ListFeeds()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": err.Error()})
		return
	}

	count := 0
	for _, feed := range feeds {
		if feed.Enabled == 1 {
			go services.AsyncResetFeedCategories(feed.ID)
			count++
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"ok":      true,
		"message": fmt.Sprintf("Triggered category reset for %d feeds.", count),
	})
}

// --- Entries Handlers ---

func getUnreadEntries(c *gin.Context) {
	feedIDStr := c.Query("feed_id")
	categoryIDStr := c.Query("category_id")
	limitStr := c.DefaultQuery("limit", "50")
	offsetStr := c.DefaultQuery("offset", "0")

	limit, _ := strconv.Atoi(limitStr)
	offset, _ := strconv.Atoi(offsetStr)

	var queryParts []string
	var args []interface{}

	queryParts = append(queryParts, "e.is_read = 0")

	if feedIDStr != "" {
		if feedID, err := strconv.Atoi(feedIDStr); err == nil {
			queryParts = append(queryParts, "e.feed_id = ?")
			args = append(args, feedID)
		}
	}

	if categoryIDStr != "" {
		if categoryID, err := strconv.Atoi(categoryIDStr); err == nil {
			queryParts = append(queryParts, "e.category_id = ?")
			args = append(args, categoryID)
		}
	}

	whereClause := strings.Join(queryParts, " AND ")
	query := fmt.Sprintf(`
		SELECT e.id, e.feed_id, e.category_id, e.guid, e.title, e.url, e.author, e.published_at, e.fetched_at, e.raw_content,
		       e.attention, e.likely_no_text, e.fulltext_ready, e.is_read, e.read_at, e.classified_at, e.is_starred, e.starred_at,
		       f.title as feed_title, c.name as category_name
		FROM entries e
		JOIN feeds f ON f.id = e.feed_id
		LEFT JOIN categories c ON c.id = e.category_id
		WHERE %s
		ORDER BY e.published_at DESC
		LIMIT ? OFFSET ?
	`, whereClause)

	args = append(args, limit, offset)

	rows, err := db.DB.Query(query, args...)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": err.Error()})
		return
	}
	defer rows.Close()

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
			c.JSON(http.StatusInternalServerError, gin.H{"detail": err.Error()})
			return
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

	c.JSON(http.StatusOK, entries)
}

func getStarredEntries(c *gin.Context) {
	limitStr := c.DefaultQuery("limit", "50")
	offsetStr := c.DefaultQuery("offset", "0")

	limit, _ := strconv.Atoi(limitStr)
	offset, _ := strconv.Atoi(offsetStr)

	query := `
		SELECT e.id, e.feed_id, e.category_id, e.guid, e.title, e.url, e.author, e.published_at, e.fetched_at, e.raw_content,
		       e.attention, e.likely_no_text, e.fulltext_ready, e.is_read, e.read_at, e.classified_at, e.is_starred, e.starred_at,
		       f.title as feed_title, c.name as category_name
		FROM entries e
		JOIN feeds f ON f.id = e.feed_id
		LEFT JOIN categories c ON c.id = e.category_id
		WHERE e.is_starred = 1
		ORDER BY e.starred_at DESC, e.published_at DESC
		LIMIT ? OFFSET ?
	`

	rows, err := db.DB.Query(query, limit, offset)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": err.Error()})
		return
	}
	defer rows.Close()

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
			c.JSON(http.StatusInternalServerError, gin.H{"detail": err.Error()})
			return
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

	c.JSON(http.StatusOK, entries)
}

func getStarredEntriesCount(c *gin.Context) {
	var count int
	err := db.DB.QueryRow("SELECT COUNT(*) FROM entries WHERE is_starred = 1").Scan(&count)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"total_count": count})
}

func getEntryDetails(c *gin.Context) {
	entryIDStr := c.Param("entry_id")
	entryID, err := strconv.Atoi(entryIDStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "Invalid entry_id"})
		return
	}

	entry, err := crud.GetEntryByID(entryID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": err.Error()})
		return
	}

	if entry == nil {
		c.JSON(http.StatusNotFound, gin.H{"detail": "Entry not found"})
		return
	}

	c.JSON(http.StatusOK, entry)
}

type ReadEntriesBody struct {
	IDs []int `json:"ids" binding:"required"`
}

func markMultipleEntriesRead(c *gin.Context) {
	var body ReadEntriesBody
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": err.Error()})
		return
	}

	count, err := crud.SetEntriesReadStatus(body.IDs, true)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"ok": true, "count": count, "ids": body.IDs})
}

func markMultipleEntriesUnread(c *gin.Context) {
	var body ReadEntriesBody
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": err.Error()})
		return
	}

	count, err := crud.SetEntriesReadStatus(body.IDs, false)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"ok": true, "count": count})
}

func setEntryReadStatus(c *gin.Context) {
	entryIDStr := c.Param("entry_id")
	entryID, err := strconv.Atoi(entryIDStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "Invalid entry_id"})
		return
	}

	if err := crud.SetEntryReadStatus(entryID, true); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func setEntryStarredStatus(c *gin.Context) {
	entryIDStr := c.Param("entry_id")
	entryID, err := strconv.Atoi(entryIDStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "Invalid entry_id"})
		return
	}

	if err := crud.SetEntryStarredStatus(entryID, true); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func setEntryUnstarredStatus(c *gin.Context) {
	entryIDStr := c.Param("entry_id")
	entryID, err := strconv.Atoi(entryIDStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "Invalid entry_id"})
		return
	}

	if err := crud.SetEntryStarredStatus(entryID, false); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func getEntrySummary(c *gin.Context) {
	entryIDStr := c.Param("entry_id")
	entryID, err := strconv.Atoi(entryIDStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "Invalid entry_id"})
		return
	}

	streamQuery := c.Query("stream")
	forceQuery := c.Query("force")
	cacheOnly := c.Query("cache_only") == "true"

	stream := config.GlobalConfig.AI.Stream
	if streamQuery != "" {
		stream = streamQuery == "true"
	}
	force := forceQuery == "true"

	entry, err := crud.GetEntryByID(entryID)
	if err != nil || entry == nil {
		c.JSON(http.StatusNotFound, gin.H{"detail": "Entry not found"})
		return
	}

	if force {
		_ = crud.DeleteSummary(entryID)
	} else {
		summaryRow, _ := crud.GetEntrySummary(entryID)
		if summaryRow != nil {
			if stream {
				c.Header("Content-Type", "text/event-stream")
				c.Header("Cache-Control", "no-cache")
				c.Header("Connection", "keep-alive")
				c.Header("Transfer-Encoding", "chunked")
				c.Stream(func(w io.Writer) bool {
					if summaryRow.ClickbaitNote != "" {
						payload, _ := json.Marshal(gin.H{"summary": "", "clickbait_note": summaryRow.ClickbaitNote, "status": "streaming"})
						fmt.Fprintf(w, "data: %s\n\n", string(payload))
						c.Writer.Flush()
					}
					payloadSummary, _ := json.Marshal(gin.H{"summary": summaryRow.Content, "clickbait_note": nil, "status": "streaming"})
					fmt.Fprintf(w, "data: %s\n\n", string(payloadSummary))
					c.Writer.Flush()
					
					payloadDone, _ := json.Marshal(gin.H{"summary": "", "clickbait_note": nil, "status": "done"})
					fmt.Fprintf(w, "data: %s\n\n", string(payloadDone))
					c.Writer.Flush()
					return false
				})
				return
			} else {
				c.JSON(http.StatusOK, gin.H{
					"summary":        summaryRow.Content,
					"clickbait_note": summaryRow.ClickbaitNote,
					"status":         "ok",
				})
				return
			}
		}
	}

	if cacheOnly {
		c.JSON(http.StatusOK, gin.H{
			"summary":        "",
			"clickbait_note": nil,
			"status":         "no_cache",
		})
		return
	}

	// Fetch fulltext or extract it
	ftRow, _ := crud.GetEntryFulltext(entryID)
	var ftText string
	var ftStatus string

	if ftRow == nil {
		ftText = crud.CleanHTML(entry.RawContent)
		ftStatus = "ok"
		_ = crud.SaveFulltext(entryID, ftText, ftStatus, "feed")
	} else {
		ftText = ftRow.Content
		ftStatus = ftRow.Status
	}

	minChars := config.GlobalConfig.Fulltext.MinTextChars
	if minChars == 0 {
		minChars = 200
	}

	if ftStatus != "ok" || ftText == "" || len([]rune(ftText)) < minChars {
		noTextMsg := "此文主要为视频/图片，无正文可总结。"
		_ = crud.SaveSummary(entryID, noTextMsg, "", "system")

		if stream {
			c.Header("Content-Type", "text/event-stream")
			c.Header("Cache-Control", "no-cache")
			c.Header("Connection", "keep-alive")
			c.Stream(func(w io.Writer) bool {
				payload, _ := json.Marshal(gin.H{"summary": noTextMsg, "clickbait_note": nil, "status": "no_text"})
				fmt.Fprintf(w, "data: %s\n\n", string(payload))
				c.Writer.Flush()
				return false
			})
			return
		} else {
			c.JSON(http.StatusOK, gin.H{
				"summary":        noTextMsg,
				"clickbait_note": nil,
				"status":         "no_text",
			})
			return
		}
	}

	cleanLen := services.EstimateCleanTextLength(ftText)
	targetChars := cleanLen / 10
	if targetChars < 100 {
		targetChars = 100
	}
	if targetChars > 900 {
		targetChars = 900
	}

	if stream {
		c.Header("Content-Type", "text/event-stream")
		c.Header("Cache-Control", "no-cache")
		c.Header("Connection", "keep-alive")
		c.Header("Transfer-Encoding", "chunked")

		c.Stream(func(w io.Writer) bool {
			aiStreamEnabled := config.GlobalConfig.AI.Stream

			if aiStreamEnabled {
				targetLang := config.GlobalConfig.AI.SummaryLanguage
				if targetLang == "auto" || targetLang == "" {
					targetLang = config.GlobalConfig.SystemLanguage
				}
				if targetLang == "" {
					targetLang = "zh"
				}
				engName, localName, chnName := services.GetLanguageNames(targetLang)

				// Construct prompt
				prompt := fmt.Sprintf(`请为以下文章生成摘要。要求：
1. 必须使用 %s (%s) 以无序列表（Markdown Bullet Points，以 "- " 开头）的形式输出 3 到 5 条核心要点。
2. 语言需简明扼要，直达核心。
3. 突出重点：选择性地将最重要的结论、核心词句或关键数据加粗（使用 **加粗文本**），让读者能一眼扫视出文章的精髓，但注意不要过度加粗。
4. 动态控制摘要总字数在 %d 字以内。
5. 【重要】如果文章标题存在夸大其词、标题党、或者标题结论与正文事实严重不符的情况，请在摘要的最后添加一行以 "【标题警告】" 开头的提示（提示内容使用 %s (%s)，但前缀固定为 "【标题警告】"），指出标题中与事实不符或夸大的点。若无此情况，则绝对不写任何标题警告内容。
6. 【重要】必须使用 %s (%s) 撰写上述摘要，绝对不要使用原文语言。`, chnName, localName, targetChars, chnName, localName, engName, localName)

				messages := []services.ChatMessage{
					{Role: "system", Content: prompt},
					{Role: "user", Content: fmt.Sprintf("文章标题: %s\n文章链接: %s\n文章正文内容: %s", entry.Title, entry.URL, ftText)},
				}

				respStream, err := services.CallChatCompletionStream(messages, "summary")
				if err != nil {
					payload, _ := json.Marshal(gin.H{"summary": "", "clickbait_note": nil, "status": "error", "detail": err.Error()})
					fmt.Fprintf(w, "data: %s\n\n", string(payload))
					c.Writer.Flush()
					return false
				}
				defer respStream.Body.Close()

				summaryFull := ""
				errRead := services.ReadSSEResponse(respStream, func(chunk string) error {
					summaryFull += chunk
					payload, _ := json.Marshal(gin.H{"summary": chunk, "clickbait_note": nil, "status": "streaming"})
					fmt.Fprintf(w, "data: %s\n\n", string(payload))
					c.Writer.Flush()
					return nil
				})

				if errRead != nil {
					payload, _ := json.Marshal(gin.H{"summary": "", "clickbait_note": nil, "status": "error", "detail": errRead.Error()})
					fmt.Fprintf(w, "data: %s\n\n", string(payload))
					c.Writer.Flush()
					return false
				}

				// Parse response to save to DB
				summaryText, clickbait := services.ParseAISummaryResponse(summaryFull)
				model := config.GlobalConfig.AI.Default.Model
				_ = crud.SaveSummary(entryID, summaryText, clickbait, model)

				if clickbait != "" {
					// Resend parsed warning
					payload, _ := json.Marshal(gin.H{"summary": "", "clickbait_note": clickbait, "status": "streaming"})
					fmt.Fprintf(w, "data: %s\n\n", string(payload))
					c.Writer.Flush()
				}

				payloadDone, _ := json.Marshal(gin.H{"summary": "", "clickbait_note": nil, "status": "done"})
				fmt.Fprintf(w, "data: %s\n\n", string(payloadDone))
				c.Writer.Flush()
			} else {
				// Non-streaming summary inside stream
				rawSummary, err := services.GenerateSummarySync(entry.Title, entry.URL, ftText, targetChars, config.GlobalConfig.AI.SummaryLanguage)
				if err != nil {
					payload, _ := json.Marshal(gin.H{"summary": "", "clickbait_note": nil, "status": "error", "detail": err.Error()})
					fmt.Fprintf(w, "data: %s\n\n", string(payload))
					c.Writer.Flush()
					return false
				}

				summaryText, clickbait := services.ParseAISummaryResponse(rawSummary)
				model := config.GlobalConfig.AI.Default.Model
				_ = crud.SaveSummary(entryID, summaryText, clickbait, model)

				if clickbait != "" {
					payload, _ := json.Marshal(gin.H{"summary": "", "clickbait_note": clickbait, "status": "streaming"})
					fmt.Fprintf(w, "data: %s\n\n", string(payload))
					c.Writer.Flush()
				}

				payload, _ := json.Marshal(gin.H{"summary": summaryText, "clickbait_note": nil, "status": "streaming"})
				fmt.Fprintf(w, "data: %s\n\n", string(payload))
				c.Writer.Flush()

				payloadDone, _ := json.Marshal(gin.H{"summary": "", "clickbait_note": nil, "status": "done"})
				fmt.Fprintf(w, "data: %s\n\n", string(payloadDone))
				c.Writer.Flush()
			}
			return false
		})
	} else {
		rawSummary, err := services.GenerateSummarySync(entry.Title, entry.URL, ftText, targetChars, config.GlobalConfig.AI.SummaryLanguage)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"detail": err.Error()})
			return
		}

		summaryText, clickbait := services.ParseAISummaryResponse(rawSummary)
		model := config.GlobalConfig.AI.Default.Model
		_ = crud.SaveSummary(entryID, summaryText, clickbait, model)

		c.JSON(http.StatusOK, gin.H{
			"summary":        summaryText,
			"clickbait_note": clickbait,
			"status":         "ok",
		})
	}
}

type ChatRequestBody struct {
	Message string `json:"message" binding:"required"`
}

func chatWithEntry(c *gin.Context) {
	entryIDStr := c.Param("entry_id")
	entryID, err := strconv.Atoi(entryIDStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "Invalid entry_id"})
		return
	}

	var body ChatRequestBody
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": err.Error()})
		return
	}

	userMsg := strings.TrimSpace(body.Message)
	if userMsg == "" {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "Empty message"})
		return
	}

	entry, err := crud.GetEntryByID(entryID)
	if err != nil || entry == nil {
		c.JSON(http.StatusNotFound, gin.H{"detail": "Entry not found"})
		return
	}

	// Fetch fulltext
	ftRow, _ := crud.GetEntryFulltext(entryID)
	ftText := ""
	if ftRow != nil {
		ftText = ftRow.Content
	} else {
		ftText = crud.CleanHTML(entry.RawContent)
	}

	// Load chat history
	history, _ := crud.GetChatMessages(entryID)

	// Save user message
	_ = crud.SaveChatMessage(entryID, "user", userMsg)

	targetLang := config.GlobalConfig.AI.SummaryLanguage
	if targetLang == "auto" || targetLang == "" {
		targetLang = config.GlobalConfig.SystemLanguage
	}
	if targetLang == "" {
		targetLang = "zh"
	}

	var langRule string
	if targetLang == "auto" {
		langRule = "请使用与用户提问相同的语言（或原文语言）来回答用户。"
	} else {
		_, localName, chnName := services.GetLanguageNames(targetLang)
		langRule = fmt.Sprintf("【语言硬性要求】\n你必须且只能使用 %s (%s) 来回答用户的问题。无论原文是用何种语言编写，也无论用户用何种语言提问，你的回复语言必须完全是 %s (%s)。", chnName, localName, chnName, localName)
	}

	// Construct system prompt
	systemPrompt := fmt.Sprintf(`你是一个专业的 RSS 阅读助手。请基于以下提供的文章内容，回答用户的问题。
你必须仅依据文章的事实进行专业、深入、客观的解答，如果有任何论点超出文章事实，请明确指出“根据原文未提及”。
请使用 Markdown 格式排版你的回答。

%s

文章标题: %s
文章原文内容:
---
%s
---`, langRule, entry.Title, ftText)

	var messages []services.ChatMessage
	messages = append(messages, services.ChatMessage{Role: "system", Content: systemPrompt})

	// Add history (up to last 15 rounds to keep tokens small)
	maxHistory := 30
	startIdx := 0
	if len(history) > maxHistory {
		startIdx = len(history) - maxHistory
	}
	for _, h := range history[startIdx:] {
		messages = append(messages, services.ChatMessage{Role: h.Role, Content: h.Content})
	}

	// Add current message
	messages = append(messages, services.ChatMessage{Role: "user", Content: userMsg})

	streamQuery := c.Query("stream")
	stream := config.GlobalConfig.AI.Stream
	if streamQuery != "" {
		stream = streamQuery == "true"
	}

	if stream {
		c.Header("Content-Type", "text/event-stream")
		c.Header("Cache-Control", "no-cache")
		c.Header("Connection", "keep-alive")
		c.Header("Transfer-Encoding", "chunked")

		c.Stream(func(w io.Writer) bool {
			aiStreamEnabled := config.GlobalConfig.AI.Stream

			if aiStreamEnabled {
				respStream, err := services.CallChatCompletionStream(messages, "chat")
				if err != nil {
					payload, _ := json.Marshal(gin.H{"reply": "", "status": "error", "detail": err.Error()})
					fmt.Fprintf(w, "data: %s\n\n", string(payload))
					c.Writer.Flush()
					return false
				}
				defer respStream.Body.Close()

				assistantFull := ""
				errRead := services.ReadSSEResponse(respStream, func(chunk string) error {
					assistantFull += chunk
					payload, _ := json.Marshal(gin.H{"reply": chunk, "status": "streaming"})
					fmt.Fprintf(w, "data: %s\n\n", string(payload))
					c.Writer.Flush()
					return nil
				})

				if errRead != nil {
					payload, _ := json.Marshal(gin.H{"reply": "", "status": "error", "detail": errRead.Error()})
					fmt.Fprintf(w, "data: %s\n\n", string(payload))
					c.Writer.Flush()
					return false
				}

				// Save assistant message to DB
				_ = crud.SaveChatMessage(entryID, "assistant", assistantFull)

				payloadDone, _ := json.Marshal(gin.H{"reply": "", "status": "done"})
				fmt.Fprintf(w, "data: %s\n\n", string(payloadDone))
				c.Writer.Flush()
			} else {
				// Non-streaming inside stream response
				assistantMsg, err := services.CallChatCompletion(messages, "chat", false)
				if err != nil {
					payload, _ := json.Marshal(gin.H{"reply": "", "status": "error", "detail": err.Error()})
					fmt.Fprintf(w, "data: %s\n\n", string(payload))
					c.Writer.Flush()
					return false
				}

				_ = crud.SaveChatMessage(entryID, "assistant", assistantMsg)

				payload, _ := json.Marshal(gin.H{"reply": assistantMsg, "status": "streaming"})
				fmt.Fprintf(w, "data: %s\n\n", string(payload))
				c.Writer.Flush()

				payloadDone, _ := json.Marshal(gin.H{"reply": "", "status": "done"})
				fmt.Fprintf(w, "data: %s\n\n", string(payloadDone))
				c.Writer.Flush()
			}
			return false
		})
	} else {
		assistantMsg, err := services.CallChatCompletion(messages, "chat", false)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"detail": err.Error()})
			return
		}

		_ = crud.SaveChatMessage(entryID, "assistant", assistantMsg)
		c.JSON(http.StatusOK, gin.H{
			"reply":  assistantMsg,
			"status": "ok",
		})
	}
}

func clearEntryChat(c *gin.Context) {
	entryIDStr := c.Param("entry_id")
	entryID, err := strconv.Atoi(entryIDStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "Invalid entry_id"})
		return
	}

	if err := crud.ClearChatMessages(entryID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// --- OPML Handlers ---

func importOPML(c *gin.Context) {
	file, err := c.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "No file uploaded"})
		return
	}

	opened, err := file.Open()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": err.Error()})
		return
	}
	defer opened.Close()

	added, err := services.ImportOPML(opened)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": err.Error()})
		return
	}

	// Trigger cold start for newly imported feeds in background
	for _, f := range added {
		if f.Seeded == 0 {
			go services.AsyncColdStartFeed(f.ID)
		}
	}

	c.JSON(http.StatusOK, gin.H{"ok": true, "added": len(added)})
}

func exportOPML(c *gin.Context) {
	feeds, err := crud.ListFeeds()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": err.Error()})
		return
	}

	opmlStr, err := services.ExportOPML(feeds)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": err.Error()})
		return
	}

	c.Header("Content-Disposition", "attachment; filename=kickrss_subscriptions.opml")
	c.Data(http.StatusOK, "application/xml", []byte(opmlStr))
}

// --- Maintenance & Refresh Handlers ---

func triggerMaintenance(c *gin.Context) {
	report, err := services.RunAllFeedsMaintenance()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "result": report})
}

func triggerRefresh(c *gin.Context) {
	processed, totalNew := scheduler.RefreshAllFeeds()
	c.JSON(http.StatusOK, gin.H{"ok": true, "fetched": processed, "new_entries": totalNew})
}

func getInterestProfile(c *gin.Context) {
	if !config.GlobalConfig.InterestProfileEnabled {
		c.JSON(http.StatusOK, gin.H{"status": "disabled"})
		return
	}

	latest, err := crud.GetLatestUserInterest()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": err.Error()})
		return
	}
	if latest == nil {
		c.JSON(http.StatusOK, gin.H{
			"status":  "cold_start",
			"message": "阅读数据积累中，需至少15篇文章的阅读行为",
		})
		return
	}

	var topics interface{}
	if err := json.Unmarshal([]byte(latest.TopicsJson), &topics); err != nil {
		topics = gin.H{"high_interest": []string{}, "low_interest": []string{}, "concentration_note": nil}
	}

	// Get activity timestamps (last 30 days)
	activityRows, err := db.DB.Query(`
		SELECT recorded_at 
		FROM engagement 
		WHERE recorded_at IS NOT NULL AND datetime(recorded_at) >= datetime('now', '-30 days')
	`)
	activityTimestamps := []string{}
	if err == nil {
		defer activityRows.Close()
		for activityRows.Next() {
			var ts string
			if err := activityRows.Scan(&ts); err == nil {
				activityTimestamps = append(activityTimestamps, ts)
			}
		}
	}

	// Get category distribution
	categoryRows, err := db.DB.Query(`
		SELECT COALESCE(c.name, '未分类') as category_name, COUNT(e.id) as read_count
		FROM engagement g
		JOIN entries e ON g.entry_id = e.id
		LEFT JOIN categories c ON e.category_id = c.id
		GROUP BY e.category_id
		ORDER BY read_count DESC
	`)
	type CatDist struct {
		Name  string `json:"name"`
		Count int    `json:"count"`
	}
	categoryDistribution := []CatDist{}
	if err == nil {
		defer categoryRows.Close()
		for categoryRows.Next() {
			var name string
			var count int
			if err := categoryRows.Scan(&name, &count); err == nil {
				categoryDistribution = append(categoryDistribution, CatDist{Name: name, Count: count})
			}
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"snapshot_date":         latest.SnapshotDate,
		"total_articles":        latest.TotalArticles,
		"high_engagement":       latest.HighEngagement,
		"low_engagement":        latest.LowEngagement,
		"topics":                topics,
		"attention_guide":       latest.PromptText,
		"activity_timestamps":   activityTimestamps,
		"category_distribution": categoryDistribution,
	})
}

func getTopicDetail(c *gin.Context) {
	if !config.GlobalConfig.InterestProfileEnabled {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "Personalization profile is disabled"})
		return
	}

	topic := c.Query("topic")
	if topic == "" {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "Missing topic query parameter"})
		return
	}

	detail, err := crud.GetTopicDetail(topic)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": err.Error()})
		return
	}

	if detail == nil {
		c.JSON(http.StatusNotFound, gin.H{"detail": "Topic not found or no data available"})
		return
	}

	c.JSON(http.StatusOK, detail)
}

func getCategoryEntries(c *gin.Context) {
	categoryIDStr := c.Param("category_id")
	categoryID, err := strconv.Atoi(categoryIDStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "Invalid category_id"})
		return
	}

	unreadStr := c.DefaultQuery("unread", "1")
	unreadOnly := unreadStr == "1"

	limitStr := c.DefaultQuery("limit", "50")
	offsetStr := c.DefaultQuery("offset", "0")
	limit, _ := strconv.Atoi(limitStr)
	offset, _ := strconv.Atoi(offsetStr)

	entries, err := crud.GetEntriesForCategory(categoryID, unreadOnly, limit, offset)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": err.Error()})
		return
	}

	c.JSON(http.StatusOK, entries)
}

func readCategory(c *gin.Context) {
	categoryIDStr := c.Param("category_id")
	categoryID, err := strconv.Atoi(categoryIDStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "Invalid category_id"})
		return
	}

	ids, err := crud.MarkEntriesAsRead(categoryID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"ok": true, "ids": ids})
}

func getFeedEntries(c *gin.Context) {
	feedIDStr := c.Param("feed_id")
	feedID, err := strconv.Atoi(feedIDStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "Invalid feed_id"})
		return
	}

	unreadStr := c.DefaultQuery("unread", "1")
	unreadOnly := unreadStr == "1"

	limitStr := c.DefaultQuery("limit", "50")
	offsetStr := c.DefaultQuery("offset", "0")
	limit, _ := strconv.Atoi(limitStr)
	offset, _ := strconv.Atoi(offsetStr)

	entries, err := crud.GetEntriesForFeed(feedID, unreadOnly, limit, offset)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": err.Error()})
		return
	}

	c.JSON(http.StatusOK, entries)
}

func readFeed(c *gin.Context) {
	feedIDStr := c.Param("feed_id")
	feedID, err := strconv.Atoi(feedIDStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "Invalid feed_id"})
		return
	}

	ids, err := crud.MarkFeedAsRead(feedID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"ok": true, "ids": ids})
}

func setEntryUnreadStatus(c *gin.Context) {
	entryIDStr := c.Param("entry_id")
	entryID, err := strconv.Atoi(entryIDStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "Invalid entry_id"})
		return
	}

	if err := crud.SetEntryReadStatus(entryID, false); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"ok": true})
}

type AttentionBody struct {
	Attention string `json:"attention" binding:"required"`
}

func updateEntryAttention(c *gin.Context) {
	entryIDStr := c.Param("entry_id")
	entryID, err := strconv.Atoi(entryIDStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "Invalid entry_id"})
		return
	}

	var body AttentionBody
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": err.Error()})
		return
	}

	if err := crud.UpdateEntryAttention(entryID, body.Attention); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func getEntryTranslation(c *gin.Context) {
	entryIDStr := c.Param("entry_id")
	entryID, err := strconv.Atoi(entryIDStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "Invalid entry_id"})
		return
	}

	forceQuery := c.Query("force")
	force := forceQuery == "true"

	streamQuery := c.Query("stream")
	stream := config.GlobalConfig.AI.Stream
	if streamQuery != "" {
		stream = streamQuery == "true"
	}

	if stream {
		c.Header("Content-Type", "text/event-stream")
		c.Header("Cache-Control", "no-cache")
		c.Header("Connection", "keep-alive")
		c.Header("Transfer-Encoding", "chunked")

		c.Stream(func(w io.Writer) bool {
			aiStreamEnabled := config.GlobalConfig.AI.Stream
			targetLang := config.GlobalConfig.AI.SummaryLanguage
			if targetLang == "auto" || targetLang == "" {
				targetLang = config.GlobalConfig.SystemLanguage
			}
			if targetLang == "" {
				targetLang = "zh"
			}

			if !force {
				transRow, _ := crud.GetTranslation(entryID, targetLang)
				if transRow != nil {
					payload, _ := json.Marshal(gin.H{"translated_content": transRow.Content, "target_lang": transRow.Lang, "status": "streaming"})
					fmt.Fprintf(w, "data: %s\n\n", string(payload))
					c.Writer.Flush()
					payloadDone, _ := json.Marshal(gin.H{"translated_content": "", "target_lang": transRow.Lang, "status": "done"})
					fmt.Fprintf(w, "data: %s\n\n", string(payloadDone))
					c.Writer.Flush()
					return false
				}
			}

			entry, err := crud.GetEntryByID(entryID)
			if err != nil || entry == nil {
				payload, _ := json.Marshal(gin.H{"translated_content": "", "status": "error", "detail": "Entry not found"})
				fmt.Fprintf(w, "data: %s\n\n", string(payload))
				c.Writer.Flush()
				return false
			}

			ftRow, _ := crud.GetEntryFulltext(entryID)
			var ftContent string
			if ftRow != nil {
				ftContent = ftRow.Content
			} else {
				ftContent = crud.CleanHTML(entry.RawContent)
			}

			if ftContent == "" || len([]rune(ftContent)) < 5 {
				payload, _ := json.Marshal(gin.H{"translated_content": "", "status": "error", "detail": "No content to translate"})
				fmt.Fprintf(w, "data: %s\n\n", string(payload))
				c.Writer.Flush()
				return false
			}

			sourceLang := services.DetectLanguage(ftContent)
			isSourceZh := (sourceLang == "zh" || sourceLang == "zh-hant")
			isTargetZh := (targetLang == "zh" || targetLang == "zh-hant")

			if sourceLang == targetLang || (isSourceZh && isTargetZh) {
				_ = crud.SaveTranslation(entryID, ftContent, targetLang)
				payload, _ := json.Marshal(gin.H{"translated_content": ftContent, "target_lang": targetLang, "status": "streaming"})
				fmt.Fprintf(w, "data: %s\n\n", string(payload))
				c.Writer.Flush()
				payloadDone, _ := json.Marshal(gin.H{"translated_content": "", "target_lang": targetLang, "status": "done"})
				fmt.Fprintf(w, "data: %s\n\n", string(payloadDone))
				c.Writer.Flush()
				return false
			}

			if aiStreamEnabled {
				paragraphs := strings.Split(ftContent, "\n")
				var chunks []string
				var currentChunk []string
				currentLen := 0
				for _, p := range paragraphs {
					pLen := len([]rune(p))
					if currentLen+pLen > 800 && len(currentChunk) > 0 {
						chunks = append(chunks, strings.Join(currentChunk, "\n"))
						currentChunk = []string{p}
						currentLen = pLen
					} else {
						currentChunk = append(currentChunk, p)
						currentLen += pLen + 1
					}
				}
				if len(currentChunk) > 0 {
					chunks = append(chunks, strings.Join(currentChunk, "\n"))
				}

				engName, localName, chnName := "English", "English", "英文"
				langMap := map[string][3]string{
					"zh":      {"Simplified Chinese (简体中文)", "简体中文", "简体中文"},
					"zh-hant": {"Traditional Chinese (繁体中文)", "繁體中文", "繁体中文"},
					"en":      {"English", "English", "英文"},
					"ja":      {"Japanese (日本語)", "日本語", "日语"},
					"ko":      {"Korean (한국어)", "한국어", "韩语"},
					"fr":      {"French (Français)", "Français", "法语"},
					"es":      {"Spanish (Español)", "Español", "西班牙语"},
					"de":      {"German (Deutsch)", "Deutsch", "德语"},
					"ru":      {"Russian (Русский)", "Русский", "俄语"},
					"pt":      {"Portuguese (Português)", "Português", "葡萄牙语"},
					"it":      {"Italian (Italiano)", "Italiano", "意大利语"},
				}
				if val, ok := langMap[targetLang]; ok {
					engName = val[0]
					localName = val[1]
					chnName = val[2]
				}

				systemPrompt := fmt.Sprintf(`You are a professional translator.
Your task is to translate the given text into %s (%s).
Rules:
- Keep the paragraph structure and line breaks EXACTLY identical to the source text.
- Do NOT add any notes, explanations, introduction, or prefix. Output ONLY the translated paragraphs.
- Translate to %s (%s) faithfully, maintaining the original tone and style.
- CRITICAL: Regardless of the source language, you must translate it into %s (%s). Do NOT copy or output the original text if it is in a different language. You MUST output the translation in %s.
- 必须且只能将文本翻译为 %s (%s)，绝对不要直接输出原文！请输出完整的翻译后文本。`, engName, localName, engName, localName, engName, localName, localName, chnName, localName)

				translatedFull := ""
				for _, chunkText := range chunks {
					if strings.TrimSpace(chunkText) == "" {
						translatedFull += chunkText + "\n"
						payload, _ := json.Marshal(gin.H{"translated_content": chunkText + "\n", "target_lang": targetLang, "status": "streaming"})
						fmt.Fprintf(w, "data: %s\n\n", string(payload))
						c.Writer.Flush()
						continue
					}

					userContent := fmt.Sprintf("Please translate the following text into %s (%s). Do NOT output the original text, only output the translated text:\n\n%s", engName, localName, chunkText)
					if chnName != "英文" {
						userContent = fmt.Sprintf("请将以下文本翻译为%s (%s)。请务必翻译，不要输出原文，只输出翻译后的文本：\n\n%s", chnName, localName, chunkText)
					}

					messages := []services.ChatMessage{
						{Role: "system", Content: systemPrompt},
						{Role: "user", Content: userContent},
					}

					respStream, err := services.CallChatCompletionStream(messages, "summary")
					if err != nil {
						payload, _ := json.Marshal(gin.H{"translated_content": "", "status": "error", "detail": err.Error()})
						fmt.Fprintf(w, "data: %s\n\n", string(payload))
						c.Writer.Flush()
						return false
					}

					chunkFull := ""
					errRead := services.ReadSSEResponse(respStream, func(chunk string) error {
						chunkFull += chunk
						payload, _ := json.Marshal(gin.H{"translated_content": chunk, "target_lang": targetLang, "status": "streaming"})
						fmt.Fprintf(w, "data: %s\n\n", string(payload))
						c.Writer.Flush()
						return nil
					})
					respStream.Body.Close()

					if errRead != nil {
						payload, _ := json.Marshal(gin.H{"translated_content": "", "status": "error", "detail": errRead.Error()})
						fmt.Fprintf(w, "data: %s\n\n", string(payload))
						c.Writer.Flush()
						return false
					}

					translatedFull += chunkFull + "\n"
				}

				_ = crud.SaveTranslation(entryID, translatedFull, targetLang)

				payloadDone, _ := json.Marshal(gin.H{"translated_content": "", "target_lang": targetLang, "status": "done"})
				fmt.Fprintf(w, "data: %s\n\n", string(payloadDone))
				c.Writer.Flush()

			} else {
				// Sync translation, streamed in single SSE chunk
				translated, err := services.GenerateTranslation(entry.Title, ftContent, targetLang)
				if err != nil {
					payload, _ := json.Marshal(gin.H{"translated_content": "", "status": "error", "detail": err.Error()})
					fmt.Fprintf(w, "data: %s\n\n", string(payload))
					c.Writer.Flush()
					return false
				}

				_ = crud.SaveTranslation(entryID, translated, targetLang)

				payload, _ := json.Marshal(gin.H{"translated_content": translated, "target_lang": targetLang, "status": "streaming"})
				fmt.Fprintf(w, "data: %s\n\n", string(payload))
				c.Writer.Flush()

				payloadDone, _ := json.Marshal(gin.H{"translated_content": "", "target_lang": targetLang, "status": "done"})
				fmt.Fprintf(w, "data: %s\n\n", string(payloadDone))
				c.Writer.Flush()
			}
			return false
		})
	} else {
		res, err := services.GetEntryTranslation(entryID, force)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"detail": err.Error()})
			return
		}
		c.JSON(http.StatusOK, res)
	}
}

type TranslateParagraphBody struct {
	ParaIndex int    `json:"para_index"`
	Text      string `json:"text" binding:"required"`
}

func translateEntryParagraph(c *gin.Context) {
	entryIDStr := c.Param("entry_id")
	entryID, err := strconv.Atoi(entryIDStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "Invalid entry_id"})
		return
	}

	var body TranslateParagraphBody
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": err.Error()})
		return
	}

	res, err := services.TranslateEntryParagraph(entryID, body.ParaIndex, body.Text)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": err.Error()})
		return
	}

	c.JSON(http.StatusOK, res)
}

func getEntryChatHistory(c *gin.Context) {
	entryIDStr := c.Param("entry_id")
	entryID, err := strconv.Atoi(entryIDStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "Invalid entry_id"})
		return
	}

	history, err := crud.GetChatMessages(entryID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": err.Error()})
		return
	}

	c.JSON(http.StatusOK, history)
}

type EngagementBody struct {
	ActiveDwellMs  int     `json:"active_dwell_ms"`
	ScrolledPct    float64 `json:"scrolled_pct"`
	OpenedOriginal bool    `json:"opened_original"`
}

func recordEngagement(c *gin.Context) {
	entryIDStr := c.Param("entry_id")
	entryID, err := strconv.Atoi(entryIDStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "Invalid entry_id"})
		return
	}

	var body EngagementBody
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": err.Error()})
		return
	}

	var originalVal int
	if body.OpenedOriginal {
		originalVal = 1
	}

	toBottom := 0
	if body.ScrolledPct >= 0.9 {
		toBottom = 1
	}

	err = crud.SaveEngagement(entryID, body.ActiveDwellMs, body.ScrolledPct, toBottom, originalVal, 0, "")
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func toggleFavorite(c *gin.Context) {
	entryIDStr := c.Param("entry_id")
	entryID, err := strconv.Atoi(entryIDStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "Invalid entry_id"})
		return
	}

	entry, err := crud.GetEntryByID(entryID)
	if err != nil || entry == nil {
		c.JSON(http.StatusNotFound, gin.H{"detail": "Entry not found"})
		return
	}

	newStarred := 1
	if entry.IsStarred > 0 {
		newStarred = 0
	}

	err = crud.SetEntryStarredStatus(entryID, newStarred > 0)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"is_favorited": newStarred})
}

func getEntryFulltext(c *gin.Context) {
	entryIDStr := c.Param("entry_id")
	entryID, err := strconv.Atoi(entryIDStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"detail": "Invalid entry_id"})
		return
	}

	res, err := services.GetEntryFulltext(entryID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": err.Error()})
		return
	}

	c.JSON(http.StatusOK, res)
}

type TestLLMRequest struct {
	AIBaseURL string `json:"ai_base_url"`
	AIAPIKey  string `json:"ai_api_key"`
	AIModel   string `json:"ai_model"`
}

func testLLMConnection(c *gin.Context) {
	var body TestLLMRequest
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "请求体格式错误: " + err.Error()})
		return
	}

	url := fmt.Sprintf("%s/chat/completions", strings.TrimRight(body.AIBaseURL, "/"))
	reqBody := map[string]interface{}{
		"model": body.AIModel,
		"messages": []map[string]string{
			{"role": "user", "content": "ping"},
		},
		"max_tokens": 10,
	}

	jsonBytes, err := json.Marshal(reqBody)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "构建请求失败: " + err.Error()})
		return
	}

	req, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonBytes))
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "创建请求对象失败: " + err.Error()})
		return
	}

	req.Header.Set("Content-Type", "application/json")
	if body.AIAPIKey != "" {
		req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", body.AIAPIKey))
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "请求接口失败: " + err.Error()})
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": fmt.Sprintf("API 返回状态码 %d: %s", resp.StatusCode, string(bodyBytes)),
		})
		return
	}

	var completionResp struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&completionResp); err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "解析 API 响应 JSON 失败: " + err.Error()})
		return
	}

	if len(completionResp.Choices) == 0 {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "API 返回的 choices 列表为空"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success":        true,
		"message":        "连接成功！",
		"model_response": completionResp.Choices[0].Message.Content,
	})
}

func getTokenStats(c *gin.Context) {
	stats, err := crud.GetDailyTokenStats()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": err.Error()})
		return
	}
	c.JSON(http.StatusOK, stats)
}

func getNotesEntries(c *gin.Context) {
	limitStr := c.DefaultQuery("limit", "50")
	offsetStr := c.DefaultQuery("offset", "0")

	limit, _ := strconv.Atoi(limitStr)
	offset, _ := strconv.Atoi(offsetStr)

	query := `
		SELECT e.id, e.feed_id, e.category_id, e.guid, e.title, e.url, e.author, e.published_at, e.fetched_at, e.raw_content,
		       e.attention, e.likely_no_text, e.fulltext_ready, e.is_read, e.read_at, e.classified_at, e.is_starred, e.starred_at,
		       f.title as feed_title, c.name as category_name
		FROM entries e
		JOIN feeds f ON f.id = e.feed_id
		LEFT JOIN categories c ON c.id = e.category_id
		WHERE e.id IN (SELECT DISTINCT entry_id FROM chat_messages WHERE content IS NOT NULL AND trim(content) != '')
		ORDER BY e.published_at DESC
		LIMIT ? OFFSET ?
	`

	rows, err := db.DB.Query(query, limit, offset)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": err.Error()})
		return
	}
	defer rows.Close()

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
			c.JSON(http.StatusInternalServerError, gin.H{"detail": err.Error()})
			return
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

	c.JSON(http.StatusOK, entries)
}

func getNotesEntriesCount(c *gin.Context) {
	var count int
	err := db.DB.QueryRow("SELECT COUNT(DISTINCT entry_id) FROM chat_messages WHERE content IS NOT NULL AND trim(content) != ''").Scan(&count)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"total_count": count})
}
