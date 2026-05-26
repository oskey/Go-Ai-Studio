package models

import "time"

type GeneralGuideTag struct {
	ID          uint      `json:"id" gorm:"primaryKey"`
	Name        string    `json:"name"`
	Slug        string    `json:"slug" gorm:"uniqueIndex"`
	Description string    `json:"description"`
	Rules       string    `json:"rules"`
	SortOrder   int       `json:"sort_order" gorm:"default:0"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type GeneralGuideProject struct {
	ID                      uint      `json:"id" gorm:"primaryKey"`
	Name                    string    `json:"name"`
	Code                    string    `json:"code" gorm:"uniqueIndex"`
	Description             string    `json:"description"`
	PresenterGender         string    `json:"presenter_gender" gorm:"default:'female'"`
	PresenterPersona        string    `json:"presenter_persona" gorm:"default:'female_natural'"`
	AutoGenerateContent     string    `json:"auto_generate_content"`
	TagIDsJSON              string    `json:"-" gorm:"column:tag_ids_json"`
	TagIDs                  []uint    `json:"tag_ids,omitempty" gorm:"-"`
	PresenterReferenceImage string    `json:"presenter_reference_image"`
	SelectedReferenceID     uint      `json:"selected_reference_id"`
	CurrentPlanningTaskID   string    `json:"current_planning_task_id"`
	LastPlanningError       string    `json:"last_planning_error"`
	CreatedAt               time.Time `json:"created_at"`
	UpdatedAt               time.Time `json:"updated_at"`
}

type GeneralGuideReference struct {
	ID         uint      `json:"id" gorm:"primaryKey"`
	ProjectID  uint      `json:"project_id" gorm:"index"`
	SortOrder  int       `json:"sort_order" gorm:"default:1"`
	ImagePath  string    `json:"image_path"`
	IsSelected bool      `json:"is_selected" gorm:"default:false"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

type GeneralGuideScene struct {
	ID                     uint      `json:"id" gorm:"primaryKey"`
	ProjectID              uint      `json:"project_id" gorm:"index"`
	SortOrder              int       `json:"sort_order" gorm:"default:1"`
	Title                  string    `json:"title"`
	SceneType              string    `json:"scene_type" gorm:"default:'presenter_scene'"`
	EnvironmentType        string    `json:"environment_type" gorm:"default:'indoor'"`
	NeedPresenter          bool      `json:"need_presenter" gorm:"default:true"`
	ImagePreset            string    `json:"image_preset" gorm:"default:'presenter_room_foreground'"`
	UploadHeadline         string    `json:"upload_headline"`
	UploadRequirement      string    `json:"upload_requirement"`
	IntroText              string    `json:"intro_text"`
	ReferenceImage         string    `json:"reference_image"`
	ImagePositivePrompt    string    `json:"image_positive_prompt"`
	ImageNegativePrompt    string    `json:"image_negative_prompt"`
	VideoPositivePrompt    string    `json:"video_positive_prompt"`
	VideoNegativePrompt    string    `json:"video_negative_prompt"`
	VideoDurationSeconds   int       `json:"video_duration_seconds" gorm:"default:8"`
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

type GeneralGuideTransition struct {
	ID                     uint      `json:"id" gorm:"primaryKey"`
	ProjectID              uint      `json:"project_id" gorm:"index"`
	FromSceneID            uint      `json:"from_scene_id" gorm:"index"`
	ToSceneID              uint      `json:"to_scene_id" gorm:"index"`
	FromSortOrder          int       `json:"from_sort_order" gorm:"default:1"`
	ToSortOrder            int       `json:"to_sort_order" gorm:"default:2"`
	TransitionPrompt       string    `json:"transition_prompt"`
	DurationSeconds        int       `json:"duration_seconds" gorm:"default:2"`
	FramesFromEnd          int       `json:"frames_from_end" gorm:"default:3"`
	TailFrameImage         string    `json:"tail_frame_image"`
	TailFrameSourceVideo   string    `json:"tail_frame_source_video"`
	VideoStatus            string    `json:"video_status" gorm:"default:'draft'"`
	VideoCurrentTaskID     string    `json:"video_current_task_id"`
	VideoLastError         string    `json:"video_last_error"`
	GeneratedVideo         string    `json:"generated_video"`
	VideoGeneratedWorkflow string    `json:"video_generated_workflow"`
	CreatedAt              time.Time `json:"created_at"`
	UpdatedAt              time.Time `json:"updated_at"`
}
