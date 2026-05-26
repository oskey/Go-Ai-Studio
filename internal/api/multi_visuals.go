package api

import (
	"archive/zip"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"kt-ai-studio/internal/db"
	"kt-ai-studio/internal/models"
	"kt-ai-studio/internal/task"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

const multiVisualWorkflowPath = "workflows/image_qwen_image_edit_2511_multiangle_camera.json"
const multiVisualFaceCloseupWorkflowPath = "workflows/image_qwen_image_edit_2509_face_closeup.json"
const multiVisualOutputRoot = "output/multi_visual"
const (
	multiVisualTypeCharacter = "character"
	multiVisualTypeProp      = "prop"
	multiVisualTypeScene     = "scene"
)

type multiVisualShotSizePreset struct {
	Label string
	Zoom  int
}

type multiVisualViewPreset struct {
	Label           string
	HorizontalAngle int
	VerticalAngle   int
	CameraView      string
}

type multiVisualImagePreset struct {
	ShotSizeLabel   string
	ViewLabel       string
	HorizontalAngle int
	VerticalAngle   int
	Zoom            int
	CameraView      string
}

func normalizeMultiVisualHorizontalAngle(angle int) int {
	if angle < 0 {
		angle = 360 + (angle % 360)
	}
	if angle >= 360 {
		angle = angle % 360
	}
	return angle
}

type multiVisualRenderTaskPayload struct {
	ProjectID uint `json:"project_id"`
}

type queuedMultiVisualPrompt struct {
	ImageID     uint
	SortOrder   int
	PromptID    string
	ProjectID   uint
	ProjectCode string
}

type multiVisualExportRequest struct {
	ImageIDs []uint `json:"image_ids"`
}

type multiVisualBatchDeleteRequest struct {
	ImageIDs []uint `json:"image_ids"`
}

var multiVisualCharacterShotSizePresets = []multiVisualShotSizePreset{
	{Label: "头肩特写", Zoom: 10},
	{Label: "近景", Zoom: 3},
}

var multiVisualCharacterFaceCloseupPresets = []multiVisualImagePreset{
	{
		ShotSizeLabel:   "脸部特写",
		ViewLabel:       "正面",
		HorizontalAngle: 0,
		VerticalAngle:   0,
		CameraView:      "Turn the camera to a tight close-up of the face only.",
	},
	{
		ShotSizeLabel:   "脸部特写",
		ViewLabel:       "左前30°",
		HorizontalAngle: 30,
		VerticalAngle:   0,
		CameraView:      "Turn the camera to a close-up of the face, then rotate 30 degrees to the left.",
	},
	{
		ShotSizeLabel:   "脸部特写",
		ViewLabel:       "右前30°",
		HorizontalAngle: -30,
		VerticalAngle:   0,
		CameraView:      "Turn the camera to a close-up of the face, then rotate 30 degrees to the right.",
	},
}

var multiVisualCharacterViewPresets = []multiVisualViewPreset{
	{Label: "正面", HorizontalAngle: 0, VerticalAngle: 0},
	{Label: "左前30°", HorizontalAngle: 30, VerticalAngle: 0},
	{Label: "左前45°", HorizontalAngle: 45, VerticalAngle: 0},
	{Label: "左侧偏前", HorizontalAngle: 60, VerticalAngle: 0},
	{Label: "左侧面", HorizontalAngle: 90, VerticalAngle: 0},
	{Label: "左后45°", HorizontalAngle: 135, VerticalAngle: 0},
	{Label: "背面", HorizontalAngle: 180, VerticalAngle: 0},
	{Label: "顶部俯视，正面", HorizontalAngle: 0, VerticalAngle: 26},
	{Label: "顶部俯视，左前30°", HorizontalAngle: 30, VerticalAngle: 26},
	{Label: "底部仰视，正面", HorizontalAngle: 0, VerticalAngle: -26},
	{Label: "底部仰视，左前30°", HorizontalAngle: 30, VerticalAngle: -26},
}

var multiVisualPropPresets = []multiVisualImagePreset{
	{ShotSizeLabel: "主体", ViewLabel: "正面", HorizontalAngle: 0, VerticalAngle: 0, Zoom: 3},
	{ShotSizeLabel: "主体", ViewLabel: "左前45°", HorizontalAngle: 45, VerticalAngle: 0, Zoom: 3},
	{ShotSizeLabel: "主体", ViewLabel: "右前45°", HorizontalAngle: -45, VerticalAngle: 0, Zoom: 3},
	{ShotSizeLabel: "主体", ViewLabel: "左侧面", HorizontalAngle: 90, VerticalAngle: 0, Zoom: 3},
	{ShotSizeLabel: "主体", ViewLabel: "右侧面", HorizontalAngle: -90, VerticalAngle: 0, Zoom: 3},
	{ShotSizeLabel: "主体", ViewLabel: "左后45°", HorizontalAngle: 135, VerticalAngle: 0, Zoom: 3},
	{ShotSizeLabel: "主体", ViewLabel: "右后45°", HorizontalAngle: -135, VerticalAngle: 0, Zoom: 3},
	{ShotSizeLabel: "主体", ViewLabel: "背面", HorizontalAngle: 180, VerticalAngle: 0, Zoom: 3},
	{ShotSizeLabel: "主体", ViewLabel: "正面，轻微仰视", HorizontalAngle: 0, VerticalAngle: -12, Zoom: 3},
	{ShotSizeLabel: "主体", ViewLabel: "顶部俯视，正面", HorizontalAngle: 0, VerticalAngle: 26, Zoom: 3},
	{ShotSizeLabel: "主体", ViewLabel: "顶部俯视，左侧面", HorizontalAngle: 90, VerticalAngle: 26, Zoom: 3},
	{ShotSizeLabel: "主体", ViewLabel: "顶部俯视，右侧面", HorizontalAngle: -90, VerticalAngle: 26, Zoom: 3},
	{ShotSizeLabel: "主体", ViewLabel: "顶部俯视，背面", HorizontalAngle: 180, VerticalAngle: 26, Zoom: 3},
	{ShotSizeLabel: "主体", ViewLabel: "底部仰视，正面", HorizontalAngle: 0, VerticalAngle: -26, Zoom: 3},
	{ShotSizeLabel: "主体", ViewLabel: "底部仰视，左侧面", HorizontalAngle: 90, VerticalAngle: -26, Zoom: 3},
	{ShotSizeLabel: "主体", ViewLabel: "底部仰视，右侧面", HorizontalAngle: -90, VerticalAngle: -26, Zoom: 3},
	{ShotSizeLabel: "主体", ViewLabel: "底部仰视，背面", HorizontalAngle: 180, VerticalAngle: -26, Zoom: 3},
	{ShotSizeLabel: "特写", ViewLabel: "正面局部细节", HorizontalAngle: 0, VerticalAngle: 0, Zoom: 8},
	{ShotSizeLabel: "特写", ViewLabel: "侧向局部细节", HorizontalAngle: 45, VerticalAngle: 0, Zoom: 8},
	{ShotSizeLabel: "特写", ViewLabel: "上部局部细节", HorizontalAngle: 0, VerticalAngle: 6, Zoom: 9},
	{ShotSizeLabel: "特写", ViewLabel: "下部局部细节", HorizontalAngle: 0, VerticalAngle: 20, Zoom: 7},
}

var multiVisualScenePresets = []multiVisualImagePreset{
	{ShotSizeLabel: "全景", ViewLabel: "正主视角", HorizontalAngle: 0, VerticalAngle: 0, Zoom: 2},
	{ShotSizeLabel: "全景", ViewLabel: "左偏主视角", HorizontalAngle: 18, VerticalAngle: 0, Zoom: 2},
	{ShotSizeLabel: "全景", ViewLabel: "右偏主视角", HorizontalAngle: -18, VerticalAngle: 0, Zoom: 2},
	{ShotSizeLabel: "全景", ViewLabel: "高位总览", HorizontalAngle: 0, VerticalAngle: 18, Zoom: 2},
	{ShotSizeLabel: "中景", ViewLabel: "入口看向内部", HorizontalAngle: 0, VerticalAngle: 0, Zoom: 4},
	{ShotSizeLabel: "中景", ViewLabel: "内部看向入口", HorizontalAngle: 180, VerticalAngle: 0, Zoom: 4},
	{ShotSizeLabel: "中景", ViewLabel: "左侧主通道", HorizontalAngle: 30, VerticalAngle: 0, Zoom: 4},
	{ShotSizeLabel: "中景", ViewLabel: "右侧主通道", HorizontalAngle: -30, VerticalAngle: 0, Zoom: 4},
	{ShotSizeLabel: "中景", ViewLabel: "主功能区正视", HorizontalAngle: 0, VerticalAngle: 0, Zoom: 4},
	{ShotSizeLabel: "中景", ViewLabel: "主功能区左偏", HorizontalAngle: 22, VerticalAngle: 0, Zoom: 4},
	{ShotSizeLabel: "中景", ViewLabel: "主功能区右偏", HorizontalAngle: -22, VerticalAngle: 0, Zoom: 4},
	{ShotSizeLabel: "近景", ViewLabel: "标志区域", HorizontalAngle: 0, VerticalAngle: 0, Zoom: 6},
	{ShotSizeLabel: "近景", ViewLabel: "关键角落", HorizontalAngle: 26, VerticalAngle: 0, Zoom: 6},
	{ShotSizeLabel: "近景", ViewLabel: "灯光区域", HorizontalAngle: -26, VerticalAngle: 10, Zoom: 6},
	{ShotSizeLabel: "近景", ViewLabel: "材质区域", HorizontalAngle: 0, VerticalAngle: 8, Zoom: 6},
	{ShotSizeLabel: "局部", ViewLabel: "前台或主设施细节", HorizontalAngle: 0, VerticalAngle: 0, Zoom: 8},
	{ShotSizeLabel: "局部", ViewLabel: "门口或通道细节", HorizontalAngle: 24, VerticalAngle: 0, Zoom: 8},
	{ShotSizeLabel: "局部", ViewLabel: "地面或动线细节", HorizontalAngle: 0, VerticalAngle: 18, Zoom: 8},
	{ShotSizeLabel: "局部", ViewLabel: "座位或陈设细节", HorizontalAngle: -24, VerticalAngle: 0, Zoom: 8},
	{ShotSizeLabel: "局部", ViewLabel: "俯视空间细节", HorizontalAngle: 0, VerticalAngle: 24, Zoom: 8},
}

func normalizeMultiVisualType(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case multiVisualTypeProp:
		return multiVisualTypeProp
	case multiVisualTypeScene:
		return multiVisualTypeScene
	default:
		return multiVisualTypeCharacter
	}
}

func multiVisualTypeLabel(visualType string) string {
	switch normalizeMultiVisualType(visualType) {
	case multiVisualTypeProp:
		return "道具"
	case multiVisualTypeScene:
		return "场景"
	default:
		return "人物"
	}
}

func buildCharacterMultiVisualPresets() []multiVisualImagePreset {
	presets := make([]multiVisualImagePreset, 0, len(multiVisualCharacterFaceCloseupPresets)+(len(multiVisualCharacterShotSizePresets)*len(multiVisualCharacterViewPresets)))
	presets = append(presets, multiVisualCharacterFaceCloseupPresets...)
	for _, shotSize := range multiVisualCharacterShotSizePresets {
		for _, view := range multiVisualCharacterViewPresets {
			presets = append(presets, multiVisualImagePreset{
				ShotSizeLabel:   shotSize.Label,
				ViewLabel:       view.Label,
				HorizontalAngle: view.HorizontalAngle,
				VerticalAngle:   view.VerticalAngle,
				Zoom:            shotSize.Zoom,
				CameraView:      view.CameraView,
			})
		}
	}
	return presets
}

func multiVisualPresetsByType(visualType string) []multiVisualImagePreset {
	switch normalizeMultiVisualType(visualType) {
	case multiVisualTypeProp:
		return multiVisualPropPresets
	case multiVisualTypeScene:
		return multiVisualScenePresets
	default:
		return buildCharacterMultiVisualPresets()
	}
}

func multiVisualTotalCountByType(visualType string) int {
	return len(multiVisualPresetsByType(visualType))
}

func multiVisualProjectDir(code string) string {
	return filepath.Join(multiVisualOutputRoot, code)
}

func multiVisualReferenceDir(code string) string {
	return filepath.Join(multiVisualProjectDir(code), "reference")
}

func multiVisualImagesDir(code string) string {
	return filepath.Join(multiVisualProjectDir(code), "images")
}

func multiVisualExportsDir(code string) string {
	return filepath.Join(multiVisualProjectDir(code), "exports")
}

func multiVisualTrainingTag(project models.MultiVisualProject) string {
	tag := strings.TrimSpace(project.Description)
	if tag == "" {
		tag = strings.TrimSpace(project.Code)
	}
	return tag
}

func buildMultiVisualMergedTag(project models.MultiVisualProject, shotSizeLabel string, viewLabel string) string {
	base := multiVisualTrainingTag(project)
	extra := ""

	trimmedShot := strings.TrimSpace(shotSizeLabel)
	trimmedView := strings.TrimSpace(viewLabel)
	switch normalizeMultiVisualType(project.VisualType) {
	case multiVisualTypeProp:
		switch {
		case trimmedShot == "特写":
			extra = "close-up"
		case strings.Contains(trimmedView, "背面"):
			extra = "back view"
		case strings.Contains(trimmedView, "侧面"):
			extra = "side view"
		case strings.Contains(trimmedView, "俯视"):
			extra = "top view"
		case strings.Contains(trimmedView, "仰视"):
			extra = "low angle"
		}
	case multiVisualTypeScene:
		switch {
		case trimmedShot == "全景":
			extra = "wide shot"
		case trimmedShot == "局部":
			extra = "detail"
		case strings.Contains(trimmedView, "俯视"):
			extra = "top view"
		}
	default:
		switch {
		case strings.Contains(trimmedView, "背面") && strings.Contains(trimmedView, "顶部俯视"):
			extra = "back top-down"
		case strings.Contains(trimmedView, "背面") && strings.Contains(trimmedView, "底部仰视"):
			extra = "back low angle"
		case strings.Contains(trimmedView, "背面"):
			extra = "back view"
		case strings.Contains(trimmedView, "后45°"):
			extra = "left rear 45"
		case strings.Contains(trimmedView, "侧面") && strings.Contains(trimmedView, "顶部俯视"):
			extra = "side top-down"
		case strings.Contains(trimmedView, "侧面") && strings.Contains(trimmedView, "底部仰视"):
			extra = "side low angle"
		case strings.Contains(trimmedView, "侧面"):
			extra = "side view"
		case strings.Contains(trimmedView, "顶部俯视"):
			extra = "top-down"
		case strings.Contains(trimmedView, "底部仰视"):
			extra = "low angle"
		}
	}

	if extra == "" {
		return base
	}
	if base == "" {
		return extra
	}
	return fmt.Sprintf("%s,%s", base, extra)
}

func buildMultiVisualLabel(shotSizeLabel string, viewLabel string) string {
	parts := []string{}
	if strings.TrimSpace(shotSizeLabel) != "" {
		parts = append(parts, strings.TrimSpace(shotSizeLabel))
	}
	if strings.TrimSpace(viewLabel) != "" {
		parts = append(parts, strings.TrimSpace(viewLabel))
	}
	return strings.Join(parts, "，")
}

func buildMultiVisualImageSpecs(project models.MultiVisualProject) []models.MultiVisualImage {
	presets := multiVisualPresetsByType(project.VisualType)
	specs := make([]models.MultiVisualImage, 0, len(presets))
	sortOrder := 1
	now := time.Now()
	for _, preset := range presets {
		specs = append(specs, models.MultiVisualImage{
			ProjectID:       project.ID,
			SortOrder:       sortOrder,
			Label:           buildMultiVisualMergedTag(project, preset.ShotSizeLabel, preset.ViewLabel),
			TrainingTag:     buildMultiVisualMergedTag(project, preset.ShotSizeLabel, preset.ViewLabel),
			ShotSizeLabel:   preset.ShotSizeLabel,
			ViewLabel:       preset.ViewLabel,
			HorizontalAngle: normalizeMultiVisualHorizontalAngle(preset.HorizontalAngle),
			VerticalAngle:   preset.VerticalAngle,
			Zoom:            preset.Zoom,
			CameraView:      preset.CameraView,
			Status:          "pending",
			CreatedAt:       now,
			UpdatedAt:       now,
		})
		sortOrder++
	}
	return specs
}

func loadMultiVisualProjectOr404(c *gin.Context) (*models.MultiVisualProject, error) {
	projectID := strings.TrimSpace(c.Param("id"))
	var project models.MultiVisualProject
	if err := db.DB.First(&project, projectID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "多视觉图项目不存在"})
		return nil, err
	}
	project.VisualType = normalizeMultiVisualType(project.VisualType)
	return &project, nil
}

func setMultiVisualProjectStatus(projectID uint, status string, taskID string, lastError string) error {
	updates := map[string]interface{}{
		"status":          status,
		"updated_at":      time.Now(),
		"last_error":      strings.TrimSpace(lastError),
		"current_task_id": strings.TrimSpace(taskID),
	}
	return db.DB.Model(&models.MultiVisualProject{}).Where("id = ?", projectID).Updates(updates).Error
}

func isMultiVisualTaskCurrent(projectID uint, taskID string) bool {
	var project models.MultiVisualProject
	if err := db.DB.Select("current_task_id").First(&project, projectID).Error; err != nil {
		return false
	}
	return strings.TrimSpace(project.CurrentTaskID) == strings.TrimSpace(taskID)
}

func ListMultiVisualProjects(c *gin.Context) {
	var projects []models.MultiVisualProject
	if err := db.DB.Order("created_at desc").Find(&projects).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "获取多视觉图项目失败"})
		return
	}
	for i := range projects {
		projects[i].VisualType = normalizeMultiVisualType(projects[i].VisualType)
	}
	c.JSON(http.StatusOK, projects)
}

func GetMultiVisualProject(c *gin.Context) {
	project, err := loadMultiVisualProjectOr404(c)
	if err != nil {
		return
	}
	c.JSON(http.StatusOK, project)
}

func CreateMultiVisualProject(c *gin.Context) {
	name := strings.TrimSpace(c.PostForm("name"))
	code := strings.TrimSpace(c.PostForm("code"))
	visualType := normalizeMultiVisualType(c.PostForm("visual_type"))
	description := strings.TrimSpace(c.PostForm("description"))

	if name == "" || code == "" || description == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请填写名称、文件夹、类型和描述"})
		return
	}
	matched, _ := regexp.MatchString(`^[a-zA-Z0-9_-]+$`, code)
	if !matched {
		c.JSON(http.StatusBadRequest, gin.H{"error": "文件夹只允许输入英文、数字、下划线或连字符"})
		return
	}

	var count int64
	db.DB.Model(&models.MultiVisualProject{}).Where("code = ?", code).Count(&count)
	if count > 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "文件夹已被占用"})
		return
	}

	projectDir := multiVisualProjectDir(code)
	if _, err := os.Stat(projectDir); err == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "文件夹已被占用"})
		return
	}

	file, err := c.FormFile("reference_image")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请上传参考图"})
		return
	}

	if err := os.MkdirAll(multiVisualReferenceDir(code), 0755); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "创建项目目录失败"})
		return
	}

	ext := strings.ToLower(filepath.Ext(file.Filename))
	if ext == "" {
		ext = ".png"
	}
	referenceFilename := fmt.Sprintf("reference_%d%s", time.Now().UnixNano(), ext)
	referencePath := filepath.Join(multiVisualReferenceDir(code), referenceFilename)
	if err := c.SaveUploadedFile(file, referencePath); err != nil {
		_ = os.RemoveAll(projectDir)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "保存参考图失败"})
		return
	}

	now := time.Now()
	project := models.MultiVisualProject{
		Name:           name,
		Code:           code,
		VisualType:     visualType,
		Description:    description,
		ReferenceImage: "/" + filepath.ToSlash(referencePath),
		Status:         "draft",
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	if err := db.DB.Create(&project).Error; err != nil {
		_ = os.RemoveAll(projectDir)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "创建多视觉图项目失败"})
		return
	}

	Log(LogLevelInfo, "创建多视觉图项目", fmt.Sprintf("创建了多视觉图项目: %s (%s)", project.Name, project.Code))
	c.JSON(http.StatusCreated, project)
}

func UpdateMultiVisualProject(c *gin.Context) {
	project, err := loadMultiVisualProjectOr404(c)
	if err != nil {
		return
	}
	if project.Status == "generating" {
		c.JSON(http.StatusConflict, gin.H{"error": "项目正在生成中，暂时不能编辑"})
		return
	}

	name := strings.TrimSpace(c.PostForm("name"))
	code := strings.TrimSpace(c.PostForm("code"))
	visualType := normalizeMultiVisualType(c.PostForm("visual_type"))
	description := strings.TrimSpace(c.PostForm("description"))
	if name == "" || code == "" || description == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请填写名称、文件夹、类型和描述"})
		return
	}
	matched, _ := regexp.MatchString(`^[a-zA-Z0-9_-]+$`, code)
	if !matched {
		c.JSON(http.StatusBadRequest, gin.H{"error": "文件夹只允许输入英文、数字、下划线或连字符"})
		return
	}

	oldCode := strings.TrimSpace(project.Code)
	oldDir := multiVisualProjectDir(oldCode)
	newDir := multiVisualProjectDir(code)
	if code != oldCode {
		var count int64
		db.DB.Model(&models.MultiVisualProject{}).Where("code = ? AND id <> ?", code, project.ID).Count(&count)
		if count > 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "文件夹已被占用"})
			return
		}
		if _, statErr := os.Stat(newDir); statErr == nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "文件夹已被占用"})
			return
		}
	}

	file, fileErr := c.FormFile("reference_image")
	hasNewReference := fileErr == nil

	if code != oldCode {
		if _, statErr := os.Stat(oldDir); statErr == nil {
			if err := os.Rename(oldDir, newDir); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "重命名项目目录失败"})
				return
			}
		} else if os.IsNotExist(statErr) {
			if err := os.MkdirAll(newDir, 0755); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "创建新项目目录失败"})
				return
			}
		}
	}

	if err := os.MkdirAll(multiVisualReferenceDir(code), 0755); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "创建参考图目录失败"})
		return
	}

	newReferenceWebPath := strings.TrimSpace(project.ReferenceImage)
	if code != oldCode && newReferenceWebPath != "" {
		oldPrefix := "/" + filepath.ToSlash(oldDir)
		newPrefix := "/" + filepath.ToSlash(newDir)
		newReferenceWebPath = strings.Replace(newReferenceWebPath, oldPrefix, newPrefix, 1)
	}

	if hasNewReference {
		entries, _ := os.ReadDir(multiVisualReferenceDir(code))
		for _, entry := range entries {
			_ = os.Remove(filepath.Join(multiVisualReferenceDir(code), entry.Name()))
		}
		ext := strings.ToLower(filepath.Ext(file.Filename))
		if ext == "" {
			ext = ".png"
		}
		referenceFilename := fmt.Sprintf("reference_%d%s", time.Now().UnixNano(), ext)
		referencePath := filepath.Join(multiVisualReferenceDir(code), referenceFilename)
		if err := c.SaveUploadedFile(file, referencePath); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "保存参考图失败"})
			return
		}
		newReferenceWebPath = "/" + filepath.ToSlash(referencePath)
	}

	now := time.Now()
	if err := db.DB.Transaction(func(tx *gorm.DB) error {
		if code != oldCode {
			oldPrefix := "/" + filepath.ToSlash(oldDir)
			newPrefix := "/" + filepath.ToSlash(newDir)
			var images []models.MultiVisualImage
			if err := tx.Where("project_id = ?", project.ID).Find(&images).Error; err != nil {
				return err
			}
			for _, image := range images {
				if strings.TrimSpace(image.GeneratedImage) == "" {
					continue
				}
				updatedPath := strings.Replace(image.GeneratedImage, oldPrefix, newPrefix, 1)
				if err := tx.Model(&models.MultiVisualImage{}).Where("id = ?", image.ID).Updates(map[string]interface{}{
					"generated_image": updatedPath,
					"updated_at":      now,
				}).Error; err != nil {
					return err
				}
			}
		}

		project.Name = name
		project.Code = code
		project.VisualType = visualType
		project.Description = description
		project.ReferenceImage = newReferenceWebPath
		project.UpdatedAt = now
		return tx.Save(project).Error
	}); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "更新多视觉图项目失败"})
		return
	}

	Log(LogLevelInfo, "编辑多视觉图项目", fmt.Sprintf("更新了多视觉图项目: %s (%s)", project.Name, project.Code))
	c.JSON(http.StatusOK, project)
}

func DeleteMultiVisualProject(c *gin.Context) {
	project, err := loadMultiVisualProjectOr404(c)
	if err != nil {
		return
	}
	if project.Status == "generating" {
		c.JSON(http.StatusConflict, gin.H{"error": "项目正在生成中，暂时不能删除"})
		return
	}

	projectDir := multiVisualProjectDir(project.Code)
	if err := db.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Delete(&models.MultiVisualImage{}, "project_id = ?", project.ID).Error; err != nil {
			return err
		}
		if err := tx.Delete(project).Error; err != nil {
			return err
		}
		return nil
	}); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "删除多视觉图项目失败"})
		return
	}
	if project.Code != "" && project.Code != "." && project.Code != ".." {
		_ = os.RemoveAll(projectDir)
	}
	Log(LogLevelInfo, "删除多视觉图项目", fmt.Sprintf("删除了多视觉图项目: %s (%s)", project.Name, project.Code))
	c.JSON(http.StatusOK, gin.H{"message": "多视觉图项目已删除"})
}

func ListMultiVisualImages(c *gin.Context) {
	project, err := loadMultiVisualProjectOr404(c)
	if err != nil {
		return
	}
	var images []models.MultiVisualImage
	if err := db.DB.Where("project_id = ?", project.ID).Order("sort_order asc, id asc").Find(&images).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "获取多视觉图图片失败"})
		return
	}
	c.JSON(http.StatusOK, images)
}

func clearMultiVisualProjectImages(tx *gorm.DB, project models.MultiVisualProject) error {
	var images []models.MultiVisualImage
	if err := tx.Where("project_id = ?", project.ID).Find(&images).Error; err != nil {
		return err
	}
	for _, image := range images {
		if err := removeGeneratedAsset(image.GeneratedImage); err != nil {
			return err
		}
	}
	_ = os.RemoveAll(multiVisualImagesDir(project.Code))
	_ = os.RemoveAll(multiVisualExportsDir(project.Code))
	return tx.Where("project_id = ?", project.ID).Delete(&models.MultiVisualImage{}).Error
}

func RegenerateMultiVisualProject(c *gin.Context) {
	project, err := loadMultiVisualProjectOr404(c)
	if err != nil {
		return
	}
	if project.Status == "generating" {
		c.JSON(http.StatusConflict, gin.H{"error": "项目正在生成中，请等待当前任务完成"})
		return
	}

	now := time.Now()
	if err := db.DB.Transaction(func(tx *gorm.DB) error {
		if err := clearMultiVisualProjectImages(tx, *project); err != nil {
			return err
		}
		project.Status = "generating"
		project.CurrentTaskID = ""
		project.LastError = ""
		project.UpdatedAt = now
		if err := tx.Save(project).Error; err != nil {
			return err
		}
		specs := buildMultiVisualImageSpecs(*project)
		if len(specs) == 0 {
			return fmt.Errorf("未生成任何多视觉图规格")
		}
		return tx.Create(&specs).Error
	}); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "初始化多视觉图生成任务失败"})
		return
	}

	payload := multiVisualRenderTaskPayload{ProjectID: project.ID}
	taskRecord, err := task.GlobalTaskManager.AddTask("render_multi_visual_project", payload)
	if err != nil {
		_ = setMultiVisualProjectStatus(project.ID, "failed", "", err.Error())
		c.JSON(http.StatusInternalServerError, gin.H{"error": "提交多视觉图生成任务失败"})
		return
	}
	_ = setMultiVisualProjectStatus(project.ID, "generating", taskRecord.ID, "")
	BroadcastUpdate("multi_visual_project", project.ID)

	c.JSON(http.StatusOK, gin.H{
		"message": "多视觉图生成任务已提交",
		"task_id": taskRecord.ID,
	})
}

func ResetMultiVisualProjectState(c *gin.Context) {
	project, err := loadMultiVisualProjectOr404(c)
	if err != nil {
		return
	}

	var images []models.MultiVisualImage
	if err := db.DB.Where("project_id = ?", project.ID).Find(&images).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "读取图片状态失败"})
		return
	}

	now := time.Now()
	if err := db.DB.Transaction(func(tx *gorm.DB) error {
		for _, image := range images {
			if err := removeGeneratedAsset(image.GeneratedImage); err != nil {
				return err
			}
			if err := tx.Model(&models.MultiVisualImage{}).Where("id = ?", image.ID).Updates(map[string]interface{}{
				"status":          "draft",
				"generated_image": "",
				"updated_at":      now,
			}).Error; err != nil {
				return err
			}
		}
		_ = os.RemoveAll(multiVisualImagesDir(project.Code))
		_ = os.RemoveAll(multiVisualExportsDir(project.Code))

		project.Status = "draft"
		project.CurrentTaskID = ""
		project.LastError = ""
		project.UpdatedAt = now
		return tx.Save(project).Error
	}); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "重置多视觉图状态失败"})
		return
	}

	BroadcastUpdate("multi_visual_project", project.ID)
	c.JSON(http.StatusOK, gin.H{"message": "多视觉图状态已重置并清空生成结果"})
}

func DeleteMultiVisualImage(c *gin.Context) {
	project, err := loadMultiVisualProjectOr404(c)
	if err != nil {
		return
	}
	if project.Status == "generating" {
		c.JSON(http.StatusConflict, gin.H{"error": "项目正在生成中，暂时不能删除图片"})
		return
	}

	imageID := strings.TrimSpace(c.Param("imageId"))
	var image models.MultiVisualImage
	if err := db.DB.Where("project_id = ?", project.ID).First(&image, imageID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "图片不存在"})
		return
	}
	if err := removeGeneratedAsset(image.GeneratedImage); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "删除图片文件失败"})
		return
	}
	if err := db.DB.Delete(&image).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "删除图片记录失败"})
		return
	}
	BroadcastUpdate("multi_visual_project", project.ID)
	c.JSON(http.StatusOK, gin.H{"message": "图片已删除"})
}

func BatchDeleteMultiVisualImages(c *gin.Context) {
	project, err := loadMultiVisualProjectOr404(c)
	if err != nil {
		return
	}
	if project.Status == "generating" {
		c.JSON(http.StatusConflict, gin.H{"error": "项目正在生成中，暂时不能删除图片"})
		return
	}

	var req multiVisualBatchDeleteRequest
	if err := c.ShouldBindJSON(&req); err != nil || len(req.ImageIDs) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请选择要删除的图片"})
		return
	}

	var images []models.MultiVisualImage
	if err := db.DB.Where("project_id = ? AND id IN ?", project.ID, req.ImageIDs).Find(&images).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "查询图片失败"})
		return
	}
	for _, image := range images {
		if err := removeGeneratedAsset(image.GeneratedImage); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "删除图片文件失败"})
			return
		}
	}
	if err := db.DB.Delete(&models.MultiVisualImage{}, req.ImageIDs).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "删除图片记录失败"})
		return
	}
	BroadcastUpdate("multi_visual_project", project.ID)
	c.JSON(http.StatusOK, gin.H{"message": "图片已删除"})
}

func ExportMultiVisualImages(c *gin.Context) {
	project, err := loadMultiVisualProjectOr404(c)
	if err != nil {
		return
	}
	var req multiVisualExportRequest
	if err := c.ShouldBindJSON(&req); err != nil && err.Error() != "EOF" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "导出参数无效"})
		return
	}

	query := db.DB.Where("project_id = ?", project.ID)
	if len(req.ImageIDs) > 0 {
		query = query.Where("id IN ?", req.ImageIDs)
	}
	var images []models.MultiVisualImage
	if err := query.Order("sort_order asc, id asc").Find(&images).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "查询导出图片失败"})
		return
	}
	filtered := make([]models.MultiVisualImage, 0, len(images))
	for _, image := range images {
		if strings.TrimSpace(image.GeneratedImage) != "" {
			filtered = append(filtered, image)
		}
	}
	if len(filtered) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "当前没有可导出的图片"})
		return
	}

	if err := os.MkdirAll(multiVisualExportsDir(project.Code), 0755); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "创建导出目录失败"})
		return
	}
	zipPath := filepath.Join(multiVisualExportsDir(project.Code), fmt.Sprintf("%s_multi_visual_%d.zip", project.Code, time.Now().Unix()))
	if err := buildMultiVisualExportArchive(project.Code, filtered, zipPath); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "生成 ZIP 失败"})
		return
	}

	c.FileAttachment(zipPath, filepath.Base(zipPath))
}

func buildMultiVisualExportArchive(projectCode string, images []models.MultiVisualImage, zipPath string) error {
	zipFile, err := os.Create(zipPath)
	if err != nil {
		return err
	}
	defer zipFile.Close()

	zipWriter := zip.NewWriter(zipFile)
	for idx, image := range images {
		sourcePath, err := assetWebPathToAbs(image.GeneratedImage)
		if err != nil {
			return err
		}
		ext := filepath.Ext(sourcePath)
		if ext == "" {
			ext = ".png"
		}
		baseName := fmt.Sprintf("%s_%03d", projectCode, idx+1)
		if err := addFileToZip(zipWriter, baseName+ext, sourcePath); err != nil {
			return err
		}
		if err := addTextToZip(zipWriter, baseName+".txt", strings.TrimSpace(image.Label)); err != nil {
			return err
		}
	}
	return zipWriter.Close()
}

func loadMultiVisualWorkflowTemplate(path string) (map[string]interface{}, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var workflowJSON map[string]interface{}
	if err := json.Unmarshal(raw, &workflowJSON); err != nil {
		return nil, err
	}
	return workflowJSON, nil
}

func isMultiVisualFaceCloseupImage(image models.MultiVisualImage) bool {
	return strings.TrimSpace(image.ShotSizeLabel) == "脸部特写" && strings.TrimSpace(image.CameraView) != ""
}

func cloneGenericWorkflowJSON(src map[string]interface{}) (map[string]interface{}, error) {
	bytes, err := json.Marshal(src)
	if err != nil {
		return nil, err
	}
	var dst map[string]interface{}
	if err := json.Unmarshal(bytes, &dst); err != nil {
		return nil, err
	}
	return dst, nil
}

func setWorkflowNodeInput(workflowJSON map[string]interface{}, nodeID string, inputKey string, value interface{}) error {
	nodeRaw, ok := workflowJSON[nodeID]
	if !ok {
		return fmt.Errorf("workflow missing node %s", nodeID)
	}
	node, ok := nodeRaw.(map[string]interface{})
	if !ok {
		return fmt.Errorf("workflow node %s is invalid", nodeID)
	}
	inputsRaw, ok := node["inputs"]
	if !ok {
		return fmt.Errorf("workflow node %s missing inputs", nodeID)
	}
	inputs, ok := inputsRaw.(map[string]interface{})
	if !ok {
		return fmt.Errorf("workflow node %s inputs invalid", nodeID)
	}
	inputs[inputKey] = value
	return nil
}

func buildMultiVisualFaceCloseupWorkflow(template map[string]interface{}, comfyImageName string, image models.MultiVisualImage, project models.MultiVisualProject) (map[string]interface{}, error) {
	workflowJSON, err := cloneGenericWorkflowJSON(template)
	if err != nil {
		return nil, err
	}
	seed := getConfiguredGlobalSeed()
	if err := setWorkflowNodeInput(workflowJSON, "25", "image", comfyImageName); err != nil {
		return nil, err
	}
	if err := setWorkflowNodeInput(workflowJSON, "66", "value", image.CameraView); err != nil {
		return nil, err
	}
	if err := setWorkflowNodeInput(workflowJSON, "31", "filename_prefix", fmt.Sprintf("%s_mv_%03d", project.Code, image.SortOrder)); err != nil {
		return nil, err
	}
	if err := setWorkflowNodeInput(workflowJSON, "65:33:21", "seed", seed); err != nil {
		return nil, err
	}
	return workflowJSON, nil
}

func buildMultiVisualWorkflow(template map[string]interface{}, comfyImageName string, image models.MultiVisualImage, project models.MultiVisualProject) (map[string]interface{}, error) {
	if isMultiVisualFaceCloseupImage(image) {
		return buildMultiVisualFaceCloseupWorkflow(template, comfyImageName, image, project)
	}
	workflowJSON, err := cloneGenericWorkflowJSON(template)
	if err != nil {
		return nil, err
	}
	seed := getConfiguredGlobalSeed()
	if err := setWorkflowNodeInput(workflowJSON, "41", "image", comfyImageName); err != nil {
		return nil, err
	}
	if err := setWorkflowNodeInput(workflowJSON, "111", "horizontal_angle", normalizeMultiVisualHorizontalAngle(image.HorizontalAngle)); err != nil {
		return nil, err
	}
	if err := setWorkflowNodeInput(workflowJSON, "111", "vertical_angle", image.VerticalAngle); err != nil {
		return nil, err
	}
	if err := setWorkflowNodeInput(workflowJSON, "111", "zoom", image.Zoom); err != nil {
		return nil, err
	}
	if err := setWorkflowNodeInput(workflowJSON, "111", "camera_view", image.CameraView); err != nil {
		return nil, err
	}
	if err := setWorkflowNodeInput(workflowJSON, "9", "filename_prefix", fmt.Sprintf("%s_mv_%03d", project.Code, image.SortOrder)); err != nil {
		return nil, err
	}
	if err := setWorkflowNodeInput(workflowJSON, "112:105", "seed", seed); err != nil {
		return nil, err
	}
	return workflowJSON, nil
}

func waitForMultiVisualImageOutput(promptID string, projectCode string, sortOrder int) (string, error) {
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		history, err := GetComfyHistory(promptID)
		if err != nil {
			continue
		}
		outputs, ok := history["outputs"].(map[string]interface{})
		if !ok {
			continue
		}
		for _, nodeOutput := range outputs {
			imageOutputs, ok := nodeOutput.(map[string]interface{})
			if !ok {
				continue
			}
			images, ok := imageOutputs["images"].([]interface{})
			if !ok || len(images) == 0 {
				continue
			}
			imgData, ok := images[0].(map[string]interface{})
			if !ok {
				continue
			}
			filename, _ := imgData["filename"].(string)
			subfolder, _ := imgData["subfolder"].(string)
			typeStr, _ := imgData["type"].(string)
			if filename == "" {
				continue
			}

			saveDir := multiVisualImagesDir(projectCode)
			if err := os.MkdirAll(saveDir, 0755); err != nil {
				return "", err
			}
			ext := filepath.Ext(filename)
			if ext == "" {
				ext = ".png"
			}
			saveFilename := fmt.Sprintf("%s_%03d_%d%s", projectCode, sortOrder, time.Now().UnixNano(), ext)
			savePath := filepath.Join(saveDir, saveFilename)
			if err := DownloadComfyImage(filename, subfolder, typeStr, savePath); err != nil {
				return "", err
			}
			return "/" + filepath.ToSlash(savePath), nil
		}
	}
	return "", nil
}

func waitForQueuedMultiVisualOutput(prompt queuedMultiVisualPrompt) (string, error) {
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()
	inactiveCount := 0

	for range ticker.C {
		history, historyErr := GetComfyHistory(prompt.PromptID)
		if historyErr == nil {
			outputs, ok := history["outputs"].(map[string]interface{})
			if ok {
				for _, nodeOutput := range outputs {
					imageOutputs, ok := nodeOutput.(map[string]interface{})
					if !ok {
						continue
					}
					images, ok := imageOutputs["images"].([]interface{})
					if !ok || len(images) == 0 {
						continue
					}
					imgData, ok := images[0].(map[string]interface{})
					if !ok {
						continue
					}
					filename, _ := imgData["filename"].(string)
					subfolder, _ := imgData["subfolder"].(string)
					typeStr, _ := imgData["type"].(string)
					if filename == "" {
						continue
					}

					saveDir := multiVisualImagesDir(prompt.ProjectCode)
					if err := os.MkdirAll(saveDir, 0755); err != nil {
						return "", err
					}
					ext := filepath.Ext(filename)
					if ext == "" {
						ext = ".png"
					}
					saveFilename := fmt.Sprintf("%s_%03d_%d%s", prompt.ProjectCode, prompt.SortOrder, time.Now().UnixNano(), ext)
					savePath := filepath.Join(saveDir, saveFilename)
					if err := DownloadComfyImage(filename, subfolder, typeStr, savePath); err != nil {
						return "", err
					}
					return "/" + filepath.ToSlash(savePath), nil
				}
			}
		}

		active, activeErr := IsComfyPromptActive(prompt.PromptID)
		if activeErr == nil && active {
			inactiveCount = 0
			continue
		}
		inactiveCount++
		if inactiveCount < 5 {
			continue
		}
		return "", nil
	}

	return "", nil
}

func HandleRenderMultiVisualProjectTask(t *models.Task) (interface{}, error) {
	var payload multiVisualRenderTaskPayload
	if err := json.Unmarshal([]byte(t.Payload), &payload); err != nil {
		return nil, fmt.Errorf("invalid multi visual payload: %w", err)
	}

	var project models.MultiVisualProject
	if err := db.DB.First(&project, payload.ProjectID).Error; err != nil {
		return nil, fmt.Errorf("multi visual project not found: %w", err)
	}
	var images []models.MultiVisualImage
	if err := db.DB.Where("project_id = ?", project.ID).Order("sort_order asc, id asc").Find(&images).Error; err != nil {
		return nil, fmt.Errorf("failed to load multi visual images: %w", err)
	}
	if len(images) == 0 {
		return nil, fmt.Errorf("no multi visual images to render")
	}

	referencePath, err := assetWebPathToAbs(project.ReferenceImage)
	if err != nil {
		return nil, err
	}
	comfyImageName, err := UploadToComfyUIInput(referencePath)
	if err != nil {
		_ = setMultiVisualProjectStatus(project.ID, "failed", "", err.Error())
		return nil, err
	}

	template, err := loadMultiVisualWorkflowTemplate(multiVisualWorkflowPath)
	if err != nil {
		_ = setMultiVisualProjectStatus(project.ID, "failed", "", err.Error())
		return nil, err
	}
	faceCloseupTemplate, err := loadMultiVisualWorkflowTemplate(multiVisualFaceCloseupWorkflowPath)
	if err != nil {
		_ = setMultiVisualProjectStatus(project.ID, "failed", "", err.Error())
		return nil, err
	}

	var failed []string
	total := len(images)
	queuedPrompts := make([]queuedMultiVisualPrompt, 0, total)

	for idx := range images {
		if !isMultiVisualTaskCurrent(project.ID, t.ID) {
			return gin.H{
				"generated": 0,
				"failed":    0,
				"message":   "task aborted because project state was reset or replaced",
			}, nil
		}
		image := &images[idx]
		_ = db.DB.Model(&models.MultiVisualImage{}).Where("id = ?", image.ID).Updates(map[string]interface{}{
			"status":     "queueing",
			"updated_at": time.Now(),
		}).Error

		selectedTemplate := template
		if isMultiVisualFaceCloseupImage(*image) {
			selectedTemplate = faceCloseupTemplate
		}

		workflowJSON, err := buildMultiVisualWorkflow(selectedTemplate, comfyImageName, *image, project)
		if err != nil {
			failed = append(failed, fmt.Sprintf("%03d 构建工作流失败", image.SortOrder))
			_ = db.DB.Model(&models.MultiVisualImage{}).Where("id = ?", image.ID).Updates(map[string]interface{}{
				"status":     "failed",
				"updated_at": time.Now(),
			}).Error
			continue
		}

		promptID, err := QueueComfyPrompt(workflowJSON)
		if err != nil {
			failed = append(failed, fmt.Sprintf("%03d 提交失败", image.SortOrder))
			_ = db.DB.Model(&models.MultiVisualImage{}).Where("id = ?", image.ID).Updates(map[string]interface{}{
				"status":     "failed",
				"updated_at": time.Now(),
			}).Error
			continue
		}

		_ = db.DB.Model(&models.MultiVisualImage{}).Where("id = ?", image.ID).Updates(map[string]interface{}{
			"status":     "queued",
			"updated_at": time.Now(),
		}).Error
		queuedPrompts = append(queuedPrompts, queuedMultiVisualPrompt{
			ImageID:     image.ID,
			SortOrder:   image.SortOrder,
			PromptID:    promptID,
			ProjectID:   project.ID,
			ProjectCode: project.Code,
		})
		task.GlobalTaskManager.UpdateTaskProgress(t.ID, int(float64(len(queuedPrompts))/float64(total)*50), "")
		BroadcastUpdate("multi_visual_project", project.ID)
	}

	queuedTotal := len(queuedPrompts)
	if queuedTotal == 0 {
		queuedTotal = 1
	}
	for idx, prompt := range queuedPrompts {
		if !isMultiVisualTaskCurrent(project.ID, t.ID) {
			return gin.H{
				"generated": 0,
				"failed":    0,
				"message":   "task aborted because project state was reset or replaced",
			}, nil
		}
		_ = db.DB.Model(&models.MultiVisualImage{}).Where("id = ?", prompt.ImageID).Updates(map[string]interface{}{
			"status":     "generating",
			"updated_at": time.Now(),
		}).Error
		BroadcastUpdate("multi_visual_project", project.ID)

		webPath, err := waitForQueuedMultiVisualOutput(prompt)
		if err != nil {
			failed = append(failed, fmt.Sprintf("%03d 下载失败", prompt.SortOrder))
			_ = db.DB.Model(&models.MultiVisualImage{}).Where("id = ?", prompt.ImageID).Updates(map[string]interface{}{
				"status":     "failed",
				"updated_at": time.Now(),
			}).Error
			continue
		}
		if strings.TrimSpace(webPath) == "" {
			failed = append(failed, fmt.Sprintf("%03d 未获取到输出", prompt.SortOrder))
			_ = db.DB.Model(&models.MultiVisualImage{}).Where("id = ?", prompt.ImageID).Updates(map[string]interface{}{
				"status":     "failed",
				"updated_at": time.Now(),
			}).Error
			continue
		}

		_ = db.DB.Model(&models.MultiVisualImage{}).Where("id = ?", prompt.ImageID).Updates(map[string]interface{}{
			"generated_image": webPath,
			"status":          "generated",
			"updated_at":      time.Now(),
		}).Error
		task.GlobalTaskManager.UpdateTaskProgress(t.ID, 50+int(float64(idx+1)/float64(queuedTotal)*50), "")
		BroadcastUpdate("multi_visual_project", project.ID)
	}

	if len(failed) > 0 {
		errMsg := strings.Join(failed, "；")
		_ = setMultiVisualProjectStatus(project.ID, "failed", "", errMsg)
		BroadcastUpdate("multi_visual_project", project.ID)
		return gin.H{
			"generated": total - len(failed),
			"failed":    len(failed),
			"error":     errMsg,
		}, fmt.Errorf("%s", errMsg)
	}

	_ = setMultiVisualProjectStatus(project.ID, "generated", "", "")
	BroadcastUpdate("multi_visual_project", project.ID)
	return gin.H{
		"generated": total,
		"failed":    0,
	}, nil
}
