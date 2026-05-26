package api

import (
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"kt-ai-studio/internal/db"
	"kt-ai-studio/internal/models"
	"kt-ai-studio/internal/task"
	"kt-ai-studio/internal/workflow"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

const (
	storeVisitImageWorkflowPath          = "workflows/b_qwen_Image_edit_subgraphed.json"
	storeVisitVideoWorkflowPath          = "workflows/video_new_ltx2_3_i2v.json"
	storeVisitOutputRoot                 = "output/store_visit"
	storeVisitDefaultVideoFPS            = 25
	storeVisitDefaultVideoDurationSecond = 10
	storeVisitVideoWidth                 = 720
	storeVisitVideoHeight                = 1280
)

const (
	storeVisitSpotTypeEntrance            = "entrance"
	storeVisitSpotTypeLobby               = "lobby"
	storeVisitSpotTypePrivateRoom         = "private_room"
	storeVisitSpotTypeKitchen             = "kitchen"
	storeVisitSpotTypeFeaturedArea        = "featured_area"
	storeVisitSpotTypeTableDishes         = "table_dishes"
	storeVisitSpotTypeDishGeneration      = "dish_generation"
	storeVisitSpotTypeSignatureDish       = "signature_dish"
	storeVisitSpotTypeTasteRecommendation = "taste_recommendation"
	storeVisitSpotTypePromotion           = "promotion"
)

type storeVisitSpotDefinition struct {
	Type string
	Name string
}

var storeVisitDefaultSpotDefinitions = []storeVisitSpotDefinition{
	{Type: storeVisitSpotTypeEntrance, Name: "门头"},
	{Type: storeVisitSpotTypeLobby, Name: "大厅"},
	{Type: storeVisitSpotTypePrivateRoom, Name: "包间"},
	{Type: storeVisitSpotTypeKitchen, Name: "厨房"},
	{Type: storeVisitSpotTypeFeaturedArea, Name: "特色区域"},
	{Type: storeVisitSpotTypeTableDishes, Name: "整桌菜品"},
	{Type: storeVisitSpotTypeDishGeneration, Name: "菜品生成"},
	{Type: storeVisitSpotTypePromotion, Name: "优惠信息"},
}

const storeVisitDefaultImagePositivePrompt = `将参考人物放入当前场景中，仅保留人物面部身份。

半身构图（头到腰），人物位于画面左侧或右侧约1/3位置，
人物不在中心。

人物面向镜头，身体略微侧转，
姿态自然，轻微手势空间。

人物占画面50%~60%，
头顶留空间，不贴边。

光线与环境一致，
有自然阴影，
颜色受环境影响，
整体真实融合。

背景保持清晰完整，不被遮挡。
真实摄影风格。`

const storeVisitInteriorImagePositivePrompt = `将参考人物放入当前场景中，仅保留人物面部身份。

半身构图（头到腰），人物正对镜头。

姿态自然，轻微手势空间。

人物占画面50%~60%，
头顶留空间，不贴边。

光线与环境一致，
有自然阴影，
颜色受环境影响，
整体真实融合。

背景保持清晰完整，不被遮挡。
真实摄影风格。`

const storeVisitTableDishesImagePositivePrompt = `将参考人物放入当前场景中，仅保留人物面部身份，不参考原图姿态和背景。

人物坐在画面中的椅子上，身体自然坐姿，上半身微微前倾，面向镜头或略微侧向，
双手自然放在桌面或腿上。

人物位置在正上方，与桌椅空间关系合理，面对镜头，人物面前是菜品，
身体与椅子接触自然，不悬空，不错位。

人物大小与桌椅比例一致，符合正常成年人尺寸，
上半身为主要展示（半身或三分之二身）。

光线与环境一致，有自然阴影，
颜色受环境影响，真实融合。

画面保持真实摄影风格，细节清晰自然。`

const storeVisitDefaultImageNegativePrompt = `居中构图，全身，人物过大，遮挡背景，大面积遮挡，比例错误，
漂浮，脚离地，抠图边缘，光影错误，假人感，塑料皮肤，
卡通，二次元`

type storeVisitMediaTaskPayload struct {
	ProjectID uint  `json:"project_id"`
	SpotID    uint  `json:"spot_id"`
	Seed      int64 `json:"seed"`
}

func getStoreVisitDefaultImagePositivePrompt(spotType string) string {
	switch normalizeStoreVisitSpotType(spotType, "") {
	case storeVisitSpotTypeEntrance:
		return storeVisitDefaultImagePositivePrompt
	case storeVisitSpotTypeTableDishes:
		return storeVisitTableDishesImagePositivePrompt
	case storeVisitSpotTypeDishGeneration:
		return ""
	default:
		return storeVisitInteriorImagePositivePrompt
	}
}

func storeVisitBloggerReferencePath(code string, id uint, ext string) string {
	return filepath.Join(storeVisitReferenceDir(code), fmt.Sprintf("blogger_%d%s", id, ext))
}

func storeVisitProjectDir(code string) string {
	return filepath.Join(storeVisitOutputRoot, code)
}

func storeVisitReferenceDir(code string) string {
	return filepath.Join(storeVisitProjectDir(code), "reference")
}

func storeVisitImagesDir(code string) string {
	return filepath.Join(storeVisitProjectDir(code), "images")
}

func storeVisitVideosDir(code string) string {
	return filepath.Join(storeVisitProjectDir(code), "videos")
}

func storeVisitDishGenerationDir(code string) string {
	return filepath.Join(storeVisitProjectDir(code), "dish_generation")
}

func storeVisitDishGenerationFramesDir(code string, spotKey string) string {
	return filepath.Join(storeVisitDishGenerationDir(code), spotKey, "frames")
}

func storeVisitDishGenerationVideosDir(code string, spotKey string) string {
	return filepath.Join(storeVisitDishGenerationDir(code), spotKey, "videos")
}

func loadStoreVisitProjectOr404(c *gin.Context) (*models.StoreVisitProject, error) {
	projectID := strings.TrimSpace(c.Param("id"))
	var project models.StoreVisitProject
	if err := db.DB.First(&project, projectID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "博主探店项目不存在"})
		return nil, err
	}
	return &project, nil
}

func loadStoreVisitBloggerReferenceOr404(c *gin.Context) (*models.StoreVisitBloggerReference, error) {
	referenceID := strings.TrimSpace(c.Param("referenceId"))
	var ref models.StoreVisitBloggerReference
	if err := db.DB.First(&ref, referenceID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "博主参考图不存在"})
		return nil, err
	}
	return &ref, nil
}

func loadStoreVisitSpotOr404(c *gin.Context) (*models.StoreVisitSpot, error) {
	spotID := strings.TrimSpace(c.Param("spotId"))
	var spot models.StoreVisitSpot
	if err := db.DB.First(&spot, spotID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "探店区域不存在"})
		return nil, err
	}
	return &spot, nil
}

func normalizeStoreVisitSpotType(spotType string, name string) string {
	normalized := strings.TrimSpace(strings.ToLower(spotType))
	switch normalized {
	case storeVisitSpotTypeEntrance, storeVisitSpotTypeLobby, storeVisitSpotTypePrivateRoom, storeVisitSpotTypeKitchen, storeVisitSpotTypeFeaturedArea, storeVisitSpotTypeTableDishes, storeVisitSpotTypeDishGeneration, storeVisitSpotTypeSignatureDish, storeVisitSpotTypeTasteRecommendation, storeVisitSpotTypePromotion:
		return normalized
	}

	name = strings.TrimSpace(name)
	switch {
	case strings.Contains(name, "大厅"):
		return storeVisitSpotTypeLobby
	case strings.Contains(name, "包间"):
		return storeVisitSpotTypePrivateRoom
	case strings.Contains(name, "厨房"):
		return storeVisitSpotTypeKitchen
	case strings.Contains(name, "特色"):
		return storeVisitSpotTypeFeaturedArea
	case strings.Contains(name, "整桌"), strings.Contains(name, "整桌菜"), strings.Contains(name, "整桌菜品"):
		return storeVisitSpotTypeTableDishes
	case strings.Contains(name, "菜品生成"), strings.Contains(name, "菜品视频"), strings.Contains(name, "keyframe"):
		return storeVisitSpotTypeDishGeneration
	case strings.Contains(name, "招牌菜"), strings.Contains(name, "招牌"):
		return storeVisitSpotTypeSignatureDish
	case strings.Contains(name, "口味推荐"), strings.Contains(name, "口味"), strings.Contains(name, "味型"):
		return storeVisitSpotTypeTasteRecommendation
	case strings.Contains(name, "优惠"), strings.Contains(name, "福利"), strings.Contains(name, "套餐"):
		return storeVisitSpotTypePromotion
	default:
		return storeVisitSpotTypeEntrance
	}
}

func getStoreVisitSpotDefinition(spotType string) storeVisitSpotDefinition {
	normalized := normalizeStoreVisitSpotType(spotType, "")
	for _, item := range storeVisitDefaultSpotDefinitions {
		if item.Type == normalized {
			return item
		}
	}
	return storeVisitDefaultSpotDefinitions[0]
}

func getStoreVisitSpotDisplayName(spot models.StoreVisitSpot) string {
	name := strings.TrimSpace(spot.Name)
	if name != "" {
		return name
	}
	return getStoreVisitSpotDefinition(spot.SpotType).Name
}

func getStoreVisitSpotFileKey(spot models.StoreVisitSpot) string {
	return normalizeStoreVisitSpotType(spot.SpotType, spot.Name)
}

func ensureStoreVisitBloggerReferences(project *models.StoreVisitProject) error {
	if project == nil || project.ID == 0 {
		return nil
	}
	return db.DB.Transaction(func(tx *gorm.DB) error {
		var refs []models.StoreVisitBloggerReference
		if err := tx.Where("project_id = ?", project.ID).Order("sort_order asc, id asc").Find(&refs).Error; err != nil {
			return err
		}

		if len(refs) == 0 && strings.TrimSpace(project.BloggerReferenceImage) != "" {
			now := time.Now()
			ref := models.StoreVisitBloggerReference{
				ProjectID:  project.ID,
				SortOrder:  1,
				ImagePath:  strings.TrimSpace(project.BloggerReferenceImage),
				IsSelected: true,
				CreatedAt:  now,
				UpdatedAt:  now,
			}
			if err := tx.Create(&ref).Error; err != nil {
				return err
			}
			if err := tx.Model(&models.StoreVisitProject{}).Where("id = ?", project.ID).Updates(map[string]interface{}{
				"blogger_reference_image":       ref.ImagePath,
				"selected_blogger_reference_id": ref.ID,
				"updated_at":                    time.Now(),
			}).Error; err != nil {
				return err
			}
			project.BloggerReferenceImage = ref.ImagePath
			project.SelectedBloggerReferenceID = ref.ID
			return nil
		}
		if len(refs) == 0 {
			return nil
		}

		selectedID := project.SelectedBloggerReferenceID
		if selectedID == 0 {
			for _, ref := range refs {
				if ref.IsSelected {
					selectedID = ref.ID
					break
				}
			}
		}
		if selectedID == 0 {
			selectedID = refs[0].ID
		}

		selectedPath := ""
		for _, ref := range refs {
			isSelected := ref.ID == selectedID
			if ref.IsSelected != isSelected {
				if err := tx.Model(&models.StoreVisitBloggerReference{}).Where("id = ?", ref.ID).Updates(map[string]interface{}{
					"is_selected": isSelected,
					"updated_at":  time.Now(),
				}).Error; err != nil {
					return err
				}
			}
			if isSelected {
				selectedPath = ref.ImagePath
			}
		}
		if strings.TrimSpace(selectedPath) == "" {
			selectedID = refs[0].ID
			selectedPath = refs[0].ImagePath
			if err := tx.Model(&models.StoreVisitBloggerReference{}).Where("id = ?", selectedID).Updates(map[string]interface{}{
				"is_selected": true,
				"updated_at":  time.Now(),
			}).Error; err != nil {
				return err
			}
		}

		if project.SelectedBloggerReferenceID != selectedID || strings.TrimSpace(project.BloggerReferenceImage) != strings.TrimSpace(selectedPath) {
			if err := tx.Model(&models.StoreVisitProject{}).Where("id = ?", project.ID).Updates(map[string]interface{}{
				"blogger_reference_image":       selectedPath,
				"selected_blogger_reference_id": selectedID,
				"updated_at":                    time.Now(),
			}).Error; err != nil {
				return err
			}
			project.SelectedBloggerReferenceID = selectedID
			project.BloggerReferenceImage = selectedPath
		}
		return nil
	})
}

func createDefaultStoreVisitSpots(projectID uint) []models.StoreVisitSpot {
	now := time.Now()
	spots := make([]models.StoreVisitSpot, 0, len(storeVisitDefaultSpotDefinitions))
	for idx, def := range storeVisitDefaultSpotDefinitions {
		spots = append(spots, models.StoreVisitSpot{
			ProjectID:            projectID,
			SortOrder:            idx + 1,
			SpotType:             def.Type,
			Name:                 def.Name,
			ImagePositivePrompt:  getStoreVisitDefaultImagePositivePrompt(def.Type),
			ImageNegativePrompt:  storeVisitDefaultImageNegativePrompt,
			VideoPositivePrompt:  "",
			VideoNegativePrompt:  getFixedLTXVideoNegativePromptEN(),
			VideoDurationSeconds: storeVisitDefaultVideoDurationSecond,
			VideoWidth:           storeVisitVideoWidth,
			VideoHeight:          storeVisitVideoHeight,
			ImageStatus:          "draft",
			VideoStatus:          "draft",
			CreatedAt:            now,
			UpdatedAt:            now,
		})
	}
	return spots
}

func isDeprecatedStoreVisitSpotType(spotType string) bool {
	switch normalizeStoreVisitSpotType(spotType, "") {
	case storeVisitSpotTypeSignatureDish, storeVisitSpotTypeTasteRecommendation:
		return true
	default:
		return false
	}
}

func canAutoRemoveDeprecatedStoreVisitSpot(spot models.StoreVisitSpot) bool {
	if !isDeprecatedStoreVisitSpotType(spot.SpotType) {
		return false
	}
	if strings.TrimSpace(spot.IntroText) != "" ||
		strings.TrimSpace(spot.ReferenceImage) != "" ||
		strings.TrimSpace(spot.GeneratedImage) != "" ||
		strings.TrimSpace(spot.GeneratedVideo) != "" ||
		strings.TrimSpace(spot.VideoPositivePrompt) != "" ||
		strings.TrimSpace(spot.VideoCurrentTaskID) != "" ||
		strings.TrimSpace(spot.ImageCurrentTaskID) != "" ||
		spot.ImageStatus != "draft" ||
		spot.VideoStatus != "draft" {
		return false
	}
	return true
}

func ensureStoreVisitDefaultSpots(projectID uint) error {
	return db.DB.Transaction(func(tx *gorm.DB) error {
		var spots []models.StoreVisitSpot
		if err := tx.Where("project_id = ?", projectID).Order("sort_order asc, id asc").Find(&spots).Error; err != nil {
			return err
		}

		existing := make(map[string]uint, len(spots))
		filteredSpots := make([]models.StoreVisitSpot, 0, len(spots))
		for _, spot := range spots {
			if canAutoRemoveDeprecatedStoreVisitSpot(spot) {
				if err := tx.Delete(&models.StoreVisitSpot{}, spot.ID).Error; err != nil {
					return err
				}
				continue
			}
			normalizedType := normalizeStoreVisitSpotType(spot.SpotType, spot.Name)
			existing[normalizedType] = spot.ID
			updates := map[string]interface{}{}
			if strings.TrimSpace(spot.SpotType) != normalizedType {
				updates["spot_type"] = normalizedType
			}
			expectedPrompt := getStoreVisitDefaultImagePositivePrompt(normalizedType)
			currentPrompt := strings.TrimSpace(spot.ImagePositivePrompt)
			shouldRefreshPrompt := normalizedType != storeVisitSpotTypeEntrance && (currentPrompt == strings.TrimSpace(storeVisitDefaultImagePositivePrompt) || currentPrompt == strings.TrimSpace(expectedPrompt))
			if normalizedType == storeVisitSpotTypeTableDishes && currentPrompt == strings.TrimSpace(storeVisitInteriorImagePositivePrompt) {
				shouldRefreshPrompt = true
			}
			if shouldRefreshPrompt {
				updates["image_positive_prompt"] = expectedPrompt
			}
			if len(updates) > 0 {
				updates["updated_at"] = time.Now()
				if err := tx.Model(&models.StoreVisitSpot{}).Where("id = ?", spot.ID).Updates(updates).Error; err != nil {
					return err
				}
			}
			filteredSpots = append(filteredSpots, spot)
		}
		spots = filteredSpots

		now := time.Now()
		for idx, def := range storeVisitDefaultSpotDefinitions {
			if _, ok := existing[def.Type]; ok {
				continue
			}
			record := models.StoreVisitSpot{
				ProjectID:            projectID,
				SortOrder:            idx + 1,
				SpotType:             def.Type,
				Name:                 def.Name,
				ImagePositivePrompt:  getStoreVisitDefaultImagePositivePrompt(def.Type),
				ImageNegativePrompt:  storeVisitDefaultImageNegativePrompt,
				VideoPositivePrompt:  "",
				VideoNegativePrompt:  getFixedLTXVideoNegativePromptEN(),
				VideoDurationSeconds: storeVisitDefaultVideoDurationSecond,
				VideoWidth:           storeVisitVideoWidth,
				VideoHeight:          storeVisitVideoHeight,
				ImageStatus:          "draft",
				VideoStatus:          "draft",
				CreatedAt:            now,
				UpdatedAt:            now,
			}
			if err := tx.Create(&record).Error; err != nil {
				return err
			}
			spots = append(spots, record)
		}

		desiredOrder := make(map[string]int, len(storeVisitDefaultSpotDefinitions))
		for idx, def := range storeVisitDefaultSpotDefinitions {
			desiredOrder[def.Type] = idx + 1
		}

		fallbackOrder := len(storeVisitDefaultSpotDefinitions) + 1
		for _, spot := range spots {
			normalizedType := normalizeStoreVisitSpotType(spot.SpotType, spot.Name)
			targetOrder, ok := desiredOrder[normalizedType]
			if !ok {
				targetOrder = fallbackOrder
				fallbackOrder++
			}
			if spot.SortOrder == targetOrder {
				continue
			}
			if err := tx.Model(&models.StoreVisitSpot{}).Where("id = ?", spot.ID).Updates(map[string]interface{}{
				"sort_order": targetOrder,
				"updated_at": time.Now(),
			}).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

func storeVisitRandomSeed() int64 {
	seed := time.Now().UnixNano()
	if seed <= 0 {
		return 1
	}
	return seed
}

func shouldApplyStoreVisitImageTaskResult(spotID uint, taskID string) bool {
	var current models.StoreVisitSpot
	if err := db.DB.Select("image_current_task_id").First(&current, spotID).Error; err != nil {
		return false
	}
	return strings.TrimSpace(current.ImageCurrentTaskID) == strings.TrimSpace(taskID)
}

func shouldApplyStoreVisitVideoTaskResult(spotID uint, taskID string) bool {
	var current models.StoreVisitSpot
	if err := db.DB.Select("video_current_task_id").First(&current, spotID).Error; err != nil {
		return false
	}
	return strings.TrimSpace(current.VideoCurrentTaskID) == strings.TrimSpace(taskID)
}

func resetStoreVisitSpotAssetsAndState(spot *models.StoreVisitSpot) error {
	if err := removeGeneratedAsset(spot.GeneratedImage); err != nil {
		return err
	}
	if err := removeGeneratedVideoAsset(spot.GeneratedVideo); err != nil {
		return err
	}
	return db.DB.Model(&models.StoreVisitSpot{}).Where("id = ?", spot.ID).Updates(map[string]interface{}{
		"image_status":             "draft",
		"image_current_task_id":    "",
		"image_last_error":         "",
		"generated_image":          "",
		"image_generated_workflow": "",
		"video_status":             "draft",
		"video_current_task_id":    "",
		"video_last_error":         "",
		"generated_video":          "",
		"video_generated_workflow": "",
		"updated_at":               time.Now(),
	}).Error
}

func resetStoreVisitSpotImageState(spot *models.StoreVisitSpot) error {
	if spot == nil {
		return fmt.Errorf("探店区域不存在")
	}
	if err := removeGeneratedAsset(spot.GeneratedImage); err != nil {
		return err
	}
	return db.DB.Model(&models.StoreVisitSpot{}).Where("id = ?", spot.ID).Updates(map[string]interface{}{
		"image_status":             "draft",
		"image_current_task_id":    "",
		"image_last_error":         "",
		"generated_image":          "",
		"image_generated_workflow": "",
		"updated_at":               time.Now(),
	}).Error
}

func resetStoreVisitSpotVideoState(spot *models.StoreVisitSpot) error {
	if spot == nil {
		return fmt.Errorf("探店区域不存在")
	}
	if err := removeGeneratedVideoAsset(spot.GeneratedVideo); err != nil {
		return err
	}
	return db.DB.Model(&models.StoreVisitSpot{}).Where("id = ?", spot.ID).Updates(map[string]interface{}{
		"video_status":             "draft",
		"video_current_task_id":    "",
		"video_last_error":         "",
		"generated_video":          "",
		"video_generated_workflow": "",
		"updated_at":               time.Now(),
	}).Error
}

func interruptStoreVisitSpotGeneration(spot *models.StoreVisitSpot) error {
	if spot == nil {
		return fmt.Errorf("探店区域不存在")
	}

	updates := map[string]interface{}{
		"updated_at": time.Now(),
	}

	if spot.ImageStatus == "generating" {
		if err := removeGeneratedAsset(spot.GeneratedImage); err != nil {
			return err
		}
		if err := removeGeneratedVideoAsset(spot.GeneratedVideo); err != nil {
			return err
		}
		updates["image_status"] = "draft"
		updates["image_current_task_id"] = ""
		updates["image_last_error"] = ""
		updates["generated_image"] = ""
		updates["image_generated_workflow"] = ""
		updates["video_status"] = "draft"
		updates["video_current_task_id"] = ""
		updates["video_last_error"] = ""
		updates["generated_video"] = ""
		updates["video_generated_workflow"] = ""
	} else if spot.VideoStatus == "generating" {
		if err := removeGeneratedVideoAsset(spot.GeneratedVideo); err != nil {
			return err
		}
		updates["video_status"] = "draft"
		updates["video_current_task_id"] = ""
		updates["video_last_error"] = ""
		updates["generated_video"] = ""
		updates["video_generated_workflow"] = ""
	}

	return db.DB.Model(&models.StoreVisitSpot{}).Where("id = ?", spot.ID).Updates(updates).Error
}

func queueStoreVisitImageTask(c *gin.Context, spot *models.StoreVisitSpot, project *models.StoreVisitProject, seed int64, successMessage string) {
	spotLabel := getStoreVisitSpotDisplayName(*spot)
	if strings.TrimSpace(spot.ReferenceImage) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("请先上传%s参考图", spotLabel)})
		return
	}
	if strings.TrimSpace(project.BloggerReferenceImage) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请先上传博主参考图"})
		return
	}

	if err := removeGeneratedAsset(spot.GeneratedImage); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("清理旧%s图片失败", spotLabel)})
		return
	}
	if err := removeGeneratedVideoAsset(spot.GeneratedVideo); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("清理旧%s视频失败", spotLabel)})
		return
	}

	payload := storeVisitMediaTaskPayload{ProjectID: project.ID, SpotID: spot.ID, Seed: seed}
	taskRecord, err := task.GlobalTaskManager.AddTask("render_store_visit_spot_image", payload)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("提交%s图片生成任务失败", spotLabel)})
		return
	}

	now := time.Now()
	if err := db.DB.Model(&models.StoreVisitSpot{}).Where("id = ?", spot.ID).Updates(map[string]interface{}{
		"image_status":             "generating",
		"image_current_task_id":    taskRecord.ID,
		"image_last_error":         "",
		"generated_image":          "",
		"image_generated_workflow": "",
		"video_status":             "draft",
		"video_current_task_id":    "",
		"video_last_error":         "",
		"generated_video":          "",
		"video_generated_workflow": "",
		"updated_at":               now,
	}).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("更新%s图片状态失败", spotLabel)})
		return
	}

	BroadcastUpdate("store_visit_spot", spot.ID)
	c.JSON(http.StatusOK, gin.H{
		"message": successMessage,
		"task_id": taskRecord.ID,
	})
}

func queueStoreVisitVideoTask(c *gin.Context, spot *models.StoreVisitSpot, project *models.StoreVisitProject, seed int64, successMessage string) {
	spotLabel := getStoreVisitSpotDisplayName(*spot)
	if strings.TrimSpace(spot.GeneratedImage) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("请先生成%s图片", spotLabel)})
		return
	}

	if err := removeGeneratedVideoAsset(spot.GeneratedVideo); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("清理旧%s视频失败", spotLabel)})
		return
	}

	payload := storeVisitMediaTaskPayload{ProjectID: project.ID, SpotID: spot.ID, Seed: seed}
	taskRecord, err := task.GlobalTaskManager.AddTask("render_store_visit_spot_video", payload)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("提交%s视频生成任务失败", spotLabel)})
		return
	}

	now := time.Now()
	if err := db.DB.Model(&models.StoreVisitSpot{}).Where("id = ?", spot.ID).Updates(map[string]interface{}{
		"video_status":             "generating",
		"video_current_task_id":    taskRecord.ID,
		"video_last_error":         "",
		"generated_video":          "",
		"video_generated_workflow": "",
		"updated_at":               now,
	}).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("更新%s视频状态失败", spotLabel)})
		return
	}

	BroadcastUpdate("store_visit_spot", spot.ID)
	c.JSON(http.StatusOK, gin.H{
		"message": successMessage,
		"task_id": taskRecord.ID,
	})
}

func ListStoreVisitProjects(c *gin.Context) {
	var projects []models.StoreVisitProject
	if err := db.DB.Order("created_at desc").Find(&projects).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "获取博主探店项目失败"})
		return
	}
	c.JSON(http.StatusOK, projects)
}

func GetStoreVisitProject(c *gin.Context) {
	project, err := loadStoreVisitProjectOr404(c)
	if err != nil {
		return
	}
	if err := ensureStoreVisitBloggerReferences(project); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "补齐博主参考图失败"})
		return
	}
	c.JSON(http.StatusOK, project)
}

func ListStoreVisitBloggerReferences(c *gin.Context) {
	project, err := loadStoreVisitProjectOr404(c)
	if err != nil {
		return
	}
	if err := ensureStoreVisitBloggerReferences(project); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "补齐博主参考图失败"})
		return
	}
	var refs []models.StoreVisitBloggerReference
	if err := db.DB.Where("project_id = ?", project.ID).Order("sort_order asc, id asc").Find(&refs).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "获取博主参考图失败"})
		return
	}
	c.JSON(http.StatusOK, refs)
}

func SelectStoreVisitBloggerReference(c *gin.Context) {
	project, err := loadStoreVisitProjectOr404(c)
	if err != nil {
		return
	}
	if err := ensureStoreVisitBloggerReferences(project); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "补齐博主参考图失败"})
		return
	}
	ref, err := loadStoreVisitBloggerReferenceOr404(c)
	if err != nil {
		return
	}
	if ref.ProjectID != project.ID {
		c.JSON(http.StatusBadRequest, gin.H{"error": "博主参考图不属于当前项目"})
		return
	}

	now := time.Now()
	if err := db.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&models.StoreVisitBloggerReference{}).
			Where("project_id = ?", project.ID).
			Updates(map[string]interface{}{"is_selected": false, "updated_at": now}).Error; err != nil {
			return err
		}
		if err := tx.Model(&models.StoreVisitBloggerReference{}).
			Where("id = ?", ref.ID).
			Updates(map[string]interface{}{"is_selected": true, "updated_at": now}).Error; err != nil {
			return err
		}
		return tx.Model(&models.StoreVisitProject{}).
			Where("id = ?", project.ID).
			Updates(map[string]interface{}{
				"blogger_reference_image":       ref.ImagePath,
				"selected_blogger_reference_id": ref.ID,
				"updated_at":                    now,
			}).Error
	}); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "切换博主参考图失败"})
		return
	}

	project.BloggerReferenceImage = ref.ImagePath
	project.SelectedBloggerReferenceID = ref.ID
	project.UpdatedAt = now
	c.JSON(http.StatusOK, project)
}

func ListStoreVisitSpots(c *gin.Context) {
	project, err := loadStoreVisitProjectOr404(c)
	if err != nil {
		return
	}
	if err := ensureStoreVisitDefaultSpots(project.ID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "补齐探店区域失败"})
		return
	}
	var spots []models.StoreVisitSpot
	if err := db.DB.Where("project_id = ?", project.ID).Order("sort_order asc, id asc").Find(&spots).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "获取探店区域失败"})
		return
	}
	c.JSON(http.StatusOK, spots)
}

func CreateStoreVisitProject(c *gin.Context) {
	name := strings.TrimSpace(c.PostForm("name"))
	code := strings.TrimSpace(c.PostForm("code"))
	description := strings.TrimSpace(c.PostForm("description"))
	autoGenerateContent := strings.TrimSpace(c.PostForm("auto_generate_content"))
	if name == "" || code == "" || description == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请填写项目名称、项目文件名和备注"})
		return
	}
	matched, _ := regexp.MatchString(`^[a-zA-Z0-9_-]+$`, code)
	if !matched {
		c.JSON(http.StatusBadRequest, gin.H{"error": "项目文件名只允许英文、数字、下划线或连字符"})
		return
	}

	var count int64
	db.DB.Model(&models.StoreVisitProject{}).Where("code = ?", code).Count(&count)
	if count > 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "项目文件名已被占用"})
		return
	}
	projectDir := storeVisitProjectDir(code)
	if _, err := os.Stat(projectDir); err == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "项目文件名已被占用"})
		return
	}

	var files []*multipart.FileHeader
	if form, err := c.MultipartForm(); err == nil && form != nil {
		files = append(files, form.File["blogger_reference_images"]...)
	}
	if len(files) == 0 {
		if file, err := c.FormFile("blogger_reference_image"); err == nil && file != nil {
			files = append(files, file)
		}
	}
	if len(files) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请至少上传一张博主参考图"})
		return
	}

	if err := os.MkdirAll(storeVisitReferenceDir(code), 0755); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "创建项目目录失败"})
		return
	}

	now := time.Now()
	project := models.StoreVisitProject{
		Name:                name,
		Code:                code,
		Description:         description,
		AutoGenerateContent: autoGenerateContent,
		CreatedAt:           now,
		UpdatedAt:           now,
	}

	savedPaths := make([]string, 0, len(files))

	if err := db.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&project).Error; err != nil {
			return err
		}
		spots := createDefaultStoreVisitSpots(project.ID)
		if err := tx.Create(&spots).Error; err != nil {
			return err
		}

		for idx, file := range files {
			ref := models.StoreVisitBloggerReference{
				ProjectID:  project.ID,
				SortOrder:  idx + 1,
				IsSelected: idx == 0,
				CreatedAt:  now,
				UpdatedAt:  now,
			}
			if err := tx.Create(&ref).Error; err != nil {
				return err
			}
			ext := strings.ToLower(filepath.Ext(file.Filename))
			if ext == "" {
				ext = ".png"
			}
			absPath := storeVisitBloggerReferencePath(code, ref.ID, ext)
			if err := c.SaveUploadedFile(file, absPath); err != nil {
				return err
			}
			savedPaths = append(savedPaths, absPath)
			webPath := "/" + filepath.ToSlash(absPath)
			if err := tx.Model(&models.StoreVisitBloggerReference{}).Where("id = ?", ref.ID).Updates(map[string]interface{}{
				"image_path": webPath,
				"updated_at": now,
			}).Error; err != nil {
				return err
			}
			if idx == 0 {
				project.BloggerReferenceImage = webPath
				project.SelectedBloggerReferenceID = ref.ID
			}
		}

		return tx.Model(&models.StoreVisitProject{}).Where("id = ?", project.ID).Updates(map[string]interface{}{
			"blogger_reference_image":       project.BloggerReferenceImage,
			"selected_blogger_reference_id": project.SelectedBloggerReferenceID,
			"updated_at":                    now,
		}).Error
	}); err != nil {
		for _, path := range savedPaths {
			_ = os.Remove(path)
		}
		_ = os.RemoveAll(projectDir)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "创建博主探店项目失败"})
		return
	}

	Log(LogLevelInfo, "创建博主探店项目", fmt.Sprintf("创建了博主探店项目: %s (%s)", project.Name, project.Code))
	c.JSON(http.StatusCreated, project)
}

func UpdateStoreVisitProject(c *gin.Context) {
	project, err := loadStoreVisitProjectOr404(c)
	if err != nil {
		return
	}

	var runningCount int64
	db.DB.Model(&models.StoreVisitSpot{}).
		Where("project_id = ? AND (image_status = ? OR video_status = ?)", project.ID, "generating", "generating").
		Count(&runningCount)
	if runningCount == 0 {
		db.DB.Model(&models.StoreVisitDishGenerationItem{}).
			Where("project_id = ? AND video_status = ?", project.ID, "generating").
			Count(&runningCount)
	}
	if runningCount > 0 {
		c.JSON(http.StatusConflict, gin.H{"error": "项目仍在生成中，暂时不能编辑"})
		return
	}

	name := strings.TrimSpace(c.PostForm("name"))
	code := strings.TrimSpace(c.PostForm("code"))
	description := strings.TrimSpace(c.PostForm("description"))
	autoGenerateContent, hasAutoGenerateContent := c.GetPostForm("auto_generate_content")
	if name == "" || code == "" || description == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请填写项目名称、项目文件名和备注"})
		return
	}
	matched, _ := regexp.MatchString(`^[a-zA-Z0-9_-]+$`, code)
	if !matched {
		c.JSON(http.StatusBadRequest, gin.H{"error": "项目文件名只允许英文、数字、下划线或连字符"})
		return
	}

	oldCode := project.Code
	oldProjectDir := storeVisitProjectDir(oldCode)
	newProjectDir := storeVisitProjectDir(code)
	if code != oldCode {
		var count int64
		db.DB.Model(&models.StoreVisitProject{}).Where("code = ? AND id <> ?", code, project.ID).Count(&count)
		if count > 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "项目文件名已被占用"})
			return
		}
		if _, err := os.Stat(newProjectDir); err == nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "项目文件名已被占用"})
			return
		}
	}

	var files []*multipart.FileHeader
	if form, err := c.MultipartForm(); err == nil && form != nil {
		files = append(files, form.File["blogger_reference_images"]...)
	}
	if len(files) == 0 {
		if file, err := c.FormFile("blogger_reference_image"); err == nil && file != nil {
			files = append(files, file)
		}
	}

	type refUploadInfo struct {
		ID      uint
		AbsPath string
		WebPath string
	}

	var refs []models.StoreVisitBloggerReference
	if err := db.DB.Where("project_id = ?", project.ID).Order("sort_order asc, id asc").Find(&refs).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "读取博主参考图失败"})
		return
	}

	if err := os.MkdirAll(storeVisitReferenceDir(code), 0755); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "创建项目目录失败"})
		return
	}

	now := time.Now()
	uploadedRefs := make([]refUploadInfo, 0, len(files))

	if err := db.DB.Transaction(func(tx *gorm.DB) error {
		if code != oldCode {
			if err := tx.Where("project_id = ?", project.ID).Order("sort_order asc, id asc").Find(&refs).Error; err != nil {
				return err
			}
			var spots []models.StoreVisitSpot
			if err := tx.Where("project_id = ?", project.ID).Find(&spots).Error; err != nil {
				return err
			}

			oldPrefix := "/" + filepath.ToSlash(oldProjectDir)
			newPrefix := "/" + filepath.ToSlash(newProjectDir)
			replacePrefix := func(path string) string {
				trimmed := strings.TrimSpace(path)
				if trimmed == "" {
					return ""
				}
				if strings.HasPrefix(trimmed, oldPrefix) {
					return newPrefix + strings.TrimPrefix(trimmed, oldPrefix)
				}
				return trimmed
			}

			if err := os.Rename(oldProjectDir, newProjectDir); err != nil && !os.IsNotExist(err) {
				return err
			}

			for _, ref := range refs {
				nextPath := replacePrefix(ref.ImagePath)
				if nextPath == ref.ImagePath {
					continue
				}
				if err := tx.Model(&models.StoreVisitBloggerReference{}).Where("id = ?", ref.ID).Updates(map[string]interface{}{
					"image_path": nextPath,
					"updated_at": now,
				}).Error; err != nil {
					return err
				}
			}
			for _, spot := range spots {
				if err := tx.Model(&models.StoreVisitSpot{}).Where("id = ?", spot.ID).Updates(map[string]interface{}{
					"reference_image": strings.TrimSpace(replacePrefix(spot.ReferenceImage)),
					"generated_image": strings.TrimSpace(replacePrefix(spot.GeneratedImage)),
					"generated_video": strings.TrimSpace(replacePrefix(spot.GeneratedVideo)),
					"updated_at":      now,
				}).Error; err != nil {
					return err
				}
			}
			project.BloggerReferenceImage = replacePrefix(project.BloggerReferenceImage)
		}

		nextSortOrder := len(refs) + 1
		for idx, file := range files {
			ref := models.StoreVisitBloggerReference{
				ProjectID:  project.ID,
				SortOrder:  nextSortOrder + idx,
				IsSelected: false,
				CreatedAt:  now,
				UpdatedAt:  now,
			}
			if err := tx.Create(&ref).Error; err != nil {
				return err
			}
			ext := strings.ToLower(filepath.Ext(file.Filename))
			if ext == "" {
				ext = ".png"
			}
			absPath := storeVisitBloggerReferencePath(code, ref.ID, ext)
			if err := c.SaveUploadedFile(file, absPath); err != nil {
				return err
			}
			webPath := "/" + filepath.ToSlash(absPath)
			uploadedRefs = append(uploadedRefs, refUploadInfo{
				ID:      ref.ID,
				AbsPath: absPath,
				WebPath: webPath,
			})
			if err := tx.Model(&models.StoreVisitBloggerReference{}).Where("id = ?", ref.ID).Updates(map[string]interface{}{
				"image_path": webPath,
				"updated_at": now,
			}).Error; err != nil {
				return err
			}
		}

		updates := map[string]interface{}{
			"name":        name,
			"code":        code,
			"description": description,
			"updated_at":  now,
		}
		if hasAutoGenerateContent {
			updates["auto_generate_content"] = strings.TrimSpace(autoGenerateContent)
		}
		if strings.TrimSpace(project.BloggerReferenceImage) == "" && len(uploadedRefs) > 0 {
			updates["blogger_reference_image"] = uploadedRefs[0].WebPath
			updates["selected_blogger_reference_id"] = uploadedRefs[0].ID
			if err := tx.Model(&models.StoreVisitBloggerReference{}).Where("id = ?", uploadedRefs[0].ID).Updates(map[string]interface{}{
				"is_selected": true,
				"updated_at":  now,
			}).Error; err != nil {
				return err
			}
		} else {
			updates["blogger_reference_image"] = project.BloggerReferenceImage
			updates["selected_blogger_reference_id"] = project.SelectedBloggerReferenceID
		}
		return tx.Model(&models.StoreVisitProject{}).Where("id = ?", project.ID).Updates(updates).Error
	}); err != nil {
		for _, item := range uploadedRefs {
			_ = os.Remove(item.AbsPath)
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "更新博主探店项目失败"})
		return
	}

	project.Name = name
	project.Code = code
	project.Description = description
	if hasAutoGenerateContent {
		project.AutoGenerateContent = strings.TrimSpace(autoGenerateContent)
	}
	project.UpdatedAt = now
	if strings.TrimSpace(project.BloggerReferenceImage) == "" && len(uploadedRefs) > 0 {
		project.BloggerReferenceImage = uploadedRefs[0].WebPath
		project.SelectedBloggerReferenceID = uploadedRefs[0].ID
	}

	Log(LogLevelInfo, "更新博主探店项目", fmt.Sprintf("更新了博主探店项目: %s (%s)", project.Name, project.Code))
	c.JSON(http.StatusOK, project)
}

func DeleteStoreVisitProject(c *gin.Context) {
	project, err := loadStoreVisitProjectOr404(c)
	if err != nil {
		return
	}

	var runningCount int64
	db.DB.Model(&models.StoreVisitSpot{}).
		Where("project_id = ? AND (image_status = ? OR video_status = ?)", project.ID, "generating", "generating").
		Count(&runningCount)
	if runningCount == 0 {
		db.DB.Model(&models.StoreVisitDishGenerationItem{}).
			Where("project_id = ? AND video_status = ?", project.ID, "generating").
			Count(&runningCount)
	}
	if runningCount > 0 {
		c.JSON(http.StatusConflict, gin.H{"error": "项目仍在生成中，暂时不能删除"})
		return
	}

	projectDir := storeVisitProjectDir(project.Code)
	if err := db.DB.Transaction(func(tx *gorm.DB) error {
		var spots []models.StoreVisitSpot
		if err := tx.Where("project_id = ?", project.ID).Find(&spots).Error; err != nil {
			return err
		}
		var refs []models.StoreVisitBloggerReference
		if err := tx.Where("project_id = ?", project.ID).Find(&refs).Error; err != nil {
			return err
		}
		var dishItems []models.StoreVisitDishGenerationItem
		if err := tx.Where("project_id = ?", project.ID).Find(&dishItems).Error; err != nil {
			return err
		}
		removedPaths := map[string]struct{}{}
		removeOnce := func(path string, remover func(string) error) error {
			trimmed := strings.TrimSpace(path)
			if trimmed == "" {
				return nil
			}
			if _, exists := removedPaths[trimmed]; exists {
				return nil
			}
			if err := remover(trimmed); err != nil {
				return err
			}
			removedPaths[trimmed] = struct{}{}
			return nil
		}
		for _, spot := range spots {
			if err := removeOnce(spot.GeneratedImage, removeGeneratedAsset); err != nil {
				return err
			}
			if err := removeOnce(spot.GeneratedVideo, removeGeneratedVideoAsset); err != nil {
				return err
			}
			if err := removeOnce(spot.ReferenceImage, removeGeneratedAsset); err != nil {
				return err
			}
		}
		for _, ref := range refs {
			if err := removeOnce(ref.ImagePath, removeGeneratedAsset); err != nil {
				return err
			}
		}
		for _, item := range dishItems {
			for _, path := range decodeStoreVisitDishGenerationFrames(item) {
				if err := removeOnce(path, removeGeneratedAsset); err != nil {
					return err
				}
			}
			if err := removeOnce(item.GeneratedVideo, removeGeneratedVideoAsset); err != nil {
				return err
			}
		}
		if err := removeOnce(project.BloggerReferenceImage, removeGeneratedAsset); err != nil {
			return err
		}
		if err := tx.Where("project_id = ?", project.ID).Delete(&models.StoreVisitBloggerReference{}).Error; err != nil {
			return err
		}
		if err := tx.Where("project_id = ?", project.ID).Delete(&models.StoreVisitDishGenerationItem{}).Error; err != nil {
			return err
		}
		if err := tx.Where("project_id = ?", project.ID).Delete(&models.StoreVisitSpot{}).Error; err != nil {
			return err
		}
		return tx.Delete(project).Error
	}); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "删除博主探店项目失败"})
		return
	}

	if project.Code != "" && project.Code != "." && project.Code != ".." {
		_ = os.RemoveAll(projectDir)
	}
	c.JSON(http.StatusOK, gin.H{"message": "博主探店项目已删除"})
}

func UpdateStoreVisitSpot(c *gin.Context) {
	spot, err := loadStoreVisitSpotOr404(c)
	if err != nil {
		return
	}
	if spot.ImageStatus == "generating" || spot.VideoStatus == "generating" {
		c.JSON(http.StatusConflict, gin.H{"error": "当前条目正在生成中，暂时不能修改"})
		return
	}

	var project models.StoreVisitProject
	if err := db.DB.First(&project, spot.ProjectID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "所属项目不存在"})
		return
	}

	introText, hasIntroText := c.GetPostForm("intro_text")
	imagePositive, hasImagePositive := c.GetPostForm("image_positive_prompt")
	imageNegative, hasImageNegative := c.GetPostForm("image_negative_prompt")
	videoPositive, hasVideoPositive := c.GetPostForm("video_positive_prompt")
	videoNegative, hasVideoNegative := c.GetPostForm("video_negative_prompt")
	clearReferenceImage := strings.TrimSpace(c.PostForm("clear_reference_image")) == "1"
	duration := spot.VideoDurationSeconds
	videoWidth := spot.VideoWidth
	videoHeight := spot.VideoHeight
	if rawDuration, ok := c.GetPostForm("video_duration_seconds"); ok && strings.TrimSpace(rawDuration) != "" {
		var parsed int
		fmt.Sscanf(strings.TrimSpace(rawDuration), "%d", &parsed)
		if parsed > 0 {
			duration = parsed
		}
	}
	if rawWidth, ok := c.GetPostForm("video_width"); ok && strings.TrimSpace(rawWidth) != "" {
		var parsed int
		fmt.Sscanf(strings.TrimSpace(rawWidth), "%d", &parsed)
		if parsed > 0 {
			videoWidth = parsed
		}
	}
	if rawHeight, ok := c.GetPostForm("video_height"); ok && strings.TrimSpace(rawHeight) != "" {
		var parsed int
		fmt.Sscanf(strings.TrimSpace(rawHeight), "%d", &parsed)
		if parsed > 0 {
			videoHeight = parsed
		}
	}

	now := time.Now()
	updates := map[string]interface{}{
		"updated_at":             now,
		"video_duration_seconds": duration,
		"video_width":            videoWidth,
		"video_height":           videoHeight,
	}
	if hasIntroText {
		updates["intro_text"] = strings.TrimSpace(introText)
	}
	if hasImagePositive {
		updates["image_positive_prompt"] = strings.TrimSpace(imagePositive)
	}
	if hasImageNegative {
		updates["image_negative_prompt"] = strings.TrimSpace(imageNegative)
	}
	if hasVideoPositive {
		updates["video_positive_prompt"] = strings.TrimSpace(videoPositive)
	}
	if hasVideoNegative {
		updates["video_negative_prompt"] = strings.TrimSpace(videoNegative)
	}

	file, fileErr := c.FormFile("reference_image")
	hasNewReference := fileErr == nil
	if clearReferenceImage && hasNewReference {
		c.JSON(http.StatusBadRequest, gin.H{"error": "不能同时上传和清空参考图"})
		return
	}

	if clearReferenceImage {
		oldReference := strings.TrimSpace(spot.ReferenceImage)
		updates["reference_image"] = ""
		updates["image_status"] = "draft"
		updates["image_current_task_id"] = ""
		updates["image_last_error"] = ""
		updates["generated_image"] = ""
		updates["image_generated_workflow"] = ""
		updates["video_status"] = "draft"
		updates["video_current_task_id"] = ""
		updates["video_last_error"] = ""
		updates["generated_video"] = ""
		updates["video_generated_workflow"] = ""
		if err := removeGeneratedAsset(oldReference); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("删除旧%s参考图失败", getStoreVisitSpotDisplayName(*spot))})
			return
		}
		if err := removeGeneratedAsset(spot.GeneratedImage); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("删除旧%s图片失败", getStoreVisitSpotDisplayName(*spot))})
			return
		}
		if err := removeGeneratedVideoAsset(spot.GeneratedVideo); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("删除旧%s视频失败", getStoreVisitSpotDisplayName(*spot))})
			return
		}
	}
	if hasNewReference {
		ext := strings.ToLower(filepath.Ext(file.Filename))
		if ext == "" {
			ext = ".png"
		}
		refFilename := fmt.Sprintf("%s_%d%s", getStoreVisitSpotFileKey(*spot), time.Now().UnixNano(), ext)
		refPath := filepath.Join(storeVisitReferenceDir(project.Code), refFilename)
		if err := os.MkdirAll(storeVisitReferenceDir(project.Code), 0755); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "创建参考图目录失败"})
			return
		}
		if err := c.SaveUploadedFile(file, refPath); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("保存%s参考图失败", getStoreVisitSpotDisplayName(*spot))})
			return
		}
		oldReference := strings.TrimSpace(spot.ReferenceImage)
		updates["reference_image"] = "/" + filepath.ToSlash(refPath)
		updates["image_status"] = "draft"
		updates["image_current_task_id"] = ""
		updates["image_last_error"] = ""
		updates["generated_image"] = ""
		updates["image_generated_workflow"] = ""
		updates["video_status"] = "draft"
		updates["video_current_task_id"] = ""
		updates["video_last_error"] = ""
		updates["generated_video"] = ""
		updates["video_generated_workflow"] = ""
		if err := removeGeneratedAsset(oldReference); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("删除旧%s参考图失败", getStoreVisitSpotDisplayName(*spot))})
			return
		}
		if err := removeGeneratedAsset(spot.GeneratedImage); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("删除旧%s图片失败", getStoreVisitSpotDisplayName(*spot))})
			return
		}
		if err := removeGeneratedVideoAsset(spot.GeneratedVideo); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("删除旧%s视频失败", getStoreVisitSpotDisplayName(*spot))})
			return
		}
	}

	if err := db.DB.Model(&models.StoreVisitSpot{}).Where("id = ?", spot.ID).Updates(updates).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("保存%s内容失败", getStoreVisitSpotDisplayName(*spot))})
		return
	}

	var refreshed models.StoreVisitSpot
	if err := db.DB.First(&refreshed, spot.ID).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "读取更新后的探店区域失败"})
		return
	}
	BroadcastUpdate("store_visit_spot", refreshed.ID)
	c.JSON(http.StatusOK, refreshed)
}

func GenerateStoreVisitSpotImage(c *gin.Context) {
	spot, err := loadStoreVisitSpotOr404(c)
	if err != nil {
		return
	}
	if spot.ImageStatus == "generating" || spot.VideoStatus == "generating" {
		c.JSON(http.StatusConflict, gin.H{"error": "当前条目仍在生成中，请等待完成后再操作"})
		return
	}
	if strings.TrimSpace(spot.ReferenceImage) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("请先上传%s参考图", getStoreVisitSpotDisplayName(*spot))})
		return
	}
	var project models.StoreVisitProject
	if err := db.DB.First(&project, spot.ProjectID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "所属项目不存在"})
		return
	}
	queueStoreVisitImageTask(c, spot, &project, getConfiguredGlobalSeed(), fmt.Sprintf("%s图片生成任务已提交", getStoreVisitSpotDisplayName(*spot)))
}

func RerollStoreVisitSpotImage(c *gin.Context) {
	spot, err := loadStoreVisitSpotOr404(c)
	if err != nil {
		return
	}
	if spot.ImageStatus == "generating" || spot.VideoStatus == "generating" {
		c.JSON(http.StatusConflict, gin.H{"error": "当前条目仍在生成中，请等待完成后再操作"})
		return
	}
	var project models.StoreVisitProject
	if err := db.DB.First(&project, spot.ProjectID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "所属项目不存在"})
		return
	}
	queueStoreVisitImageTask(c, spot, &project, storeVisitRandomSeed(), fmt.Sprintf("%s图片重新抽卡任务已提交", getStoreVisitSpotDisplayName(*spot)))
}

func GenerateStoreVisitSpotVideo(c *gin.Context) {
	spot, err := loadStoreVisitSpotOr404(c)
	if err != nil {
		return
	}
	if spot.ImageStatus == "generating" || spot.VideoStatus == "generating" {
		c.JSON(http.StatusConflict, gin.H{"error": "当前条目仍在生成中，请等待完成后再操作"})
		return
	}
	if strings.TrimSpace(spot.GeneratedImage) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("请先生成%s图片", getStoreVisitSpotDisplayName(*spot))})
		return
	}
	if strings.TrimSpace(spot.VideoPositivePrompt) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请先填写视频提示词，或先使用项目级一键生成提示词"})
		return
	}
	var project models.StoreVisitProject
	if err := db.DB.First(&project, spot.ProjectID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "所属项目不存在"})
		return
	}
	queueStoreVisitVideoTask(c, spot, &project, getConfiguredGlobalSeed(), fmt.Sprintf("%s视频生成任务已提交", getStoreVisitSpotDisplayName(*spot)))
}

func RerollStoreVisitSpotVideo(c *gin.Context) {
	spot, err := loadStoreVisitSpotOr404(c)
	if err != nil {
		return
	}
	if spot.ImageStatus == "generating" || spot.VideoStatus == "generating" {
		c.JSON(http.StatusConflict, gin.H{"error": "当前条目仍在生成中，请等待完成后再操作"})
		return
	}
	if strings.TrimSpace(spot.GeneratedImage) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("请先生成%s图片", getStoreVisitSpotDisplayName(*spot))})
		return
	}
	if strings.TrimSpace(spot.VideoPositivePrompt) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请先填写视频提示词，或先使用项目级一键生成提示词"})
		return
	}
	var project models.StoreVisitProject
	if err := db.DB.First(&project, spot.ProjectID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "所属项目不存在"})
		return
	}
	queueStoreVisitVideoTask(c, spot, &project, storeVisitRandomSeed(), fmt.Sprintf("%s视频重新抽卡任务已提交", getStoreVisitSpotDisplayName(*spot)))
}

func ResetStoreVisitSpotState(c *gin.Context) {
	spot, err := loadStoreVisitSpotOr404(c)
	if err != nil {
		return
	}
	if err := resetStoreVisitSpotAssetsAndState(spot); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("重置%s状态失败", getStoreVisitSpotDisplayName(*spot))})
		return
	}
	BroadcastUpdate("store_visit_spot", spot.ID)
	c.JSON(http.StatusOK, gin.H{"message": fmt.Sprintf("%s状态已重置", getStoreVisitSpotDisplayName(*spot))})
}

func InterruptStoreVisitSpotGeneration(c *gin.Context) {
	spot, err := loadStoreVisitSpotOr404(c)
	if err != nil {
		return
	}
	if spot.ImageStatus != "generating" && spot.VideoStatus != "generating" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "当前没有正在生成的任务"})
		return
	}
	if err := StopComfyUI(); err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	if err := interruptStoreVisitSpotGeneration(spot); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("中断后重置%s状态失败", getStoreVisitSpotDisplayName(*spot))})
		return
	}
	BroadcastUpdate("store_visit_spot", spot.ID)
	c.JSON(http.StatusOK, gin.H{"message": "已中断当前生成任务"})
}

func loadStoreVisitWorkflowTemplate(path string) (map[string]interface{}, error) {
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

func cloneStoreVisitWorkflow(src map[string]interface{}) (map[string]interface{}, error) {
	raw, err := json.Marshal(src)
	if err != nil {
		return nil, err
	}
	var dst map[string]interface{}
	if err := json.Unmarshal(raw, &dst); err != nil {
		return nil, err
	}
	return dst, nil
}

func setStoreVisitWorkflowInput(workflowJSON map[string]interface{}, nodeID string, inputKey string, value interface{}) error {
	nodeRaw, ok := workflowJSON[nodeID]
	if !ok {
		return fmt.Errorf("workflow missing node %s", nodeID)
	}
	node, ok := nodeRaw.(map[string]interface{})
	if !ok {
		return fmt.Errorf("workflow node %s invalid", nodeID)
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

func setStoreVisitPrimitiveIntByTitle(workflowJSON map[string]interface{}, title string, value int) {
	for _, nodeRaw := range workflowJSON {
		node, ok := nodeRaw.(map[string]interface{})
		if !ok {
			continue
		}
		classType, _ := node["class_type"].(string)
		if classType != "PrimitiveInt" {
			continue
		}
		meta, _ := node["_meta"].(map[string]interface{})
		nodeTitle, _ := meta["title"].(string)
		if strings.TrimSpace(nodeTitle) != title {
			continue
		}
		inputs, _ := node["inputs"].(map[string]interface{})
		if inputs == nil {
			continue
		}
		inputs["value"] = value
	}
}

func buildStoreVisitImageWorkflow(template map[string]interface{}, bloggerImageName string, areaImageName string, spot models.StoreVisitSpot, project models.StoreVisitProject, seed int64) (map[string]interface{}, error) {
	workflowJSON, err := cloneStoreVisitWorkflow(template)
	if err != nil {
		return nil, err
	}
	if seed <= 0 {
		seed = getConfiguredGlobalSeed()
	}
	image1Name := bloggerImageName
	image2Name := areaImageName
	if getConfiguredStoreVisitImageReferenceOrder() == StoreVisitImageOrderSceneFirst {
		image1Name = areaImageName
		image2Name = bloggerImageName
	}
	if err := setStoreVisitWorkflowInput(workflowJSON, "78", "image", image1Name); err != nil {
		return nil, err
	}
	if err := setStoreVisitWorkflowInput(workflowJSON, "120", "image", image2Name); err != nil {
		return nil, err
	}
	if err := setStoreVisitWorkflowInput(workflowJSON, "115:111", "prompt", strings.TrimSpace(spot.ImagePositivePrompt)); err != nil {
		return nil, err
	}
	if err := setStoreVisitWorkflowInput(workflowJSON, "115:110", "prompt", strings.TrimSpace(spot.ImageNegativePrompt)); err != nil {
		return nil, err
	}
	if err := setStoreVisitWorkflowInput(workflowJSON, "115:3", "seed", seed); err != nil {
		return nil, err
	}
	if err := setStoreVisitWorkflowInput(workflowJSON, "60", "filename_prefix", fmt.Sprintf("%s_%s_image_%d", project.Code, getStoreVisitSpotFileKey(spot), spot.ID)); err != nil {
		return nil, err
	}
	return workflowJSON, nil
}

func buildStoreVisitVideoWorkflow(spot models.StoreVisitSpot, project models.StoreVisitProject, seed int64) (map[string]interface{}, string, error) {
	data, err := os.ReadFile(storeVisitVideoWorkflowPath)
	if err != nil {
		return nil, "", err
	}
	var workflowJSON map[string]interface{}
	if err := json.Unmarshal(data, &workflowJSON); err != nil {
		return nil, "", err
	}

	meta, err := workflow.ParseWorkflow(storeVisitVideoWorkflowPath)
	if err != nil {
		return nil, "", err
	}
	workflowLabel := workflowDisplayNameFromPath(storeVisitVideoWorkflowPath)
	setInput := func(nodeID string, key string, value interface{}) {
		if strings.TrimSpace(nodeID) == "" {
			return
		}
		if node, ok := workflowJSON[nodeID].(map[string]interface{}); ok {
			if inputs, ok := node["inputs"].(map[string]interface{}); ok {
				inputs[key] = value
			}
		}
	}

	setInput(meta.PositiveNodeID, meta.PositiveInputKey, strings.TrimSpace(spot.VideoPositivePrompt))
	negativePrompt := buildSegmentNegativePrompt(spot.VideoNegativePrompt)
	setInput(meta.NegativeNodeID, meta.NegativeInputKey, negativePrompt)
	if seed <= 0 {
		seed = getConfiguredGlobalSeed()
	}
	setInput(meta.SeedNodeID, meta.SeedInputKey, seed)
	width := spot.VideoWidth
	height := spot.VideoHeight
	if width <= 0 {
		width = storeVisitVideoWidth
	}
	if height <= 0 {
		height = storeVisitVideoHeight
	}
	setStoreVisitPrimitiveIntByTitle(workflowJSON, "Width", width)
	setStoreVisitPrimitiveIntByTitle(workflowJSON, "Height", height)
	setStoreVisitPrimitiveIntByTitle(workflowJSON, "Frame Rate", storeVisitDefaultVideoFPS)
	duration := spot.VideoDurationSeconds
	if duration <= 0 {
		duration = storeVisitDefaultVideoDurationSecond
	}
	// Keep the stored duration unchanged, but give ComfyUI one extra second of runway
	// so the final expression/ending is less likely to be cut off.
	duration++
	frameCount := storeVisitDefaultVideoFPS*duration + 1
	setStoreVisitPrimitiveIntByTitle(workflowJSON, "Length", frameCount)

	var imageNodeID string
	for id, node := range workflowJSON {
		if nodeMap, ok := node.(map[string]interface{}); ok {
			if classType, ok := nodeMap["class_type"].(string); ok && classType == "LoadImage" {
				imageNodeID = id
				break
			}
		}
	}
	if imageNodeID == "" {
		return nil, "", fmt.Errorf("video workflow missing LoadImage node")
	}
	imageAbsPath, err := assetWebPathToAbs(spot.GeneratedImage)
	if err != nil {
		return nil, "", err
	}
	uploadedName, err := UploadToComfyUIInput(imageAbsPath)
	if err != nil {
		setInput(imageNodeID, "image", imageAbsPath)
	} else {
		setInput(imageNodeID, "image", uploadedName)
	}

	for _, node := range workflowJSON {
		nodeMap, ok := node.(map[string]interface{})
		if !ok {
			continue
		}
		classType, _ := nodeMap["class_type"].(string)
		if classType == "SaveVideo" || classType == "VHS_VideoCombine" {
			if inputs, ok := nodeMap["inputs"].(map[string]interface{}); ok {
				inputs["filename_prefix"] = fmt.Sprintf("%s_%s_video_%d", project.Code, getStoreVisitSpotFileKey(spot), spot.ID)
			}
		}
	}

	return workflowJSON, workflowLabel, nil
}

func waitForStoreVisitImageOutput(promptID string, projectCode string, spotKey string, spotID uint, shouldContinue func() bool) (string, error) {
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		if shouldContinue != nil && !shouldContinue() {
			return "", fmt.Errorf("store visit image generation interrupted")
		}
		history, err := GetComfyHistory(promptID)
		if err != nil {
			continue
		}
		outputs, ok := history["outputs"].(map[string]interface{})
		if !ok {
			continue
		}
		for _, nodeOutput := range outputs {
			outputMap, ok := nodeOutput.(map[string]interface{})
			if !ok {
				continue
			}
			images, ok := outputMap["images"].([]interface{})
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
			saveDir := storeVisitImagesDir(projectCode)
			if err := os.MkdirAll(saveDir, 0755); err != nil {
				return "", err
			}
			ext := filepath.Ext(filename)
			if ext == "" {
				ext = ".png"
			}
			saveFilename := fmt.Sprintf("%s_%d_%d%s", spotKey, spotID, time.Now().UnixNano(), ext)
			savePath := filepath.Join(saveDir, saveFilename)
			if err := DownloadComfyImage(filename, subfolder, typeStr, savePath); err != nil {
				return "", err
			}
			return "/" + filepath.ToSlash(savePath), nil
		}
	}
	return "", nil
}

func waitForStoreVisitVideoOutput(promptID string, projectCode string, spotKey string, spotID uint, shouldContinue func() bool) (string, error) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		if shouldContinue != nil && !shouldContinue() {
			return "", fmt.Errorf("store visit video generation interrupted")
		}
		history, err := GetComfyHistory(promptID)
		if err != nil {
			continue
		}
		outputs, ok := history["outputs"].(map[string]interface{})
		if !ok {
			continue
		}
		for _, nodeOutput := range outputs {
			outputMap, ok := nodeOutput.(map[string]interface{})
			if !ok {
				continue
			}
			var fileData map[string]interface{}
			if gifs, ok := outputMap["gifs"].([]interface{}); ok && len(gifs) > 0 {
				fileData, _ = gifs[0].(map[string]interface{})
			} else if images, ok := outputMap["images"].([]interface{}); ok && len(images) > 0 {
				fileData, _ = images[0].(map[string]interface{})
			}
			if fileData == nil {
				continue
			}
			filename, _ := fileData["filename"].(string)
			subfolder, _ := fileData["subfolder"].(string)
			typeStr, _ := fileData["type"].(string)
			if filename == "" {
				continue
			}
			saveDir := storeVisitVideosDir(projectCode)
			if err := os.MkdirAll(saveDir, 0755); err != nil {
				return "", err
			}
			ext := filepath.Ext(filename)
			if ext == "" {
				ext = ".mp4"
			}
			saveFilename := fmt.Sprintf("%s_%d_%d%s", spotKey, spotID, time.Now().UnixNano(), ext)
			savePath := filepath.Join(saveDir, saveFilename)
			if err := DownloadComfyImage(filename, subfolder, typeStr, savePath); err != nil {
				return "", err
			}
			return "/" + filepath.ToSlash(savePath), nil
		}
	}
	return "", nil
}

func HandleRenderStoreVisitSpotImageTask(t *models.Task) (interface{}, error) {
	var payload storeVisitMediaTaskPayload
	if err := json.Unmarshal([]byte(t.Payload), &payload); err != nil {
		return nil, fmt.Errorf("invalid payload: %w", err)
	}

	var project models.StoreVisitProject
	if err := db.DB.First(&project, payload.ProjectID).Error; err != nil {
		return nil, fmt.Errorf("project not found: %w", err)
	}
	var spot models.StoreVisitSpot
	if err := db.DB.First(&spot, payload.SpotID).Error; err != nil {
		return nil, fmt.Errorf("spot not found: %w", err)
	}
	spotLabel := getStoreVisitSpotDisplayName(spot)
	spotKey := getStoreVisitSpotFileKey(spot)

	template, err := loadStoreVisitWorkflowTemplate(storeVisitImageWorkflowPath)
	if err != nil {
		_ = db.DB.Model(&models.StoreVisitSpot{}).Where("id = ?", spot.ID).Updates(map[string]interface{}{
			"image_status":          "failed",
			"image_current_task_id": "",
			"image_last_error":      err.Error(),
			"updated_at":            time.Now(),
		}).Error
		return nil, err
	}

	bloggerRefAbs, err := assetWebPathToAbs(project.BloggerReferenceImage)
	if err != nil {
		return nil, err
	}
	spotRefAbs, err := assetWebPathToAbs(spot.ReferenceImage)
	if err != nil {
		return nil, err
	}

	bloggerImageName, err := UploadToComfyUIInput(bloggerRefAbs)
	if err != nil {
		_ = db.DB.Model(&models.StoreVisitSpot{}).Where("id = ?", spot.ID).Updates(map[string]interface{}{
			"image_status":          "failed",
			"image_current_task_id": "",
			"image_last_error":      err.Error(),
			"updated_at":            time.Now(),
		}).Error
		return nil, err
	}
	spotImageName, err := UploadToComfyUIInput(spotRefAbs)
	if err != nil {
		_ = db.DB.Model(&models.StoreVisitSpot{}).Where("id = ?", spot.ID).Updates(map[string]interface{}{
			"image_status":          "failed",
			"image_current_task_id": "",
			"image_last_error":      err.Error(),
			"updated_at":            time.Now(),
		}).Error
		return nil, err
	}

	workflowJSON, err := buildStoreVisitImageWorkflow(template, bloggerImageName, spotImageName, spot, project, payload.Seed)
	if err != nil {
		if shouldApplyStoreVisitImageTaskResult(spot.ID, t.ID) {
			_ = db.DB.Model(&models.StoreVisitSpot{}).Where("id = ?", spot.ID).Updates(map[string]interface{}{
				"image_status":          "failed",
				"image_current_task_id": "",
				"image_last_error":      err.Error(),
				"updated_at":            time.Now(),
			}).Error
		}
		return nil, err
	}

	logComfyWorkflowPayload("Store Visit Image ComfyUI Payload", workflowDisplayNameFromPath(storeVisitImageWorkflowPath), workflowJSON)
	promptID, err := QueueComfyPrompt(workflowJSON)
	if err != nil {
		if shouldApplyStoreVisitImageTaskResult(spot.ID, t.ID) {
			_ = db.DB.Model(&models.StoreVisitSpot{}).Where("id = ?", spot.ID).Updates(map[string]interface{}{
				"image_status":          "failed",
				"image_current_task_id": "",
				"image_last_error":      err.Error(),
				"updated_at":            time.Now(),
			}).Error
		}
		return nil, err
	}
	Log(LogLevelInfo, fmt.Sprintf("博主探店%s图片已提交到 ComfyUI 队列", spotLabel), fmt.Sprintf("ProjectID: %d\nSpotID: %d\nPromptID: %s", project.ID, spot.ID, promptID))
	task.GlobalTaskManager.UpdateTaskProgress(t.ID, 40, "")

	webPath, err := waitForStoreVisitImageOutput(promptID, project.Code, spotKey, spot.ID, func() bool {
		return shouldApplyStoreVisitImageTaskResult(spot.ID, t.ID)
	})
	if err != nil {
		if shouldApplyStoreVisitImageTaskResult(spot.ID, t.ID) {
			_ = db.DB.Model(&models.StoreVisitSpot{}).Where("id = ?", spot.ID).Updates(map[string]interface{}{
				"image_status":          "failed",
				"image_current_task_id": "",
				"image_last_error":      err.Error(),
				"updated_at":            time.Now(),
			}).Error
		}
		return nil, err
	}
	if strings.TrimSpace(webPath) == "" {
		err = fmt.Errorf("未获取到%s图片输出", spotLabel)
		if shouldApplyStoreVisitImageTaskResult(spot.ID, t.ID) {
			_ = db.DB.Model(&models.StoreVisitSpot{}).Where("id = ?", spot.ID).Updates(map[string]interface{}{
				"image_status":          "failed",
				"image_current_task_id": "",
				"image_last_error":      err.Error(),
				"updated_at":            time.Now(),
			}).Error
		}
		return nil, err
	}

	task.GlobalTaskManager.UpdateTaskProgress(t.ID, 90, "")
	if !shouldApplyStoreVisitImageTaskResult(spot.ID, t.ID) {
		return gin.H{"skipped": true}, nil
	}
	if err := db.DB.Model(&models.StoreVisitSpot{}).Where("id = ?", spot.ID).Updates(map[string]interface{}{
		"generated_image":          webPath,
		"image_status":             "generated",
		"image_current_task_id":    "",
		"image_last_error":         "",
		"image_generated_workflow": workflowDisplayNameFromPath(storeVisitImageWorkflowPath),
		"updated_at":               time.Now(),
	}).Error; err != nil {
		return nil, err
	}
	BroadcastUpdate("store_visit_spot", spot.ID)
	return gin.H{"generated_image": webPath}, nil
}

func HandleRenderStoreVisitSpotVideoTask(t *models.Task) (interface{}, error) {
	var payload storeVisitMediaTaskPayload
	if err := json.Unmarshal([]byte(t.Payload), &payload); err != nil {
		return nil, fmt.Errorf("invalid payload: %w", err)
	}

	var project models.StoreVisitProject
	if err := db.DB.First(&project, payload.ProjectID).Error; err != nil {
		return nil, fmt.Errorf("project not found: %w", err)
	}
	var spot models.StoreVisitSpot
	if err := db.DB.First(&spot, payload.SpotID).Error; err != nil {
		return nil, fmt.Errorf("spot not found: %w", err)
	}
	spotLabel := getStoreVisitSpotDisplayName(spot)
	spotKey := getStoreVisitSpotFileKey(spot)

	workflowJSON, workflowLabel, err := buildStoreVisitVideoWorkflow(spot, project, payload.Seed)
	if err != nil {
		if shouldApplyStoreVisitVideoTaskResult(spot.ID, t.ID) {
			_ = db.DB.Model(&models.StoreVisitSpot{}).Where("id = ?", spot.ID).Updates(map[string]interface{}{
				"video_status":          "failed",
				"video_current_task_id": "",
				"video_last_error":      err.Error(),
				"updated_at":            time.Now(),
			}).Error
		}
		return nil, err
	}

	logComfyWorkflowPayload("Store Visit Video ComfyUI Payload", workflowLabel, workflowJSON)
	promptID, err := QueueComfyPrompt(workflowJSON)
	if err != nil {
		if shouldApplyStoreVisitVideoTaskResult(spot.ID, t.ID) {
			_ = db.DB.Model(&models.StoreVisitSpot{}).Where("id = ?", spot.ID).Updates(map[string]interface{}{
				"video_status":          "failed",
				"video_current_task_id": "",
				"video_last_error":      err.Error(),
				"updated_at":            time.Now(),
			}).Error
		}
		return nil, err
	}
	Log(LogLevelInfo, fmt.Sprintf("博主探店%s视频已提交到 ComfyUI 队列", spotLabel), fmt.Sprintf("ProjectID: %d\nSpotID: %d\nPromptID: %s\nWorkflow: %s", project.ID, spot.ID, promptID, strings.TrimSpace(workflowLabel)))

	task.GlobalTaskManager.UpdateTaskProgress(t.ID, 40, "")
	webPath, err := waitForStoreVisitVideoOutput(promptID, project.Code, spotKey, spot.ID, func() bool {
		return shouldApplyStoreVisitVideoTaskResult(spot.ID, t.ID)
	})
	if err != nil {
		if shouldApplyStoreVisitVideoTaskResult(spot.ID, t.ID) {
			_ = db.DB.Model(&models.StoreVisitSpot{}).Where("id = ?", spot.ID).Updates(map[string]interface{}{
				"video_status":          "failed",
				"video_current_task_id": "",
				"video_last_error":      err.Error(),
				"updated_at":            time.Now(),
			}).Error
		}
		return nil, err
	}
	if strings.TrimSpace(webPath) == "" {
		err = fmt.Errorf("未获取到%s视频输出", spotLabel)
		if shouldApplyStoreVisitVideoTaskResult(spot.ID, t.ID) {
			_ = db.DB.Model(&models.StoreVisitSpot{}).Where("id = ?", spot.ID).Updates(map[string]interface{}{
				"video_status":          "failed",
				"video_current_task_id": "",
				"video_last_error":      err.Error(),
				"updated_at":            time.Now(),
			}).Error
		}
		return nil, err
	}

	task.GlobalTaskManager.UpdateTaskProgress(t.ID, 90, "")
	if !shouldApplyStoreVisitVideoTaskResult(spot.ID, t.ID) {
		return gin.H{"skipped": true}, nil
	}
	if err := db.DB.Model(&models.StoreVisitSpot{}).Where("id = ?", spot.ID).Updates(map[string]interface{}{
		"generated_video":          webPath,
		"video_status":             "generated",
		"video_current_task_id":    "",
		"video_last_error":         "",
		"video_generated_workflow": workflowLabel,
		"updated_at":               time.Now(),
	}).Error; err != nil {
		return nil, err
	}
	BroadcastUpdate("store_visit_spot", spot.ID)
	return gin.H{"generated_video": webPath}, nil
}
