package models

import "time"

type MultiVisualProject struct {
	ID             uint      `json:"id" gorm:"primaryKey"`
	Name           string    `json:"name"`
	Code           string    `json:"code" gorm:"uniqueIndex"`
	VisualType     string    `json:"visual_type" gorm:"default:'character'"`
	Description    string    `json:"description"`
	ReferenceImage string    `json:"reference_image"`
	Status         string    `json:"status" gorm:"default:'draft'"`
	CurrentTaskID  string    `json:"current_task_id"`
	LastError      string    `json:"last_error"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

type MultiVisualImage struct {
	ID              uint      `json:"id" gorm:"primaryKey"`
	ProjectID       uint      `json:"project_id" gorm:"index"`
	SortOrder       int       `json:"sort_order" gorm:"index"`
	Label           string    `json:"label"`
	TrainingTag     string    `json:"training_tag"`
	ShotSizeLabel   string    `json:"shot_size_label"`
	ViewLabel       string    `json:"view_label"`
	HorizontalAngle int       `json:"horizontal_angle"`
	VerticalAngle   int       `json:"vertical_angle"`
	Zoom            int       `json:"zoom"`
	CameraView      string    `json:"camera_view"`
	Status          string    `json:"status" gorm:"default:'pending'"`
	GeneratedImage  string    `json:"generated_image"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}
