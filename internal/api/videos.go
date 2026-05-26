package api

import (
	"archive/zip"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"kt-ai-studio/internal/db"
	"kt-ai-studio/internal/models"
	"kt-ai-studio/internal/task"
	"kt-ai-studio/internal/workflow"

	"github.com/gin-gonic/gin"
)

type jimengVideoStatusResponse struct {
	TaskID         string `json:"task_id"`
	HTTPStatus     int    `json:"http_status"`
	Code           int    `json:"code"`
	Message        string `json:"message"`
	RequestID      string `json:"request_id"`
	Status         string `json:"status"`
	VideoURL       string `json:"video_url"`
	AIGCTagged     bool   `json:"aigc_meta_tagged"`
	CanRetrieve    bool   `json:"can_retrieve"`
	AlreadyFetched bool   `json:"already_fetched"`
	PrettyJSON     string `json:"pretty_json"`
}

const temporaryVideoExportRoot = "output/_temp_exports"

func removeGeneratedVideoAsset(assetPath string) error {
	cleanPath := strings.TrimSpace(strings.TrimPrefix(assetPath, "/"))
	if cleanPath == "" {
		return nil
	}
	if err := os.Remove(cleanPath); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func loadVideoDefaults() (int, int) {
	return getConfiguredVideoRenderSize()
}

func rebuildVideoRecordFromScene(video *models.Video, scene models.Scene) {
	video.ProjectID = scene.ProjectID
	video.SceneID = scene.ID
	video.Scene = scene
	video.Narration = scene.Narration
	video.BackgroundAudio = scene.BackgroundAudio
	video.Dialogue = ""
	video.Fingerprint = ""
	video.VideoFingerprint = scene.VideoFingerprint
	video.VideoPrompt = scene.VideoPrompt
	video.DurationSeconds = scene.DurationSeconds
	video.Width = 0
	video.Height = 0
	video.Seed = 0
	video.PositivePrompt = ""
	video.NegativePrompt = getFixedLTXVideoNegativePromptEN()
	video.JMTaskID = ""
	video.GeneratedVideo = ""
	video.GeneratedWorkflow = ""
	video.Status = "pending"
	video.UpdatedAt = time.Now()
}

func resetVideoState(video *models.Video, status string) error {
	if video == nil {
		return fmt.Errorf("video is nil")
	}

	if err := removeGeneratedVideoAsset(video.GeneratedVideo); err != nil {
		return err
	}
	if err := clearVideoSegments(video.ID); err != nil {
		return err
	}

	video.PositivePrompt = ""
	video.NegativePrompt = getFixedLTXVideoNegativePromptEN()
	video.JMTaskID = ""
	video.GeneratedVideo = ""
	video.GeneratedWorkflow = ""
	video.Status = status
	video.UpdatedAt = time.Now()
	return db.DB.Save(video).Error
}

func resetVideoForGeneration(video *models.Video) error {
	return resetVideoState(video, "pending")
}

func resetVideoToDraft(video *models.Video) error {
	return resetVideoState(video, "draft")
}

func ensureVideoSceneLoaded(video *models.Video, preloadCharacters bool) error {
	return hydrateVideoScene(video, preloadCharacters)
}

func validateVideoReadyForGeneration(video *models.Video) error {
	if err := ensureVideoSceneLoaded(video, false); err != nil {
		return err
	}
	if strings.TrimSpace(video.VideoPrompt) == "" {
		return fmt.Errorf("please generate or provide the video prompt before generating video")
	}
	if strings.TrimSpace(video.Scene.GeneratedImage) == "" {
		return fmt.Errorf("please generate the scene image before generating video")
	}
	return nil
}

func loadProjectVideoForAction(c *gin.Context) (*models.Project, *models.Video, error) {
	projectID := c.Param("id")
	videoID := c.Param("videoId")

	var project models.Project
	if err := db.DB.Select("id", "code").First(&project, projectID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Project not found"})
		return nil, nil, fmt.Errorf("project not found")
	}

	var video models.Video
	if err := db.DB.First(&video, videoID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Video not found"})
		return nil, nil, fmt.Errorf("video not found")
	}
	if video.ProjectID != project.ID {
		c.JSON(http.StatusNotFound, gin.H{"error": "Video not found in project"})
		return nil, nil, fmt.Errorf("video not in project")
	}

	return &project, &video, nil
}

func GetJimengVideoStatus(c *gin.Context) {
	_, video, err := loadProjectVideoForAction(c)
	if err != nil {
		return
	}
	if strings.TrimSpace(video.JMTaskID) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "当前视频没有 jm_task_id，无法查询即梦状态"})
		return
	}

	snapshot, err := queryJimengTaskStatus(video.JMTaskID)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, jimengVideoStatusResponse{
		TaskID:         snapshot.TaskID,
		HTTPStatus:     snapshot.HTTPStatus,
		Code:           snapshot.Code,
		Message:        snapshot.Message,
		RequestID:      snapshot.RequestID,
		Status:         snapshot.Status,
		VideoURL:       snapshot.VideoURL,
		AIGCTagged:     snapshot.AIGCTagged,
		CanRetrieve:    snapshot.CanRetrieve,
		AlreadyFetched: strings.TrimSpace(video.GeneratedVideo) != "" && strings.TrimSpace(video.Status) == "generated",
		PrettyJSON:     snapshot.PrettyJSON,
	})
}

func RetrieveJimengVideoResult(c *gin.Context) {
	project, video, err := loadProjectVideoForAction(c)
	if err != nil {
		return
	}
	if strings.TrimSpace(video.JMTaskID) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "当前视频没有 jm_task_id，无法取回即梦结果"})
		return
	}

	snapshot, err := queryJimengTaskStatus(video.JMTaskID)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	if !snapshot.CanRetrieve {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":       "即梦任务尚未完成，暂时不能取回",
			"status":      snapshot.Status,
			"pretty_json": snapshot.PrettyJSON,
		})
		return
	}

	preset := getConfiguredJimengVideoPreset()
	webPath, err := persistJimengRetrievedVideo(video.ID, project.Code, snapshot.VideoURL, preset)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	var refreshed models.Video
	if err := db.DB.First(&refreshed, video.ID).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "视频结果已取回，但刷新本地记录失败"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message":         "即梦视频已取回",
		"generated_video": webPath,
		"status":          refreshed.Status,
		"task_id":         refreshed.JMTaskID,
		"pretty_json":     snapshot.PrettyJSON,
	})
}

// ListVideos retrieves all videos for a project
func ListVideos(c *gin.Context) {
	projectID := c.Param("id")
	summaryMode := strings.TrimSpace(c.Query("summary"))
	if summaryMode == "episodes" {
		type episodeSummary struct {
			Episode      int   `json:"episode"`
			Count        int64 `json:"count"`
			AllGenerated bool  `json:"all_generated"`
		}
		var summaries []episodeSummary
		if err := db.DB.Model(&models.Video{}).
			Select("episode as episode, COUNT(id) as count, SUM(CASE WHEN video_status = 'generated' THEN 1 ELSE 0 END) = COUNT(id) as all_generated").
			Where("project_id = ?", projectID).
			Group("episode").
			Order("episode asc").
			Scan(&summaries).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch video summaries"})
			return
		}
		c.JSON(http.StatusOK, summaries)
		return
	}

	episodeFilter := strings.TrimSpace(c.Query("episode"))
	offsetValue := strings.TrimSpace(c.Query("offset"))
	limitValue := strings.TrimSpace(c.Query("limit"))
	var project models.Project
	if err := db.DB.Select("id", "code").First(&project, projectID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Project not found"})
		return
	}

	var videos []models.Video
	query := db.DB.Preload("Segments").Where("project_id = ?", projectID)
	if episodeFilter != "" {
		episode, err := strconv.Atoi(episodeFilter)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid episode"})
			return
		}
		query = query.Where("episode = ?", episode)
	}
	if episodeFilter != "" && (offsetValue != "" || limitValue != "") {
		offset := 0
		if offsetValue != "" {
			parsedOffset, err := strconv.Atoi(offsetValue)
			if err != nil || parsedOffset < 0 {
				c.JSON(http.StatusBadRequest, gin.H{"error": "invalid offset"})
				return
			}
			offset = parsedOffset
		}

		limit := 5
		if limitValue != "" {
			parsedLimit, err := strconv.Atoi(limitValue)
			if err != nil || parsedLimit <= 0 {
				c.JSON(http.StatusBadRequest, gin.H{"error": "invalid limit"})
				return
			}
			limit = parsedLimit
		}

		var total int64
		countQuery := db.DB.Model(&models.Video{}).
			Where("project_id = ?", projectID).
			Where("episode = ?", strings.TrimSpace(episodeFilter))
		if err := countQuery.Count(&total).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to count videos"})
			return
		}
		if err := query.Order("episode asc, scene_number asc").Offset(offset).Limit(limit).Find(&videos).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch videos"})
			return
		}
		reconcileVideoOutputsFromDisk(project.Code, videos)
		if err := hydrateVideoScenes(videos, true); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to hydrate video scenes"})
			return
		}
		c.JSON(http.StatusOK, gin.H{
			"items":    videos,
			"total":    total,
			"offset":   offset,
			"limit":    limit,
			"has_more": int64(offset+len(videos)) < total,
		})
		return
	}
	if err := query.Order("episode asc, scene_number asc").Find(&videos).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch videos"})
		return
	}
	reconcileVideoOutputsFromDisk(project.Code, videos)
	if err := hydrateVideoScenes(videos, true); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to hydrate video scenes"})
		return
	}

	c.JSON(http.StatusOK, videos)
}

func reconcileVideoOutputsFromDisk(projectCode string, videos []models.Video) {
	if strings.TrimSpace(projectCode) == "" || len(videos) == 0 {
		return
	}

	for i := range videos {
		reconciledPath, ok := findLatestVideoOutputPath(projectCode, videos[i].ID)
		if !ok {
			continue
		}
		if strings.TrimSpace(videos[i].GeneratedVideo) == reconciledPath && strings.TrimSpace(videos[i].Status) == "generated" {
			continue
		}

		videos[i].GeneratedVideo = reconciledPath
		videos[i].Status = "generated"
		videos[i].UpdatedAt = time.Now()
		_ = db.DB.Model(&models.Video{}).
			Where("id = ?", videos[i].ID).
			Updates(map[string]interface{}{
				"generated_video": reconciledPath,
				"video_status":    "generated",
				"updated_at":      videos[i].UpdatedAt,
			}).Error
	}
}

func findLatestVideoOutputPath(projectCode string, videoID uint) (string, bool) {
	if strings.TrimSpace(projectCode) == "" || videoID == 0 {
		return "", false
	}

	patterns := []string{
		filepath.Join("output", projectCode, "videos", fmt.Sprintf("video_%d_merged_*.mp4", videoID)),
		filepath.Join("output", projectCode, "videos", fmt.Sprintf("video_%d_segment_*.mp4", videoID)),
		filepath.Join("output", projectCode, "videos", fmt.Sprintf("video_%d_merged_*.mov", videoID)),
		filepath.Join("output", projectCode, "videos", fmt.Sprintf("video_%d_segment_*.mov", videoID)),
		filepath.Join("output", projectCode, "videos", fmt.Sprintf("video_%d_merged_*.webm", videoID)),
		filepath.Join("output", projectCode, "videos", fmt.Sprintf("video_%d_segment_*.webm", videoID)),
	}

	latestPath := ""
	var latestModTime time.Time
	for _, pattern := range patterns {
		matches, err := filepath.Glob(pattern)
		if err != nil || len(matches) == 0 {
			continue
		}
		for _, match := range matches {
			info, err := os.Stat(match)
			if err != nil || info.IsDir() {
				continue
			}
			if latestPath == "" || info.ModTime().After(latestModTime) {
				latestPath = match
				latestModTime = info.ModTime()
			}
		}
	}

	if latestPath == "" {
		return "", false
	}
	return "/" + filepath.ToSlash(latestPath), true
}

func DeleteVideo(c *gin.Context) {
	project, video, err := loadProjectVideoForAction(c)
	if err != nil {
		return
	}

	if err := deleteShotWithAssets(video.ID); err != nil {
		Log(LogLevelError, "删除视频失败", err.Error())
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete video"})
		return
	}

	Log(LogLevelInfo, "删除视频", fmt.Sprintf("删除项目 %d 的视频/镜头 ID: %d", project.ID, video.ID))
	c.JSON(http.StatusOK, gin.H{"message": "Video deleted successfully"})
}

// UpdateVideo updates an existing video (Edit mode)
func UpdateVideo(c *gin.Context) {
	videoID := c.Param("videoId")
	var video models.Video
	if err := db.DB.First(&video, videoID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Video not found"})
		return
	}

	var updateData models.Video
	if err := c.ShouldBindJSON(&updateData); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Update fields allowed for editing
	video.Fingerprint = ""
	video.VideoPrompt = strings.TrimSpace(updateData.VideoPrompt)
	if updateData.DurationSeconds != 0 {
		if updateData.DurationSeconds < minVideoTotalDurationSeconds {
			c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("duration_seconds must be at least %d", minVideoTotalDurationSeconds)})
			return
		}
		video.DurationSeconds = updateData.DurationSeconds
	}
	if video.VideoPrompt != "" {
		if video.DurationSeconds < minVideoTotalDurationSeconds {
			c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("duration_seconds must be at least %d", minVideoTotalDurationSeconds)})
			return
		}
		videoFingerprint, err := buildLegacyVideoFingerprintFromPrompt(video.VideoPrompt, video.DurationSeconds)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("invalid video_prompt: %v", err)})
			return
		}
		video.VideoFingerprint = videoFingerprint
	} else {
		video.VideoFingerprint = updateData.VideoFingerprint
	}
	video.PositivePrompt = updateData.PositivePrompt
	video.NegativePrompt = updateData.NegativePrompt
	video.Seed = 0
	video.Width = 0
	video.Height = 0
	video.UpdatedAt = time.Now()

	if err := db.DB.Save(&video).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update video"})
		return
	}

	Log(LogLevelInfo, "更新视频", fmt.Sprintf("Updated video ID: %d", video.ID))
	c.JSON(http.StatusOK, video)
}

// ReextractVideo resets video data from the linked scene
func ReextractVideo(c *gin.Context) {
	videoID := c.Param("videoId")
	var video models.Video
	if err := db.DB.First(&video, videoID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Video not found"})
		return
	}
	if err := ensureVideoSceneLoaded(&video, false); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if err := removeGeneratedVideoAsset(video.GeneratedVideo); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to remove generated video file"})
		return
	}
	if err := clearVideoSegments(video.ID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to clear video segments"})
		return
	}

	rebuildVideoRecordFromScene(&video, video.Scene)

	if err := db.DB.Save(&video).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to re-extract video data"})
		return
	}

	BroadcastUpdate("video", video.ID)
	c.JSON(http.StatusOK, video)
}

// AutoGenerateVideo handles LLM re-inference for video_fingerprint.
func AutoGenerateVideo(c *gin.Context) {
	c.JSON(http.StatusGone, gin.H{
		"error": "video prompt regeneration via secondary LLM is disabled in lightweight story mode",
	})
}

// ResetProjectVideos deletes generated video files, clears generated prompts/segments, and resets statuses to draft.
func ResetProjectVideos(c *gin.Context) {
	projectID := c.Param("id")

	var videos []models.Video
	if err := db.DB.Where("project_id = ?", projectID).Find(&videos).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch videos"})
		return
	}

	resetCount := 0
	for _, video := range videos {
		if err := resetVideoToDraft(&video); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to reset videos"})
			return
		}
		BroadcastUpdate("video", video.ID)
		resetCount++
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "Videos reset successfully",
		"count":   resetCount,
	})
}

func ResetVideo(c *gin.Context) {
	projectID := c.Param("id")
	videoID := c.Param("videoId")

	var video models.Video
	if err := db.DB.First(&video, videoID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Video not found"})
		return
	}
	if fmt.Sprintf("%d", video.ProjectID) != projectID {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Video does not belong to this project"})
		return
	}

	if err := resetVideoToDraft(&video); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to reset video status"})
		return
	}

	BroadcastUpdate("video", video.ID)
	c.JSON(http.StatusOK, gin.H{
		"message": "Video reset successfully",
		"video":   video,
	})
}

type exportEpisodeVideosRequest struct {
	Episode   int    `json:"episode"`
	ExportDir string `json:"export_dir"`
}

func CleanTemporaryVideoExportFiles() {
	_ = os.RemoveAll(temporaryVideoExportRoot)
	_ = os.MkdirAll(temporaryVideoExportRoot, 0755)
}

func validateEpisodeExportRequest(c *gin.Context) (string, exportEpisodeVideosRequest, bool) {
	projectID := c.Param("id")
	var req exportEpisodeVideosRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return "", exportEpisodeVideosRequest{}, false
	}
	if req.Episode <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "episode is required"})
		return "", exportEpisodeVideosRequest{}, false
	}
	return projectID, req, true
}

func loadCompletedEpisodeVideos(projectID string, episode int) ([]models.Video, int, error) {
	var allEpisodeVideos []models.Video
	if err := db.DB.Where("project_id = ?", projectID).
		Where("episode = ?", episode).
		Order("scene_number asc").
		Find(&allEpisodeVideos).Error; err != nil {
		return nil, http.StatusInternalServerError, fmt.Errorf("Failed to fetch episode videos")
	}
	if err := hydrateVideoScenes(allEpisodeVideos, false); err != nil {
		return nil, http.StatusInternalServerError, fmt.Errorf("Failed to hydrate video scenes")
	}

	if len(allEpisodeVideos) == 0 {
		return nil, http.StatusBadRequest, fmt.Errorf("No videos found for this episode")
	}

	for _, video := range allEpisodeVideos {
		if video.Status != "generated" || video.Scene.SceneNumber <= 0 || strings.TrimSpace(video.GeneratedVideo) == "" {
			return nil, http.StatusBadRequest, fmt.Errorf("This episode is not fully completed yet")
		}
	}

	videos := allEpisodeVideos
	sort.Slice(videos, func(i, j int) bool {
		return videos[i].Scene.SceneNumber < videos[j].Scene.SceneNumber
	})
	return videos, http.StatusOK, nil
}

func exportSceneNarration(video models.Video) string {
	if narration := strings.TrimSpace(video.Narration); narration != "" {
		return narration
	}
	return strings.TrimSpace(video.Scene.Narration)
}

func exportSceneTimelineDuration(video models.Video) (int, error) {
	duration := video.DurationSeconds
	if duration <= 0 {
		duration = video.Scene.DurationSeconds
	}
	if duration <= 0 {
		return 0, fmt.Errorf("video %d has invalid duration_seconds", video.ID)
	}
	return duration, nil
}

func buildMergedEpisodeTimelineText(videos []models.Video) (string, error) {
	lines := make([]string, 0, len(videos))
	currentSecond := 1
	for _, video := range videos {
		duration, err := exportSceneTimelineDuration(video)
		if err != nil {
			return "", err
		}
		startSecond := currentSecond
		endSecond := currentSecond + duration - 1
		lines = append(lines, fmt.Sprintf("%d-%d：%s", startSecond, endSecond, exportSceneNarration(video)))
		currentSecond = endSecond + 1
	}
	return strings.Join(lines, "\n"), nil
}

func createTemporaryVideoExportWorkspace(prefix string) (string, error) {
	if err := os.MkdirAll(temporaryVideoExportRoot, 0755); err != nil {
		return "", err
	}
	dir, err := os.MkdirTemp(temporaryVideoExportRoot, prefix)
	if err != nil {
		return "", err
	}
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	return absDir, nil
}

func addFileToZip(zipWriter *zip.Writer, entryName string, srcPath string) error {
	sourceFile, err := os.Open(srcPath)
	if err != nil {
		return err
	}
	defer sourceFile.Close()

	writer, err := zipWriter.Create(entryName)
	if err != nil {
		return err
	}
	if _, err := io.Copy(writer, sourceFile); err != nil {
		return err
	}
	return nil
}

func addTextToZip(zipWriter *zip.Writer, entryName string, content string) error {
	writer, err := zipWriter.Create(entryName)
	if err != nil {
		return err
	}
	_, err = writer.Write([]byte(content))
	return err
}

func buildEpisodeExportArchive(videos []models.Video, episode int, zipPath string) error {
	zipFile, err := os.Create(zipPath)
	if err != nil {
		return err
	}
	defer zipFile.Close()

	zipWriter := zip.NewWriter(zipFile)

	for _, video := range videos {
		sourcePath, err := assetWebPathToAbs(video.GeneratedVideo)
		if err != nil {
			return err
		}
		ext := filepath.Ext(sourcePath)
		if ext == "" {
			ext = ".mp4"
		}
		baseName := fmt.Sprintf("%d-%d", episode, video.Scene.SceneNumber)
		if err := addFileToZip(zipWriter, baseName+ext, sourcePath); err != nil {
			return err
		}
		if err := addTextToZip(zipWriter, baseName+".txt", exportSceneNarration(video)); err != nil {
			return err
		}
	}

	return zipWriter.Close()
}

func buildMergedEpisodeExportArchive(mergedVideoPath string, episode int, timelineText string, zipPath string) error {
	zipFile, err := os.Create(zipPath)
	if err != nil {
		return err
	}
	defer zipFile.Close()

	zipWriter := zip.NewWriter(zipFile)

	if err := addFileToZip(zipWriter, fmt.Sprintf("%d-merged.mp4", episode), mergedVideoPath); err != nil {
		return err
	}
	if err := addTextToZip(zipWriter, fmt.Sprintf("%d-merged.txt", episode), timelineText); err != nil {
		return err
	}
	return zipWriter.Close()
}

func ffmpegConcatListPath(inputPath string) string {
	return strings.ReplaceAll(filepath.ToSlash(inputPath), "'", "'\\''")
}

func mergeEpisodeVideosToTarget(videos []models.Video, targetOutputPath string) error {
	if len(videos) == 0 {
		return fmt.Errorf("no videos to merge")
	}
	if err := os.MkdirAll(filepath.Dir(targetOutputPath), 0755); err != nil {
		return err
	}
	listFile, err := os.CreateTemp(filepath.Dir(targetOutputPath), "episode_concat_*.txt")
	if err != nil {
		return err
	}
	listPath := listFile.Name()
	defer os.Remove(listPath)
	defer listFile.Close()

	lines := make([]string, 0, len(videos))
	for _, video := range videos {
		absPath, err := assetWebPathToAbs(video.GeneratedVideo)
		if err != nil {
			return err
		}
		lines = append(lines, fmt.Sprintf("file '%s'", ffmpegConcatListPath(absPath)))
	}
	if _, err := listFile.WriteString(strings.Join(lines, "\n")); err != nil {
		return err
	}
	if err := listFile.Close(); err != nil {
		return err
	}

	return runFFmpeg(
		"-y",
		"-v", "error",
		"-f", "concat",
		"-safe", "0",
		"-i", listPath,
		"-c:v", "libx264",
		"-c:a", "aac",
		"-pix_fmt", "yuv420p",
		"-movflags", "+faststart",
		targetOutputPath,
	)
}

// ExportEpisodeVideos exports all generated videos and narration text for a completed episode as a zip download.
func ExportEpisodeVideos(c *gin.Context) {
	projectID, req, ok := validateEpisodeExportRequest(c)
	if !ok {
		return
	}

	videos, status, err := loadCompletedEpisodeVideos(projectID, req.Episode)
	if err != nil {
		c.JSON(status, gin.H{"error": err.Error()})
		return
	}

	workspaceDir, err := createTemporaryVideoExportWorkspace(fmt.Sprintf("episode_%d_export_", req.Episode))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("Failed to prepare export workspace: %v", err)})
		return
	}
	zipPath := filepath.Join(workspaceDir, fmt.Sprintf("%d-export.zip", req.Episode))
	if err := buildEpisodeExportArchive(videos, req.Episode, zipPath); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("Failed to build export archive: %v", err)})
		return
	}

	c.FileAttachment(zipPath, filepath.Base(zipPath))
}

// ExportMergedEpisodeVideo merges all generated scene videos of an episode and returns a zip download.
func ExportMergedEpisodeVideo(c *gin.Context) {
	projectID, req, ok := validateEpisodeExportRequest(c)
	if !ok {
		return
	}

	videos, status, err := loadCompletedEpisodeVideos(projectID, req.Episode)
	if err != nil {
		c.JSON(status, gin.H{"error": err.Error()})
		return
	}

	workspaceDir, err := createTemporaryVideoExportWorkspace(fmt.Sprintf("episode_%d_merged_", req.Episode))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("Failed to prepare merged export workspace: %v", err)})
		return
	}
	outputPath := filepath.Join(workspaceDir, fmt.Sprintf("%d-merged.mp4", req.Episode))
	if err := mergeEpisodeVideosToTarget(videos, outputPath); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("Failed to merge episode videos: %v", err)})
		return
	}
	timelineText, err := buildMergedEpisodeTimelineText(videos)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("Failed to build merged timeline text: %v", err)})
		return
	}
	zipPath := filepath.Join(workspaceDir, fmt.Sprintf("%d-merged.zip", req.Episode))
	if err := buildMergedEpisodeExportArchive(outputPath, req.Episode, timelineText, zipPath); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("Failed to build merged export archive: %v", err)})
		return
	}

	c.FileAttachment(zipPath, filepath.Base(zipPath))
}

func copyFile(src string, dst string) error {
	sourceFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer sourceFile.Close()

	destFile, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer destFile.Close()

	if _, err := io.Copy(destFile, sourceFile); err != nil {
		return err
	}

	return destFile.Sync()
}

func constructVideoFingerprintRefinePrompt(video models.Video, project models.Project, lang string) (string, string) {
	_ = lang
	multiModeAddendum := ""
	if isMultiSceneMode(project.SceneMode) {
		if project.DisableReferenceImages {
			multiModeAddendum = buildPromptOnlyVideoFingerprintRefineAddendum()
		} else {
			multiModeAddendum = buildMultiVideoFingerprintRefineAddendum()
		}
	}
	systemPrompt := `
你是一位专门为 ComfyUI LTX2.3 编写 video_fingerprint 的提示词导演。

你的任务是根据场景基础描述、场景图中文提示词和对白，重新生成完整的 video_fingerprint JSON。

硬性要求：
1. 只返回一个合法 JSON 对象，不要返回任何额外说明。
2. JSON 字段必须固定包含：recommended_fps、total_duration_seconds、prompt_neg_zh、style_zh、player_desc_zh、phases_zh。
3. phases_zh 必须是数组，数组元素必须包含：index、time_range、content、audio。
4. 这是给 ComfyUI LTX2.3 直接消费的提示词，不是摘要，不是解释，不是数据库说明。
5. 所有人物都必须改写成视觉身份描述，不能出现角色正式姓名。
6. 所有返回字段都使用中文。
7. 不要返回 narration，也不要把任何解说、旁白、内心独白写进 audio 或 content。
8. recommended_fps 固定返回 24，并统一按 24 fps 理解口型、动作连续性和总时长。
9. total_duration_seconds 必须在 3 到 15 秒之间。
10. 如果当前镜头有对白，必须严格按真实中文对白内容长度、标点停顿、换气、情绪留白与必要听者反应来估时；只要自然总长仍在 15 秒内，就给出能完整承载这段对白的时长，并在最后一句台词后自然保留少量收尾，不要因为保守而主动再拆碎；只有当自然总长明显超过 15 秒时，才说明这一镜上游应该拆开，不能靠 video_fingerprint 硬兜。
11. 如果当前镜头没有对白，则按可见动作、情绪停顿、环境微动和镜头任务估时。要更积极利用 LTX2.3 在稳定镜头中承载眼神、呼吸、手部、肩线、衣摆、发丝等细腻微表演，不要把已经能在同一镜成立的细微情绪再机械拆碎。
12. style_zh 必须写成可直接作为顶部 Style: 头部使用的风格句，简洁明确地概括镜头约束、色温、质感和主要微动方向。phases_zh 的 content 与 audio 只写内容本身，不要再自己写 Style:、Audio:、Phase 1: 这类标签前缀。
13. 台词只能出现在 phases_zh[*].audio 中；style_zh、player_desc_zh、phases_zh[*].content 只能写可见画面与动作，严禁把对白做成字幕条、对白字卡、歌词条、title card、logo、水印或其它烧录文字。phases_zh[*].audio 逐段必填，不允许空字符串；无对白段就写该段真实会听到的环境声/背景音，有对白段就写环境声加这一段真实说出的那句对白。若 phases_zh[*].audio 中存在人物对白，必须使用固定句式：用中文以【声线身份】、带【情绪或发声状态】清晰说到："中文台词"。禁止使用“角色名: 台词”“某某说：台词”这类说话人名字前缀。
13.1 只要输入里的“对白”非空，你就必须把每一句原始对白按原顺序、原文内容、原标点完整分配进 phases_zh[*].audio；每句台词必须恰好出现一次，不能遗漏、不能合并改写、不能只留在“对白”上下文字段里而不进入最终 audio。
13.2 多轮对话必须拆到多个 phase 中承载，让口型和听反应在时间上成立；禁止把整段对白粗暴塞进单个 phase 的一大串 audio 文本。
13.3 单个 phase 里的 spoken chunk 必须是短句或一个自然换气组；不要把长劝说、长电话输出、长独白整段塞进 2 到 3 秒的 phase。
13.4 如果输入对白里出现“远端中年女声”“画外成年男声”“系统提示音”等通用画外/远端声线标签，你必须把它们理解成未上镜声音来源：这些台词只能存在于 audio 中，不能在 style_zh、player_desc_zh、phases_zh[*].content 里补成第二张会说话的脸。
13.5 如果输入对白某行本身采用“说话者标签: 台词正文”的结构，那只是结构化分配格式，不是实际会被念出来的内容。写入 phases_zh[*].audio 引号里的只能是“台词正文”本身，绝不能把“说话者标签:”继续留在引号外壳子或引号内正文里让 LTX2.3 念出来。
13.6 如果当前镜头的说话者是非生命正式角色（设备、器物、产品、工具、家具、载具等），audio 壳子应使用设备/合成/电子类泛化声线，例如“中性智能设备语音”“平稳清晰合成语音”“冷静简短电子女声”；content 里只能通过灯带脉冲、指示灯亮灭、屏幕微亮、机身极轻振动、网孔区微亮、面板光感变化等非生命物理征象表现“它在说话”，不得把它拟人化成嘴唇、口腔、人脸或会张嘴的器物。
13.7 若当前镜头同时有人类/类人正式角色与非生命正式角色，并且当前段由非生命正式角色开口，人类/类人角色必须保持闭口听位；若当前段由人类/类人角色开口，非生命正式角色只作为稳定锚点和轻微灯带/屏幕/机身微动存在。不要在同一段里让两者都像主说话者。
14. prompt_neg_zh 必须主动排除 subtitle, subtitles, caption, text, on-screen text, lower-third, title card, logo, logos, watermark, watermarks, speech bubble, dialogue box, readable signage, overlay, titles, has blurbox, has subtitles, artifacts around text, unreadable text, incorrect lettering, incorrect slogan。负向提示词要直接罗列要排除的对象或错误词，不要写“不要文字水印”“别出现 logo”“禁止字幕”这类带引导词的句式。
15. 对 LTX2.3 的 i2v 而言，你必须把提示词重点放在“这张场景图里已经存在的元素接下来会如何运动和变化”，而不是长段复述静态场景本身。输入图已经定义了静态布局；你应优先描述谁在动、什么在动、背景里哪些元素持续变化。
15.0 Phase 1 必须从当前输入静态图已经成立的状态直接开始，不能先倒回到更早的到达、进门、开门、起身、落座前状态。若输入图里人物已经落位、已对坐、已入座、已在车内或已贴耳听位，phase 1 就必须从这个状态继续往后演。
15.1 每个 phase 只允许 1 个主导视觉事件：1 个主表演者、1 个主反应者，或 1 个主环境变化。不要在同一 phase 里并列写两到三名清晰人物的平权动作链；若画面里还有其他人物，他们只能保持弱活动状态，例如呼吸、视线稳定、衣摆/发丝轻动、扇面轻晃或极轻的手部反应。
15.2 如果当前静态图是 3 人及以上群像，video_fingerprint 应优先把它理解成建立镜头、观察镜头、群像反应或战位镜头：可以保留多人同框，但不要在单个 phase 内要求每个人都完成一套清晰独立动作，也不要让 3 个以上正式角色在同一 phase 里平权清晰口型。
15.2.1 若画面里的背景人物属于朝堂班列、会议座次、课堂座位、宴席席位、门侧守位、廊下守位、队伍队列、仪仗位置、柜台内外或其他固定站位系统，你必须默认他们原地维持位置关系，只做原地弱反应，例如转头、压低议论、衣袖轻晃、肩线收紧、微微躬身、同步收声或同向后撤半步；除非输入明确要求出列、换位、围拢、让路、追赶、奔逃、离席或穿越前景，否则不要让他们在 phase 里无缘由走来走去、互换前后排、跨越镜头中心或进出画面。
15.3 player_desc_zh 必须先写谁是当前镜头的主位/主表演者，再写其他人作为次位、陪体或背景层的分层站位；即使是群像无对白镜头，也必须明确哪一个人承担当前第一视觉重心。
15.4 如果当前镜头有 2 人及以上，但对白里此时只有 1 位说话者，优先把画面理解为 speaker-led single、reverse single、over-the-shoulder 或 listener reaction。非说话角色只能作为前景肩线、耳侧、后脑、边缘半身或很小的陪体存在，并默认通过浅景深、背景虚化或脏边框压低存在感；不要继续写成平权双人对坐或双人同清晰主位。
15.5 如果当前镜头的戏剧信息源来自平板、邮件、消息、文件、文档、合同、信件或其他可阅读设备/纸面，优先把画面理解为设备特写 insert、过肩读屏、主观视角 POV 或 reader reaction single。若此时角色还有台词，应表现为读后低声自语、轻声念出或压低嗓音短促反应，而不是面对空处完整发言。
15.6 如果当前镜头属于卧室、客厅、办公室、车内、酒店房间等单人室内连续表演，不要只在床边镜头和茶几/桌面镜头之间来回重复。应在不破坏空间结构、人物站位和轴线的前提下，主动组合 establishing single、环境关系镜、侧面或侧背反应、门框/窗框 framing、桌面/床头/设备 insert、手部特写、眼神反应、前景遮挡或已存在反射面的反应镜。
15.7 单人室内连续镜头之间，至少让景别、角度、焦点层次、前景遮挡或信息源主物发生一种合理变化；不要机械重复同两个固定构图点位。
15.8 若当前镜头是“人类/类人正式角色 + 非生命正式角色”的连续互动，不要连续几镜都停在同一个固定宽镜。应主动在 object-led insert、人类反应 single、过肩看向设备、设备/手部 insert、人类 speaking single、listener reaction 之间轮换，让镜头真正有 coverage 变化。
15.9 若当前镜头绑定了 2 名正式角色，它应当承载一个更完整的互动单元，通常可落在约 5 到 9 秒：至少是一句说话 + 明显反应，或一问一答，或一方说话后另一方的可见听位变化。若本镜只有一方说了一句短台词而另一方没有重要可见反应，就更适合上游拆成单主角镜、object-led insert 或独立 reaction single。
16. 每个 phase 都必须至少写出 1 到 3 个可执行微动锚点，例如连续滴水、火苗持续轻跳、雾气缓慢贴地移动、衣摆轻擦、发束轻摆、积水反光变化、脚尖持续加压、手指缓慢收紧、胸口轻微起伏。phase 时长通常可落在约 2.5 到 5 秒；稳定对白或反应段可以更饱满，不要机械全切成 2 到 4 秒。
17. 如果当前镜头里已经有雨、雪、滴水、烟雾、蒸汽、火苗、布帘、树叶、草绳、挂饰、倒影、水面、发丝、衣摆或其它可持续变化的背景元素，这些内容必须明确进入最终会送给视频模型的提示词字段，不要在视频阶段忽略掉。
18. 对人物镜头，优先写姿态延续、口型节拍、局部微动作和稳定背景微动；对空镜，优先写环境、锁定构图、环境微动和禁人物补全。不要让所有镜头都写成同一套模板。
19. 如果当前镜头已经明确人物朝向，例如背影、侧背、过肩、三分之四侧脸或低头不露正脸，你必须保持这个朝向约束，不允许模型自行补出突然转身、突然回头或突然出现完整正脸。
19.1 只要当前镜头有对白并需要口型，当前主说话者就必须同时是当前画面最可读的口型承载位；优先让说话者占据清晰正脸、三分之四侧脸或清晰半侧脸主位。若听者反而是更大的清晰正脸，而说话者只在前景肩背、边缘半脸、背侧、虚焦层或过小比例层，你不得把口型错绑到听者脸上，并应把这种情况视为上游 coverage 需要改成说话者主位的风险镜头。
19.2 听者即使清楚入画，也必须保持闭口，只承担听位微反应；不要因为听者更靠近镜头、更清晰或更居中，就让他接管错误口型。
19.3 若当前静态图里的菜单、账单夹、茶杯、手机、钥匙、文件夹、伞等稳定道具已经处于明确状态，例如“已平放桌上”“已拿在手里”“已贴耳听位”“已合上放在右前”，phases 必须从这个当前状态继续，不得先倒回更早状态再重新拿起、放下、打开、合上或贴耳。
19.4 若当前静态图里人物正在看平板、邮件界面、消息界面、文件页、合同页或其他信息源，phases 应持续保持“阅读关系”成立：设备仍是视线焦点、手部支撑点或前景主物。若有台词，应表现为对着设备低声念出或读后短促自语，而不是突然脱离信息源关系去对空宣告。
19.5 单人远程通讯镜头里，如果 audio 同时出现可见人物和远端/画外声音，画面中的可见人物一次只能承担一个角色：当远端/画外声音在说话时，可见人物必须闭口听位，content 应只写听位反应、设备、手部、肩线、呼吸或环境微动；不要让同一张脸在前后 phase 里像两个人一样来回切换身份。
19.6 若当前静态图里同时存在人类/类人正式角色与非生命正式角色，并且该非生命正式角色是当前说话主位，phases 应优先围绕非生命角色本体近景、灯带/屏幕/面板微动、人类闭口听位反应、过肩看向非生命角色、或人类手部与设备的空间关系来组织；不要让人类脸去承接非生命角色的说话声，也不要让设备每段都被拍成同一张固定居中宽镜。
19.7 当前镜头里只要仍有可见听者、陪体、边缘主角或非生命正式角色存在，phases 也应给这些可见主体 1 到 2 个细小微动，例如眨眼、呼吸起伏、眼神微偏、手指收紧、肩线轻动、吞咽停顿，或设备灯带/屏幕/指示灯/机身的轻微变化；不要把非主说话者写成完全冻结的静止摆件。
20. 不允许为了“让画面更丰富”而凭空新增当前场景图中文提示词与场景基础描述里都没有出现的新主体、新手部、新动物、新器物或新生活化行为。
21. 只要镜头主体是人类或类人角色，就必须默认保持正常人体结构：两只手、两条手臂、两条腿、手指数量正常、肢体连接合理；并保持关键道具的正确握姿、朝向和连续性。
` + multiModeAddendum

	userPrompt := fmt.Sprintf(`
请根据以下内容，重新生成完整 video_fingerprint JSON。

场景基础描述：
%s

场景图中文提示词：
%s

对白：
%s

补充要求：
- 你必须优先继承场景基础描述与场景图中文提示词里已经出现的可动背景元素，不要只写人物动作。
- 场景基础描述主要用来理解这一镜的静态画面与叙事重点；场景图中文提示词主要用来理解后续视频应抓住哪些动态锚点、朝向约束、微动作和环境可动元素。你应区分这两者的职责，不要把它们都写成一段静态复述。
- 如果当前画面里已经有草、树叶、枝条、雨、雨丝、滴水、积水、涟漪、倒影、雾气、烟雾、蒸汽、火苗、布帘、草绳、挂饰、纸角、窗纸、尘土、发丝、衣摆等能持续变化的元素，这些元素必须尽量写回 style 与 phases。
- 如果当前镜头里下雨，就必须明确继续下雨；如果场景里有草，就应写草叶或碎草在风里轻晃；如果有积水，就应写反光或涟漪的轻微变化。不要把这些已存在的动态背景省略掉。
- 对 LTX2.3 i2v 而言，不要长段复述输入图里已经静止可见的背景本体；更重要的是把这些背景元素“接下来怎么动”写出来。
- Phase 1 必须从输入静态图已经成立的状态直接开始，不得先回退到更早的门口出现、移门滑开、人物走近、人物起身或入座前状态；如果你发现必须回退，说明这一镜在上游还该继续拆。
- 即使当前镜头是多人群像，phases 里也只能保留 1 个主导动作、1 个主导反应或 1 个主导环境变化；不要在同一个 phase 里用分号并列多名清晰人物的独立动作。其他人物最多只保留弱活动，例如呼吸、视线维持、衣摆/发丝轻动、扇面轻晃。
- 若当前镜头里的背景人物本来就属于固定站位系统，例如朝堂班列、会议座次、课堂座位、宴席席位、侍卫守位、柜台内外或队列站位，你必须默认他们原地维持位置，只做原地弱反应；不要无缘由把他们写成来回走动、前后穿插、互换位置或穿越主位人物前景。
- 如果当前镜头是稳定的双人对话回合，只要总量还能自然落在约 8 到 10 秒内，就不要把它机械切成一串 2 到 3 秒小片；应优先保持更完整的 master/two-shot、dirty single、reverse single、over-the-shoulder、listener reaction 与 insert 组合带来的镜头感。
- 如果当前镜头的戏剧重点是惊讶、迟疑、被戳中、认知转折、沉默压迫或高潮反应，请优先把它理解成单主角 reaction single、close-up、medium close-up、tight single、眼神反应或手部 insert；除非第二个正式角色或非生命正式角色必须作为清晰边缘锚点存在，不要继续沿用双人平铺宽镜。
- 只要下面“对白”非空，你就必须把每一句原始对白逐句拆进 phases_zh[*].audio，保持原顺序、原文内容和原标点；不能只在上面的对白上下文里保留整段文本，而让最终 audio 只剩环境声。
- 如果某条对白在结构上写成“说话者标签: 台词正文”，audio 引号里只能保留“台词正文”；无论这个标签是正式角色名还是“远端中年女声”“系统提示音”，都不得再被放进引号外壳子或引号内正文开头。
- 如果当前镜头的说话者是非生命正式角色（设备、器物、产品、工具、家具、载具等），audio 壳子应使用设备/合成/电子类泛化声线，例如“中性智能设备语音”“平稳清晰合成语音”；content 里只能写灯带脉冲、指示灯亮灭、屏幕微亮、机身极轻振动、网孔区微亮等非生命物理征象，不得把它拟人化成嘴唇、人脸或会张嘴的器物。
- 如果当前镜头同时有人类/类人正式角色与非生命正式角色，而当前段由非生命正式角色开口，人类/类人角色必须保持闭口听位；反过来若当前段由人类/类人角色开口，非生命正式角色只作为稳定锚点和轻微灯带/屏幕/机身微动存在。
- 多轮对白必须分配到多个 phase，不要把整段台词堆进单个 phase；对白 phase 的 content 只写可见动作与口型承载位，不要把对白文字抄进 content。
- 若单条对白本身很长，必须按自然停顿、换气点和情绪节点拆到多个 phase；不要出现 2 到 3 秒的 phase 却承载一整段长劝说或长电话内容。
- 只要当前镜头有对白并需要口型，当前主说话者就必须同时是当前画面最可读的口型承载位；若听者反而是更大的清晰正脸，而说话者只在前景肩背、边缘半脸、背侧、虚焦层或过小比例层，不要为了配合音频让听者张嘴；听者必须闭口，只保留听位微反应。
- 如果当前画面里有 2 人及以上，但这一段只有 1 位说话者，请优先把镜头写成单人主位、反打、过肩或听位反应，并默认使用浅景深、背景虚化、前景脏边框去压低非说话角色的存在感；不要继续把两个人都写成同等清晰、同等面积、同等中心权重的双人平铺对话镜头。
- 如果当前镜头是“人类/类人正式角色 + 非生命正式角色”的连续互动，请主动在 object-led insert、人类反应 single、过肩看向设备、设备/手部 insert、人类 speaking single、listener reaction 之间轮换，不要连续几镜都停在同一个固定宽镜。
- 如果当前镜头绑定了 2 名正式角色，它应当承载一个更完整的互动单元，通常可落在约 5 到 9 秒：至少是一句说话 + 明显反应，或一问一答，或一方说话后另一方的可见听位变化。若本镜只有一方说了一句短台词而另一方没有重要可见反应，就更适合上游拆成单主角镜、object-led insert 或独立 reaction single。
- 如果当前画面里有 3 人及以上，请把当前 phase 的正式主位控制在 1 到 2 人，其余人物只能作为模糊背影、肩线、耳侧、边缘半身、遮挡前景或群像弱反应存在；不要让 3 个以上正式角色在同一 phase 里平权清晰开口。
- 如果当前静态图里的菜单、账单夹、茶杯、手机、钥匙、文件夹、伞等稳定道具已经处于明确状态，你必须从这个当前状态继续写视频，不得先倒回更早状态再重新拿起、放下、打开、合上或贴耳。
- 如果当前静态图里的戏剧信息源是平板、邮件、消息、文件、文档、合同、信件或其他阅读对象，请优先写成设备特写、过肩读屏、主观视角或读后反应；若此时角色开口，应让它成为看着信息源的低声自语、轻声念出或读后短促判断，不要写成角色对空气完整发言。
- 不要依赖生成可读小字来表达阅读内容；只需让设备或纸面明确属于“邮件界面”“消息界面”“文件页”“合同页”等类别，再用视线停顿和读后反应承担叙事。
- 对卧室、客厅、办公室、车内、酒店房间等单人室内连续镜头，请主动给出更丰富的 coverage 变化，不要只在床、沙发、茶几、书桌等两个固定点位间机械往返；在同一空间锁定下，应优先变化景别、角度、前景遮挡、焦点层次、设备/手部 insert、眼神反应或门框/窗框 framing。
- 当前镜头里只要仍有可见听者、陪体、边缘主角或非生命正式角色存在，phases 也应给这些可见主体 1 到 2 个细小微动，例如眨眼、呼吸起伏、眼神微偏、手指收紧、肩线轻动、吞咽停顿，或设备灯带/屏幕/指示灯/机身的轻微变化；不要把非主说话者写成完全冻结的静止摆件。
- 只要当前镜头已经能够靠稳定构图、眼神、呼吸和细微动作讲清，就不要为了“更细”而主动把一个成立的稳定镜头写得过碎。`, strings.TrimSpace(video.Scene.Description), strings.TrimSpace(video.Scene.PositivePrompt), "")

	return systemPrompt, userPrompt
}

func callLLMVideoFingerprint(provider models.LLMProvider, system, user string, taskID string) (*VideoFingerprintPayload, error) {
	content, err := requestLLMContent(provider, system, user, taskID, 10*time.Minute, 5, "正在请求 LLM 重新生成视频指纹...", "视频指纹重推")
	if err != nil {
		return nil, err
	}

	db.DB.Create(&models.SystemLog{
		Level:     LogLevelInfo,
		Message:   llmLogMessage("LLM 完整返回(视频指纹重推)", provider),
		Details:   content,
		CreatedAt: time.Now(),
	})

	jsonContent := cleanupLLMJSON(content)
	payload, err := parseVideoFingerprintPayload(jsonContent)
	if err != nil {
		return nil, err
	}
	warnNegativePromptLeadIn("video_fingerprint.prompt_neg_zh", payload.PromptNegZH)
	warnNegativePromptLeadIn("video_fingerprint.prompt_neg_en", payload.PromptNegEN)
	if strings.TrimSpace(payload.PromptNegZH) == "" || strings.TrimSpace(payload.StyleZH) == "" || strings.TrimSpace(payload.PlayerDescZH) == "" {
		return nil, fmt.Errorf("video_fingerprint missing required negative/style/player_desc fields")
	}
	if len(payload.PhasesZH) == 0 {
		return nil, fmt.Errorf("video_fingerprint phases must not be empty")
	}
	if err := validateVideoFingerprintAudioText(payload); err != nil {
		return nil, err
	}
	if err := validateVideoFingerprintDuration(payload); err != nil {
		return nil, err
	}
	if err := validateVideoFingerprintPhaseContent(payload); err != nil {
		return nil, err
	}
	return payload, nil
}

// HandleAutoGenerateVideoTask processes LLM regeneration of video_fingerprint only.
func HandleAutoGenerateVideoTask(t *models.Task) (interface{}, error) {
	var payload struct {
		VideoID   uint `json:"video_id"`
		ProjectID uint `json:"project_id"`
	}
	if err := json.Unmarshal([]byte(t.Payload), &payload); err != nil {
		return nil, fmt.Errorf("invalid payload: %v", err)
	}

	var video models.Video
	if err := db.DB.First(&video, payload.VideoID).Error; err != nil {
		return nil, fmt.Errorf("video not found")
	}
	if err := ensureVideoSceneLoaded(&video, true); err != nil {
		return nil, err
	}
	var project models.Project
	if err := db.DB.First(&project, payload.ProjectID).Error; err != nil {
		return nil, fmt.Errorf("project not found")
	}
	var llmProvider models.LLMProvider
	if err := db.DB.Where("is_active = ?", true).First(&llmProvider).Error; err != nil {
		return nil, fmt.Errorf("no active LLM provider found")
	}

	lang := loadPromptLanguage()
	systemPrompt, userPrompt := constructVideoFingerprintRefinePrompt(video, project, lang)
	Log(LogLevelInfo, llmLogMessage("LLM Request", llmProvider), fmt.Sprintf("Starting video fingerprint regeneration for video: %d", video.ID))
	Log(LogLevelInfo, llmLogMessage("LLM Request Prompt", llmProvider), fmt.Sprintf("System: %s\n\nUser: %s", systemPrompt, userPrompt))

	fingerprintPayload, err := callLLMVideoFingerprint(llmProvider, systemPrompt, userPrompt, t.ID)
	if err != nil {
		return nil, err
	}

	serialized, err := json.MarshalIndent(fingerprintPayload, "", "  ")
	if err != nil {
		return nil, err
	}

	video.VideoFingerprint = string(serialized)
	video.UpdatedAt = time.Now()
	if err := db.DB.Save(&video).Error; err != nil {
		return nil, fmt.Errorf("failed to update video fingerprint: %v", err)
	}
	BroadcastUpdate("video", video.ID)

	return "Video fingerprint regenerated successfully", nil
}

// waitForVideoCompletion blocks until video is generated or failed
func waitForVideoCompletion(promptID string, videoID uint, projectID uint) error {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	var project models.Project
	db.DB.First(&project, projectID)

	for {
		select {
		case <-ticker.C:
			history, err := GetComfyHistory(promptID)
			if err == nil {
				if outputs, ok := history["outputs"].(map[string]interface{}); ok {
					for _, nodeOutput := range outputs {
						var fileData map[string]interface{}
						if nodeMap, ok := nodeOutput.(map[string]interface{}); ok {
							if gifs, ok := nodeMap["gifs"].([]interface{}); ok && len(gifs) > 0 {
								fileData = gifs[0].(map[string]interface{})
							} else if images, ok := nodeMap["images"].([]interface{}); ok && len(images) > 0 {
								fileData = images[0].(map[string]interface{})
							}
						}

						if fileData != nil {
							filename := fileData["filename"].(string)
							subfolder := fileData["subfolder"].(string)
							typeStr := fileData["type"].(string)

							saveDir := filepath.Join("output", project.Code, "videos")
							if err := os.MkdirAll(saveDir, 0755); err != nil {
								return err
							}
							ext := filepath.Ext(filename)
							saveFilename := fmt.Sprintf("video_%d_%d%s", videoID, time.Now().Unix(), ext)
							savePath := filepath.Join(saveDir, saveFilename)

							if err := DownloadComfyImage(filename, subfolder, typeStr, savePath); err == nil {
								var v models.Video
								if err := db.DB.First(&v, videoID).Error; err == nil {
									webPath := "/" + filepath.ToSlash(savePath)
									v.GeneratedVideo = webPath
									v.Status = "generated"
									db.DB.Save(&v)
									Log(LogLevelInfo, "Video Saved", webPath)
									Log(LogLevelInfo, "视频生成完成", fmt.Sprintf("Video ID %d 已完成，文件: %s", videoID, webPath))
									BroadcastUpdate("video", videoID)
								}
								return nil
							}
						}
					}
				}
				continue
			}
		}
	}
}

func resolveSelectedVideoWorkflowFamily() (string, error) {
	var setting models.SystemSettings
	if err := db.DB.Where("key = ?", KeyDefaultVideoModel).First(&setting).Error; err != nil {
		return "", err
	}
	workflowName := strings.TrimSpace(setting.Value)
	if workflowName == "" {
		return "", fmt.Errorf("default video model is empty")
	}

	files, err := filepath.Glob(filepath.Join("workflows", "*.json"))
	if err != nil {
		return "", err
	}
	for _, file := range files {
		meta, err := workflow.ParseWorkflow(file)
		if err != nil || meta.Type != "video" || meta.WorkflowName != workflowName {
			continue
		}
		name := strings.ToLower(strings.TrimSpace(meta.WorkflowName))
		fileName := strings.ToLower(filepath.Base(file))
		if strings.Contains(name, "ltx") || strings.Contains(fileName, "ltx") {
			return "ltx", nil
		}
		return "", fmt.Errorf("only the LTX video workflow is supported in this version")
	}

	return "", fmt.Errorf("workflow file for '%s' not found", workflowName)
}

func sanitizeRecommendedVideoFPS(recommended int, fallback int) int {
	_ = recommended
	_ = fallback
	return 24
}

func sanitizeRecommendedDurationSeconds(recommended int) int {
	if recommended >= minVideoTotalDurationSeconds {
		return recommended
	}
	return 0
}

func convertRecommendedDurationToFrameCount(recommendedSeconds int, fps int, fallback int) int {
	seconds := sanitizeRecommendedDurationSeconds(recommendedSeconds)
	if seconds == 0 {
		if fallback > 0 {
			return fallback
		}
		return 0
	}

	fps = sanitizeRecommendedVideoFPS(fps, 24)
	return fps*seconds + 1
}

func validateSilentVideoPromptResponse(resp *ScenePromptResponse) error {
	forbiddenTerms := []string{
		"subtitle", "subtitles", "caption", "text", "speech bubble", "dialogue box", "watermark", "on-screen text",
		"字幕", "字幕条", "文字", "文本", "对话框", "气泡", "水印", "台词", "对白",
	}
	combined := strings.ToLower(strings.Join([]string{
		resp.PromptPosZH,
		resp.PromptNegZH,
		resp.PromptPosEN,
		resp.PromptNegEN,
		resp.PlayerDesc,
	}, "\n"))
	for _, term := range forbiddenTerms {
		if strings.Contains(combined, strings.ToLower(term)) {
			return fmt.Errorf("返回内容含有无对白场景禁用词: %s", term)
		}
	}
	return nil
}

// GenerateVideo handles manual video generation request
func GenerateVideo(c *gin.Context) {
	videoID := c.Param("videoId")
	var video models.Video
	if err := db.DB.First(&video, videoID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Video not found"})
		return
	}
	if err := validateVideoReadyForGeneration(&video); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if err := resetVideoForGeneration(&video); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to reset video before generation"})
		return
	}
	BroadcastUpdate("video", video.ID)

	taskPayload := map[string]interface{}{
		"video_id":   video.ID,
		"project_id": video.ProjectID,
	}
	t, err := task.GlobalTaskManager.AddTask("render_video", taskPayload)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to submit task"})
		return
	}
	c.JSON(http.StatusAccepted, gin.H{"message": "Video generation task submitted", "task_id": t.ID})
}

// BatchGenerateVideos handles batch video generation
func BatchGenerateVideos(c *gin.Context) {
	projectID := c.Param("id")
	// Verify project
	var project models.Project
	if err := db.DB.First(&project, projectID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Project not found"})
		return
	}
	var readyCount int64
	if err := db.DB.Model(&models.Video{}).
		Where("project_id = ? AND video_status IN ?", project.ID, []string{"draft", "pending", "failed"}).
		Where("COALESCE(generated_image, '') <> ''").
		Count(&readyCount).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to inspect videos"})
		return
	}
	if readyCount == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "No ready videos found. Please generate scene images first."})
		return
	}

	taskPayload := map[string]interface{}{
		"project_id": project.ID,
	}

	t, err := task.GlobalTaskManager.AddTask("batch_generate_videos", taskPayload)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to submit task"})
		return
	}

	c.JSON(http.StatusAccepted, gin.H{"message": "Batch video generation task submitted", "task_id": t.ID})
}

// HandleBatchGenerateVideosTask processes batch video generation
func HandleBatchGenerateVideosTask(t *models.Task) (interface{}, error) {
	var payload struct {
		ProjectID uint `json:"project_id"`
	}
	if err := json.Unmarshal([]byte(t.Payload), &payload); err != nil {
		return nil, fmt.Errorf("invalid payload: %v", err)
	}

	var videos []models.Video
	// Batch generation should include both pending and previously failed videos.
	// Keep a deterministic shot order so the queue follows episode/scene progression.
	if err := db.DB.
		Where("project_id = ? AND video_status IN ?", payload.ProjectID, []string{"draft", "pending", "failed"}).
		Order("episode asc, scene_number asc, id asc").
		Find(&videos).Error; err != nil {
		return nil, fmt.Errorf("failed to fetch videos: %v", err)
	}

	total := len(videos)
	if total == 0 {
		return "No pending or failed videos to generate", nil
	}

	if getConfiguredVideoGenerationProvider() == VideoGenerationProviderLocal {
		if _, err := resolveSelectedVideoWorkflowFamily(); err != nil {
			return nil, err
		}
	}

	queuedCount := 0
	for i, video := range videos {
		progress := int(float64(i) / float64(total) * 100)
		task.GlobalTaskManager.UpdateTaskProgress(t.ID, progress, fmt.Sprintf("Processing video %d/%d (ID: %d)", i+1, total, video.ID))

		if err := resetVideoForGeneration(&video); err != nil {
			Log(LogLevelError, "Batch Video Error", fmt.Sprintf("Failed to reset video %d: %v", video.ID, err))
			continue
		}
		BroadcastUpdate("video", video.ID)

		if err := queueConfiguredVideoRender(video.ID, payload.ProjectID); err != nil {
			Log(LogLevelError, "Batch Video Error", fmt.Sprintf("Failed to queue video %d: %v", video.ID, err))
			continue
		}
		queuedCount++
		time.Sleep(300 * time.Millisecond)
	}

	return fmt.Sprintf("Batch processing completed. %d/%d videos queued for generation.", queuedCount, total), nil
}

func HandleRenderVideoTask(t *models.Task) (interface{}, error) {
	var payload struct {
		VideoID   uint `json:"video_id"`
		ProjectID uint `json:"project_id"`
	}
	if err := json.Unmarshal([]byte(t.Payload), &payload); err != nil {
		return nil, fmt.Errorf("invalid payload: %v", err)
	}

	if err := queueConfiguredVideoRender(payload.VideoID, payload.ProjectID); err != nil {
		return nil, err
	}
	return "Video render submitted successfully", nil
}

// triggerVideoGeneration queues the ComfyUI workflow
func triggerVideoGeneration(video models.Video) (string, error) {
	// 1. Get Project & Settings
	var project models.Project
	if err := db.DB.First(&project, video.ProjectID).Error; err != nil {
		return "", fmt.Errorf("project not found")
	}

	var setting models.SystemSettings
	if err := db.DB.Where("key = ?", KeyDefaultVideoModel).First(&setting).Error; err != nil {
		return "", fmt.Errorf("default video model not set")
	}
	workflowName := setting.Value
	if workflowName == "" {
		return "", fmt.Errorf("default video model is empty")
	}

	// 2. Load Workflow
	files, _ := filepath.Glob(filepath.Join("workflows", "*.json"))
	var targetFile string
	for _, file := range files {
		meta, err := workflow.ParseWorkflow(file)
		if err == nil && meta.WorkflowName == workflowName {
			targetFile = file
			break
		}
	}
	if targetFile == "" {
		return "", fmt.Errorf("workflow file for '%s' not found", workflowName)
	}
	workflowLabel := workflowDisplayNameFromPath(targetFile)

	meta, err := workflow.ParseWorkflow(targetFile)
	if err != nil {
		return "", fmt.Errorf("failed to parse workflow: %v", err)
	}

	data, err := os.ReadFile(targetFile)
	if err != nil {
		return "", fmt.Errorf("failed to read workflow file: %v", err)
	}
	var wfJSON map[string]interface{}
	if err := json.Unmarshal(data, &wfJSON); err != nil {
		return "", fmt.Errorf("failed to unmarshal workflow: %v", err)
	}

	// 3. Inject Parameters
	setInput := func(nodeID string, key string, value interface{}) {
		if nodeID == "" {
			return
		}
		if node, ok := wfJSON[nodeID].(map[string]interface{}); ok {
			if inputs, ok := node["inputs"].(map[string]interface{}); ok {
				inputs[key] = value
			}
		}
	}

	var firstSegment models.VideoSegment
	if err := db.DB.Where("video_id = ?", video.ID).Order("segment_index asc").First(&firstSegment).Error; err != nil {
		return "", fmt.Errorf("video has no planned segments")
	}
	warnNegativePromptLeadIn(fmt.Sprintf("video=%d first segment negative prompt", video.ID), firstSegment.NegativePrompt)
	positivePrompt := strings.TrimSpace(firstSegment.PositivePrompt)
	negativePrompt := buildSegmentNegativePrompt(firstSegment.NegativePrompt)
	setInput(meta.PositiveNodeID, meta.PositiveInputKey, positivePrompt)
	setInput(meta.NegativeNodeID, meta.NegativeInputKey, negativePrompt)

	// Seed
	seed := getConfiguredGlobalSeed()
	setInput(meta.SeedNodeID, meta.SeedInputKey, seed)

	// Dims
	width, height := getConfiguredVideoSize()
	setInput(meta.WidthNodeID, meta.WidthInputKey, width)
	setInput(meta.HeightNodeID, meta.HeightInputKey, height)

	if firstSegment.FPS > 0 {
		setInput(meta.FPSNodeID, meta.FPSInputKey, firstSegment.FPS)
	}
	if firstSegment.Length > 0 {
		setInput(meta.LengthNodeID, meta.LengthInputKey, firstSegment.Length)
	}

	// Inject Input Image (Scene Generated Image)
	// Need to find LoadImage node or similar. Video workflows usually take an image input.
	// We need to identify the image input node.
	// Strategy: Search for "LoadImage" node.
	var imageNodeID string
	for id, node := range wfJSON {
		if nodeMap, ok := node.(map[string]interface{}); ok {
			if classType, ok := nodeMap["class_type"].(string); ok {
				if classType == "LoadImage" {
					imageNodeID = id
					break
				}
			}
		}
	}

	if err := ensureVideoSceneLoaded(&video, false); err != nil {
		return "", err
	}
	scene := video.Scene
	if scene.GeneratedImage == "" {
		return "", fmt.Errorf("scene has no generated image")
	}

	// Upload/Set Image
	cleanPath := strings.TrimPrefix(scene.GeneratedImage, "/")
	absPath, _ := filepath.Abs(cleanPath)
	uploadedName, err := UploadToComfyUIInput(absPath)
	if err != nil {
		Log(LogLevelError, "ComfyUI Upload Failed", fmt.Sprintf("Failed to upload scene image %s: %v", absPath, err))
		if imageNodeID != "" {
			setInput(imageNodeID, "image", absPath)
		}
	} else {
		if imageNodeID != "" {
			setInput(imageNodeID, "image", uploadedName)
		}
	}

	// 4. Queue
	logComfyWorkflowPayload("Video ComfyUI Workflow Payload", workflowLabel, wfJSON)

	promptID, err := QueueComfyPrompt(wfJSON)
	if err == nil {
		if workflowLabel != "" {
			if saveErr := db.DB.Model(&models.Video{}).
				Where("id = ?", video.ID).
				Update("video_generated_workflow", workflowLabel).Error; saveErr != nil {
				Log(LogLevelWarn, "Video Workflow Save Failed", fmt.Sprintf("video=%d workflow=%s err=%v", video.ID, workflowLabel, saveErr))
			}
		}
	}
	return promptID, err
}

func pollVideoGeneration(promptID string, videoID uint, projectID uint) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	// Get Project Code
	var project models.Project
	db.DB.First(&project, projectID)

	for {
		select {
		case <-ticker.C:
			history, err := GetComfyHistory(promptID)
			if err == nil {
				if outputs, ok := history["outputs"].(map[string]interface{}); ok {
					for _, nodeOutput := range outputs {
						// Check for GIFs/Videos (ComfyUI usually returns gifs or mp4s in 'gifs' or 'images' key depending on node)
						// VHS_VideoCombine returns 'gifs' key usually containing filename
						// Or 'images' if it saves as image sequence/gif

						var fileData map[string]interface{}

						if nodeMap, ok := nodeOutput.(map[string]interface{}); ok {
							if gifs, ok := nodeMap["gifs"].([]interface{}); ok && len(gifs) > 0 {
								fileData = gifs[0].(map[string]interface{})
							} else if images, ok := nodeMap["images"].([]interface{}); ok && len(images) > 0 {
								fileData = images[0].(map[string]interface{})
							}
						}

						if fileData != nil {
							filename := fileData["filename"].(string)
							subfolder := fileData["subfolder"].(string)
							typeStr := fileData["type"].(string)

							// Download
							saveDir := filepath.Join("output", project.Code, "videos")
							if err := os.MkdirAll(saveDir, 0755); err != nil {
								return
							}
							// Extension
							ext := filepath.Ext(filename)
							saveFilename := fmt.Sprintf("video_%d_%d%s", videoID, time.Now().Unix(), ext)
							savePath := filepath.Join(saveDir, saveFilename)

							if err := DownloadComfyImage(filename, subfolder, typeStr, savePath); err == nil {
								var v models.Video
								if err := db.DB.First(&v, videoID).Error; err == nil {
									webPath := "/" + filepath.ToSlash(savePath)
									v.GeneratedVideo = webPath
									v.Status = "generated"
									db.DB.Save(&v)
									Log(LogLevelInfo, "Video Saved", webPath)
									BroadcastUpdate("video", videoID)
								}
							}
							return
						}
					}
				}
				continue
			}
		}
	}
}
