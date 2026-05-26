package api

import (
	"archive/zip"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"kt-ai-studio/internal/db"
	"kt-ai-studio/internal/models"

	"github.com/gin-gonic/gin"
)

var qwenTTSExportFilenameCleaner = regexp.MustCompile(`[\\/:*?"<>|]+`)

func sanitizeQwenTTSExportFilename(name string) string {
	cleaned := strings.TrimSpace(name)
	if cleaned == "" {
		return "未命名"
	}
	cleaned = qwenTTSExportFilenameCleaner.ReplaceAllString(cleaned, "_")
	cleaned = strings.ReplaceAll(cleaned, "\n", "_")
	cleaned = strings.ReplaceAll(cleaned, "\r", "_")
	cleaned = strings.Trim(cleaned, " .")
	if cleaned == "" {
		return "未命名"
	}
	return cleaned
}

func qwenTTSExportAssetReady(webPath string) bool {
	trimmed := strings.TrimSpace(webPath)
	if trimmed == "" {
		return false
	}
	absPath, err := assetWebPathToAbs(trimmed)
	if err != nil {
		return false
	}
	info, err := os.Stat(absPath)
	return err == nil && !info.IsDir() && info.Size() > 0
}

func listQwenTTSLinesForExport(projectID uint) ([]models.QwenTTSLine, error) {
	var lines []models.QwenTTSLine
	if err := db.DB.Where("project_id = ?", projectID).Order("sort_order asc, id asc").Find(&lines).Error; err != nil {
		return nil, err
	}
	if len(lines) == 0 {
		return nil, fmt.Errorf("当前项目还没有可导出的台词行")
	}

	missingAudio := make([]string, 0)
	for idx, line := range lines {
		if qwenTTSExportAssetReady(line.GeneratedAudio) {
			continue
		}
		missingAudio = append(missingAudio, fmt.Sprintf("%d", idx+1))
	}
	if len(missingAudio) > 0 {
		return nil, fmt.Errorf("还有 %d 行未生成音频，暂不能导出：第 %s 行", len(missingAudio), strings.Join(missingAudio, "、"))
	}
	return lines, nil
}

func buildQwenTTSExportText(lines []models.QwenTTSLine) string {
	rows := make([]string, 0, len(lines))
	for idx, line := range lines {
		lineNumber := line.SortOrder
		if lineNumber <= 0 {
			lineNumber = idx + 1
		}
		characterName := strings.TrimSpace(line.CharacterName)
		if characterName == "" {
			characterName = "未命名角色"
		}
		rows = append(rows, fmt.Sprintf("%d-%s:%s", lineNumber, characterName, strings.TrimSpace(line.Text)))
	}
	return strings.Join(rows, "\n")
}

func buildQwenTTSExportArchive(lines []models.QwenTTSLine, zipPath string) error {
	zipFile, err := os.Create(zipPath)
	if err != nil {
		return err
	}
	defer zipFile.Close()

	zipWriter := zip.NewWriter(zipFile)
	defer zipWriter.Close()

	for idx, line := range lines {
		lineNumber := line.SortOrder
		if lineNumber <= 0 {
			lineNumber = idx + 1
		}
		sourcePath, err := assetWebPathToAbs(line.GeneratedAudio)
		if err != nil {
			return err
		}
		ext := filepath.Ext(sourcePath)
		if ext == "" {
			ext = ".mp3"
		}
		if err := addFileToZip(zipWriter, fmt.Sprintf("%d%s", lineNumber, ext), sourcePath); err != nil {
			return err
		}
	}

	if err := addTextToZip(zipWriter, "all.txt", buildQwenTTSExportText(lines)); err != nil {
		return err
	}
	return nil
}

func ExportQwenTTSProjectArchive(c *gin.Context) {
	project, err := loadQwenTTSProjectOr404(c)
	if err != nil {
		return
	}

	lines, err := listQwenTTSLinesForExport(project.ID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	workspaceDir, err := createTemporaryVideoExportWorkspace(fmt.Sprintf("qwen_tts_%d_export_", project.ID))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("导出空间创建失败: %v", err)})
		return
	}

	filenameBase := sanitizeQwenTTSExportFilename(project.Code)
	if filenameBase == "未命名" {
		filenameBase = fmt.Sprintf("qwen_tts_%d", project.ID)
	}
	zipPath := filepath.Join(workspaceDir, fmt.Sprintf("%s_export.zip", filenameBase))
	if err := buildQwenTTSExportArchive(lines, zipPath); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("构建导出压缩包失败: %v", err)})
		return
	}

	c.FileAttachment(zipPath, filepath.Base(zipPath))
}
