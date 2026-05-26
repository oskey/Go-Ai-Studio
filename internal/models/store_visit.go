package models

import "time"

type StoreVisitProject struct {
	ID                         uint      `json:"id" gorm:"primaryKey"`
	Name                       string    `json:"name"`
	Code                       string    `json:"code" gorm:"uniqueIndex"`
	Description                string    `json:"description"`
	AutoGenerateContent        string    `json:"auto_generate_content"`
	BloggerReferenceImage      string    `json:"blogger_reference_image"`
	SelectedBloggerReferenceID uint      `json:"selected_blogger_reference_id"`
	CreatedAt                  time.Time `json:"created_at"`
	UpdatedAt                  time.Time `json:"updated_at"`
}

type StoreVisitBloggerReference struct {
	ID         uint      `json:"id" gorm:"primaryKey"`
	ProjectID  uint      `json:"project_id" gorm:"index"`
	SortOrder  int       `json:"sort_order" gorm:"default:1"`
	ImagePath  string    `json:"image_path"`
	IsSelected bool      `json:"is_selected" gorm:"default:false"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

type StoreVisitSpot struct {
	ID                     uint      `json:"id" gorm:"primaryKey"`
	ProjectID              uint      `json:"project_id" gorm:"index"`
	SortOrder              int       `json:"sort_order" gorm:"default:1"`
	SpotType               string    `json:"spot_type" gorm:"default:'entrance'"`
	Name                   string    `json:"name"`
	IntroText              string    `json:"intro_text"`
	ReferenceImage         string    `json:"reference_image"`
	ImagePositivePrompt    string    `json:"image_positive_prompt"`
	ImageNegativePrompt    string    `json:"image_negative_prompt"`
	VideoPositivePrompt    string    `json:"video_positive_prompt"`
	VideoNegativePrompt    string    `json:"video_negative_prompt"`
	VideoDurationSeconds   int       `json:"video_duration_seconds" gorm:"default:10"`
	VideoWidth             int       `json:"video_width" gorm:"default:720"`
	VideoHeight            int       `json:"video_height" gorm:"default:1280"`
	ImageStatus            string    `json:"image_status" gorm:"default:'draft'"`
	ImageCurrentTaskID     string    `json:"image_current_task_id"`
	ImageLastError         string    `json:"image_last_error"`
	GeneratedImage         string    `json:"generated_image"`
	ImageGeneratedWorkflow string    `json:"image_generated_workflow"`
	VideoStatus            string    `json:"video_status" gorm:"default:'draft'"`
	VideoCurrentTaskID     string    `json:"video_current_task_id"`
	VideoLastError         string    `json:"video_last_error"`
	GeneratedVideo         string    `json:"generated_video"`
	VideoGeneratedWorkflow string    `json:"video_generated_workflow"`
	CreatedAt              time.Time `json:"created_at"`
	UpdatedAt              time.Time `json:"updated_at"`
}
