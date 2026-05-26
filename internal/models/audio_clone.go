package models

import "time"

type AudioCloneProject struct {
	ID          uint      `json:"id" gorm:"primaryKey"`
	Name        string    `json:"name"`
	Code        string    `json:"code" gorm:"uniqueIndex"`
	Description string    `json:"description"`
	ScriptText  string    `json:"script_text"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type AudioCloneCharacter struct {
	ID                         uint      `json:"id" gorm:"primaryKey"`
	ProjectID                  uint      `json:"project_id" gorm:"index"`
	SortOrder                  int       `json:"sort_order" gorm:"default:1"`
	Name                       string    `json:"name" gorm:"index"`
	ReferenceAudio             string    `json:"reference_audio"`
	ReferenceText              string    `json:"reference_text"`
	ReferenceTextStatus        string    `json:"reference_text_status" gorm:"default:'draft'"`
	ReferenceTextCurrentTaskID string    `json:"reference_text_current_task_id"`
	ReferenceTextError         string    `json:"reference_text_error"`
	CreatedAt                  time.Time `json:"created_at"`
	UpdatedAt                  time.Time `json:"updated_at"`
}

type AudioCloneLine struct {
	ID                uint      `json:"id" gorm:"primaryKey"`
	ProjectID         uint      `json:"project_id" gorm:"index"`
	SortOrder         int       `json:"sort_order" gorm:"default:1"`
	CharacterName     string    `json:"character_name" gorm:"index"`
	Text              string    `json:"text"`
	Seed              int64     `json:"seed"`
	Status            string    `json:"status" gorm:"default:'draft'"`
	CurrentTaskID     string    `json:"current_task_id"`
	LastError         string    `json:"last_error"`
	GeneratedAudio    string    `json:"generated_audio"`
	GeneratedWorkflow string    `json:"generated_workflow"`
	CreatedAt         time.Time `json:"created_at"`
	UpdatedAt         time.Time `json:"updated_at"`
}

type QwenTTSProject struct {
	ID                     uint      `json:"id" gorm:"primaryKey"`
	Name                   string    `json:"name"`
	Code                   string    `json:"code" gorm:"uniqueIndex"`
	Description            string    `json:"description"`
	ScriptText             string    `json:"script_text"`
	Instruct               string    `json:"instruct"`
	Temperature            float64   `json:"temperature" gorm:"default:0.5"`
	XVectorOnly            bool      `json:"x_vector_only" gorm:"default:false"`
	AutoParseSourceText    string    `json:"auto_parse_source_text"`
	AutoParsePartialJSON   string    `json:"auto_parse_partial_json"`
	AutoParseReturnedCount int       `json:"auto_parse_returned_count" gorm:"default:0"`
	AutoParseTotal         int       `json:"auto_parse_total" gorm:"default:0"`
	LastAutoParseTaskID    string    `json:"last_auto_parse_task_id"`
	LastAutoParseError     string    `json:"last_auto_parse_error"`
	CreatedAt              time.Time `json:"created_at"`
	UpdatedAt              time.Time `json:"updated_at"`
}

type QwenTTSCharacter struct {
	ID                         uint      `json:"id" gorm:"primaryKey"`
	ProjectID                  uint      `json:"project_id" gorm:"index"`
	SortOrder                  int       `json:"sort_order" gorm:"default:1"`
	Name                       string    `json:"name" gorm:"index"`
	ReferenceAudio             string    `json:"reference_audio"`
	ReferenceText              string    `json:"reference_text"`
	ReferenceTextStatus        string    `json:"reference_text_status" gorm:"default:'draft'"`
	ReferenceTextCurrentTaskID string    `json:"reference_text_current_task_id"`
	ReferenceTextError         string    `json:"reference_text_error"`
	ReferenceTestAudio         string    `json:"reference_test_audio"`
	CreatedAt                  time.Time `json:"created_at"`
	UpdatedAt                  time.Time `json:"updated_at"`
}

type QwenTTSLine struct {
	ID                uint      `json:"id" gorm:"primaryKey"`
	ProjectID         uint      `json:"project_id" gorm:"index"`
	SortOrder         int       `json:"sort_order" gorm:"default:1"`
	CharacterName     string    `json:"character_name" gorm:"index"`
	Text              string    `json:"text"`
	Instruct          string    `json:"instruct"`
	Temperature       float64   `json:"temperature" gorm:"default:0.5"`
	Seed              int64     `json:"seed"`
	Status            string    `json:"status" gorm:"default:'draft'"`
	CurrentTaskID     string    `json:"current_task_id"`
	LastError         string    `json:"last_error"`
	GeneratedAudio    string    `json:"generated_audio"`
	GeneratedWorkflow string    `json:"generated_workflow"`
	CreatedAt         time.Time `json:"created_at"`
	UpdatedAt         time.Time `json:"updated_at"`
}

type AudioProductionProject struct {
	ID               uint      `json:"id" gorm:"primaryKey"`
	Mode             string    `json:"mode" gorm:"index"`
	Name             string    `json:"name"`
	Code             string    `json:"code" gorm:"uniqueIndex"`
	Description      string    `json:"description"`
	Text             string    `json:"text"`
	Speaker          string    `json:"speaker"`
	Instruct         string    `json:"instruct"`
	VoiceInstruction string    `json:"voice_instruction"`
	Temperature      float64   `json:"temperature" gorm:"default:0.7"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

type AudioProductionLine struct {
	ID                uint      `json:"id" gorm:"primaryKey"`
	ProjectID         uint      `json:"project_id" gorm:"index"`
	SortOrder         int       `json:"sort_order" gorm:"default:1"`
	Text              string    `json:"text"`
	Speaker           string    `json:"speaker"`
	Instruct          string    `json:"instruct"`
	VoiceInstruction  string    `json:"voice_instruction"`
	Temperature       float64   `json:"temperature" gorm:"default:0.7"`
	Status            string    `json:"status" gorm:"default:'draft'"`
	CurrentTaskID     string    `json:"current_task_id"`
	LastError         string    `json:"last_error"`
	GeneratedAudio    string    `json:"generated_audio"`
	GeneratedWorkflow string    `json:"generated_workflow"`
	CreatedAt         time.Time `json:"created_at"`
	UpdatedAt         time.Time `json:"updated_at"`
}
