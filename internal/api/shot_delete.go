package api

import (
	"fmt"

	"kt-ai-studio/internal/db"
	"kt-ai-studio/internal/models"

	"gorm.io/gorm"
)

func deleteShotWithAssets(shotID uint) error {
	if shotID == 0 {
		return fmt.Errorf("invalid shot id")
	}

	var scene models.Scene
	if err := db.DB.Preload("Characters").First(&scene, shotID).Error; err != nil {
		return err
	}

	var video models.Video
	if err := db.DB.First(&video, shotID).Error; err != nil && err != gorm.ErrRecordNotFound {
		return err
	}

	if err := removeGeneratedAsset(scene.GeneratedImage); err != nil {
		return err
	}
	if err := removeGeneratedVideoAsset(video.GeneratedVideo); err != nil {
		return err
	}
	if err := clearVideoSegments(shotID); err != nil {
		return err
	}

	return db.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Table("shot_characters").Where("shot_id = ?", shotID).Delete(map[string]interface{}{}).Error; err != nil {
			return err
		}
		if err := tx.Delete(&models.Scene{}, shotID).Error; err != nil {
			return err
		}
		return nil
	})
}
