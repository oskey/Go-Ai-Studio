package api

import (
	"fmt"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"kt-ai-studio/internal/db"
	"kt-ai-studio/internal/models"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

const (
	audioCloneOutputRoot   = "output/audio_clone"
	audioCloneWorkflowPath = "workflows/LongCat-AudioDIT-TTS.json"

	audioCloneLineStatusDraft      = "draft"
	audioCloneLineStatusGenerating = "generating"
	audioCloneLineStatusGenerated  = "generated"
	audioCloneLineStatusFailed     = "failed"
)

type audioCloneParsedLine struct {
	SortOrder     int    `json:"sort_order"`
	CharacterName string `json:"character_name"`
	Text          string `json:"text"`
}

func audioCloneProjectDir(code string) string {
	return filepath.Join(audioCloneOutputRoot, code)
}

func audioCloneCharacterDir(code string) string {
	return filepath.Join(audioCloneProjectDir(code), "characters")
}

func audioCloneGeneratedDir(code string) string {
	return filepath.Join(audioCloneProjectDir(code), "generated")
}

func audioCloneCharacterAudioPath(code string, id uint, ext string) string {
	return filepath.Join(audioCloneCharacterDir(code), fmt.Sprintf("character_%d%s", id, ext))
}

func normalizeAudioCloneCode(code string) string {
	return strings.TrimSpace(code)
}

func validateAudioCloneProjectCode(code string) bool {
	matched, _ := regexp.MatchString(`^[a-zA-Z0-9_-]+$`, code)
	return matched
}

func loadAudioCloneProjectOr404(c *gin.Context) (*models.AudioCloneProject, error) {
	projectID := strings.TrimSpace(c.Param("id"))
	var project models.AudioCloneProject
	if err := db.DB.First(&project, projectID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "音频复制项目不存在"})
		return nil, err
	}
	return &project, nil
}

func loadAudioCloneCharacterOr404(c *gin.Context) (*models.AudioCloneCharacter, error) {
	characterID := strings.TrimSpace(c.Param("characterId"))
	var character models.AudioCloneCharacter
	if err := db.DB.First(&character, characterID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "角色声音资产不存在"})
		return nil, err
	}
	return &character, nil
}

func loadAudioCloneLineOr404(c *gin.Context) (*models.AudioCloneLine, error) {
	lineID := strings.TrimSpace(c.Param("lineId"))
	var line models.AudioCloneLine
	if err := db.DB.First(&line, lineID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "音频复制台词行不存在"})
		return nil, err
	}
	return &line, nil
}

func removeAudioCloneAsset(path string) error {
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

func parseAudioCloneScript(script string) ([]audioCloneParsedLine, error) {
	lines := strings.Split(script, "\n")
	re := regexp.MustCompile(`^\s*\{([^{}]+)\}\s*(.+?)\s*$`)
	parsed := make([]audioCloneParsedLine, 0, len(lines))
	for _, rawLine := range lines {
		line := strings.TrimSpace(rawLine)
		if line == "" {
			continue
		}
		match := re.FindStringSubmatch(line)
		if len(match) != 3 {
			return nil, fmt.Errorf("脚本格式错误：%s。请使用 {角色名}台词", line)
		}
		name := strings.TrimSpace(match[1])
		text := strings.TrimSpace(match[2])
		if name == "" || text == "" {
			return nil, fmt.Errorf("脚本格式错误：%s。角色名和台词都不能为空", line)
		}
		parsed = append(parsed, audioCloneParsedLine{
			SortOrder:     len(parsed) + 1,
			CharacterName: name,
			Text:          text,
		})
	}
	if len(parsed) == 0 {
		return nil, fmt.Errorf("请先填写至少一行脚本")
	}
	return parsed, nil
}

func audioCloneLinePreserveKey(sortOrder int, characterName string, text string) string {
	return fmt.Sprintf("%d\x00%s\x00%s", sortOrder, strings.TrimSpace(characterName), strings.TrimSpace(text))
}

func replaceAudioCloneLines(projectID uint, script string, defaultSeed int64) ([]models.AudioCloneLine, error) {
	parsed, err := parseAudioCloneScript(script)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	created := make([]models.AudioCloneLine, 0, len(parsed))
	oldGeneratedAudio := make([]string, 0)
	err = db.DB.Transaction(func(tx *gorm.DB) error {
		var oldLines []models.AudioCloneLine
		if err := tx.Where("project_id = ?", projectID).Find(&oldLines).Error; err != nil {
			return err
		}
		oldSeedByKey := make(map[string]int64, len(oldLines))
		for _, line := range oldLines {
			if strings.TrimSpace(line.GeneratedAudio) != "" {
				oldGeneratedAudio = append(oldGeneratedAudio, line.GeneratedAudio)
			}
			if line.Seed > 0 {
				oldSeedByKey[audioCloneLinePreserveKey(line.SortOrder, line.CharacterName, line.Text)] = line.Seed
			}
		}
		if err := tx.Where("project_id = ?", projectID).Delete(&models.AudioCloneLine{}).Error; err != nil {
			return err
		}
		for _, item := range parsed {
			seed := normalizeAudioCloneSeed(defaultSeed)
			if existing, ok := oldSeedByKey[audioCloneLinePreserveKey(item.SortOrder, item.CharacterName, item.Text)]; ok && existing > 0 {
				seed = normalizeAudioCloneSeed(existing)
			}
			line := models.AudioCloneLine{
				ProjectID:     projectID,
				SortOrder:     item.SortOrder,
				CharacterName: item.CharacterName,
				Text:          item.Text,
				Seed:          seed,
				Status:        audioCloneLineStatusDraft,
				CreatedAt:     now,
				UpdatedAt:     now,
			}
			if err := tx.Create(&line).Error; err != nil {
				return err
			}
			created = append(created, line)
		}
		return nil
	})
	if err == nil {
		for _, path := range oldGeneratedAudio {
			_ = removeAudioCloneAsset(path)
		}
	}
	return created, err
}

type audioCloneMissingCharacter struct {
	Name          string `json:"name"`
	MissingReason string `json:"missing_reason"`
}

func validateAudioCloneLineAssets(projectID uint, parsed []audioCloneParsedLine) []audioCloneMissingCharacter {
	var characters []models.AudioCloneCharacter
	_ = db.DB.Where("project_id = ?", projectID).Find(&characters).Error
	charMap := make(map[string]models.AudioCloneCharacter, len(characters))
	for _, character := range characters {
		charMap[strings.TrimSpace(character.Name)] = character
	}
	missingMap := make(map[string]string)
	for _, line := range parsed {
		character, ok := charMap[line.CharacterName]
		switch {
		case !ok:
			missingMap[line.CharacterName] = "没有创建这个角色"
		case strings.TrimSpace(character.ReferenceAudio) == "":
			missingMap[line.CharacterName] = "缺少参考音频"
		case strings.TrimSpace(character.ReferenceText) == "" && character.ReferenceTextStatus == audioCloneLineStatusGenerating:
			missingMap[line.CharacterName] = "参考音频内容正在识别，请稍后再生成"
		case strings.TrimSpace(character.ReferenceText) == "":
			missingMap[line.CharacterName] = "缺少参考音频内容，请先识别或手动填写"
		}
	}
	names := make([]string, 0, len(missingMap))
	for name := range missingMap {
		names = append(names, name)
	}
	sort.Strings(names)
	result := make([]audioCloneMissingCharacter, 0, len(names))
	for _, name := range names {
		result = append(result, audioCloneMissingCharacter{Name: name, MissingReason: missingMap[name]})
	}
	return result
}

func ListAudioCloneProjects(c *gin.Context) {
	var projects []models.AudioCloneProject
	if err := db.DB.Order("created_at desc").Find(&projects).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "读取音频复制项目失败"})
		return
	}
	c.JSON(http.StatusOK, projects)
}

func GetAudioCloneProject(c *gin.Context) {
	project, err := loadAudioCloneProjectOr404(c)
	if err != nil {
		return
	}
	c.JSON(http.StatusOK, project)
}

func CreateAudioCloneProject(c *gin.Context) {
	var req struct {
		Name        string `json:"name"`
		Code        string `json:"code"`
		Description string `json:"description"`
		ScriptText  string `json:"script_text"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求格式错误"})
		return
	}
	name := strings.TrimSpace(req.Name)
	code := normalizeAudioCloneCode(req.Code)
	if name == "" || code == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请填写项目名称和项目文件名"})
		return
	}
	if !validateAudioCloneProjectCode(code) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "项目文件名只允许英文、数字、下划线或连字符"})
		return
	}
	var count int64
	db.DB.Model(&models.AudioCloneProject{}).Where("code = ?", code).Count(&count)
	if count > 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "项目文件名已被占用"})
		return
	}
	if _, err := os.Stat(audioCloneProjectDir(code)); err == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "项目文件名已被占用"})
		return
	}
	now := time.Now()
	project := models.AudioCloneProject{
		Name:        name,
		Code:        code,
		Description: strings.TrimSpace(req.Description),
		ScriptText:  strings.TrimSpace(req.ScriptText),
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := os.MkdirAll(audioCloneProjectDir(code), 0755); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "创建项目目录失败"})
		return
	}
	if err := db.DB.Create(&project).Error; err != nil {
		_ = os.RemoveAll(audioCloneProjectDir(code))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "创建音频复制项目失败"})
		return
	}
	c.JSON(http.StatusCreated, project)
}

func UpdateAudioCloneProject(c *gin.Context) {
	project, err := loadAudioCloneProjectOr404(c)
	if err != nil {
		return
	}
	var req struct {
		Name        string `json:"name"`
		Code        string `json:"code"`
		Description string `json:"description"`
		ScriptText  string `json:"script_text"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求格式错误"})
		return
	}
	name := strings.TrimSpace(req.Name)
	code := normalizeAudioCloneCode(req.Code)
	if name == "" || code == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请填写项目名称和项目文件名"})
		return
	}
	if !validateAudioCloneProjectCode(code) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "项目文件名只允许英文、数字、下划线或连字符"})
		return
	}
	if code != project.Code {
		var count int64
		db.DB.Model(&models.AudioCloneProject{}).Where("code = ? AND id <> ?", code, project.ID).Count(&count)
		if count > 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "项目文件名已被占用"})
			return
		}
		if _, err := os.Stat(audioCloneProjectDir(code)); err == nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "项目文件名已被占用"})
			return
		}
		if err := os.Rename(audioCloneProjectDir(project.Code), audioCloneProjectDir(code)); err != nil && !os.IsNotExist(err) {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "重命名项目目录失败"})
			return
		}
		oldPrefix := "/" + filepath.ToSlash(audioCloneProjectDir(project.Code))
		newPrefix := "/" + filepath.ToSlash(audioCloneProjectDir(code))
		replacePrefix := func(path string) string {
			if strings.HasPrefix(path, oldPrefix) {
				return newPrefix + strings.TrimPrefix(path, oldPrefix)
			}
			return path
		}
		var characters []models.AudioCloneCharacter
		_ = db.DB.Where("project_id = ?", project.ID).Find(&characters).Error
		for _, character := range characters {
			_ = db.DB.Model(&models.AudioCloneCharacter{}).Where("id = ?", character.ID).Update("reference_audio", replacePrefix(character.ReferenceAudio)).Error
		}
		var lines []models.AudioCloneLine
		_ = db.DB.Where("project_id = ?", project.ID).Find(&lines).Error
		for _, line := range lines {
			_ = db.DB.Model(&models.AudioCloneLine{}).Where("id = ?", line.ID).Update("generated_audio", replacePrefix(line.GeneratedAudio)).Error
		}
	}
	updates := map[string]interface{}{
		"name":        name,
		"code":        code,
		"description": strings.TrimSpace(req.Description),
		"script_text": strings.TrimSpace(req.ScriptText),
		"updated_at":  time.Now(),
	}
	if err := db.DB.Model(&models.AudioCloneProject{}).Where("id = ?", project.ID).Updates(updates).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "更新音频复制项目失败"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "项目已更新"})
}

func DeleteAudioCloneProject(c *gin.Context) {
	project, err := loadAudioCloneProjectOr404(c)
	if err != nil {
		return
	}
	if err := db.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("project_id = ?", project.ID).Delete(&models.AudioCloneLine{}).Error; err != nil {
			return err
		}
		if err := tx.Where("project_id = ?", project.ID).Delete(&models.AudioCloneCharacter{}).Error; err != nil {
			return err
		}
		return tx.Delete(&models.AudioCloneProject{}, project.ID).Error
	}); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "删除音频复制项目失败"})
		return
	}
	_ = os.RemoveAll(audioCloneProjectDir(project.Code))
	c.JSON(http.StatusOK, gin.H{"message": "项目已删除"})
}

func ListAudioCloneCharacters(c *gin.Context) {
	project, err := loadAudioCloneProjectOr404(c)
	if err != nil {
		return
	}
	var characters []models.AudioCloneCharacter
	if err := db.DB.Where("project_id = ?", project.ID).Order("sort_order asc, id asc").Find(&characters).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "读取角色声音资产失败"})
		return
	}
	c.JSON(http.StatusOK, characters)
}

func saveAudioCloneCharacterAudio(c *gin.Context, project models.AudioCloneProject, characterID uint, file *multipart.FileHeader) (string, error) {
	if file == nil {
		return "", nil
	}
	ext := strings.ToLower(filepath.Ext(file.Filename))
	if ext == "" {
		ext = ".mp3"
	}
	if err := os.MkdirAll(audioCloneCharacterDir(project.Code), 0755); err != nil {
		return "", err
	}
	absPath := audioCloneCharacterAudioPath(project.Code, characterID, ext)
	if err := c.SaveUploadedFile(file, absPath); err != nil {
		return "", err
	}
	return "/" + filepath.ToSlash(absPath), nil
}

func CreateAudioCloneCharacter(c *gin.Context) {
	project, err := loadAudioCloneProjectOr404(c)
	if err != nil {
		return
	}
	name := strings.TrimSpace(c.PostForm("name"))
	referenceText := strings.TrimSpace(c.PostForm("reference_text"))
	if name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请填写角色名"})
		return
	}
	var count int64
	db.DB.Model(&models.AudioCloneCharacter{}).Where("project_id = ? AND name = ?", project.ID, name).Count(&count)
	if count > 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "角色名已存在"})
		return
	}
	var existingCount int64
	db.DB.Model(&models.AudioCloneCharacter{}).Where("project_id = ?", project.ID).Count(&existingCount)
	now := time.Now()
	character := models.AudioCloneCharacter{
		ProjectID:           project.ID,
		SortOrder:           int(existingCount) + 1,
		Name:                name,
		ReferenceText:       referenceText,
		ReferenceTextStatus: audioCloneLineStatusDraft,
		CreatedAt:           now,
		UpdatedAt:           now,
	}
	if referenceText != "" {
		character.ReferenceTextStatus = audioCloneLineStatusGenerated
	}
	file, _ := c.FormFile("reference_audio")
	if err := db.DB.Create(&character).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "创建角色声音资产失败"})
		return
	}
	if file != nil {
		webPath, err := saveAudioCloneCharacterAudio(c, *project, character.ID, file)
		if err != nil {
			_ = db.DB.Delete(&models.AudioCloneCharacter{}, character.ID).Error
			c.JSON(http.StatusInternalServerError, gin.H{"error": "保存参考音频失败"})
			return
		}
		character.ReferenceAudio = webPath
		_ = db.DB.Model(&models.AudioCloneCharacter{}).Where("id = ?", character.ID).Updates(map[string]interface{}{
			"reference_audio": webPath,
			"reference_text_status": func() string {
				if referenceText != "" {
					return audioCloneLineStatusGenerated
				}
				return audioCloneLineStatusDraft
			}(),
			"reference_text_error": "",
			"updated_at":           time.Now(),
		}).Error
		if referenceText == "" {
			if taskID, err := startAudioCloneReferenceRecognitionTask(&character, project); err == nil {
				character.ReferenceTextStatus = audioCloneLineStatusGenerating
				character.ReferenceTextCurrentTaskID = taskID
				_ = db.DB.Model(&models.AudioCloneCharacter{}).Where("id = ?", character.ID).Updates(map[string]interface{}{
					"reference_text_status":          audioCloneLineStatusGenerating,
					"reference_text_current_task_id": taskID,
					"reference_text_error":           "",
					"updated_at":                     time.Now(),
				}).Error
			} else {
				character.ReferenceTextStatus = audioCloneLineStatusFailed
				character.ReferenceTextError = err.Error()
				_ = db.DB.Model(&models.AudioCloneCharacter{}).Where("id = ?", character.ID).Updates(map[string]interface{}{
					"reference_text_status": audioCloneLineStatusFailed,
					"reference_text_error":  err.Error(),
					"updated_at":            time.Now(),
				}).Error
			}
		}
	}
	c.JSON(http.StatusCreated, character)
}

func UpdateAudioCloneCharacter(c *gin.Context) {
	character, err := loadAudioCloneCharacterOr404(c)
	if err != nil {
		return
	}
	var project models.AudioCloneProject
	if err := db.DB.First(&project, character.ProjectID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "所属项目不存在"})
		return
	}
	name := strings.TrimSpace(c.PostForm("name"))
	referenceText := strings.TrimSpace(c.PostForm("reference_text"))
	if name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请填写角色名"})
		return
	}
	var count int64
	db.DB.Model(&models.AudioCloneCharacter{}).Where("project_id = ? AND name = ? AND id <> ?", character.ProjectID, name, character.ID).Count(&count)
	if count > 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "角色名已存在"})
		return
	}
	updates := map[string]interface{}{
		"name":                           name,
		"reference_text":                 referenceText,
		"reference_text_current_task_id": "",
		"reference_text_error":           "",
		"reference_text_status":          audioCloneLineStatusDraft,
		"updated_at":                     time.Now(),
	}
	if referenceText != "" {
		updates["reference_text_status"] = audioCloneLineStatusGenerated
	}
	audioChanged := false
	if file, err := c.FormFile("reference_audio"); err == nil && file != nil {
		webPath, err := saveAudioCloneCharacterAudio(c, project, character.ID, file)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "保存参考音频失败"})
			return
		}
		_ = removeAudioCloneAsset(character.ReferenceAudio)
		updates["reference_audio"] = webPath
		audioChanged = true
	}
	if err := db.DB.Model(&models.AudioCloneCharacter{}).Where("id = ?", character.ID).Updates(updates).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "更新角色声音资产失败"})
		return
	}
	if audioChanged && referenceText == "" {
		var updated models.AudioCloneCharacter
		if err := db.DB.First(&updated, character.ID).Error; err == nil {
			if taskID, err := startAudioCloneReferenceRecognitionTask(&updated, &project); err == nil {
				_ = db.DB.Model(&models.AudioCloneCharacter{}).Where("id = ?", updated.ID).Updates(map[string]interface{}{
					"reference_text_status":          audioCloneLineStatusGenerating,
					"reference_text_current_task_id": taskID,
					"reference_text_error":           "",
					"updated_at":                     time.Now(),
				}).Error
			} else {
				_ = db.DB.Model(&models.AudioCloneCharacter{}).Where("id = ?", updated.ID).Updates(map[string]interface{}{
					"reference_text_status": audioCloneLineStatusFailed,
					"reference_text_error":  err.Error(),
					"updated_at":            time.Now(),
				}).Error
			}
		}
	}
	c.JSON(http.StatusOK, gin.H{"message": "角色声音资产已更新"})
}

func DeleteAudioCloneCharacter(c *gin.Context) {
	character, err := loadAudioCloneCharacterOr404(c)
	if err != nil {
		return
	}
	if err := db.DB.Delete(&models.AudioCloneCharacter{}, character.ID).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "删除角色声音资产失败"})
		return
	}
	_ = removeAudioCloneAsset(character.ReferenceAudio)
	c.JSON(http.StatusOK, gin.H{"message": "角色声音资产已删除"})
}

func RecognizeAudioCloneCharacterReference(c *gin.Context) {
	character, err := loadAudioCloneCharacterOr404(c)
	if err != nil {
		return
	}
	if strings.TrimSpace(character.ReferenceAudio) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请先上传参考音频"})
		return
	}
	var project models.AudioCloneProject
	if err := db.DB.First(&project, character.ProjectID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "所属项目不存在"})
		return
	}
	taskID, err := startAudioCloneReferenceRecognitionTask(character, &project)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "提交参考音频识别任务失败"})
		return
	}
	if err := db.DB.Model(&models.AudioCloneCharacter{}).Where("id = ?", character.ID).Updates(map[string]interface{}{
		"reference_text_status":          audioCloneLineStatusGenerating,
		"reference_text_current_task_id": taskID,
		"reference_text_error":           "",
		"updated_at":                     time.Now(),
	}).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "更新参考音频识别状态失败"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "参考音频识别任务已提交", "task_id": taskID})
}

func ListAudioCloneLines(c *gin.Context) {
	project, err := loadAudioCloneProjectOr404(c)
	if err != nil {
		return
	}
	var lines []models.AudioCloneLine
	if err := db.DB.Where("project_id = ?", project.ID).Order("sort_order asc, id asc").Find(&lines).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "读取生成行失败"})
		return
	}
	defaultSeed := normalizeAudioCloneSeed(getConfiguredGlobalSeed())
	for idx := range lines {
		if lines[idx].Seed <= 0 {
			lines[idx].Seed = defaultSeed
			_ = db.DB.Model(&models.AudioCloneLine{}).Where("id = ?", lines[idx].ID).Updates(map[string]interface{}{
				"seed":       defaultSeed,
				"updated_at": time.Now(),
			}).Error
		}
	}
	c.JSON(http.StatusOK, lines)
}

func SaveAudioCloneScriptLines(c *gin.Context) {
	project, err := loadAudioCloneProjectOr404(c)
	if err != nil {
		return
	}
	var req struct {
		ScriptText string `json:"script_text"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求格式错误"})
		return
	}
	script := strings.TrimSpace(req.ScriptText)
	lines, err := replaceAudioCloneLines(project.ID, script, getConfiguredGlobalSeed())
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := db.DB.Model(&models.AudioCloneProject{}).Where("id = ?", project.ID).Updates(map[string]interface{}{
		"script_text": script,
		"updated_at":  time.Now(),
	}).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "保存脚本失败"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"lines": lines})
}

func GenerateAudioCloneProjectLines(c *gin.Context) {
	project, err := loadAudioCloneProjectOr404(c)
	if err != nil {
		return
	}
	var req struct {
		ScriptText string `json:"script_text"`
		RandomSeed bool   `json:"random_seed"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求格式错误"})
		return
	}
	script := strings.TrimSpace(req.ScriptText)
	parsed, err := parseAudioCloneScript(script)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	missing := validateAudioCloneLineAssets(project.ID, parsed)
	if len(missing) > 0 {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":              "存在缺少声音资产的角色，请先补齐",
			"missing_characters": missing,
		})
		return
	}
	lines, err := replaceAudioCloneLines(project.ID, script, getConfiguredGlobalSeed())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "保存生成行失败"})
		return
	}
	if err := db.DB.Model(&models.AudioCloneProject{}).Where("id = ?", project.ID).Updates(map[string]interface{}{
		"script_text": script,
		"updated_at":  time.Now(),
	}).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "保存脚本失败"})
		return
	}
	started := 0
	seedForBatch := int64(0)
	if req.RandomSeed {
		seedForBatch = randomAudioCloneSeed()
	}
	for _, line := range lines {
		lineCopy := line
		seed := normalizeAudioCloneSeed(line.Seed)
		if line.Seed <= 0 {
			seed = normalizeAudioCloneSeed(getConfiguredGlobalSeed())
		}
		if req.RandomSeed {
			seed = seedForBatch
		}
		lineCopy.Seed = seed
		promptID, workflowLabel, err := queueAudioCloneLinePrompt(*project, lineCopy, seed)
		if err != nil {
			_ = db.DB.Model(&models.AudioCloneLine{}).Where("id = ?", line.ID).Updates(map[string]interface{}{
				"status":     audioCloneLineStatusFailed,
				"last_error": err.Error(),
				"seed":       seed,
				"updated_at": time.Now(),
			}).Error
			continue
		}
		taskID, err := startAudioCloneLineTask(&lineCopy, project, seed, promptID, workflowLabel)
		if err != nil {
			_ = db.DB.Model(&models.AudioCloneLine{}).Where("id = ?", line.ID).Updates(map[string]interface{}{
				"status":     audioCloneLineStatusFailed,
				"last_error": err.Error(),
				"updated_at": time.Now(),
			}).Error
			continue
		}
		_ = db.DB.Model(&models.AudioCloneLine{}).Where("id = ?", line.ID).Updates(map[string]interface{}{
			"status":          audioCloneLineStatusGenerating,
			"current_task_id": taskID,
			"seed":            seed,
			"last_error":      "",
			"updated_at":      time.Now(),
		}).Error
		started++
	}
	c.JSON(http.StatusOK, gin.H{"message": fmt.Sprintf("已提交 %d 条音频复制任务", started), "lines": lines})
}

func GenerateAudioCloneLine(c *gin.Context) {
	line, err := loadAudioCloneLineOr404(c)
	if err != nil {
		return
	}
	var project models.AudioCloneProject
	if err := db.DB.First(&project, line.ProjectID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "所属项目不存在"})
		return
	}
	var req struct {
		RandomSeed bool `json:"random_seed"`
	}
	_ = c.ShouldBindJSON(&req)
	parsed := []audioCloneParsedLine{{CharacterName: line.CharacterName, Text: line.Text, SortOrder: line.SortOrder}}
	missing := validateAudioCloneLineAssets(project.ID, parsed)
	if len(missing) > 0 {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":              "存在缺少声音资产的角色，请先补齐",
			"missing_characters": missing,
		})
		return
	}
	seed := normalizeAudioCloneSeed(line.Seed)
	if line.Seed <= 0 {
		seed = normalizeAudioCloneSeed(getConfiguredGlobalSeed())
	}
	if req.RandomSeed {
		seed = randomAudioCloneSeed()
	}
	line.Seed = seed
	taskID, err := startAudioCloneLineTask(line, &project, seed, "", "")
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "提交音频复制任务失败"})
		return
	}
	if err := db.DB.Model(&models.AudioCloneLine{}).Where("id = ?", line.ID).Updates(map[string]interface{}{
		"status":          audioCloneLineStatusGenerating,
		"current_task_id": taskID,
		"seed":            seed,
		"last_error":      "",
		"updated_at":      time.Now(),
	}).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "更新音频复制行状态失败"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "音频复制任务已提交", "task_id": taskID})
}

func UpdateAudioCloneLine(c *gin.Context) {
	line, err := loadAudioCloneLineOr404(c)
	if err != nil {
		return
	}
	var req struct {
		Text string `json:"text"`
		Seed int64  `json:"seed"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求格式错误"})
		return
	}
	text := strings.TrimSpace(req.Text)
	if text == "" {
		text = strings.TrimSpace(line.Text)
	}
	seed := normalizeAudioCloneSeed(req.Seed)
	if req.Seed <= 0 {
		seed = normalizeAudioCloneSeed(getConfiguredGlobalSeed())
	}
	changed := strings.TrimSpace(line.Text) != text || normalizeAudioCloneSeed(line.Seed) != seed
	updates := map[string]interface{}{
		"text":       text,
		"seed":       seed,
		"updated_at": time.Now(),
	}
	if changed {
		_ = removeAudioCloneAsset(line.GeneratedAudio)
		updates["status"] = audioCloneLineStatusDraft
		updates["current_task_id"] = ""
		updates["last_error"] = ""
		updates["generated_audio"] = ""
		updates["generated_workflow"] = ""
	}
	if err := db.DB.Model(&models.AudioCloneLine{}).Where("id = ?", line.ID).Updates(updates).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "更新 LongChat 行失败"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "LongChat 行已更新"})
}
