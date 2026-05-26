package api

import (
	"archive/zip"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"kt-ai-studio/internal/models"

	"github.com/gin-gonic/gin"
)

type generalGuideExportPreparedClip struct {
	Label   string
	AbsPath string
}

var generalGuideExportFilenameCleaner = regexp.MustCompile(`[\\/:*?"<>|]+`)

func sanitizeGeneralGuideExportFilename(name string) string {
	cleaned := strings.TrimSpace(name)
	if cleaned == "" {
		return "未命名"
	}
	cleaned = generalGuideExportFilenameCleaner.ReplaceAllString(cleaned, "_")
	cleaned = strings.ReplaceAll(cleaned, "\n", "_")
	cleaned = strings.ReplaceAll(cleaned, "\r", "_")
	cleaned = strings.Trim(cleaned, " .")
	if cleaned == "" {
		return "未命名"
	}
	return cleaned
}

func generalGuideSceneExportLabel(scene models.GeneralGuideScene) string {
	title := strings.TrimSpace(scene.Title)
	if title != "" {
		return title
	}
	return fmt.Sprintf("第%d行", scene.SortOrder)
}

func generalGuideTransitionExportLabel(transition models.GeneralGuideTransition) string {
	return fmt.Sprintf("转场_%02d_%02d", transition.FromSortOrder, transition.ToSortOrder)
}

func listGeneralGuideGeneratedScenesForExport(projectID uint) ([]models.GeneralGuideScene, error) {
	allScenes, err := listGeneralGuideScenesForProject(projectID)
	if err != nil {
		return nil, err
	}
	filtered := make([]models.GeneralGuideScene, 0, len(allScenes))
	for _, scene := range allScenes {
		if !generalGuideExportAssetReady(scene.GeneratedVideo) {
			continue
		}
		filtered = append(filtered, scene)
	}
	if len(filtered) == 0 {
		return nil, fmt.Errorf("当前项目还没有可导出的已生成视频")
	}
	return filtered, nil
}

func listGeneralGuideGeneratedTransitionsForExport(projectID uint) (map[uint]models.GeneralGuideTransition, error) {
	allTransitions, err := listGeneralGuideTransitionsForProject(projectID)
	if err != nil {
		return nil, err
	}
	transitionByFromSceneID := make(map[uint]models.GeneralGuideTransition)
	for _, transition := range allTransitions {
		if !generalGuideExportAssetReady(transition.GeneratedVideo) {
			continue
		}
		transitionByFromSceneID[transition.FromSceneID] = transition
	}
	return transitionByFromSceneID, nil
}

func generalGuideExportAssetReady(webPath string) bool {
	trimmed := strings.TrimSpace(webPath)
	if trimmed == "" {
		return false
	}
	absPath, err := assetWebPathToAbs(trimmed)
	if err != nil {
		return false
	}
	if info, err := os.Stat(absPath); err == nil && !info.IsDir() && info.Size() > 0 {
		return true
	}
	return false
}

func trimGeneralGuideSceneVideoForExport(sourceWebPath string, trimStartFrames int, trimEndFrames int, targetAbsPath string) error {
	sourceAbsPath, err := assetWebPathToAbs(sourceWebPath)
	if err != nil {
		return err
	}
	totalFrames, fps, err := ffprobeVideoFramesAndFPS(sourceAbsPath)
	if err != nil {
		return err
	}
	if fps <= 0 {
		fps = float64(generalGuideTransitionFPS)
	}
	startFrames := trimStartFrames
	if startFrames < 0 {
		startFrames = 0
	}
	endFrames := trimEndFrames
	if endFrames < 0 {
		endFrames = 0
	}
	remainingFrames := totalFrames - startFrames - endFrames
	if remainingFrames <= 1 {
		return fmt.Errorf("视频可导出帧数不足")
	}
	startSeconds := float64(startFrames) / fps
	durationSeconds := float64(remainingFrames) / fps
	args := []string{
		"-y",
		"-v", "error",
	}
	if startSeconds > 0 {
		args = append(args, "-ss", fmt.Sprintf("%.6f", startSeconds))
	}
	args = append(args,
		"-i", sourceAbsPath,
		"-t", fmt.Sprintf("%.6f", durationSeconds),
		"-c:v", "libx264",
		"-c:a", "aac",
		"-pix_fmt", "yuv420p",
		"-movflags", "+faststart",
		targetAbsPath,
	)
	return runFFmpeg(args...)
}

func addSilentAudioToGeneralGuideTransitionForExport(sourceWebPath string, targetAbsPath string) error {
	sourceAbsPath, err := assetWebPathToAbs(sourceWebPath)
	if err != nil {
		return err
	}
	totalFrames, fps, err := ffprobeVideoFramesAndFPS(sourceAbsPath)
	if err != nil {
		return err
	}
	if fps <= 0 {
		fps = float64(generalGuideTransitionFPS)
	}
	durationSeconds := float64(totalFrames) / fps
	return runFFmpeg(
		"-y",
		"-v", "error",
		"-i", sourceAbsPath,
		"-f", "lavfi",
		"-t", fmt.Sprintf("%.6f", durationSeconds),
		"-i", "anullsrc=channel_layout=stereo:sample_rate=48000",
		"-map", "0:v:0",
		"-map", "1:a:0",
		"-shortest",
		"-c:v", "libx264",
		"-c:a", "aac",
		"-pix_fmt", "yuv420p",
		"-movflags", "+faststart",
		targetAbsPath,
	)
}

func buildGeneralGuidePreparedExportClips(projectID uint, workspaceDir string) ([]generalGuideExportPreparedClip, error) {
	scenes, err := listGeneralGuideGeneratedScenesForExport(projectID)
	if err != nil {
		return nil, err
	}
	transitionsByFromSceneID, err := listGeneralGuideGeneratedTransitionsForExport(projectID)
	if err != nil {
		return nil, err
	}
	prepared := make([]generalGuideExportPreparedClip, 0)
	for idx, scene := range scenes {
		var prevTransition *models.GeneralGuideTransition
		if idx > 0 {
			if candidate, ok := transitionsByFromSceneID[scenes[idx-1].ID]; ok && candidate.ToSceneID == scene.ID {
				prevTransition = &candidate
			}
		}
		var nextTransition *models.GeneralGuideTransition
		if idx+1 < len(scenes) {
			if candidate, ok := transitionsByFromSceneID[scene.ID]; ok && candidate.ToSceneID == scenes[idx+1].ID {
				nextTransition = &candidate
			}
		}

		sceneTrimStartFrames := 0
		if prevTransition != nil {
			sceneTrimStartFrames = 1
		}
		sceneTrimEndFrames := 0
		if nextTransition != nil {
			sceneTrimEndFrames = sanitizeGeneralGuideTransitionFramesFromEnd(nextTransition.FramesFromEnd)
		}

		sceneClipPath := filepath.Join(workspaceDir, fmt.Sprintf("%02d_scene_%02d.mp4", len(prepared)+1, scene.SortOrder))
		if err := trimGeneralGuideSceneVideoForExport(scene.GeneratedVideo, sceneTrimStartFrames, sceneTrimEndFrames, sceneClipPath); err != nil {
			return nil, fmt.Errorf("%s 导出裁剪失败: %w", generalGuideSceneExportLabel(scene), err)
		}
		prepared = append(prepared, generalGuideExportPreparedClip{
			Label:   generalGuideSceneExportLabel(scene),
			AbsPath: sceneClipPath,
		})

		if nextTransition != nil {
			transitionClipPath := filepath.Join(workspaceDir, fmt.Sprintf("%02d_transition_%02d_%02d.mp4", len(prepared)+1, nextTransition.FromSortOrder, nextTransition.ToSortOrder))
			if err := addSilentAudioToGeneralGuideTransitionForExport(nextTransition.GeneratedVideo, transitionClipPath); err != nil {
				return nil, fmt.Errorf("%s 导出处理失败: %w", generalGuideTransitionExportLabel(*nextTransition), err)
			}
			prepared = append(prepared, generalGuideExportPreparedClip{
				Label:   generalGuideTransitionExportLabel(*nextTransition),
				AbsPath: transitionClipPath,
			})
		}
	}
	if len(prepared) == 0 {
		return nil, fmt.Errorf("当前项目没有可导出的片段")
	}
	return prepared, nil
}

func buildGeneralGuideExportArchive(clips []generalGuideExportPreparedClip, zipPath string) error {
	zipFile, err := os.Create(zipPath)
	if err != nil {
		return err
	}
	defer zipFile.Close()

	zipWriter := zip.NewWriter(zipFile)
	for idx, clip := range clips {
		ext := filepath.Ext(clip.AbsPath)
		if ext == "" {
			ext = ".mp4"
		}
		filename := fmt.Sprintf("%02d_%s%s", idx+1, sanitizeGeneralGuideExportFilename(clip.Label), ext)
		if err := addFileToZip(zipWriter, filename, clip.AbsPath); err != nil {
			return err
		}
	}
	return zipWriter.Close()
}

func mergeGeneralGuidePreparedClips(clips []generalGuideExportPreparedClip, targetOutputPath string) error {
	if len(clips) == 0 {
		return fmt.Errorf("no clips to merge")
	}
	if err := os.MkdirAll(filepath.Dir(targetOutputPath), 0755); err != nil {
		return err
	}
	listFile, err := os.CreateTemp(filepath.Dir(targetOutputPath), "general_guide_concat_*.txt")
	if err != nil {
		return err
	}
	listPath := listFile.Name()
	defer os.Remove(listPath)
	defer listFile.Close()

	lines := make([]string, 0, len(clips))
	for _, clip := range clips {
		lines = append(lines, fmt.Sprintf("file '%s'", ffmpegConcatListPath(clip.AbsPath)))
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

func ExportGeneralGuideProjectArchive(c *gin.Context) {
	project, err := loadGeneralGuideProjectOr404(c)
	if err != nil {
		return
	}
	workspaceDir, err := createTemporaryVideoExportWorkspace(fmt.Sprintf("general_guide_%d_export_", project.ID))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("导出空间创建失败: %v", err)})
		return
	}
	clips, err := buildGeneralGuidePreparedExportClips(project.ID, workspaceDir)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	filenameBase := sanitizeGeneralGuideExportFilename(project.Code)
	if filenameBase == "未命名" {
		filenameBase = fmt.Sprintf("general_guide_%d", project.ID)
	}
	zipPath := filepath.Join(workspaceDir, fmt.Sprintf("%s_export.zip", filenameBase))
	if err := buildGeneralGuideExportArchive(clips, zipPath); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("构建导出压缩包失败: %v", err)})
		return
	}
	c.FileAttachment(zipPath, filepath.Base(zipPath))
}

func ExportGeneralGuideProjectMergedVideo(c *gin.Context) {
	project, err := loadGeneralGuideProjectOr404(c)
	if err != nil {
		return
	}
	workspaceDir, err := createTemporaryVideoExportWorkspace(fmt.Sprintf("general_guide_%d_merged_", project.ID))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("导出空间创建失败: %v", err)})
		return
	}
	clips, err := buildGeneralGuidePreparedExportClips(project.ID, workspaceDir)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	filenameBase := sanitizeGeneralGuideExportFilename(project.Code)
	if filenameBase == "未命名" {
		filenameBase = fmt.Sprintf("general_guide_%d", project.ID)
	}
	outputPath := filepath.Join(workspaceDir, fmt.Sprintf("%s_merged.mp4", filenameBase))
	if err := mergeGeneralGuidePreparedClips(clips, outputPath); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("合并导出视频失败: %v", err)})
		return
	}
	c.FileAttachment(outputPath, filepath.Base(outputPath))
}
