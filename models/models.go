package models

type Feed struct {
	ID                 int    `json:"id" db:"id"`
	Title              string `json:"title" db:"title"`
	URL                string `json:"url" db:"url"`
	SiteURL            string `json:"site_url" db:"site_url"`
	Etag               string `json:"etag" db:"etag"`
	LastModified       string `json:"last_modified" db:"last_modified"`
	LastFetchedAt      string `json:"last_fetched_at" db:"last_fetched_at"`
	Seeded             int    `json:"seeded" db:"seeded"`
	Enabled            int    `json:"enabled" db:"enabled"`
	NeedClassification int    `json:"need_classification" db:"need_classification"`

	// Virtual field for frontend
	UnreadCount int `json:"unread_count"`
}

type Category struct {
	ID        int    `json:"id" db:"id"`
	FeedID    int    `json:"feed_id" db:"feed_id"`
	Name      string `json:"name" db:"name"`
	IsDefault int    `json:"is_default" db:"is_default"`
	CreatedAt string `json:"created_at" db:"created_at"`
	
	// Virtual field for frontend
	UnreadCount int `json:"unread_count"`
}

type Entry struct {
	ID            int    `json:"id" db:"id"`
	FeedID        int    `json:"feed_id" db:"feed_id"`
	CategoryID    *int   `json:"category_id" db:"category_id"`
	Guid          string `json:"guid" db:"guid"`
	Title         string `json:"title" db:"title"`
	URL           string `json:"url" db:"url"`
	Author        string `json:"author" db:"author"`
	PublishedAt   string `json:"published_at" db:"published_at"`
	FetchedAt     string `json:"fetched_at" db:"fetched_at"`
	RawContent    string `json:"raw_content" db:"raw_content"`
	Attention     string `json:"attention" db:"attention"`
	LikelyNoText  int    `json:"likely_no_text" db:"likely_no_text"`
	FulltextReady int    `json:"fulltext_ready" db:"fulltext_ready"`
	IsRead        int    `json:"is_read" db:"is_read"`
	ReadAt        string `json:"read_at" db:"read_at"`
	ClassifiedAt  string `json:"classified_at" db:"classified_at"`
	IsStarred     int    `json:"is_starred" db:"is_starred"`
	StarredAt     string `json:"starred_at" db:"starred_at"`
	
	// Virtual fields for frontend
	FeedTitle    string `json:"feed_title,omitempty"`
	CategoryName string `json:"category_name,omitempty"`
}

type Fulltext struct {
	EntryID   int    `json:"entry_id" db:"entry_id"`
	Content   string `json:"content" db:"content"`
	Status    string `json:"status" db:"status"`
	FetchedAt string `json:"fetched_at" db:"fetched_at"`
	Fetcher   string `json:"fetcher" db:"fetcher"`
}

type Summary struct {
	EntryID       int    `json:"entry_id" db:"entry_id"`
	Content       string `json:"content" db:"content"`
	ClickbaitNote string `json:"clickbait_note" db:"clickbait_note"`
	Model         string `json:"model" db:"model"`
	CreatedAt     string `json:"created_at" db:"created_at"`
}

type ChatMessage struct {
	ID        int    `json:"id" db:"id"`
	EntryID   int    `json:"entry_id" db:"entry_id"`
	Role      string `json:"role" db:"role"`
	Content   string `json:"content" db:"content"`
	CreatedAt string `json:"created_at" db:"created_at"`
}

type Translation struct {
	EntryID   int    `json:"entry_id" db:"entry_id"`
	Content   string `json:"content" db:"content"`
	Lang      string `json:"lang" db:"lang"`
	CreatedAt string `json:"created_at" db:"created_at"`
}

type ParagraphTranslation struct {
	EntryID        int    `json:"entry_id" db:"entry_id"`
	ParaIndex      int    `json:"para_index" db:"para_index"`
	Lang           string `json:"lang" db:"lang"`
	OriginalText   string `json:"original_text" db:"original_text"`
	TranslatedText string `json:"translated_text" db:"translated_text"`
	CreatedAt      string `json:"created_at" db:"created_at"`
}

type Engagement struct {
	EntryID          int     `json:"entry_id" db:"entry_id"`
	Opened           int     `json:"opened" db:"opened"`
	ActiveDwellMs    int     `json:"active_dwell_ms" db:"active_dwell_ms"`
	ScrolledPct      float64 `json:"scrolled_pct" db:"scrolled_pct"`
	ScrolledToBottom int     `json:"scrolled_to_bottom" db:"scrolled_to_bottom"`
	OpenedOriginal   int     `json:"opened_original" db:"opened_original"`
	Favorited        int     `json:"favorited" db:"favorited"`
	ManualBump       string  `json:"manual_bump" db:"manual_bump"`
	RecordedAt       string  `json:"recorded_at" db:"recorded_at"`
}

type UserInterest struct {
	ID             int    `json:"id" db:"id"`
	SnapshotDate   string `json:"snapshot_date" db:"snapshot_date"`
	TotalArticles  int    `json:"total_articles" db:"total_articles"`
	HighEngagement int    `json:"high_engagement" db:"high_engagement"`
	LowEngagement  int    `json:"low_engagement" db:"low_engagement"`
	TopicsJson     string `json:"topics_json" db:"topics_json"`
	PromptText     string `json:"prompt_text" db:"prompt_text"`
	GeneratedAt    string `json:"generated_at" db:"generated_at"`
}
