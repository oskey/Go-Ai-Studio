package api

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"kt-ai-studio/internal/db"
	"kt-ai-studio/internal/models"

	volcvisual "github.com/volcengine/volc-sdk-golang/service/visual"
)

const jimengWorkflowLabel = "即梦视频3.0Pro"

type jimengAsyncSubmitResponse struct {
	Code      int    `json:"code"`
	Message   string `json:"message"`
	RequestID string `json:"request_id"`
	Data      struct {
		TaskID string `json:"task_id"`
	} `json:"data"`
}

type jimengAsyncGetResultResponse struct {
	Code      int    `json:"code"`
	Message   string `json:"message"`
	RequestID string `json:"request_id"`
	Data      struct {
		Status     string `json:"status"`
		VideoURL   string `json:"video_url"`
		AIGCTagged bool   `json:"aigc_meta_tagged"`
	} `json:"data"`
}

type jimengStatusSnapshot struct {
	TaskID       string                        `json:"task_id"`
	HTTPStatus   int                           `json:"http_status"`
	Code         int                           `json:"code"`
	Message      string                        `json:"message"`
	RequestID    string                        `json:"request_id"`
	Status       string                        `json:"status"`
	VideoURL     string                        `json:"video_url"`
	AIGCTagged   bool                          `json:"aigc_meta_tagged"`
	Raw          map[string]interface{}        `json:"raw"`
	PrettyJSON   string                        `json:"pretty_json"`
	Parsed       *jimengAsyncGetResultResponse `json:"parsed,omitempty"`
	CanRetrieve  bool                          `json:"can_retrieve"`
}

func getConfiguredVideoRenderSize() (int, int) {
	if getConfiguredVideoGenerationProvider() == VideoGenerationProviderJimeng {
		preset := getConfiguredJimengVideoPreset()
		return preset.Width, preset.Height
	}
	return getConfiguredVideoSize()
}

func queueConfiguredVideoRender(videoID uint, projectID uint) error {
	switch getConfiguredVideoGenerationProvider() {
	case VideoGenerationProviderJimeng:
		return queueJimengVideoRender(videoID, projectID)
	default:
		return queueLTXVideoRender(videoID, projectID)
	}
}

func newJimengVisualClient() (*volcvisual.Visual, error) {
	ak, sk := getConfiguredJimengCredentials()
	if strings.TrimSpace(ak) == "" || strings.TrimSpace(sk) == "" {
		return nil, fmt.Errorf("jimeng credentials are not configured")
	}

	client := volcvisual.NewInstance()
	client.Client.SetAccessKey(strings.TrimSpace(ak))
	client.Client.SetSecretKey(strings.TrimSpace(sk))
	client.SetRegion(volcvisual.DefaultRegion)

	baseURL := strings.TrimSpace(getConfiguredJimengAPIBase())
	if baseURL == "" {
		baseURL = defaultSettingValue(KeyJimengAPIBase)
	}
	if !strings.Contains(baseURL, "://") {
		baseURL = "https://" + baseURL
	}
	parsed, err := url.Parse(baseURL)
	if err != nil {
		return nil, fmt.Errorf("invalid jimeng api base: %w", err)
	}
	if parsed.Scheme != "" {
		client.SetSchema(parsed.Scheme)
	}
	if parsed.Host != "" {
		client.SetHost(parsed.Host)
	}
	return client, nil
}

func queueJimengVideoRender(videoID uint, projectID uint) error {
	var video models.Video
	if err := db.DB.First(&video, videoID).Error; err != nil {
		return fmt.Errorf("video not found")
	}
	if err := ensureVideoSceneLoaded(&video, true); err != nil {
		return err
	}

	var project models.Project
	if err := db.DB.First(&project, projectID).Error; err != nil {
		return fmt.Errorf("project not found")
	}
	if strings.TrimSpace(video.Scene.GeneratedImage) == "" {
		return fmt.Errorf("scene has no generated image")
	}

	client, err := newJimengVisualClient()
	if err != nil {
		return err
	}

	imageBase64, err := encodeVideoSourceImageBase64(video.Scene.GeneratedImage)
	if err != nil {
		return err
	}

	if err := removeGeneratedVideoAsset(video.GeneratedVideo); err != nil {
		return err
	}
	if err := clearVideoSegments(video.ID); err != nil {
		return err
	}

	preset := getConfiguredJimengVideoPreset()
	durationSeconds := video.DurationSeconds
	if durationSeconds <= 0 {
		durationSeconds = video.Scene.DurationSeconds
	}
	frames := convertRecommendedDurationToFrameCount(durationSeconds, 24, 0)
	if frames <= 0 {
		return fmt.Errorf("duration_seconds is invalid for jimeng video generation")
	}
	prompt := strings.TrimSpace(video.VideoPrompt)
	if prompt == "" {
		prompt = strings.TrimSpace(video.Scene.VideoPrompt)
	}
	if prompt == "" {
		return fmt.Errorf("video prompt is empty")
	}

	seed := getConfiguredGlobalSeed()
	if seed == 0 {
		seed = -1
	}
	requestBody := map[string]interface{}{
		"req_key":            getConfiguredJimengReqKey(),
		"prompt":             prompt,
		"binary_data_base64": []string{imageBase64},
		"seed":               seed,
		"frames":             frames,
	}
	if preset.AspectRatio != "" {
		requestBody["aspect_ratio"] = preset.AspectRatio
	}

	rawResp, statusCode, err := client.CVSync2AsyncSubmitTask(requestBody)
	if err != nil {
		return fmt.Errorf("jimeng submit failed: %w", err)
	}
	submitResp, err := parseJimengSubmitResponse(rawResp)
	if err != nil {
		return err
	}
	if statusCode != http.StatusOK || submitResp.Code != 10000 || strings.TrimSpace(submitResp.Data.TaskID) == "" {
		return fmt.Errorf("jimeng submit failed: code=%d message=%s request_id=%s", submitResp.Code, strings.TrimSpace(submitResp.Message), strings.TrimSpace(submitResp.RequestID))
	}

	video.PositivePrompt = prompt
	video.NegativePrompt = ""
	video.JMTaskID = strings.TrimSpace(submitResp.Data.TaskID)
	video.GeneratedVideo = ""
	video.GeneratedWorkflow = jimengWorkflowLabel
	video.Width = preset.Width
	video.Height = preset.Height
	video.Status = "generating"
	video.UpdatedAt = time.Now()
	if err := db.DB.Save(&video).Error; err != nil {
		return err
	}
	BroadcastUpdate("video", video.ID)

	Log(LogLevelInfo, "即梦视频任务已提交", fmt.Sprintf("video=%d project=%d task_id=%s request_id=%s preset=%s duration=%ds frames=%d", video.ID, project.ID, submitResp.Data.TaskID, submitResp.RequestID, preset.Label, durationSeconds, frames))

	go pollJimengVideoGeneration(video, project, submitResp.Data.TaskID, preset, durationSeconds, frames)
	return nil
}

func parseJimengSubmitResponse(resp map[string]interface{}) (*jimengAsyncSubmitResponse, error) {
	raw, err := json.Marshal(resp)
	if err != nil {
		return nil, fmt.Errorf("failed to parse jimeng submit response: %w", err)
	}
	var parsed jimengAsyncSubmitResponse
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, fmt.Errorf("failed to decode jimeng submit response: %w", err)
	}
	return &parsed, nil
}

func parseJimengGetResultResponse(resp map[string]interface{}) (*jimengAsyncGetResultResponse, error) {
	raw, err := json.Marshal(resp)
	if err != nil {
		return nil, fmt.Errorf("failed to parse jimeng result response: %w", err)
	}
	var parsed jimengAsyncGetResultResponse
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, fmt.Errorf("failed to decode jimeng result response: %w", err)
	}
	return &parsed, nil
}

func queryJimengTaskStatus(taskID string) (*jimengStatusSnapshot, error) {
	taskID = strings.TrimSpace(taskID)
	if taskID == "" {
		return nil, fmt.Errorf("jm_task_id is empty")
	}

	client, err := newJimengVisualClient()
	if err != nil {
		return nil, err
	}

	rawResp, statusCode, err := client.CVSync2AsyncGetResult(map[string]interface{}{
		"req_key": getConfiguredJimengReqKey(),
		"task_id": taskID,
	})
	if err != nil {
		return nil, fmt.Errorf("jimeng get result failed: %w", err)
	}

	parsed, parseErr := parseJimengGetResultResponse(rawResp)
	if parseErr != nil {
		return nil, parseErr
	}

	prettyBytes, _ := json.MarshalIndent(rawResp, "", "  ")
	snapshot := &jimengStatusSnapshot{
		TaskID:      taskID,
		HTTPStatus:  statusCode,
		Code:        parsed.Code,
		Message:     strings.TrimSpace(parsed.Message),
		RequestID:   strings.TrimSpace(parsed.RequestID),
		Status:      strings.TrimSpace(parsed.Data.Status),
		VideoURL:    strings.TrimSpace(parsed.Data.VideoURL),
		AIGCTagged:  parsed.Data.AIGCTagged,
		Raw:         rawResp,
		PrettyJSON:  string(prettyBytes),
		Parsed:      parsed,
		CanRetrieve: statusCode == http.StatusOK && parsed.Code == 10000 && strings.EqualFold(strings.TrimSpace(parsed.Data.Status), "done") && strings.TrimSpace(parsed.Data.VideoURL) != "",
	}
	return snapshot, nil
}

func encodeVideoSourceImageBase64(assetPath string) (string, error) {
	absPath, err := assetWebPathToAbs(assetPath)
	if err != nil {
		return "", err
	}
	content, err := os.ReadFile(absPath)
	if err != nil {
		return "", fmt.Errorf("failed to read source image: %w", err)
	}
	return base64.StdEncoding.EncodeToString(content), nil
}

func pollJimengVideoGeneration(video models.Video, project models.Project, taskID string, preset jimengVideoPreset, durationSeconds int, frames int) {
	timeoutAt := time.Now().Add(4 * time.Hour)
	ticker := time.NewTicker(8 * time.Second)
	defer ticker.Stop()

	for {
		if time.Now().After(timeoutAt) {
			markJimengVideoFailure(video.ID, fmt.Errorf("jimeng video generation timeout"))
			return
		}

		snapshot, err := queryJimengTaskStatus(taskID)
		if err != nil {
			select {
			case <-ticker.C:
				continue
			}
		}

		if snapshot.HTTPStatus != http.StatusOK || snapshot.Code != 10000 {
			if isRetryableJimengCode(snapshot.Code) {
				select {
				case <-ticker.C:
					continue
				}
			}
			markJimengVideoFailure(video.ID, fmt.Errorf("jimeng get result failed: code=%d message=%s request_id=%s", snapshot.Code, snapshot.Message, snapshot.RequestID))
			return
		}

		switch strings.ToLower(strings.TrimSpace(snapshot.Status)) {
		case "in_queue", "generating", "":
			select {
			case <-ticker.C:
				continue
			}
		case "done":
			if strings.TrimSpace(snapshot.VideoURL) == "" {
				markJimengVideoFailure(video.ID, fmt.Errorf("jimeng task finished without video url"))
				return
			}
			webPath, err := persistJimengRetrievedVideo(video.ID, project.Code, snapshot.VideoURL, preset)
			if err != nil {
				markJimengVideoFailure(video.ID, err)
				return
			}
			Log(LogLevelInfo, "即梦视频生成完成", fmt.Sprintf("video=%d task_id=%s preset=%s duration=%ds frames=%d file=%s", video.ID, taskID, preset.Label, durationSeconds, frames, webPath))
			return
		case "not_found", "expired":
			markJimengVideoFailure(video.ID, fmt.Errorf("jimeng task %s", strings.TrimSpace(snapshot.Status)))
			return
		default:
			select {
			case <-ticker.C:
				continue
			}
		}
	}
}

func persistJimengRetrievedVideo(videoID uint, projectCode string, sourceURL string, preset jimengVideoPreset) (string, error) {
	var video models.Video
	if err := db.DB.First(&video, videoID).Error; err != nil {
		return "", err
	}
	if strings.TrimSpace(video.GeneratedVideo) != "" && strings.TrimSpace(video.Status) == "generated" {
		return strings.TrimSpace(video.GeneratedVideo), nil
	}

	webPath, err := downloadJimengVideo(projectCode, videoID, sourceURL)
	if err != nil {
		return "", err
	}

	if video.Width <= 0 {
		video.Width = preset.Width
	}
	if video.Height <= 0 {
		video.Height = preset.Height
	}
	video.GeneratedVideo = webPath
	video.GeneratedWorkflow = jimengWorkflowLabel
	video.Status = "generated"
	video.UpdatedAt = time.Now()
	if err := db.DB.Save(&video).Error; err != nil {
		return "", err
	}
	BroadcastUpdate("video", videoID)
	return webPath, nil
}

func isRetryableJimengCode(code int) bool {
	switch code {
	case 50429, 50430, 50500, 50501, 50516, 50517:
		return true
	default:
		return false
	}
}

func markJimengVideoFailure(videoID uint, cause error) {
	details := ""
	if cause != nil {
		details = cause.Error()
		Log(LogLevelError, "即梦视频生成失败", fmt.Sprintf("video=%d err=%s", videoID, details))
	}

	var video models.Video
	if err := db.DB.First(&video, videoID).Error; err != nil {
		return
	}
	video.Status = "failed"
	video.UpdatedAt = time.Now()
	_ = db.DB.Save(&video).Error
	BroadcastUpdate("video", videoID)
}

func downloadJimengVideo(projectCode string, videoID uint, sourceURL string) (string, error) {
	saveDir := filepath.Join("output", projectCode, "videos")
	if err := os.MkdirAll(saveDir, 0755); err != nil {
		return "", err
	}

	parsedURL, err := url.Parse(strings.TrimSpace(sourceURL))
	if err != nil {
		return "", fmt.Errorf("invalid jimeng video url: %w", err)
	}
	ext := filepath.Ext(path.Base(parsedURL.Path))
	if ext == "" {
		ext = ".mp4"
	}
	savePath := filepath.Join(saveDir, fmt.Sprintf("video_%d_%d%s", videoID, time.Now().Unix(), ext))

	req, err := http.NewRequest(http.MethodGet, sourceURL, nil)
	if err != nil {
		return "", err
	}
	resp, err := (&http.Client{Timeout: 30 * time.Minute}).Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to download jimeng video: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("jimeng video download failed: %s", resp.Status)
	}

	file, err := os.Create(savePath)
	if err != nil {
		return "", err
	}
	defer file.Close()

	if _, err := io.Copy(file, resp.Body); err != nil {
		return "", err
	}

	return "/" + filepath.ToSlash(savePath), nil
}
