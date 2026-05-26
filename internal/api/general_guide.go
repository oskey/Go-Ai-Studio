package api

import (
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

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

const (
	generalGuideOutputRoot        = "output/general_guide"
	generalGuideImageWorkflowPath = "workflows/image_firered_image_edit1_1.json"
	generalGuideVideoFPS          = 25

	generalGuideSceneTypePresenter = "presenter_scene"
	generalGuideSceneTypeMaterial  = "material_scene"
	generalGuideSceneTypeClosing   = "closing_scene"

	generalGuideEnvironmentIndoor  = "indoor"
	generalGuideEnvironmentOutdoor = "outdoor"

	generalGuideImagePresetFrontHalfbody  = "presenter_front_halfbody"
	generalGuideImagePresetRoomForeground = "presenter_room_foreground"
	generalGuideImagePresetSeatedTable    = "presenter_seated_table"
	generalGuideImagePresetMaterialOnly   = "material_only"

	generalGuidePresenterGenderFemale = "female"
	generalGuidePresenterGenderMale   = "male"

	generalGuidePresenterPersonaFemaleNatural = "female_natural"
	generalGuidePresenterPersonaFemalePlayful = "female_playful"
	generalGuidePresenterPersonaFemaleSexy    = "female_sexy"
	generalGuidePresenterPersonaFemaleGentle  = "female_gentle"
	generalGuidePresenterPersonaMaleNatural   = "male_natural"
	generalGuidePresenterPersonaMaleSteady    = "male_steady"
	generalGuidePresenterPersonaMaleConfident = "male_confident"
	generalGuidePresenterPersonaMaleWarm      = "male_warm"
)

var generalGuideAllowedSceneTypes = map[string]struct{}{
	generalGuideSceneTypePresenter: {},
	generalGuideSceneTypeMaterial:  {},
	generalGuideSceneTypeClosing:   {},
}

var generalGuideAllowedEnvironmentTypes = map[string]struct{}{
	generalGuideEnvironmentIndoor:  {},
	generalGuideEnvironmentOutdoor: {},
}

var generalGuideAllowedImagePresets = map[string]struct{}{
	generalGuideImagePresetFrontHalfbody:  {},
	generalGuideImagePresetRoomForeground: {},
	generalGuideImagePresetSeatedTable:    {},
	generalGuideImagePresetMaterialOnly:   {},
}

var generalGuideAllowedPresenterPersonas = map[string]string{
	generalGuidePresenterPersonaFemaleNatural: generalGuidePresenterGenderFemale,
	generalGuidePresenterPersonaFemalePlayful: generalGuidePresenterGenderFemale,
	generalGuidePresenterPersonaFemaleSexy:    generalGuidePresenterGenderFemale,
	generalGuidePresenterPersonaFemaleGentle:  generalGuidePresenterGenderFemale,
	generalGuidePresenterPersonaMaleNatural:   generalGuidePresenterGenderMale,
	generalGuidePresenterPersonaMaleSteady:    generalGuidePresenterGenderMale,
	generalGuidePresenterPersonaMaleConfident: generalGuidePresenterGenderMale,
	generalGuidePresenterPersonaMaleWarm:      generalGuidePresenterGenderMale,
}

const generalGuideDefaultPresenterNegativePrompt = `人物过大，人物过小，全身远景，人物漂浮，边缘抠图感，比例错误，假人感，塑料皮肤，背景被遮挡，大面积遮挡，光影错误，卡通，二次元`

const generalGuideDefaultMaterialNegativePrompt = `新增人物，新增主持人，漂浮，比例错误，抠图边缘，假人感，塑料质感，卡通，二次元，字幕，文字，水印`

const generalGuideDefaultVideoNegativePrompt = `cartoon,still image,bad quality,subtitles,caption,text,text overlay,on-screen text,watermark,overlay,overlay effects`

const generalGuideOutdoorPositivePromptPrefix = `outdoor realistic scene, keep the original scene unchanged,

insert only the single referenced presenter from the reference image into the scene,
keep existing buildings, objects, signs, and scene content unchanged,
do not remove, replace, or alter anything already in the scene,

there must be exactly ONE visible person in the final image,
the inserted presenter is the only visible person anywhere in the image,
do not create any second person, extra human, background pedestrian, bystander, crowd, window person, doorway person, or reflected person,

use the identity and appearance logic from the reference image only for that single presenter,
do not create any other person besides the referenced presenter,`

var generalGuideOutdoorPositionVariants = []string{
	`the added person is placed near the left edge of frame, off-center composition,
not blocking the main subject, not the focus of the image,`,
	`the added person is placed near the right edge of frame, off-center composition,
not blocking the main subject, not the focus of the image,`,
	`the added person is placed toward the left edge of frame, off-center composition,
not blocking the main subject, not the focus of the image,`,
	`the added person is placed toward the right edge of frame, off-center composition,
not blocking the main subject, not the focus of the image,`,
}

var generalGuideOutdoorDirectionVariants = []string{
	`person facing front or slightly front-facing, looking toward camera,`,
	`person in a natural three-quarter view, body slightly turned toward the left while still oriented toward camera,`,
	`person in a natural three-quarter view, body slightly turned toward the right while still oriented toward camera,`,
}

const generalGuideOutdoorPositivePromptSuffix = `only upper body visible (waist-up framing),
cropped at waist, no legs, no full body,

natural relaxed standing posture,
no posing, no model-like behavior,

lighting direction, color temperature and shadow must match the environment,
consistent perspective and scale,

the inserted presenter blends naturally into the background,
no cutout edges, no compositing artifacts,

cinematic realistic street photography style,
high realism`

const generalGuideOutdoorNegativePrompt = `duplicate people, cloned character,
multiple people, second person, two people, couple, duo, extra pedestrians, background bystanders, crowd,
person in background, people in windows, people in entrances, reflected person,
full body, legs,
bad lighting mismatch,
cutout effect, sticker look,
overly sharp face, beauty filter`

const generalGuideOutdoorClosingPositivePrompt = `outdoor realistic scene, keep the original scene unchanged,

insert only the single referenced presenter from the reference image into the scene,
keep existing buildings, objects, signs, and scene content unchanged,
do not remove, replace, or alter anything already in the scene,

there must be exactly ONE visible person in the final image,
the inserted presenter is the only visible person anywhere in the image,
do not create any second person, extra human, background pedestrian, bystander, crowd, window person, doorway person, or reflected person,

the inserted presenter is positioned off-center, clearly in the foreground,
does not block important objects or the main exterior subject,

only upper body visible (waist-up framing),
cropped at waist, no legs, no full body,

person facing front or slightly front-facing, looking toward camera,
natural relaxed standing posture,
not posing,

lighting matches outdoor daylight and surrounding exterior light sources,
consistent shadows, color temperature, perspective, and natural scale,

the inserted presenter is part of the environment, not oversized,
clean blending, no cutout artifacts,
high realism`

const generalGuideOutdoorClosingNegativePrompt = `duplicate people, cloned character,
multiple people, second person, extra pedestrians, background bystanders, crowd,
person in side view, back view,
person centered as oversized main subject,
full body, legs,
bad lighting mismatch,
cutout effect, sticker look,
overly sharp face, beauty filter`

var generalGuideOutdoorOpeningPositionVariants = []string{
	`the added person is positioned toward the left side of frame, clearly in the foreground,
does not block important objects or the main exterior subject,`,
	`the added person is positioned toward the right side of frame, clearly in the foreground,
does not block important objects or the main exterior subject,`,
}

var generalGuideOutdoorOpeningDirectionVariants = []string{
	`person in a natural three-quarter view, body slightly turned toward the left while still oriented toward camera,`,
	`person in a natural three-quarter view, body slightly turned toward the right while still oriented toward camera,`,
}

const generalGuideIndoorPositivePrompt = `indoor realistic environment, keep the original scene unchanged,

add ONE additional person into the scene,
keep existing people, objects, furniture, and scene content unchanged,
do not remove, replace, or alter anything already in the scene,

the added person is positioned off-center, not in the middle,
does not block important objects or main subjects,

only upper body visible (waist-up framing),
cropped at waist, no legs, no full body,

person facing front or slightly front-facing,
natural relaxed posture, lightly interacting with environment,
not posing,

lighting matches indoor sources (warm light, ceiling light),
consistent shadows and skin tones,

correct perspective with furniture and space,
natural scale,

the added person is part of the environment, not the main subject,
clean blending, no cutout artifacts,
high realism`

const generalGuideIndoorNegativePrompt = `duplicate people, cloned character,
person in center, main subject,
full body, legs,
bad lighting mismatch,
cutout effect, sticker look,
overly sharp face, beauty filter`

func generalGuideProjectDir(code string) string {
	return filepath.Join(generalGuideOutputRoot, code)
}

func generalGuideReferenceDir(code string) string {
	return filepath.Join(generalGuideProjectDir(code), "reference")
}

func generalGuideSceneDir(code string) string {
	return filepath.Join(generalGuideProjectDir(code), "scene_reference")
}

func generalGuideImagesDir(code string) string {
	return filepath.Join(generalGuideProjectDir(code), "images")
}

func generalGuideVideosDir(code string) string {
	return filepath.Join(generalGuideProjectDir(code), "videos")
}

func generalGuideReferencePath(code string, id uint, ext string) string {
	return filepath.Join(generalGuideReferenceDir(code), fmt.Sprintf("presenter_%d%s", id, ext))
}

func generalGuideSceneReferencePath(code string, sceneID uint, ext string) string {
	return filepath.Join(generalGuideSceneDir(code), fmt.Sprintf("scene_%d_%d%s", sceneID, time.Now().UnixNano(), ext))
}

func normalizeGeneralGuideSceneType(raw string) string {
	value := strings.TrimSpace(strings.ToLower(raw))
	if _, ok := generalGuideAllowedSceneTypes[value]; ok {
		return value
	}
	return generalGuideSceneTypePresenter
}

func normalizeGeneralGuidePresenterGender(raw string) string {
	value := strings.TrimSpace(strings.ToLower(raw))
	switch value {
	case generalGuidePresenterGenderMale:
		return generalGuidePresenterGenderMale
	case generalGuidePresenterGenderFemale:
		return generalGuidePresenterGenderFemale
	default:
		return generalGuidePresenterGenderFemale
	}
}

func defaultGeneralGuidePresenterPersona(gender string) string {
	if normalizeGeneralGuidePresenterGender(gender) == generalGuidePresenterGenderMale {
		return generalGuidePresenterPersonaMaleNatural
	}
	return generalGuidePresenterPersonaFemaleNatural
}

func normalizeGeneralGuidePresenterPersona(raw string, gender string) string {
	value := strings.TrimSpace(strings.ToLower(raw))
	expectedGender := normalizeGeneralGuidePresenterGender(gender)
	if actualGender, ok := generalGuideAllowedPresenterPersonas[value]; ok && actualGender == expectedGender {
		return value
	}
	return defaultGeneralGuidePresenterPersona(expectedGender)
}

func generalGuidePresenterPersonaLabel(persona string) string {
	switch strings.TrimSpace(strings.ToLower(persona)) {
	case generalGuidePresenterPersonaFemalePlayful:
		return "俏皮女性"
	case generalGuidePresenterPersonaFemaleSexy:
		return "性感女性"
	case generalGuidePresenterPersonaFemaleGentle:
		return "温柔女性"
	case generalGuidePresenterPersonaMaleSteady:
		return "稳重男性"
	case generalGuidePresenterPersonaMaleConfident:
		return "自信男性"
	case generalGuidePresenterPersonaMaleWarm:
		return "温和男性"
	case generalGuidePresenterPersonaMaleNatural:
		return "自然男性"
	default:
		return "自然女性"
	}
}

func generalGuidePresenterPersonaHint(persona string, gender string) string {
	switch normalizeGeneralGuidePresenterPersona(persona, gender) {
	case generalGuidePresenterPersonaFemalePlayful:
		return "整体更灵动、俏皮、机灵一点，说话节奏可以更轻快，表情更鲜活，动作更有小巧灵气，但不要幼态夸张。"
	case generalGuidePresenterPersonaFemaleSexy:
		return "整体更成熟、自信、有魅力，语气更有拿捏感，动作和表情可以更从容、更有吸引力，但不要低俗或过度卖弄。"
	case generalGuidePresenterPersonaFemaleGentle:
		return "整体更温柔、亲和、细腻，语气更柔和，表情更有安抚感和沟通感，动作自然舒展。"
	case generalGuidePresenterPersonaMaleSteady:
		return "整体更稳重、可靠、成熟，语气沉着，动作克制有分寸，给人可信和踏实的感觉。"
	case generalGuidePresenterPersonaMaleConfident:
		return "整体更自信、利落、干脆，语气更果断，动作更明确，表现出很强的掌控感和说服力。"
	case generalGuidePresenterPersonaMaleWarm:
		return "整体更温和、耐心、有亲近感，语气不咄咄逼人，表情自然友好，动作带一点照顾感。"
	case generalGuidePresenterPersonaMaleNatural:
		return "整体更自然、真实、像生活里会认真介绍的人，语气放松但清楚，动作不过分设计。"
	default:
		return "整体更自然、真实、像生活里会认真介绍的人，语气放松但清楚，动作不过分设计。"
	}
}

func normalizeGeneralGuideImagePreset(raw string) string {
	value := strings.TrimSpace(strings.ToLower(raw))
	if _, ok := generalGuideAllowedImagePresets[value]; ok {
		return value
	}
	return generalGuideImagePresetRoomForeground
}

func deriveGeneralGuideImagePreset(sceneType string, environmentType string, needPresenter bool) string {
	if !needPresenter || normalizeGeneralGuideSceneType(sceneType) == generalGuideSceneTypeMaterial {
		return generalGuideImagePresetMaterialOnly
	}
	if normalizeGeneralGuideEnvironmentType(environmentType) == generalGuideEnvironmentOutdoor {
		return generalGuideImagePresetFrontHalfbody
	}
	return generalGuideImagePresetRoomForeground
}

func normalizeGeneralGuideEnvironmentType(raw string) string {
	value := strings.TrimSpace(strings.ToLower(raw))
	if _, ok := generalGuideAllowedEnvironmentTypes[value]; ok {
		return value
	}
	return generalGuideEnvironmentIndoor
}

func generalGuideDefaultNeedPresenter(preset string, sceneType string) bool {
	if normalizeGeneralGuideImagePreset(preset) == generalGuideImagePresetMaterialOnly {
		return false
	}
	return normalizeGeneralGuideSceneType(sceneType) != generalGuideSceneTypeMaterial
}

func generalGuideDefaultImagePrompt(preset string, environmentType string, sceneType string, sortOrder int) string {
	switch normalizeGeneralGuideImagePreset(preset) {
	case generalGuideImagePresetSeatedTable:
		return `将参考人物放入当前场景中，仅保留人物面部身份，不参考原图姿态和背景。

人物坐在椅子上，半身或三分之二身构图，正对镜头。

人物位于前景主位，桌面或展示台自然位于人物前方，空间关系合理。

光线与环境一致，有自然阴影和真实融合感。

画面保持真实摄影风格，细节清晰自然。`
	case generalGuideImagePresetMaterialOnly:
		return `保持当前场景主体不变，不加入人物。

主体清晰完整，空间和材质关系自然成立。

画面保持真实摄影风格，细节清晰自然。`
	default:
		if normalizeGeneralGuideEnvironmentType(environmentType) == generalGuideEnvironmentOutdoor {
			if sortOrder == 1 && normalizeGeneralGuideSceneType(sceneType) == generalGuideSceneTypePresenter {
				return buildGeneralGuideOutdoorOpeningPositivePrompt(0)
			}
			if normalizeGeneralGuideSceneType(sceneType) == generalGuideSceneTypeClosing {
				return generalGuideOutdoorClosingPositivePrompt
			}
			return buildGeneralGuideOutdoorPositivePrompt(0)
		}
		return generalGuideIndoorPositivePrompt
	}
}

func buildGeneralGuideOutdoorOpeningPositivePrompt(seed int64) string {
	return generalGuideOutdoorClosingPositivePrompt
}

func buildGeneralGuideOutdoorPositivePrompt(seed int64) string {
	return generalGuideOutdoorClosingPositivePrompt
}

func generalGuideDefaultImageNegativePrompt(preset string, environmentType string, sceneType string, sortOrder int) string {
	if normalizeGeneralGuideImagePreset(preset) == generalGuideImagePresetMaterialOnly {
		return generalGuideDefaultMaterialNegativePrompt
	}
	if normalizeGeneralGuideImagePreset(preset) == generalGuideImagePresetSeatedTable {
		return generalGuideDefaultPresenterNegativePrompt
	}
	if normalizeGeneralGuideEnvironmentType(environmentType) == generalGuideEnvironmentOutdoor {
		if sortOrder == 1 && normalizeGeneralGuideSceneType(sceneType) == generalGuideSceneTypePresenter {
			return generalGuideOutdoorClosingNegativePrompt
		}
		if normalizeGeneralGuideSceneType(sceneType) == generalGuideSceneTypeClosing {
			return generalGuideOutdoorClosingNegativePrompt
		}
		return generalGuideOutdoorNegativePrompt
	}
	return generalGuideIndoorNegativePrompt
}

func generalGuideDefaultVideoSize(sceneType string, needPresenter bool) (int, int) {
	if !needPresenter || normalizeGeneralGuideSceneType(sceneType) == generalGuideSceneTypeMaterial {
		return 1280, 720
	}
	return 720, 1280
}

func applyGeneralGuideProjectTagIDs(project *models.GeneralGuideProject) {
	if project == nil {
		return
	}
	project.TagIDs = parseAutoGenerateTagIDs(project.TagIDsJSON)
	project.PresenterGender = normalizeGeneralGuidePresenterGender(project.PresenterGender)
	project.PresenterPersona = normalizeGeneralGuidePresenterPersona(project.PresenterPersona, project.PresenterGender)
}

func buildGeneralGuideProjectView(project models.GeneralGuideProject) models.GeneralGuideProject {
	applyGeneralGuideProjectTagIDs(&project)
	return project
}

func loadGeneralGuideProjectOr404(c *gin.Context) (*models.GeneralGuideProject, error) {
	projectID := strings.TrimSpace(c.Param("id"))
	var project models.GeneralGuideProject
	if err := db.DB.First(&project, projectID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "综合讲解项目不存在"})
		return nil, err
	}
	applyGeneralGuideProjectTagIDs(&project)
	return &project, nil
}

func loadGeneralGuideReferenceOr404(c *gin.Context) (*models.GeneralGuideReference, error) {
	referenceID := strings.TrimSpace(c.Param("referenceId"))
	var ref models.GeneralGuideReference
	if err := db.DB.First(&ref, referenceID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "讲解人参考图不存在"})
		return nil, err
	}
	return &ref, nil
}

func loadGeneralGuideSceneOr404(c *gin.Context) (*models.GeneralGuideScene, error) {
	sceneID := strings.TrimSpace(c.Param("sceneId"))
	var scene models.GeneralGuideScene
	if err := db.DB.First(&scene, sceneID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "讲解场景不存在"})
		return nil, err
	}
	return &scene, nil
}

func removeGeneralGuideAsset(path string) error {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return nil
	}
	local := strings.TrimPrefix(trimmed, "/")
	if local == "" || local == "." || local == ".." {
		return nil
	}
	if err := os.Remove(local); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func ListGeneralGuideProjects(c *gin.Context) {
	var projects []models.GeneralGuideProject
	if err := db.DB.Order("created_at desc").Find(&projects).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "读取综合讲解项目失败"})
		return
	}
	for i := range projects {
		applyGeneralGuideProjectTagIDs(&projects[i])
	}
	c.JSON(http.StatusOK, projects)
}

func GetGeneralGuideProject(c *gin.Context) {
	project, err := loadGeneralGuideProjectOr404(c)
	if err != nil {
		return
	}
	c.JSON(http.StatusOK, buildGeneralGuideProjectView(*project))
}

func ListGeneralGuideReferences(c *gin.Context) {
	project, err := loadGeneralGuideProjectOr404(c)
	if err != nil {
		return
	}
	var refs []models.GeneralGuideReference
	if err := db.DB.Where("project_id = ?", project.ID).Order("sort_order asc, id asc").Find(&refs).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "读取讲解人参考图失败"})
		return
	}
	c.JSON(http.StatusOK, refs)
}

func SelectGeneralGuideReference(c *gin.Context) {
	project, err := loadGeneralGuideProjectOr404(c)
	if err != nil {
		return
	}
	ref, err := loadGeneralGuideReferenceOr404(c)
	if err != nil {
		return
	}
	if ref.ProjectID != project.ID {
		c.JSON(http.StatusBadRequest, gin.H{"error": "参考图不属于当前项目"})
		return
	}
	now := time.Now()
	if err := db.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&models.GeneralGuideReference{}).Where("project_id = ?", project.ID).Updates(map[string]interface{}{
			"is_selected": false,
			"updated_at":  now,
		}).Error; err != nil {
			return err
		}
		if err := tx.Model(&models.GeneralGuideReference{}).Where("id = ?", ref.ID).Updates(map[string]interface{}{
			"is_selected": true,
			"updated_at":  now,
		}).Error; err != nil {
			return err
		}
		return tx.Model(&models.GeneralGuideProject{}).Where("id = ?", project.ID).Updates(map[string]interface{}{
			"presenter_reference_image": ref.ImagePath,
			"selected_reference_id":     ref.ID,
			"updated_at":                now,
		}).Error
	}); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "切换讲解人参考图失败"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "讲解人参考图已切换"})
}

func ListGeneralGuideScenes(c *gin.Context) {
	project, err := loadGeneralGuideProjectOr404(c)
	if err != nil {
		return
	}
	var scenes []models.GeneralGuideScene
	if err := db.DB.Where("project_id = ?", project.ID).Order("sort_order asc, id asc").Find(&scenes).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "读取讲解场景失败"})
		return
	}
	c.JSON(http.StatusOK, scenes)
}

func CreateGeneralGuideProject(c *gin.Context) {
	name := strings.TrimSpace(c.PostForm("name"))
	code := strings.TrimSpace(c.PostForm("code"))
	description := strings.TrimSpace(c.PostForm("description"))
	presenterGender := normalizeGeneralGuidePresenterGender(c.PostForm("presenter_gender"))
	presenterPersona := normalizeGeneralGuidePresenterPersona(c.PostForm("presenter_persona"), presenterGender)
	autoGenerateContent := strings.TrimSpace(c.PostForm("auto_generate_content"))
	tagIDs := parseAutoGenerateTagIDs(c.PostForm("tag_ids_json"))
	if name == "" || code == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请填写项目名称和项目文件名"})
		return
	}
	matched, _ := regexp.MatchString(`^[a-zA-Z0-9_-]+$`, code)
	if !matched {
		c.JSON(http.StatusBadRequest, gin.H{"error": "项目文件名只允许英文、数字、下划线或连字符"})
		return
	}
	var count int64
	db.DB.Model(&models.GeneralGuideProject{}).Where("code = ?", code).Count(&count)
	if count > 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "项目文件名已被占用"})
		return
	}
	if _, err := os.Stat(generalGuideProjectDir(code)); err == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "项目文件名已被占用"})
		return
	}

	var files []*multipart.FileHeader
	if form, err := c.MultipartForm(); err == nil && form != nil {
		files = append(files, form.File["presenter_reference_images"]...)
	}
	if len(files) == 0 {
		if file, err := c.FormFile("presenter_reference_image"); err == nil && file != nil {
			files = append(files, file)
		}
	}
	if len(files) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请至少上传一张讲解人参考图"})
		return
	}
	if err := os.MkdirAll(generalGuideReferenceDir(code), 0755); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "创建项目目录失败"})
		return
	}

	now := time.Now()
	project := models.GeneralGuideProject{
		Name:                name,
		Code:                code,
		Description:         description,
		PresenterGender:     presenterGender,
		PresenterPersona:    presenterPersona,
		AutoGenerateContent: autoGenerateContent,
		TagIDsJSON:          encodeAutoGenerateTagIDs(tagIDs),
		CreatedAt:           now,
		UpdatedAt:           now,
	}
	savedPaths := make([]string, 0, len(files))
	if err := db.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&project).Error; err != nil {
			return err
		}
		for idx, file := range files {
			ref := models.GeneralGuideReference{
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
			absPath := generalGuideReferencePath(code, ref.ID, ext)
			if err := c.SaveUploadedFile(file, absPath); err != nil {
				return err
			}
			savedPaths = append(savedPaths, absPath)
			webPath := "/" + filepath.ToSlash(absPath)
			if err := tx.Model(&models.GeneralGuideReference{}).Where("id = ?", ref.ID).Updates(map[string]interface{}{
				"image_path": webPath,
				"updated_at": now,
			}).Error; err != nil {
				return err
			}
			if idx == 0 {
				project.PresenterReferenceImage = webPath
				project.SelectedReferenceID = ref.ID
			}
		}
		return tx.Model(&models.GeneralGuideProject{}).Where("id = ?", project.ID).Updates(map[string]interface{}{
			"presenter_reference_image": project.PresenterReferenceImage,
			"selected_reference_id":     project.SelectedReferenceID,
			"updated_at":                now,
		}).Error
	}); err != nil {
		for _, path := range savedPaths {
			_ = os.Remove(path)
		}
		_ = os.RemoveAll(generalGuideProjectDir(code))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "创建综合讲解项目失败"})
		return
	}

	Log(LogLevelInfo, "创建综合讲解项目", fmt.Sprintf("创建了综合讲解项目: %s (%s)", project.Name, project.Code))
	c.JSON(http.StatusCreated, buildGeneralGuideProjectView(project))
}

func UpdateGeneralGuideProject(c *gin.Context) {
	project, err := loadGeneralGuideProjectOr404(c)
	if err != nil {
		return
	}

	name := strings.TrimSpace(c.PostForm("name"))
	code := strings.TrimSpace(c.PostForm("code"))
	description := strings.TrimSpace(c.PostForm("description"))
	presenterGender := normalizeGeneralGuidePresenterGender(c.PostForm("presenter_gender"))
	presenterPersona := normalizeGeneralGuidePresenterPersona(c.PostForm("presenter_persona"), presenterGender)
	autoGenerateContent, hasAutoGenerateContent := c.GetPostForm("auto_generate_content")
	tagIDs := parseAutoGenerateTagIDs(c.PostForm("tag_ids_json"))
	if name == "" || code == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请填写项目名称和项目文件名"})
		return
	}
	matched, _ := regexp.MatchString(`^[a-zA-Z0-9_-]+$`, code)
	if !matched {
		c.JSON(http.StatusBadRequest, gin.H{"error": "项目文件名只允许英文、数字、下划线或连字符"})
		return
	}

	oldCode := project.Code
	oldProjectDir := generalGuideProjectDir(oldCode)
	newProjectDir := generalGuideProjectDir(code)
	if code != oldCode {
		var count int64
		db.DB.Model(&models.GeneralGuideProject{}).Where("code = ? AND id <> ?", code, project.ID).Count(&count)
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
		files = append(files, form.File["presenter_reference_images"]...)
	}
	if len(files) == 0 {
		if file, err := c.FormFile("presenter_reference_image"); err == nil && file != nil {
			files = append(files, file)
		}
	}

	if err := os.MkdirAll(generalGuideReferenceDir(code), 0755); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "创建项目目录失败"})
		return
	}

	now := time.Now()
	type refUploadInfo struct {
		ID      uint
		AbsPath string
		WebPath string
	}
	uploadedRefs := make([]refUploadInfo, 0, len(files))
	if err := db.DB.Transaction(func(tx *gorm.DB) error {
		if code != oldCode {
			var refs []models.GeneralGuideReference
			if err := tx.Where("project_id = ?", project.ID).Order("sort_order asc, id asc").Find(&refs).Error; err != nil {
				return err
			}
			var scenes []models.GeneralGuideScene
			if err := tx.Where("project_id = ?", project.ID).Find(&scenes).Error; err != nil {
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
				if err := tx.Model(&models.GeneralGuideReference{}).Where("id = ?", ref.ID).Updates(map[string]interface{}{
					"image_path": nextPath,
					"updated_at": now,
				}).Error; err != nil {
					return err
				}
			}
			for _, scene := range scenes {
				if err := tx.Model(&models.GeneralGuideScene{}).Where("id = ?", scene.ID).Updates(map[string]interface{}{
					"reference_image": strings.TrimSpace(replacePrefix(scene.ReferenceImage)),
					"generated_image": strings.TrimSpace(replacePrefix(scene.GeneratedImage)),
					"generated_video": strings.TrimSpace(replacePrefix(scene.GeneratedVideo)),
					"updated_at":      now,
				}).Error; err != nil {
					return err
				}
			}
			project.PresenterReferenceImage = replacePrefix(project.PresenterReferenceImage)
		}

		var refs []models.GeneralGuideReference
		if err := tx.Where("project_id = ?", project.ID).Order("sort_order asc, id asc").Find(&refs).Error; err != nil {
			return err
		}
		nextSort := len(refs) + 1
		for idx, file := range files {
			ref := models.GeneralGuideReference{
				ProjectID:  project.ID,
				SortOrder:  nextSort + idx,
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
			absPath := generalGuideReferencePath(code, ref.ID, ext)
			if err := c.SaveUploadedFile(file, absPath); err != nil {
				return err
			}
			webPath := "/" + filepath.ToSlash(absPath)
			uploadedRefs = append(uploadedRefs, refUploadInfo{ID: ref.ID, AbsPath: absPath, WebPath: webPath})
			if err := tx.Model(&models.GeneralGuideReference{}).Where("id = ?", ref.ID).Updates(map[string]interface{}{
				"image_path": webPath,
				"updated_at": now,
			}).Error; err != nil {
				return err
			}
		}

		updates := map[string]interface{}{
			"name":              name,
			"code":              code,
			"description":       description,
			"presenter_gender":  presenterGender,
			"presenter_persona": presenterPersona,
			"tag_ids_json":      encodeAutoGenerateTagIDs(tagIDs),
			"updated_at":        now,
		}
		if hasAutoGenerateContent {
			updates["auto_generate_content"] = strings.TrimSpace(autoGenerateContent)
		}
		if strings.TrimSpace(project.PresenterReferenceImage) == "" && len(uploadedRefs) > 0 {
			updates["presenter_reference_image"] = uploadedRefs[0].WebPath
			updates["selected_reference_id"] = uploadedRefs[0].ID
			if err := tx.Model(&models.GeneralGuideReference{}).Where("id = ?", uploadedRefs[0].ID).Updates(map[string]interface{}{
				"is_selected": true,
				"updated_at":  now,
			}).Error; err != nil {
				return err
			}
		} else {
			updates["presenter_reference_image"] = project.PresenterReferenceImage
			updates["selected_reference_id"] = project.SelectedReferenceID
		}
		return tx.Model(&models.GeneralGuideProject{}).Where("id = ?", project.ID).Updates(updates).Error
	}); err != nil {
		for _, item := range uploadedRefs {
			_ = os.Remove(item.AbsPath)
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "更新综合讲解项目失败"})
		return
	}

	project.Name = name
	project.Code = code
	project.Description = description
	project.PresenterGender = presenterGender
	project.TagIDsJSON = encodeAutoGenerateTagIDs(tagIDs)
	if hasAutoGenerateContent {
		project.AutoGenerateContent = strings.TrimSpace(autoGenerateContent)
	}
	if strings.TrimSpace(project.PresenterReferenceImage) == "" && len(uploadedRefs) > 0 {
		project.PresenterReferenceImage = uploadedRefs[0].WebPath
		project.SelectedReferenceID = uploadedRefs[0].ID
	}
	project.UpdatedAt = now
	applyGeneralGuideProjectTagIDs(project)

	Log(LogLevelInfo, "更新综合讲解项目", fmt.Sprintf("更新了综合讲解项目: %s (%s)", project.Name, project.Code))
	c.JSON(http.StatusOK, buildGeneralGuideProjectView(*project))
}

func DeleteGeneralGuideProject(c *gin.Context) {
	project, err := loadGeneralGuideProjectOr404(c)
	if err != nil {
		return
	}
	projectDir := generalGuideProjectDir(project.Code)
	if err := db.DB.Transaction(func(tx *gorm.DB) error {
		var refs []models.GeneralGuideReference
		if err := tx.Where("project_id = ?", project.ID).Find(&refs).Error; err != nil {
			return err
		}
		var scenes []models.GeneralGuideScene
		if err := tx.Where("project_id = ?", project.ID).Find(&scenes).Error; err != nil {
			return err
		}
		var transitions []models.GeneralGuideTransition
		if err := tx.Where("project_id = ?", project.ID).Find(&transitions).Error; err != nil {
			return err
		}
		for _, ref := range refs {
			if err := removeGeneralGuideAsset(ref.ImagePath); err != nil {
				return err
			}
		}
		for _, scene := range scenes {
			if err := removeGeneralGuideAsset(scene.ReferenceImage); err != nil {
				return err
			}
			if err := removeGeneralGuideAsset(scene.GeneratedImage); err != nil {
				return err
			}
			if err := removeGeneralGuideAsset(scene.GeneratedVideo); err != nil {
				return err
			}
		}
		for _, transition := range transitions {
			if err := removeGeneralGuideAsset(transition.TailFrameImage); err != nil {
				return err
			}
			if err := removeGeneralGuideAsset(transition.GeneratedVideo); err != nil {
				return err
			}
		}
		if err := tx.Where("project_id = ?", project.ID).Delete(&models.GeneralGuideReference{}).Error; err != nil {
			return err
		}
		if err := tx.Where("project_id = ?", project.ID).Delete(&models.GeneralGuideScene{}).Error; err != nil {
			return err
		}
		if err := tx.Where("project_id = ?", project.ID).Delete(&models.GeneralGuideTransition{}).Error; err != nil {
			return err
		}
		return tx.Delete(project).Error
	}); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "删除综合讲解项目失败"})
		return
	}
	if project.Code != "" && project.Code != "." && project.Code != ".." {
		_ = os.RemoveAll(projectDir)
	}
	c.JSON(http.StatusOK, gin.H{"message": "综合讲解项目已删除"})
}

func UpdateGeneralGuideScene(c *gin.Context) {
	scene, err := loadGeneralGuideSceneOr404(c)
	if err != nil {
		return
	}
	if scene.ImageStatus == "generating" || scene.VideoStatus == "generating" {
		c.JSON(http.StatusConflict, gin.H{"error": "当前场景仍在生成中，暂时不能修改"})
		return
	}
	var project models.GeneralGuideProject
	if err := db.DB.First(&project, scene.ProjectID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "所属项目不存在"})
		return
	}
	title, hasTitle := c.GetPostForm("title")
	uploadHeadline, hasUploadHeadline := c.GetPostForm("upload_headline")
	uploadRequirement, hasUploadRequirement := c.GetPostForm("upload_requirement")
	introText, hasIntroText := c.GetPostForm("intro_text")
	videoPrompt, hasVideoPrompt := c.GetPostForm("video_positive_prompt")
	imagePreset := normalizeGeneralGuideImagePreset(scene.ImagePreset)
	if rawImagePreset, ok := c.GetPostForm("image_preset"); ok {
		imagePreset = normalizeGeneralGuideImagePreset(rawImagePreset)
	}
	sceneType := normalizeGeneralGuideSceneType(scene.SceneType)
	if rawSceneType, ok := c.GetPostForm("scene_type"); ok {
		sceneType = normalizeGeneralGuideSceneType(rawSceneType)
	}
	environmentType := normalizeGeneralGuideEnvironmentType(scene.EnvironmentType)
	if rawEnvironmentType, ok := c.GetPostForm("environment_type"); ok {
		environmentType = normalizeGeneralGuideEnvironmentType(rawEnvironmentType)
	}
	needPresenter := generalGuideDefaultNeedPresenter(imagePreset, sceneType)
	if rawNeedPresenter, ok := c.GetPostForm("need_presenter"); ok {
		needPresenter = strings.TrimSpace(rawNeedPresenter) == "true" || strings.TrimSpace(rawNeedPresenter) == "1"
		if imagePreset == generalGuideImagePresetMaterialOnly {
			needPresenter = false
		}
	}

	duration := scene.VideoDurationSeconds
	if rawDuration, ok := c.GetPostForm("video_duration_seconds"); ok && strings.TrimSpace(rawDuration) != "" {
		var parsed int
		fmt.Sscanf(strings.TrimSpace(rawDuration), "%d", &parsed)
		if parsed > 0 {
			duration = parsed
		}
	}
	videoWidth, videoHeight := generalGuideDefaultVideoSize(sceneType, needPresenter)
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
	clearReference := strings.TrimSpace(c.PostForm("clear_reference_image")) == "1"
	file, fileErr := c.FormFile("reference_image")
	hasNewReference := fileErr == nil
	if clearReference && hasNewReference {
		c.JSON(http.StatusBadRequest, gin.H{"error": "不能同时上传和清空参考图"})
		return
	}

	now := time.Now()
	updates := map[string]interface{}{
		"scene_type":             sceneType,
		"environment_type":       environmentType,
		"need_presenter":         needPresenter,
		"image_preset":           imagePreset,
		"image_positive_prompt":  generalGuideDefaultImagePrompt(imagePreset, environmentType, sceneType, scene.SortOrder),
		"image_negative_prompt":  generalGuideDefaultImageNegativePrompt(imagePreset, environmentType, sceneType, scene.SortOrder),
		"video_negative_prompt":  generalGuideDefaultVideoNegativePrompt,
		"video_duration_seconds": duration,
		"video_width":            videoWidth,
		"video_height":           videoHeight,
		"updated_at":             now,
	}
	if hasTitle {
		updates["title"] = strings.TrimSpace(title)
	}
	if hasUploadRequirement {
		updates["upload_requirement"] = strings.TrimSpace(uploadRequirement)
	}
	if hasUploadHeadline {
		updates["upload_headline"] = strings.TrimSpace(uploadHeadline)
	}
	if hasIntroText {
		updates["intro_text"] = strings.TrimSpace(introText)
	}
	if hasVideoPrompt {
		updates["video_positive_prompt"] = strings.TrimSpace(videoPrompt)
	}
	if clearReference {
		oldRef := strings.TrimSpace(scene.ReferenceImage)
		updates["reference_image"] = ""
		updates["image_status"] = "draft"
		updates["generated_image"] = ""
		updates["image_generated_workflow"] = ""
		updates["video_status"] = "draft"
		updates["generated_video"] = ""
		updates["video_generated_workflow"] = ""
		if err := removeGeneralGuideAsset(oldRef); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "清空场景参考图失败"})
			return
		}
	}
	if hasNewReference {
		ext := strings.ToLower(filepath.Ext(file.Filename))
		if ext == "" {
			ext = ".png"
		}
		if err := os.MkdirAll(generalGuideSceneDir(project.Code), 0755); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "创建场景图片目录失败"})
			return
		}
		absPath := generalGuideSceneReferencePath(project.Code, scene.ID, ext)
		if err := c.SaveUploadedFile(file, absPath); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "保存场景参考图失败"})
			return
		}
		if err := removeGeneralGuideAsset(scene.ReferenceImage); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "删除旧场景参考图失败"})
			return
		}
		updates["reference_image"] = "/" + filepath.ToSlash(absPath)
		updates["image_status"] = "draft"
		updates["generated_image"] = ""
		updates["image_generated_workflow"] = ""
		updates["video_status"] = "draft"
		updates["generated_video"] = ""
		updates["video_generated_workflow"] = ""
	}

	if err := db.DB.Model(&models.GeneralGuideScene{}).Where("id = ?", scene.ID).Updates(updates).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "保存场景内容失败"})
		return
	}
	var refreshed models.GeneralGuideScene
	if err := db.DB.First(&refreshed, scene.ID).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "读取更新后的场景失败"})
		return
	}
	c.JSON(http.StatusOK, refreshed)
}
