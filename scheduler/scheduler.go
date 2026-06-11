package scheduler

import (
	"fmt"
	"log"
	"sync"
	"time"

	"kickrss/config"
	"kickrss/crud"
	"kickrss/services"
	"github.com/robfig/cron/v3"
)

var (
	cronScheduler *cron.Cron
	refreshJobID  cron.EntryID
	schedulerLock sync.Mutex
)

func RefreshSingleFeed(feedID int) (int, int, error) {
	feed, err := crud.GetFeedByID(feedID)
	if err != nil || feed == nil || feed.Enabled == 0 {
		return 0, 0, nil
	}

	log.Printf("[Scheduler] Refreshing feed %d: %s", feedID, feed.URL)

	result, err := services.FetchFeed(feed.URL, feed.Etag, feed.LastModified)
	if err != nil {
		log.Printf("[Scheduler] Failed to fetch feed %d (%s): %s", feedID, feed.URL, err)
		return 0, 0, err
	}

	if result.NotModified {
		if err := services.EnsureFeedSeeded(feedID); err != nil {
			log.Printf("[Scheduler] Auto-seeding check failed for feed %d: %s", feedID, err)
		}
		
		_ = crud.UpdateFeedFetchStatus(feedID, feed.Etag, feed.LastModified)
		return 0, 0, nil
	}

	fetchedCount := len(result.Entries)
	newCount := 0

	if fetchedCount > 0 {
		defaultCatID, _ := crud.GetDefaultCategory(feedID)
		newCount, err = crud.SaveEntries(feedID, result.Entries, defaultCatID)
		_ = crud.UpdateFeedFetchStatus(feedID, result.Etag, result.LastModified)
		log.Printf("[Scheduler] Feed %d refreshed: %d fetched, %d new entries saved.", feedID, fetchedCount, newCount)
	} else {
		_ = crud.UpdateFeedFetchStatus(feedID, result.Etag, result.LastModified)
	}

	if err := services.EnsureFeedSeeded(feedID); err != nil {
		log.Printf("[Scheduler] Auto-seeding check failed for feed %d: %s", feedID, err)
	}

	services.ClassifyFeedEntries(feedID)

	return fetchedCount, newCount, nil
}

func RefreshAllFeeds() (int, int) {
	feeds, err := crud.ListFeeds()
	if err != nil {
		log.Printf("[Scheduler] Failed to list feeds: %s", err)
		return 0, 0
	}

	processed := 0
	totalNew := 0
	for _, feed := range feeds {
		if feed.Enabled == 0 {
			continue
		}
		_, newCount, err := RefreshSingleFeed(feed.ID)
		if err == nil {
			totalNew += newCount
			processed++
		}
	}
	return processed, totalNew
}

func StartScheduler() {
	schedulerLock.Lock()
	defer schedulerLock.Unlock()

	if cronScheduler != nil {
		return
	}

	cronScheduler = cron.New(cron.WithLocation(time.Local))

	interval := config.GlobalConfig.FetchIntervalMinutes
	log.Printf("[Scheduler] Starting background scheduler with %d minutes interval", interval)
	var err error
	refreshJobID, err = cronScheduler.AddFunc(fmt.Sprintf("@every %dm", interval), func() {
		RefreshAllFeeds()
	})
	if err != nil {
		log.Printf("[Scheduler] Failed to add refresh job: %s", err)
	}

	_, err = cronScheduler.AddFunc("0 3 * * *", func() {
		services.RunAllFeedsMaintenance()
	})
	if err != nil {
		log.Printf("[Scheduler] Failed to add maintenance job: %s", err)
	}

	cronScheduler.Start()
}

func ShutdownScheduler() {
	schedulerLock.Lock()
	defer schedulerLock.Unlock()

	if cronScheduler != nil {
		log.Println("[Scheduler] Shutting down background scheduler")
		cronScheduler.Stop()
		cronScheduler = nil
	}
}

func RescheduleRefreshJob(minutes int) {
	schedulerLock.Lock()
	defer schedulerLock.Unlock()

	if cronScheduler == nil {
		return
	}

	cronScheduler.Remove(refreshJobID)
	log.Printf("[Scheduler] Rescheduled refresh job to %d minutes interval.", minutes)
	var err error
	refreshJobID, err = cronScheduler.AddFunc(fmt.Sprintf("@every %dm", minutes), func() {
		RefreshAllFeeds()
	})
	if err != nil {
		log.Printf("[Scheduler] Failed to reschedule refresh job: %s", err)
	}
}
