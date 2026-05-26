package models

import (
	"time"

	"gorm.io/gorm"
)

// LLMProvider represents an LLM service provider configuration
type LLMProvider struct {
	ID                          uint                   `json:"id" gorm:"primaryKey"`
	Name                        string                 `json:"name"`
	Provider                    string                 `json:"provider"` // OpenAI, Claude, etc.
	APIAddress                  string                 `json:"api_address"`
	APIKey                      string                 `json:"api_key"`
	ModelName                   string                 `json:"model_name"`
	EnableAdvancedRequestParams bool                   `json:"enable_advanced_request_params" gorm:"default:false"`
	RequestMaxTokens            int                    `json:"request_max_tokens" gorm:"default:0"`
	RequestTemperature          float32                `json:"request_temperature" gorm:"default:0"`
	IsActive                    bool                   `json:"is_active" gorm:"default:false"`
	UsageStats                  *LLMProviderUsageStats `json:"usage_stats,omitempty" gorm:"-"`
	CreatedAt                   time.Time              `json:"created_at"`
	UpdatedAt                   time.Time              `json:"updated_at"`
}

// LLMUsageBucket stores hourly usage aggregates for each provider.
type LLMUsageBucket struct {
	ID           uint      `json:"id" gorm:"primaryKey"`
	ProviderID   uint      `json:"provider_id" gorm:"uniqueIndex:idx_provider_bucket"`
	BucketStart  time.Time `json:"bucket_start" gorm:"uniqueIndex:idx_provider_bucket"`
	InputTokens  int64     `json:"input_tokens" gorm:"default:0"`
	OutputTokens int64     `json:"output_tokens" gorm:"default:0"`
	RequestCount int64     `json:"request_count" gorm:"default:0"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

type LLMUsageWindowStats struct {
	InputTokens  int64 `json:"input_tokens"`
	OutputTokens int64 `json:"output_tokens"`
	TotalTokens  int64 `json:"total_tokens"`
	RequestCount int64 `json:"request_count"`
}

type LLMUsagePoint struct {
	Label        string `json:"label"`
	InputTokens  int64  `json:"input_tokens"`
	OutputTokens int64  `json:"output_tokens"`
	TotalTokens  int64  `json:"total_tokens"`
	RequestCount int64  `json:"request_count"`
}

type LLMProviderUsageStats struct {
	Total LLMUsageWindowStats `json:"total"`
	Hour  LLMUsageWindowStats `json:"hour"`
	Day   LLMUsageWindowStats `json:"day"`
	Month LLMUsageWindowStats `json:"month"`
	Year  LLMUsageWindowStats `json:"year"`
}

type LLMUsageSummary struct {
	Total       LLMUsageWindowStats `json:"total"`
	Hour        LLMUsageWindowStats `json:"hour"`
	Day         LLMUsageWindowStats `json:"day"`
	Month       LLMUsageWindowStats `json:"month"`
	Year        LLMUsageWindowStats `json:"year"`
	HourSeries  []LLMUsagePoint     `json:"hour_series"`
	DaySeries   []LLMUsagePoint     `json:"day_series"`
	MonthSeries []LLMUsagePoint     `json:"month_series"`
	YearSeries  []LLMUsagePoint     `json:"year_series"`
	LastFlushed *time.Time          `json:"last_flushed,omitempty"`
}

// SystemLog represents a log entry
type SystemLog struct {
	ID        uint      `json:"id" gorm:"primaryKey"`
	Level     string    `json:"level"` // INFO, ERROR, WARN
	Message   string    `json:"message"`
	Details   string    `json:"details"`
	CreatedAt time.Time `json:"created_at"`
}

// LLMStreamState stores the latest live output of an LLM stage.
type LLMStreamState struct {
	ID           uint      `json:"id" gorm:"primaryKey"`
	TaskID       string    `json:"task_id" gorm:"index"`
	ProviderName string    `json:"provider_name"`
	Label        string    `json:"label"`
	Status       string    `json:"status"` // running, completed, failed
	Content      string    `json:"content"`
	CharCount    int       `json:"char_count"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// SystemSettings represents global default settings
type SystemSettings struct {
	ID          uint   `json:"id" gorm:"primaryKey"`
	Key         string `json:"key" gorm:"uniqueIndex"`
	Value       string `json:"value"`
	Description string `json:"description"`
}

// ArtStyle represents an art style configuration
type ArtStyle struct {
	ID          uint      `json:"id" gorm:"primaryKey"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// AutoGenerateTag represents a prompt rule bundle selectable during auto generation.
type AutoGenerateTag struct {
	ID          uint      `json:"id" gorm:"primaryKey"`
	Name        string    `json:"name"`
	Slug        string    `json:"slug" gorm:"uniqueIndex"`
	Description string    `json:"description"`
	Rules       string    `json:"rules"`
	SortOrder   int       `json:"sort_order" gorm:"default:0"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// Project represents a creative project
type Project struct {
	ID                      uint      `json:"id" gorm:"primaryKey"`
	Name                    string    `json:"name"`
	Code                    string    `json:"code" gorm:"uniqueIndex"` // Project folder name (English only)
	ArtStyleID              uint      `json:"art_style_id"`
	ArtStyle                ArtStyle  `json:"art_style" gorm:"foreignKey:ArtStyleID"`
	Description             string    `json:"description"`
	SceneMode               int       `json:"scene_mode" gorm:"default:1"` // Compatibility field, current generation always uses directed multi-person mode
	DisableReferenceImages  bool      `json:"disable_reference_images" gorm:"default:false"`
	IsEmpty                 bool      `json:"is_empty" gorm:"-"`
	HasAutoGenerateDraft    bool      `json:"has_auto_generate_draft" gorm:"-"`
	AutoGenerateDraftStage  string    `json:"auto_generate_draft_stage,omitempty" gorm:"-"`
	NextAutoGenerateEpisode int       `json:"next_auto_generate_episode" gorm:"-"`
	CreatedAt               time.Time `json:"created_at"`
	UpdatedAt               time.Time `json:"updated_at"`
}

type AutoGenerateDraft struct {
	ID                     uint      `json:"id" gorm:"primaryKey"`
	ProjectID              uint      `json:"project_id" gorm:"uniqueIndex"`
	CurrentTaskID          string    `json:"current_task_id"`
	Plot                   string    `json:"plot"`
	Episode                int       `json:"episode"`
	GenerationMode         string    `json:"generation_mode"`
	TagIDsJSON             string    `json:"-" gorm:"column:tag_ids_json"`
	TagIDs                 []uint    `json:"tag_ids,omitempty" gorm:"-"`
	AllowCharacterSpeech   bool      `json:"allow_character_speech"`
	SingleMode             bool      `json:"single_mode"` // Deprecated: ignored, kept only for compatibility with old drafts
	DisableReferenceImages bool      `json:"disable_reference_images"`
	Mode                   string    `json:"mode"`
	Stage                  string    `json:"stage"`
	CharactersJSON         string    `json:"characters_json"`
	OutlineJSON            string    `json:"outline_json"` // Current source of truth for first-pass episode_plan JSON
	ScenesJSON             string    `json:"scenes_json"`
	EpisodeMemoryJSON      string    `json:"episode_memory_json"`
	SceneMemoryJSON        string    `json:"scene_memory_json"`
	EpisodeSummary         string    `json:"episode_summary"`
	EditingNotes           string    `json:"editing_notes"`
	DetailedEditingNotes   string    `json:"detailed_editing_notes"`
	CompletedScenes        int       `json:"completed_scenes"`
	LastError              string    `json:"last_error"`
	CreatedAt              time.Time `json:"created_at"`
	UpdatedAt              time.Time `json:"updated_at"`
}

type EpisodeMemory struct {
	ID                  uint      `json:"id" gorm:"primaryKey"`
	ProjectID           uint      `json:"project_id" gorm:"uniqueIndex:idx_project_episode_memory"`
	Episode             int       `json:"episode" gorm:"uniqueIndex:idx_project_episode_memory"`
	StorySummary        string    `json:"story_summary"`
	EndingState         string    `json:"ending_state"`
	CharacterStatusJSON string    `json:"character_status_json"`
	OpenThreadsJSON     string    `json:"open_threads_json"`
	CreatedAt           time.Time `json:"created_at"`
	UpdatedAt           time.Time `json:"updated_at"`
}

type EpisodeEditorialGuide struct {
	ID                   uint      `json:"id" gorm:"primaryKey"`
	ProjectID            uint      `json:"project_id" gorm:"uniqueIndex:idx_project_episode_guide"`
	Episode              int       `json:"episode" gorm:"uniqueIndex:idx_project_episode_guide"`
	EpisodeSummary       string    `json:"episode_summary"`
	EditingNotes         string    `json:"editing_notes"`
	DetailedEditingNotes string    `json:"detailed_editing_notes"`
	CreatedAt            time.Time `json:"created_at"`
	UpdatedAt            time.Time `json:"updated_at"`
}

type ProjectAnchorMemory struct {
	ID           uint      `json:"id" gorm:"primaryKey"`
	ProjectID    uint      `json:"project_id" gorm:"uniqueIndex:idx_project_anchor_memory"`
	AnchorType   string    `json:"anchor_type" gorm:"uniqueIndex:idx_project_anchor_memory"`
	AnchorKey    string    `json:"anchor_key" gorm:"uniqueIndex:idx_project_anchor_memory"`
	Summary      string    `json:"summary"`
	EpisodeRefs  string    `json:"episode_refs"`
	FirstEpisode int       `json:"first_episode"`
	LastEpisode  int       `json:"last_episode"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// Character represents a character in a project
type Character struct {
	ID                uint      `json:"id" gorm:"primaryKey"`
	ProjectID         uint      `json:"project_id" gorm:"index"`
	Name              string    `json:"name"`
	Gender            string    `json:"gender"` // Male, Female, Other
	Age               string    `json:"age"`
	BodyHeight        string    `json:"body_height"`
	Era               string    `json:"era"`
	Country           string    `json:"country"`
	Appearance        string    `json:"appearance"`
	IsLocked          bool      `json:"is_locked" gorm:"default:false"`
	FaceFingerprint   string    `json:"face_fingerprint"`
	Description       string    `json:"description"` // Appearance/Attributes
	Fingerprint       string    `json:"fingerprint"` // For synthesis
	PositivePrompt    string    `json:"positive_prompt"`
	NegativePrompt    string    `json:"negative_prompt"`
	Width             int       `json:"width"`
	Height            int       `json:"height"`
	Seed              int64     `json:"seed"`
	OptimizeClothing  bool      `json:"optimize_clothing"`
	RefImage          string    `json:"ref_image"` // Path to reference image
	UseRefImage       bool      `json:"use_ref_image"`
	Status            string    `json:"status" gorm:"default:'draft'"` // draft, generated
	GeneratedImage    string    `json:"generated_image"`               // Path to generated base image
	GeneratedWorkflow string    `json:"generated_workflow"`
	CreatedAt         time.Time `json:"created_at"`
	UpdatedAt         time.Time `json:"updated_at"`
}

// Shot is the single source of truth for both scene-image and video data.
type Shot struct {
	ID                uint   `json:"id" gorm:"primaryKey"`
	ProjectID         uint   `json:"project_id" gorm:"index"`
	Episode           int    `json:"episode"`
	SceneID           int    `json:"scene_id" gorm:"index"`
	SceneNumber       int    `json:"scene_number"`
	DurationSeconds   int    `json:"duration_seconds"`
	Name              string `json:"name"`
	LocationID        string `json:"location_id"`
	OutlineRef        string `json:"outline_ref"`
	SceneGoal         string `json:"scene_goal"`
	CharacterBlocking string `json:"character_blocking"`
	CameraLock        string `json:"camera_lock"`
	Description       string `json:"description"`
	ImagePrompt       string `json:"image_prompt"`
	VideoPrompt       string `json:"video_prompt"`
	Narration         string `json:"narration"`
	BackgroundAudio   string `json:"background_audio"`
	Dialogue          string `json:"dialogue"`
	VideoFingerprint  string `json:"video_fingerprint"`

	ImagePositivePrompt    string `json:"image_positive_prompt"`
	ImageNegativePrompt    string `json:"image_negative_prompt"`
	ImageWidth             int    `json:"image_width"`
	ImageHeight            int    `json:"image_height"`
	ImageSeed              int64  `json:"image_seed"`
	ImageStatus            string `json:"image_status" gorm:"default:'draft'"`
	GeneratedImage         string `json:"generated_image"`
	ImageGeneratedWorkflow string `json:"image_generated_workflow"`

	VideoPositivePrompt    string `json:"video_positive_prompt"`
	VideoNegativePrompt    string `json:"video_negative_prompt"`
	VideoWidth             int    `json:"video_width"`
	VideoHeight            int    `json:"video_height"`
	VideoSeed              int64  `json:"video_seed"`
	VideoStatus            string `json:"video_status" gorm:"default:'draft'"`
	JMTaskID               string `json:"jm_task_id" gorm:"column:jm_task_id"`
	GeneratedVideo         string `json:"generated_video"`
	VideoGeneratedWorkflow string `json:"video_generated_workflow"`

	Characters []Character `json:"characters" gorm:"many2many:shot_characters;foreignKey:ID;joinForeignKey:ShotID;References:ID;joinReferences:CharacterID"`
	CreatedAt  time.Time   `json:"created_at"`
	UpdatedAt  time.Time   `json:"updated_at"`
}

func (Shot) TableName() string {
	return "shots"
}

type ShotCharacter struct {
	ShotID      uint `json:"shot_id" gorm:"primaryKey;index"`
	CharacterID uint `json:"character_id" gorm:"primaryKey;index"`
}

func (ShotCharacter) TableName() string {
	return "shot_characters"
}

// Scene is the scene-image view of a shot row.
type Scene struct {
	ID                uint        `json:"id" gorm:"primaryKey"`
	ProjectID         uint        `json:"project_id" gorm:"index"`
	Episode           int         `json:"episode"`
	SceneID           int         `json:"scene_id" gorm:"index"`
	SceneNumber       int         `json:"scene_number"`
	DurationSeconds   int         `json:"duration_seconds"`
	Name              string      `json:"name"`
	LocationID        string      `json:"location_id"`
	OutlineRef        string      `json:"outline_ref"`
	SceneGoal         string      `json:"scene_goal"`
	CharacterBlocking string      `json:"character_blocking"`
	CameraLock        string      `json:"camera_lock"`
	Description       string      `json:"description"`
	ImagePrompt       string      `json:"image_prompt"`
	VideoPrompt       string      `json:"video_prompt"`
	Narration         string      `json:"narration"`
	BackgroundAudio   string      `json:"background_audio"`
	Dialogue          string      `json:"dialogue"`
	Fingerprint       string      `json:"-" gorm:"-"`
	VideoFingerprint  string      `json:"video_fingerprint"`
	PositivePrompt    string      `json:"positive_prompt" gorm:"column:image_positive_prompt"`
	NegativePrompt    string      `json:"negative_prompt" gorm:"column:image_negative_prompt"`
	Width             int         `json:"width" gorm:"column:image_width"`
	Height            int         `json:"height" gorm:"column:image_height"`
	Seed              int64       `json:"seed" gorm:"column:image_seed"`
	Status            string      `json:"status" gorm:"column:image_status"`
	GeneratedImage    string      `json:"generated_image" gorm:"column:generated_image"`
	GeneratedWorkflow string      `json:"generated_workflow" gorm:"column:image_generated_workflow"`
	Characters        []Character `json:"characters" gorm:"many2many:shot_characters;foreignKey:ID;joinForeignKey:ShotID;References:ID;joinReferences:CharacterID"`
	CreatedAt         time.Time   `json:"created_at"`
	UpdatedAt         time.Time   `json:"updated_at"`
}

func (Scene) TableName() string {
	return "shots"
}

// Video is the video-generation view of a shot row.
type Video struct {
	ID        uint  `json:"id" gorm:"primaryKey"`
	ProjectID uint  `json:"project_id" gorm:"index"`
	SceneID   uint  `json:"scene_id" gorm:"-"`
	Scene     Scene `json:"scene" gorm:"-"`

	Narration         string         `json:"narration"`
	BackgroundAudio   string         `json:"background_audio"`
	Dialogue          string         `json:"dialogue"`
	Fingerprint       string         `json:"-" gorm:"-"`
	VideoFingerprint  string         `json:"video_fingerprint"`
	VideoPrompt       string         `json:"video_prompt"`
	DurationSeconds   int            `json:"duration_seconds"`
	Width             int            `json:"width" gorm:"column:video_width"`
	Height            int            `json:"height" gorm:"column:video_height"`
	Seed              int64          `json:"seed" gorm:"column:video_seed"`
	PositivePrompt    string         `json:"positive_prompt" gorm:"column:video_positive_prompt"`
	NegativePrompt    string         `json:"negative_prompt" gorm:"column:video_negative_prompt"`
	Status            string         `json:"status" gorm:"column:video_status"`
	JMTaskID          string         `json:"jm_task_id" gorm:"column:jm_task_id"`
	GeneratedVideo    string         `json:"generated_video" gorm:"column:generated_video"`
	GeneratedWorkflow string         `json:"generated_workflow" gorm:"column:video_generated_workflow"`
	Segments          []VideoSegment `json:"segments" gorm:"foreignKey:VideoID;references:ID;constraint:OnDelete:CASCADE;"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (Video) TableName() string {
	return "shots"
}

func (v *Video) AfterFind(tx *gorm.DB) error {
	v.SceneID = v.ID
	return nil
}

// VideoSegment represents a generated sub-segment of a long video.
type VideoSegment struct {
	ID      uint  `json:"id" gorm:"primaryKey"`
	VideoID uint  `json:"video_id" gorm:"index"`
	Video   Video `json:"-" gorm:"foreignKey:VideoID;references:ID"`

	SegmentIndex    int `json:"segment_index"`
	StartSecond     int `json:"start_second"`
	EndSecond       int `json:"end_second"`
	DurationSeconds int `json:"duration_seconds"`
	FPS             int `json:"fps"`
	Length          int `json:"length"`

	PositivePrompt string `json:"positive_prompt"`
	NegativePrompt string `json:"negative_prompt"`
	PlayerDesc     string `json:"player_desc"`

	Status          string `json:"status" gorm:"default:'pending'"`
	GeneratedVideo  string `json:"generated_video"`
	TransitionFrame string `json:"transition_frame"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// WorkflowMetadata represents parsed information from a ComfyUI workflow JSON
type WorkflowMetadata struct {
	FilePath     string `json:"file_path"`
	FileName     string `json:"file_name"`     // e.g., "video_ltx2_i2v.json"
	WorkflowName string `json:"workflow_name"` // Extracted from filename_prefix
	Type         string `json:"type"`          // "image" or "video"

	// Node IDs for key parameters
	PositiveNodeID string `json:"positive_node_id"`
	NegativeNodeID string `json:"negative_node_id"`

	// Input keys for prompts (usually "text" or "string")
	PositiveInputKey string `json:"positive_input_key"`
	NegativeInputKey string `json:"negative_input_key"`

	SeedNodeID   string `json:"seed_node_id"`
	WidthNodeID  string `json:"width_node_id"`
	HeightNodeID string `json:"height_node_id"`
	FPSNodeID    string `json:"fps_node_id"`
	LengthNodeID string `json:"length_node_id"` // Frame count

	// Input keys (in case they vary, though usually standard)
	SeedInputKey   string `json:"seed_input_key"`   // usually "seed" or "noise_seed"
	WidthInputKey  string `json:"width_input_key"`  // usually "width"
	HeightInputKey string `json:"height_input_key"` // usually "height"
	FPSInputKey    string `json:"fps_input_key"`    // usually "fps"
	LengthInputKey string `json:"length_input_key"` // usually "frame_count"
}

// ModelDependency represents a model file dependency in a workflow
type ModelDependency struct {
	FileName  string `json:"file_name"`
	Type      string `json:"type"` // checkpoint, lora, vae, etc.
	NodeID    string `json:"node_id"`
	ClassType string `json:"class_type"`
}

// ComfyNode represents a node in the workflow JSON
type ComfyNode struct {
	Inputs    map[string]interface{} `json:"inputs"`
	ClassType string                 `json:"class_type"`
	Meta      map[string]interface{} `json:"_meta"`
}

// WorkflowJSON represents the entire workflow file structure
type WorkflowJSON map[string]ComfyNode

// AutoGenerateRequest defines the payload for auto-generating characters and scenes
type AutoGenerateRequest struct {
	Plot                   string `json:"plot" binding:"required"`
	Episode                int    `json:"episode" binding:"required,min=1"`
	GenerationMode         string `json:"generation_mode"`
	TagIDs                 []uint `json:"tag_ids"`
	AllowCharacterSpeech   bool   `json:"allow_character_speech"`
	MaxCharacters          int    `json:"max_characters"` // Deprecated: auto-determined by plot
	MaxScenes              int    `json:"max_scenes"`     // Deprecated: scene count is planned by outline
	SingleMode             bool   `json:"single_mode"`    // Deprecated: ignored, kept only for compatibility with old requests
	DisableReferenceImages bool   `json:"disable_reference_images"`
	Mode                   string `json:"mode"` // Deprecated: inferred by server from whether the project already has content
}

type ImportStoryJSONRequest struct {
	Episode   int    `json:"episode" binding:"required,min=1"`
	StoryJSON string `json:"story_json" binding:"required"`
}

// Task represents an asynchronous background task
type Task struct {
	ID        string    `json:"id" gorm:"primaryKey"` // UUID
	Type      string    `json:"type"`                 // e.g., "auto_generate_project"
	Status    string    `json:"status"`               // pending, running, completed, failed
	Progress  int       `json:"progress"`             // 0-100
	Result    string    `json:"result"`               // JSON result or message
	Error     string    `json:"error"`                // Error message
	Payload   string    `json:"payload"`              // JSON payload input
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}
