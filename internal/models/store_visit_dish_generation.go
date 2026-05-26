package models

import "time"

type StoreVisitDishGenerationItem struct {
	ID                     uint      `json:"id" gorm:"primaryKey"`
	ProjectID              uint      `json:"project_id" gorm:"index"`
	SpotID                 uint      `json:"spot_id" gorm:"index"`
	SortOrder              int       `json:"sort_order" gorm:"default:1"`
	PresetKey              string    `json:"preset_key" gorm:"default:'cinematic_reveal'"`
	SegmentDurationSeconds float64   `json:"segment_duration_seconds" gorm:"default:2"`
	Prompt1                string    `json:"prompt1"`
	Prompt2                string    `json:"prompt2"`
	Prompt3                string    `json:"prompt3"`
	Prompt4                string    `json:"prompt4"`
	Prompt5                string    `json:"prompt5"`
	FramesJSON             string    `json:"-" gorm:"type:text"`
	SegmentsJSON           string    `json:"-" gorm:"type:text"`
	Frame1Image            string    `json:"frame1_image"`
	Frame2Image            string    `json:"frame2_image"`
	Frame3Image            string    `json:"frame3_image"`
	Frame4Image            string    `json:"frame4_image"`
	Frame5Image            string    `json:"frame5_image"`
	Frame6Image            string    `json:"frame6_image"`
	VideoStatus            string    `json:"video_status" gorm:"default:'draft'"`
	VideoCurrentTaskID     string    `json:"video_current_task_id"`
	VideoLastError         string    `json:"video_last_error"`
	GeneratedVideo         string    `json:"generated_video"`
	VideoGeneratedWorkflow string    `json:"video_generated_workflow"`
	CreatedAt              time.Time `json:"created_at"`
	UpdatedAt              time.Time `json:"updated_at"`
}
