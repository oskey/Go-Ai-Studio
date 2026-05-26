package api

import (
	"archive/zip"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/gin-gonic/gin"
)

type storeVisitExportVideoEntry struct {
	Label         string
	SourceWebPath string
}

var storeVisitExportFilenameCleaner = regexp.MustCompile(`[\\/:*?"<>|]+`)

func sanitizeStoreVisitExportFilename(name string) string {
	cleaned := strings.TrimSpace(name)
	if cleaned == "" {
		return "未命名"
	}
	cleaned = storeVisitExportFilenameCleaner.ReplaceAllString(cleaned, "_")
	cleaned = strings.ReplaceAll(cleaned, "\n", "_")
	cleaned = strings.ReplaceAll(cleaned, "\r", "_")
	cleaned = strings.Trim(cleaned, " .")
	if cleaned == "" {
		return "未命名"
	}
	return cleaned
}

func collectStoreVisitExportVideoEntries(projectID uint) ([]storeVisitExportVideoEntry, error) {
	spots, err := listStoreVisitSpotsForProject(projectID)
	if err != nil {
		return nil, err
	}

	entries := make([]storeVisitExportVideoEntry, 0)
	for _, spot := range spots {
		spotType := normalizeStoreVisitSpotType(spot.SpotType, spot.Name)
		if isDeprecatedStoreVisitSpotType(spotType) {
			continue
		}
		if spotType == storeVisitSpotTypeDishGeneration {
			items, err := listStoreVisitDishGenerationItemsBySpot(spot.ID)
			if err != nil {
				return nil, err
			}
			for _, item := range items {
				if strings.TrimSpace(item.GeneratedVideo) == "" || strings.TrimSpace(item.VideoStatus) != "generated" {
					continue
				}
				entries = append(entries, storeVisitExportVideoEntry{
					Label:         fmt.Sprintf("%s_%02d", getStoreVisitSpotDisplayName(spot), item.SortOrder),
					SourceWebPath: item.GeneratedVideo,
				})
			}
			continue
		}
		if strings.TrimSpace(spot.GeneratedVideo) == "" || strings.TrimSpace(spot.VideoStatus) != "generated" {
			continue
		}
		entries = append(entries, storeVisitExportVideoEntry{
			Label:         getStoreVisitSpotDisplayName(spot),
			SourceWebPath: spot.GeneratedVideo,
		})
	}

	if len(entries) == 0 {
		return nil, fmt.Errorf("当前项目还没有可导出的已生成视频")
	}
	return entries, nil
}

func buildStoreVisitExportArchive(entries []storeVisitExportVideoEntry, zipPath string) error {
	zipFile, err := os.Create(zipPath)
	if err != nil {
		return err
	}
	defer zipFile.Close()

	zipWriter := zip.NewWriter(zipFile)
	for idx, entry := range entries {
		sourcePath, err := assetWebPathToAbs(entry.SourceWebPath)
		if err != nil {
			return err
		}
		ext := filepath.Ext(sourcePath)
		if ext == "" {
			ext = ".mp4"
		}
		baseName := fmt.Sprintf("%02d_%s", idx+1, sanitizeStoreVisitExportFilename(entry.Label))
		if err := addFileToZip(zipWriter, baseName+ext, sourcePath); err != nil {
			return err
		}
	}
	return zipWriter.Close()
}

func mergeStoreVisitVideosToTarget(entries []storeVisitExportVideoEntry, targetOutputPath string) error {
	if len(entries) == 0 {
		return fmt.Errorf("no videos to merge")
	}
	if err := os.MkdirAll(filepath.Dir(targetOutputPath), 0755); err != nil {
		return err
	}
	listFile, err := os.CreateTemp(filepath.Dir(targetOutputPath), "store_visit_concat_*.txt")
	if err != nil {
		return err
	}
	listPath := listFile.Name()
	defer os.Remove(listPath)
	defer listFile.Close()

	lines := make([]string, 0, len(entries))
	for _, entry := range entries {
		absPath, err := assetWebPathToAbs(entry.SourceWebPath)
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

func ExportStoreVisitProjectArchive(c *gin.Context) {
	project, err := loadStoreVisitProjectOr404(c)
	if err != nil {
		return
	}
	entries, err := collectStoreVisitExportVideoEntries(project.ID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	workspaceDir, err := createTemporaryVideoExportWorkspace(fmt.Sprintf("store_visit_%d_export_", project.ID))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("导出空间创建失败: %v", err)})
		return
	}
	filenameBase := sanitizeStoreVisitExportFilename(project.Code)
	if filenameBase == "未命名" {
		filenameBase = fmt.Sprintf("store_visit_%d", project.ID)
	}
	zipPath := filepath.Join(workspaceDir, fmt.Sprintf("%s_export.zip", filenameBase))
	if err := buildStoreVisitExportArchive(entries, zipPath); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("构建导出压缩包失败: %v", err)})
		return
	}
	c.FileAttachment(zipPath, filepath.Base(zipPath))
}

func ExportStoreVisitProjectMergedVideo(c *gin.Context) {
	project, err := loadStoreVisitProjectOr404(c)
	if err != nil {
		return
	}
	entries, err := collectStoreVisitExportVideoEntries(project.ID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	workspaceDir, err := createTemporaryVideoExportWorkspace(fmt.Sprintf("store_visit_%d_merged_", project.ID))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("导出空间创建失败: %v", err)})
		return
	}
	filenameBase := sanitizeStoreVisitExportFilename(project.Code)
	if filenameBase == "未命名" {
		filenameBase = fmt.Sprintf("store_visit_%d", project.ID)
	}
	outputPath := filepath.Join(workspaceDir, fmt.Sprintf("%s_merged.mp4", filenameBase))
	if err := mergeStoreVisitVideosToTarget(entries, outputPath); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("合并导出视频失败: %v", err)})
		return
	}
	c.FileAttachment(outputPath, filepath.Base(outputPath))
}
