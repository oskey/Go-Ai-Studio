package api

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"kt-ai-studio/internal/db"
	"kt-ai-studio/internal/models"

	"github.com/gin-gonic/gin"
	tiktoken "github.com/pkoukk/tiktoken-go"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type llmUsageKey struct {
	ProviderID  uint
	BucketStart time.Time
}

type llmUsageDelta struct {
	InputTokens  int64
	OutputTokens int64
	RequestCount int64
}

type aggregateBucket struct {
	ProviderID   uint
	BucketStart  time.Time
	InputTokens  int64
	OutputTokens int64
	RequestCount int64
}

type llmUsageTracker struct {
	mu          sync.Mutex
	pending     map[llmUsageKey]*llmUsageDelta
	ticker      *time.Ticker
	stopCh      chan struct{}
	lastFlushed *time.Time
}

var (
	llmUsageOnce    sync.Once
	llmUsageStopper sync.Once
	globalLLMUsage  = &llmUsageTracker{
		pending: make(map[llmUsageKey]*llmUsageDelta),
		stopCh:  make(chan struct{}),
	}
)

func InitLLMUsageTracker() {
	llmUsageOnce.Do(func() {
		globalLLMUsage.ticker = time.NewTicker(time.Hour)
		go func() {
			for {
				select {
				case <-globalLLMUsage.ticker.C:
					if err := flushLLMUsage(); err != nil {
						Log(LogLevelError, "LLM 用量统计刷新失败", err.Error())
					}
				case <-globalLLMUsage.stopCh:
					return
				}
			}
		}()
	})
}

func StopLLMUsageTracker() {
	llmUsageStopper.Do(func() {
		if globalLLMUsage.ticker != nil {
			globalLLMUsage.ticker.Stop()
		}
		_ = flushLLMUsage()
		close(globalLLMUsage.stopCh)
	})
}

func RecordLLMUsageInput(provider models.LLMProvider, messages ...string) {
	if provider.ID == 0 {
		return
	}

	tokenCount := estimateChatPromptTokens(provider.ModelName, messages...)
	if tokenCount <= 0 {
		return
	}

	addLLMUsage(provider.ID, llmUsageDelta{
		InputTokens:  tokenCount,
		RequestCount: 1,
	})
}

func RecordLLMUsageOutput(provider models.LLMProvider, content string) {
	if provider.ID == 0 {
		return
	}

	tokenCount := estimateTextTokens(provider.ModelName, content)
	if tokenCount <= 0 {
		return
	}

	addLLMUsage(provider.ID, llmUsageDelta{
		OutputTokens: tokenCount,
	})
}

func addLLMUsage(providerID uint, delta llmUsageDelta) {
	if providerID == 0 {
		return
	}

	bucketStart := time.Now().In(time.Local).Truncate(time.Hour)
	key := llmUsageKey{
		ProviderID:  providerID,
		BucketStart: bucketStart,
	}

	globalLLMUsage.mu.Lock()
	defer globalLLMUsage.mu.Unlock()

	if _, ok := globalLLMUsage.pending[key]; !ok {
		globalLLMUsage.pending[key] = &llmUsageDelta{}
	}

	globalLLMUsage.pending[key].InputTokens += delta.InputTokens
	globalLLMUsage.pending[key].OutputTokens += delta.OutputTokens
	globalLLMUsage.pending[key].RequestCount += delta.RequestCount
}

func flushLLMUsage() error {
	globalLLMUsage.mu.Lock()
	if len(globalLLMUsage.pending) == 0 {
		now := time.Now()
		globalLLMUsage.lastFlushed = &now
		globalLLMUsage.mu.Unlock()
		return nil
	}

	pending := make(map[llmUsageKey]llmUsageDelta, len(globalLLMUsage.pending))
	for key, value := range globalLLMUsage.pending {
		pending[key] = *value
	}
	globalLLMUsage.pending = make(map[llmUsageKey]*llmUsageDelta)
	globalLLMUsage.mu.Unlock()

	for key, delta := range pending {
		bucket := models.LLMUsageBucket{
			ProviderID:   key.ProviderID,
			BucketStart:  key.BucketStart,
			InputTokens:  delta.InputTokens,
			OutputTokens: delta.OutputTokens,
			RequestCount: delta.RequestCount,
		}

		err := db.DB.Clauses(clause.OnConflict{
			Columns: []clause.Column{
				{Name: "provider_id"},
				{Name: "bucket_start"},
			},
			DoUpdates: clause.Assignments(map[string]interface{}{
				"input_tokens":  gormExpr("input_tokens + ?", delta.InputTokens),
				"output_tokens": gormExpr("output_tokens + ?", delta.OutputTokens),
				"request_count": gormExpr("request_count + ?", delta.RequestCount),
				"updated_at":    time.Now(),
			}),
		}).Create(&bucket).Error
		if err != nil {
			return err
		}
	}

	now := time.Now()
	globalLLMUsage.mu.Lock()
	globalLLMUsage.lastFlushed = &now
	globalLLMUsage.mu.Unlock()
	return nil
}

func gormExpr(query string, args ...interface{}) clause.Expr {
	return clause.Expr{SQL: query, Vars: args}
}

func estimateChatPromptTokens(model string, messages ...string) int64 {
	if len(messages) == 0 {
		return 0
	}
	encoding := getTiktokenEncoding(model)
	total := 0
	for _, message := range messages {
		if strings.TrimSpace(message) == "" {
			continue
		}
		total += len(encoding.Encode(message, nil, nil)) + 4
	}
	total += 2
	return int64(total)
}

func estimateTextTokens(model string, content string) int64 {
	content = strings.TrimSpace(content)
	if content == "" {
		return 0
	}
	encoding := getTiktokenEncoding(model)
	return int64(len(encoding.Encode(content, nil, nil)))
}

func getTiktokenEncoding(model string) *tiktoken.Tiktoken {
	if strings.TrimSpace(model) == "" {
		model = "gpt-4o"
	}
	encoding, err := tiktoken.EncodingForModel(model)
	if err == nil {
		return encoding
	}
	encoding, err = tiktoken.GetEncoding("cl100k_base")
	if err != nil {
		panic(fmt.Sprintf("failed to load tokenizer encoding: %v", err))
	}
	return encoding
}

func snapshotPendingUsage() map[llmUsageKey]llmUsageDelta {
	globalLLMUsage.mu.Lock()
	defer globalLLMUsage.mu.Unlock()

	snapshot := make(map[llmUsageKey]llmUsageDelta, len(globalLLMUsage.pending))
	for key, value := range globalLLMUsage.pending {
		snapshot[key] = *value
	}
	return snapshot
}

func currentLLMUsageSummary() (*models.LLMUsageSummary, error) {
	var buckets []models.LLMUsageBucket
	if err := db.DB.Find(&buckets).Error; err != nil {
		return nil, err
	}

	pending := snapshotPendingUsage()
	lastFlushed := getLLMUsageLastFlushed()
	return buildLLMUsageSummary(buckets, pending, 0, lastFlushed), nil
}

func attachProviderUsageStats(providers []models.LLMProvider) error {
	var buckets []models.LLMUsageBucket
	if err := db.DB.Find(&buckets).Error; err != nil {
		return err
	}

	pending := snapshotPendingUsage()
	lastFlushed := getLLMUsageLastFlushed()
	statsByProvider := make(map[uint]*models.LLMProviderUsageStats, len(providers))
	for _, provider := range providers {
		summary := buildLLMUsageSummary(buckets, pending, provider.ID, lastFlushed)
		statsByProvider[provider.ID] = &models.LLMProviderUsageStats{
			Total: summary.Total,
			Hour:  summary.Hour,
			Day:   summary.Day,
			Month: summary.Month,
			Year:  summary.Year,
		}
	}

	for idx := range providers {
		providers[idx].UsageStats = statsByProvider[providers[idx].ID]
	}
	return nil
}

func buildLLMUsageSummary(buckets []models.LLMUsageBucket, pending map[llmUsageKey]llmUsageDelta, providerID uint, lastFlushed *time.Time) *models.LLMUsageSummary {
	bucketMap := make(map[llmUsageKey]*aggregateBucket)
	for _, bucket := range buckets {
		if providerID != 0 && bucket.ProviderID != providerID {
			continue
		}
		key := llmUsageKey{
			ProviderID:  bucket.ProviderID,
			BucketStart: bucket.BucketStart.In(time.Local),
		}
		bucketMap[key] = &aggregateBucket{
			ProviderID:   bucket.ProviderID,
			BucketStart:  bucket.BucketStart.In(time.Local),
			InputTokens:  bucket.InputTokens,
			OutputTokens: bucket.OutputTokens,
			RequestCount: bucket.RequestCount,
		}
	}

	for key, delta := range pending {
		if providerID != 0 && key.ProviderID != providerID {
			continue
		}
		if _, ok := bucketMap[key]; !ok {
			bucketMap[key] = &aggregateBucket{
				ProviderID:  key.ProviderID,
				BucketStart: key.BucketStart.In(time.Local),
			}
		}
		bucketMap[key].InputTokens += delta.InputTokens
		bucketMap[key].OutputTokens += delta.OutputTokens
		bucketMap[key].RequestCount += delta.RequestCount
	}

	allBuckets := make([]aggregateBucket, 0, len(bucketMap))
	for _, bucket := range bucketMap {
		allBuckets = append(allBuckets, *bucket)
	}
	sort.Slice(allBuckets, func(i, j int) bool {
		return allBuckets[i].BucketStart.Before(allBuckets[j].BucketStart)
	})

	now := time.Now().In(time.Local)
	summary := &models.LLMUsageSummary{
		HourSeries:  buildHourlySeries(allBuckets, now),
		DaySeries:   buildDailySeries(allBuckets, now),
		MonthSeries: buildMonthlySeries(allBuckets, now),
		YearSeries:  buildYearlySeries(allBuckets, now),
		LastFlushed: lastFlushed,
	}

	currentHour := now.Truncate(time.Hour)
	currentDay := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	currentMonth := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())
	currentYear := time.Date(now.Year(), 1, 1, 0, 0, 0, 0, now.Location())

	for _, bucket := range allBuckets {
		stats := models.LLMUsageWindowStats{
			InputTokens:  bucket.InputTokens,
			OutputTokens: bucket.OutputTokens,
			TotalTokens:  bucket.InputTokens + bucket.OutputTokens,
			RequestCount: bucket.RequestCount,
		}
		summary.Total = addUsageWindow(summary.Total, stats)
		if !bucket.BucketStart.Before(currentHour) {
			summary.Hour = addUsageWindow(summary.Hour, stats)
		}
		if !bucket.BucketStart.Before(currentDay) {
			summary.Day = addUsageWindow(summary.Day, stats)
		}
		if !bucket.BucketStart.Before(currentMonth) {
			summary.Month = addUsageWindow(summary.Month, stats)
		}
		if !bucket.BucketStart.Before(currentYear) {
			summary.Year = addUsageWindow(summary.Year, stats)
		}
	}

	return summary
}

func addUsageWindow(base, delta models.LLMUsageWindowStats) models.LLMUsageWindowStats {
	base.InputTokens += delta.InputTokens
	base.OutputTokens += delta.OutputTokens
	base.TotalTokens += delta.TotalTokens
	base.RequestCount += delta.RequestCount
	return base
}

func buildHourlySeries(buckets []aggregateBucket, now time.Time) []models.LLMUsagePoint {
	points := make([]models.LLMUsagePoint, 0, 24)
	bucketIndex := make(map[time.Time]models.LLMUsagePoint)
	start := now.Truncate(time.Hour).Add(-23 * time.Hour)
	for _, bucket := range buckets {
		bucketHour := bucket.BucketStart.Truncate(time.Hour)
		if bucketHour.Before(start) || bucketHour.After(now.Truncate(time.Hour)) {
			continue
		}
		point := bucketIndex[bucketHour]
		point.Label = bucketHour.Format("15:00")
		point.InputTokens += bucket.InputTokens
		point.OutputTokens += bucket.OutputTokens
		point.TotalTokens += bucket.InputTokens + bucket.OutputTokens
		point.RequestCount += bucket.RequestCount
		bucketIndex[bucketHour] = point
	}
	for i := 0; i < 24; i++ {
		ts := start.Add(time.Duration(i) * time.Hour)
		point := bucketIndex[ts]
		if point.Label == "" {
			point.Label = ts.Format("15:00")
		}
		points = append(points, point)
	}
	return points
}

func buildDailySeries(buckets []aggregateBucket, now time.Time) []models.LLMUsagePoint {
	points := make([]models.LLMUsagePoint, 0, 30)
	bucketIndex := make(map[time.Time]models.LLMUsagePoint)
	start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location()).AddDate(0, 0, -29)
	for _, bucket := range buckets {
		day := time.Date(bucket.BucketStart.Year(), bucket.BucketStart.Month(), bucket.BucketStart.Day(), 0, 0, 0, 0, bucket.BucketStart.Location())
		if day.Before(start) || day.After(now) {
			continue
		}
		point := bucketIndex[day]
		point.Label = day.Format("01-02")
		point.InputTokens += bucket.InputTokens
		point.OutputTokens += bucket.OutputTokens
		point.TotalTokens += bucket.InputTokens + bucket.OutputTokens
		point.RequestCount += bucket.RequestCount
		bucketIndex[day] = point
	}
	for i := 0; i < 30; i++ {
		ts := start.AddDate(0, 0, i)
		point := bucketIndex[ts]
		if point.Label == "" {
			point.Label = ts.Format("01-02")
		}
		points = append(points, point)
	}
	return points
}

func buildMonthlySeries(buckets []aggregateBucket, now time.Time) []models.LLMUsagePoint {
	points := make([]models.LLMUsagePoint, 0, 12)
	bucketIndex := make(map[time.Time]models.LLMUsagePoint)
	start := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location()).AddDate(0, -11, 0)
	end := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())
	for _, bucket := range buckets {
		month := time.Date(bucket.BucketStart.Year(), bucket.BucketStart.Month(), 1, 0, 0, 0, 0, bucket.BucketStart.Location())
		if month.Before(start) || month.After(end) {
			continue
		}
		point := bucketIndex[month]
		point.Label = month.Format("2006-01")
		point.InputTokens += bucket.InputTokens
		point.OutputTokens += bucket.OutputTokens
		point.TotalTokens += bucket.InputTokens + bucket.OutputTokens
		point.RequestCount += bucket.RequestCount
		bucketIndex[month] = point
	}
	for i := 0; i < 12; i++ {
		ts := start.AddDate(0, i, 0)
		point := bucketIndex[ts]
		if point.Label == "" {
			point.Label = ts.Format("2006-01")
		}
		points = append(points, point)
	}
	return points
}

func buildYearlySeries(buckets []aggregateBucket, now time.Time) []models.LLMUsagePoint {
	points := make([]models.LLMUsagePoint, 0, 5)
	bucketIndex := make(map[time.Time]models.LLMUsagePoint)
	start := time.Date(now.Year()-4, 1, 1, 0, 0, 0, 0, now.Location())
	end := time.Date(now.Year(), 1, 1, 0, 0, 0, 0, now.Location())
	for _, bucket := range buckets {
		year := time.Date(bucket.BucketStart.Year(), 1, 1, 0, 0, 0, 0, bucket.BucketStart.Location())
		if year.Before(start) || year.After(end) {
			continue
		}
		point := bucketIndex[year]
		point.Label = year.Format("2006")
		point.InputTokens += bucket.InputTokens
		point.OutputTokens += bucket.OutputTokens
		point.TotalTokens += bucket.InputTokens + bucket.OutputTokens
		point.RequestCount += bucket.RequestCount
		bucketIndex[year] = point
	}
	for i := 0; i < 5; i++ {
		ts := start.AddDate(i, 0, 0)
		point := bucketIndex[ts]
		if point.Label == "" {
			point.Label = ts.Format("2006")
		}
		points = append(points, point)
	}
	return points
}

func getLLMUsageLastFlushed() *time.Time {
	globalLLMUsage.mu.Lock()
	defer globalLLMUsage.mu.Unlock()
	if globalLLMUsage.lastFlushed == nil {
		return nil
	}
	value := *globalLLMUsage.lastFlushed
	return &value
}

func GetLLMUsageSummary(c *gin.Context) {
	summary, err := currentLLMUsageSummary()
	if err != nil {
		c.JSON(500, gin.H{"error": "Failed to load LLM usage summary"})
		return
	}
	c.JSON(200, summary)
}

func ForceRefreshLLMUsage(c *gin.Context) {
	if err := flushLLMUsage(); err != nil {
		c.JSON(500, gin.H{"error": "Failed to refresh usage statistics"})
		return
	}
	summary, err := currentLLMUsageSummary()
	if err != nil {
		c.JSON(500, gin.H{"error": "Failed to load refreshed usage summary"})
		return
	}
	c.JSON(200, gin.H{
		"message": "Usage statistics refreshed successfully",
		"summary": summary,
	})
}

func ResetLLMUsage(c *gin.Context) {
	id := c.Param("id")
	var provider models.LLMProvider
	if err := db.DB.First(&provider, id).Error; err != nil {
		c.JSON(404, gin.H{"error": "Provider not found"})
		return
	}

	clearPendingUsage(provider.ID)
	if err := db.DB.Where("provider_id = ?", provider.ID).Delete(&models.LLMUsageBucket{}).Error; err != nil {
		c.JSON(500, gin.H{"error": "Failed to reset provider usage statistics"})
		return
	}

	Log(LogLevelInfo, "重置 LLM 用量统计", fmt.Sprintf("已重置引擎 %s 的统计数据", provider.Name))
	c.JSON(200, gin.H{"message": "Provider usage statistics reset successfully"})
}

func ResetAllLLMUsage(c *gin.Context) {
	clearPendingUsage(0)
	if err := db.DB.Session(&gorm.Session{AllowGlobalUpdate: true}).Delete(&models.LLMUsageBucket{}).Error; err != nil {
		c.JSON(500, gin.H{"error": "Failed to reset usage statistics"})
		return
	}

	Log(LogLevelInfo, "重置全部 LLM 用量统计", "已清空所有引擎的用量统计")
	c.JSON(200, gin.H{"message": "All usage statistics reset successfully"})
}

func clearPendingUsage(providerID uint) {
	globalLLMUsage.mu.Lock()
	defer globalLLMUsage.mu.Unlock()

	if providerID == 0 {
		globalLLMUsage.pending = make(map[llmUsageKey]*llmUsageDelta)
		return
	}

	for key := range globalLLMUsage.pending {
		if key.ProviderID == providerID {
			delete(globalLLMUsage.pending, key)
		}
	}
}
