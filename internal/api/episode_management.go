package api

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"kt-ai-studio/internal/db"
	"kt-ai-studio/internal/models"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type episodeActionRequest struct {
	Episode int `json:"episode"`
}

type episodeScopedData struct {
	projectID    uint
	episode      int
	scenes       []models.Scene
	sceneIDs     []uint
	characters   []models.Character
	characterIDs []uint
	videos       []models.Video
	videoIDs     []uint
}

func collectEpisodeScopedData(projectID uint, episode int) (*episodeScopedData, error) {
	data := &episodeScopedData{
		projectID: projectID,
		episode:   episode,
	}

	if err := db.DB.Preload("Characters").
		Where("project_id = ? AND episode = ?", projectID, episode).
		Order("scene_number asc").
		Find(&data.scenes).Error; err != nil {
		return nil, err
	}
	if len(data.scenes) == 0 {
		return data, nil
	}

	sceneIDSet := make(map[uint]struct{})
	characterIDSet := make(map[uint]struct{})
	for _, scene := range data.scenes {
		sceneIDSet[scene.ID] = struct{}{}
		for _, char := range scene.Characters {
			characterIDSet[char.ID] = struct{}{}
		}
	}

	for sceneID := range sceneIDSet {
		data.sceneIDs = append(data.sceneIDs, sceneID)
	}
	for characterID := range characterIDSet {
		data.characterIDs = append(data.characterIDs, characterID)
	}

	if len(data.characterIDs) > 0 {
		if err := db.DB.Where("id IN ?", data.characterIDs).Find(&data.characters).Error; err != nil {
			return nil, err
		}
	}

	if err := db.DB.Where("project_id = ? AND episode = ?", projectID, episode).
		Find(&data.videos).Error; err != nil {
		return nil, err
	}
	for _, video := range data.videos {
		data.videoIDs = append(data.videoIDs, video.ID)
	}

	return data, nil
}

func EpisodeResetAssets(c *gin.Context) {
	projectID := c.Param("id")

	var req episodeActionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if req.Episode <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid episode"})
		return
	}

	var project models.Project
	if err := db.DB.First(&project, projectID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Project not found"})
		return
	}

	data, err := collectEpisodeScopedData(project.ID, req.Episode)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to load episode data"})
		return
	}
	if len(data.scenes) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "No scenes found for this episode"})
		return
	}

	resetCharacters := 0
	resetScenes := 0
	resetVideos := 0

	for _, char := range data.characters {
		if strings.TrimSpace(char.GeneratedImage) != "" {
			if err := removeGeneratedAsset(char.GeneratedImage); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("Failed to remove character image: %v", err)})
				return
			}
		}
		char.GeneratedImage = ""
		char.GeneratedWorkflow = ""
		char.Status = "draft"
		char.UpdatedAt = time.Now()
		if err := db.DB.Save(&char).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update character"})
			return
		}
		BroadcastUpdate("character", char.ID)
		resetCharacters++
	}

	for _, scene := range data.scenes {
		if strings.TrimSpace(scene.GeneratedImage) != "" {
			if err := removeGeneratedAsset(scene.GeneratedImage); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("Failed to remove scene image: %v", err)})
				return
			}
		}
		scene.GeneratedImage = ""
		scene.GeneratedWorkflow = ""
		scene.Status = "draft"
		scene.UpdatedAt = time.Now()
		if err := db.DB.Save(&scene).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update scene"})
			return
		}
		BroadcastUpdate("scene", scene.ID)
		resetScenes++
	}

	for _, video := range data.videos {
		if err := resetVideoToDraft(&video); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("Failed to reset video: %v", err)})
			return
		}
		BroadcastUpdate("video", video.ID)
		resetVideos++
	}

	if err := db.DB.Transaction(func(tx *gorm.DB) error {
		var anchorMemories []models.ProjectAnchorMemory
		if err := tx.Where("project_id = ?", project.ID).Find(&anchorMemories).Error; err != nil {
			return err
		}
		for _, anchor := range anchorMemories {
			updatedRefs, firstEpisode, lastEpisode, remainingCount := removeEpisodeRef(anchor.EpisodeRefs, req.Episode)
			if remainingCount == 0 {
				if err := tx.Delete(&anchor).Error; err != nil {
					return err
				}
				continue
			}
			if err := tx.Model(&anchor).Updates(map[string]interface{}{
				"episode_refs":  updatedRefs,
				"first_episode": firstEpisode,
				"last_episode":  lastEpisode,
				"updated_at":    time.Now(),
			}).Error; err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to clear project anchor memory for this episode"})
		return
	}

	Log(LogLevelInfo, "按集重置资产", fmt.Sprintf("project=%d episode=%d characters=%d scenes=%d videos=%d", project.ID, req.Episode, resetCharacters, resetScenes, resetVideos))
	c.JSON(http.StatusOK, gin.H{
		"message":          "Episode assets reset successfully",
		"episode":          req.Episode,
		"characters_reset": resetCharacters,
		"scenes_reset":     resetScenes,
		"videos_reset":     resetVideos,
	})
}

func DeleteEpisode(c *gin.Context) {
	projectID := c.Param("id")

	var req episodeActionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if req.Episode <= 1 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Episode 1 cannot be deleted"})
		return
	}

	var project models.Project
	if err := db.DB.First(&project, projectID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Project not found"})
		return
	}

	data, err := collectEpisodeScopedData(project.ID, req.Episode)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to load episode data"})
		return
	}
	if len(data.scenes) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "No scenes found for this episode"})
		return
	}

	for _, video := range data.videos {
		if err := removeGeneratedVideoAsset(video.GeneratedVideo); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("Failed to remove video asset: %v", err)})
			return
		}
		if err := clearVideoSegments(video.ID); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("Failed to clear video segments: %v", err)})
			return
		}
	}

	for _, scene := range data.scenes {
		if err := removeGeneratedAsset(scene.GeneratedImage); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("Failed to remove scene image: %v", err)})
			return
		}
	}

	for _, char := range data.characters {
		if err := removeGeneratedAsset(char.GeneratedImage); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("Failed to remove character image: %v", err)})
			return
		}
	}

	deletedCharacters := 0
	deletedScenes := len(data.scenes)
	deletedVideos := len(data.videos)

	if err := db.DB.Transaction(func(tx *gorm.DB) error {
		if len(data.sceneIDs) > 0 {
			if err := tx.Table("shot_characters").Where("shot_id IN ?", data.sceneIDs).Delete(map[string]interface{}{}).Error; err != nil {
				return err
			}
			if err := tx.Where("id IN ?", data.sceneIDs).Delete(&models.Scene{}).Error; err != nil {
				return err
			}
		}
		if err := tx.Where("project_id = ? AND episode = ?", project.ID, req.Episode).Delete(&models.EpisodeEditorialGuide{}).Error; err != nil {
			return err
		}
		if err := tx.Where("project_id = ? AND episode = ?", project.ID, req.Episode).Delete(&models.EpisodeMemory{}).Error; err != nil {
			return err
		}
		if err := tx.Where("project_id = ? AND episode = ?", project.ID, req.Episode).Delete(&models.AutoGenerateDraft{}).Error; err != nil {
			return err
		}
		var anchorMemories []models.ProjectAnchorMemory
		if err := tx.Where("project_id = ?", project.ID).Find(&anchorMemories).Error; err != nil {
			return err
		}
		for _, anchor := range anchorMemories {
			updatedRefs, firstEpisode, lastEpisode, remainingCount := removeEpisodeRef(anchor.EpisodeRefs, req.Episode)
			if remainingCount == 0 {
				if err := tx.Delete(&anchor).Error; err != nil {
					return err
				}
				continue
			}
			if err := tx.Model(&anchor).Updates(map[string]interface{}{
				"episode_refs":  updatedRefs,
				"first_episode": firstEpisode,
				"last_episode":  lastEpisode,
				"updated_at":    time.Now(),
			}).Error; err != nil {
				return err
			}
		}

		if len(data.characterIDs) > 0 {
			var orphanIDs []uint
			for _, charID := range data.characterIDs {
				var remaining int64
				if err := tx.Table("shot_characters").
					Joins("JOIN shots ON shots.id = shot_characters.shot_id").
					Where("shot_characters.character_id = ?", charID).
					Where("shots.project_id = ?", project.ID).
					Count(&remaining).Error; err != nil {
					return err
				}
				if remaining == 0 {
					orphanIDs = append(orphanIDs, charID)
				}
			}
			if len(orphanIDs) > 0 {
				if err := tx.Where("id IN ?", orphanIDs).Delete(&models.Character{}).Error; err != nil {
					return err
				}
				deletedCharacters = len(orphanIDs)
			}
		}
		return nil
	}); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete episode"})
		return
	}

	for _, charID := range data.characterIDs {
		BroadcastUpdate("character", charID)
	}
	for _, sceneID := range data.sceneIDs {
		BroadcastUpdate("scene", sceneID)
	}
	for _, videoID := range data.videoIDs {
		BroadcastUpdate("video", videoID)
	}

	Log(LogLevelWarn, "删除整集", fmt.Sprintf("project=%d episode=%d deleted_characters=%d deleted_scenes=%d deleted_videos=%d", project.ID, req.Episode, deletedCharacters, deletedScenes, deletedVideos))
	c.JSON(http.StatusOK, gin.H{
		"message":            "Episode deleted successfully",
		"episode":            req.Episode,
		"characters_deleted": deletedCharacters,
		"scenes_deleted":     deletedScenes,
		"videos_deleted":     deletedVideos,
	})
}
