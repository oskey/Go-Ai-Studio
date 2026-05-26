package task

import (
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"kt-ai-studio/internal/db"
	"kt-ai-studio/internal/errfmt"
	"kt-ai-studio/internal/models"

	"github.com/google/uuid"
)

// TaskManager manages background tasks
type TaskManager struct {
	tasks        map[string]*models.Task
	mu           sync.RWMutex
	worker       chan *models.Task
	llmWorker    chan *models.Task
	renderWorker chan *models.Task
	audioWorker  chan *models.Task
	handlers     map[string]func(*models.Task) (interface{}, error)
}

var GlobalTaskManager *TaskManager

const (
	defaultWorkerConcurrency    = 1
	defaultLLMWorkerConcurrency = 2
	defaultRenderConcurrency    = 12
	defaultAudioConcurrency     = 32
)

// InitTaskManager initializes the global task manager
func InitTaskManager() {
	GlobalTaskManager = &TaskManager{
		tasks:        make(map[string]*models.Task),
		worker:       make(chan *models.Task, 100), // Buffer for 100 tasks
		llmWorker:    make(chan *models.Task, 100),
		renderWorker: make(chan *models.Task, 100),
		audioWorker:  make(chan *models.Task, 200),
		handlers:     make(map[string]func(*models.Task) (interface{}, error)),
	}

	// Start workers
	for i := 0; i < defaultWorkerConcurrency; i++ {
		go GlobalTaskManager.processQueue(GlobalTaskManager.worker)
	}
	for i := 0; i < defaultLLMWorkerConcurrency; i++ {
		go GlobalTaskManager.processQueue(GlobalTaskManager.llmWorker)
	}
	for i := 0; i < defaultRenderConcurrency; i++ {
		go GlobalTaskManager.processQueue(GlobalTaskManager.renderWorker)
	}
	for i := 0; i < defaultAudioConcurrency; i++ {
		go GlobalTaskManager.processQueue(GlobalTaskManager.audioWorker)
	}
}

// ClearAllTasks clears all tasks from memory and DB
func (tm *TaskManager) ClearAllTasks() error {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	// Clear memory map
	tm.tasks = make(map[string]*models.Task)

	// Drain channel (non-blocking)
loop:
	for {
		select {
		case <-tm.worker:
		case <-tm.llmWorker:
		case <-tm.renderWorker:
		case <-tm.audioWorker:
		default:
			break loop
		}
	}

	// Clear DB
	return db.DB.Exec("DELETE FROM tasks").Error
}

// RegisterHandler registers a handler function for a specific task type
func (tm *TaskManager) RegisterHandler(taskType string, handler func(*models.Task) (interface{}, error)) {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	tm.handlers[taskType] = handler
}

// AddTask adds a new task to the queue
func (tm *TaskManager) AddTask(taskType string, payload interface{}) (*models.Task, error) {
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	task := &models.Task{
		ID:        uuid.New().String(),
		Type:      taskType,
		Status:    "pending",
		Progress:  0,
		Payload:   string(payloadBytes),
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	// Save to DB
	if err := db.RetryOnBusy(func() error {
		return db.DB.Create(task).Error
	}); err != nil {
		return nil, err
	}

	tm.mu.Lock()
	tm.tasks[task.ID] = task
	tm.mu.Unlock()

	// Push to channel
	fmt.Printf("Task queued: %s (%s)\n", task.ID, task.Type)
	if isAudioTaskType(task.Type) {
		tm.audioWorker <- task
	} else if isRenderTaskType(task.Type) {
		tm.renderWorker <- task
	} else if isLLMTaskType(task.Type) {
		tm.llmWorker <- task
	} else {
		tm.worker <- task
	}

	return task, nil
}

// processQueue handles tasks from the channel
func (tm *TaskManager) processQueue(queue <-chan *models.Task) {
	for task := range queue {
		fmt.Printf("Task dequeued: %s (%s)\n", task.ID, task.Type)
		tm.executeTask(task)
	}
}

func isRenderTaskType(taskType string) bool {
	switch taskType {
	case "batch_generate_characters", "batch_generate_scenes", "batch_generate_videos", "render_video", "render_video_segments", "render_video_from_segment", "render_multi_visual_project", "render_store_visit_spot_image", "render_store_visit_spot_video", "render_store_visit_dish_generation", "batch_generate_store_visit_project_images", "batch_generate_store_visit_project_videos", "render_general_guide_scene_image", "render_general_guide_scene_video", "render_general_guide_transition_video":
		return true
	default:
		return false
	}
}

func isAudioTaskType(taskType string) bool {
	switch taskType {
	case "render_audio_clone_line", "recognize_audio_clone_character_reference", "render_qwen_tts_line", "recognize_qwen_tts_character_reference", "render_audio_production_line":
		return true
	default:
		return false
	}
}

func isLLMTaskType(taskType string) bool {
	switch taskType {
	case "auto_generate_project", "continue_auto_generate_project", "auto_generate_character_prompt", "repair_scene_video_prompts", "auto_generate_store_visit_project", "plan_general_guide_project":
		return true
	default:
		return false
	}
}

// executeTask runs the task logic
func (tm *TaskManager) executeTask(task *models.Task) {
	defer func() {
		if r := recover(); r != nil {
			tm.UpdateTaskStatus(task.ID, "failed", 0, fmt.Sprintf("task panic: %v", r))
		}
	}()

	// Update status to running
	fmt.Printf("Task running: %s (%s)\n", task.ID, task.Type)
	tm.UpdateTaskStatus(task.ID, "running", 0, "")

	var err error
	var result interface{}

	// Route based on Task Type
	var handler func(*models.Task) (interface{}, error)
	var ok bool

	tm.mu.RLock()
	handler, ok = tm.handlers[task.Type]
	tm.mu.RUnlock()

	if !ok {
		err = fmt.Errorf("no handler for task type: %s", task.Type)
	} else {
		// Execute handler
		result, err = handler(task)
	}

	if err != nil {
		tm.UpdateTaskStatus(task.ID, "failed", 0, err.Error())
	} else {
		// Convert result to JSON string if needed
		var resStr string
		if s, ok := result.(string); ok {
			resStr = s
		} else {
			b, _ := json.Marshal(result)
			resStr = string(b)
		}
		tm.UpdateTaskStatus(task.ID, "completed", 100, resStr)
	}
}

// UpdateTaskProgress updates the task progress and result (used for streaming logs)
func (tm *TaskManager) UpdateTaskProgress(taskID string, progress int, partialResult string) {
	tm.mu.Lock()
	task, ok := tm.tasks[taskID]
	if !ok {
		tm.mu.Unlock()
		return
	}

	if task.Status == "completed" || task.Status == "failed" {
		tm.mu.Unlock()
		return
	}

	task.Progress = progress
	if partialResult != "" {
		task.Result = partialResult
	}
	task.UpdatedAt = time.Now()

	// Copy values for async DB update
	status := task.Status
	result := task.Result
	taskErr := task.Error
	updatedAt := task.UpdatedAt
	tm.mu.Unlock()

	// Keep DB writes synchronous to avoid stale running/progress updates overwriting completion state.
	tm.updateTaskInDB(taskID, status, progress, result, taskErr, updatedAt)
}

// updateTaskInDB updates the task in the database
// Note: This method is now safe to call from a goroutine as it receives copied values
func (tm *TaskManager) updateTaskInDB(taskID string, status string, progress int, result string, taskErr string, updatedAt time.Time) {
	// Use passed values instead of accessing shared task pointer to avoid race conditions
	if err := db.RetryOnBusy(func() error {
		return db.DB.Model(&models.Task{}).Where("id = ?", taskID).Updates(map[string]interface{}{
			"status":     status,
			"progress":   progress,
			"result":     result,
			"error":      taskErr,
			"updated_at": updatedAt,
		}).Error
	}); err != nil {
		fmt.Printf("Failed to update task %s: %v\n", taskID, err)
	}
}

// UpdateTaskStatus updates task status in DB and Memory
func (tm *TaskManager) UpdateTaskStatus(taskID string, status string, progress int, message string) {
	tm.mu.Lock()

	task, ok := tm.tasks[taskID]
	if !ok {
		// If not in memory (e.g. restart), try load from DB
		var dbTask models.Task
		if err := db.DB.First(&dbTask, "id = ?", taskID).Error; err == nil {
			task = &dbTask
			tm.tasks[taskID] = task
		} else {
			tm.mu.Unlock()
			return
		}
	}

	task.Status = status
	task.Progress = progress
	if status == "failed" {
		task.Error = errfmt.NormalizeUserFacingError(message, 260)
	} else if status == "completed" {
		task.Result = message
	}
	task.UpdatedAt = time.Now()

	// Copy values for async DB update
	result := task.Result
	taskErr := task.Error
	updatedAt := task.UpdatedAt

	tm.mu.Unlock()

	// Keep DB writes synchronous to avoid stale state regression.
	tm.updateTaskInDB(taskID, status, progress, result, taskErr, updatedAt)
}

// GetTasks returns list of tasks (optional filter)
func (tm *TaskManager) GetTasks(limit int) []models.Task {
	var tasks []models.Task
	db.DB.Order("created_at desc").Limit(limit).Find(&tasks)
	return tasks
}

// GetTask returns a single task
func (tm *TaskManager) GetTask(id string) *models.Task {
	tm.mu.RLock()
	defer tm.mu.RUnlock()
	if task, ok := tm.tasks[id]; ok {
		return task
	}
	var task models.Task
	if err := db.DB.First(&task, "id = ?", id).Error; err == nil {
		return &task
	}
	return nil
}

// HasRunningTasks checks if there are any tasks currently in running state
func (tm *TaskManager) HasRunningTasks() bool {
	tm.mu.RLock()
	defer tm.mu.RUnlock()
	for _, task := range tm.tasks {
		if task.Status == "running" || task.Status == "in_progress" {
			return true
		}
	}
	return false
}
