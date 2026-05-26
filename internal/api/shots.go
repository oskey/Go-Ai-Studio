package api

import (
	"fmt"

	"kt-ai-studio/internal/db"
	"kt-ai-studio/internal/models"
)

func loadSceneViewByShotID(shotID uint, preloadCharacters bool) (models.Scene, error) {
	query := db.DB
	if preloadCharacters {
		query = query.Preload("Characters")
	}

	var scene models.Scene
	if err := query.First(&scene, shotID).Error; err != nil {
		return models.Scene{}, err
	}
	return scene, nil
}

func hydrateVideoScene(video *models.Video, preloadCharacters bool) error {
	if video == nil {
		return fmt.Errorf("video is nil")
	}
	if video.ID == 0 {
		return fmt.Errorf("video is not linked to a shot")
	}
	if video.Scene.ID == video.ID && (!preloadCharacters || video.Scene.Characters != nil) {
		video.SceneID = video.ID
		return nil
	}

	scene, err := loadSceneViewByShotID(video.ID, preloadCharacters)
	if err != nil {
		return err
	}
	video.SceneID = video.ID
	video.Scene = scene
	return nil
}

func hydrateVideoScenes(videos []models.Video, preloadCharacters bool) error {
	if len(videos) == 0 {
		return nil
	}

	ids := make([]uint, 0, len(videos))
	for _, video := range videos {
		if video.ID == 0 {
			continue
		}
		ids = append(ids, video.ID)
	}
	if len(ids) == 0 {
		return nil
	}

	query := db.DB.Where("id IN ?", ids)
	if preloadCharacters {
		query = query.Preload("Characters")
	}

	var scenes []models.Scene
	if err := query.Find(&scenes).Error; err != nil {
		return err
	}

	sceneByID := make(map[uint]models.Scene, len(scenes))
	for _, scene := range scenes {
		sceneByID[scene.ID] = scene
	}

	for i := range videos {
		videos[i].SceneID = videos[i].ID
		if scene, ok := sceneByID[videos[i].ID]; ok {
			videos[i].Scene = scene
		}
	}
	return nil
}
