package main

import (
	"bytes"
	"context"
	"embed"
	"log"
	"mime"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"kt-ai-studio/internal/api"
	"kt-ai-studio/internal/config"
	"kt-ai-studio/internal/db"
	"kt-ai-studio/internal/models"
	"kt-ai-studio/internal/task"
	"kt-ai-studio/internal/workflow"

	"github.com/gin-gonic/gin"
)

//go:embed all:frontend/dist
var frontendDist embed.FS

func main() {
	// 1. 加载配置
	cfg, err := config.LoadConfig()
	if err != nil {
		log.Fatalf("无法加载配置: %v", err)
	}

	// 2. 初始化数据库
	db.InitDB(cfg)

	// 初始化默认画风 (如果表为空)
	api.InitDefaultArtStyles()
	api.InitDefaultAutoGenerateTags()

	// 初始化默认系统设置
	api.InitDefaultSettings()
	api.CleanTemporaryVideoExportFiles()

	// 3. 解析工作流
	workflowDir := "workflows"
	files, err := filepath.Glob(filepath.Join(workflowDir, "*.json"))
	if err != nil {
		log.Printf("查找工作流文件时出错: %v", err)
	}

	parsedWorkflows := make([]models.WorkflowMetadata, 0)
	for _, file := range files {
		meta, err := workflow.ParseWorkflow(file)
		if err != nil {
			log.Printf("解析工作流 %s 时出错: %v", file, err)
			continue
		}
		parsedWorkflows = append(parsedWorkflows, *meta)
		log.Printf("已解析工作流: %s (类型: %s)", meta.WorkflowName, meta.Type)

		// TODO: 暂时保存在内存中，后续可能需要保存到数据库
		// 用户希望在系统设置 -> 模型选择下拉菜单中使用这些数据
	}

	// Initialize Task Manager
	task.InitTaskManager()

	// Initialize System Monitor
	api.InitSystemMonitor()
	api.InitLLMUsageTracker()

	// Register Task Handlers
	task.GlobalTaskManager.RegisterHandler("auto_generate_project", api.HandleAutoGenerateProjectTask)
	task.GlobalTaskManager.RegisterHandler("continue_auto_generate_project", api.HandleAutoGenerateProjectTask)
	task.GlobalTaskManager.RegisterHandler("auto_generate_character_prompt", api.HandleAutoGenerateCharacterPromptTask)
	task.GlobalTaskManager.RegisterHandler("batch_generate_characters", api.HandleBatchGenerateCharactersTask)
	task.GlobalTaskManager.RegisterHandler("batch_generate_scenes", api.HandleBatchGenerateScenesTask)
	task.GlobalTaskManager.RegisterHandler("batch_generate_characters_scenes", api.HandleBatchGenerateCharactersAndScenesTask)
	task.GlobalTaskManager.RegisterHandler("batch_generate_all_media", api.HandleBatchGenerateAllMediaTask)
	task.GlobalTaskManager.RegisterHandler("render_video", api.HandleRenderVideoTask)
	task.GlobalTaskManager.RegisterHandler("render_video_segments", api.HandleRenderVideoSegmentsTask)
	task.GlobalTaskManager.RegisterHandler("render_video_from_segment", api.HandleRenderVideoFromSegmentTask)
	task.GlobalTaskManager.RegisterHandler("batch_generate_videos", api.HandleBatchGenerateVideosTask)
	task.GlobalTaskManager.RegisterHandler("repair_scene_video_prompts", api.HandleRepairSceneVideoPromptsTask)
	task.GlobalTaskManager.RegisterHandler("render_multi_visual_project", api.HandleRenderMultiVisualProjectTask)
	task.GlobalTaskManager.RegisterHandler("render_store_visit_spot_image", api.HandleRenderStoreVisitSpotImageTask)
	task.GlobalTaskManager.RegisterHandler("render_store_visit_spot_video", api.HandleRenderStoreVisitSpotVideoTask)
	task.GlobalTaskManager.RegisterHandler("render_store_visit_dish_generation", api.HandleRenderStoreVisitDishGenerationTask)
	task.GlobalTaskManager.RegisterHandler("auto_generate_store_visit_project", api.HandleAutoGenerateStoreVisitProjectTask)
	task.GlobalTaskManager.RegisterHandler("batch_generate_store_visit_project_images", api.HandleBatchGenerateStoreVisitProjectImagesTask)
	task.GlobalTaskManager.RegisterHandler("batch_generate_store_visit_project_videos", api.HandleBatchGenerateStoreVisitProjectVideosTask)
	task.GlobalTaskManager.RegisterHandler("plan_general_guide_project", api.HandlePlanGeneralGuideProjectTask)
	task.GlobalTaskManager.RegisterHandler("batch_generate_general_guide_project_images", api.HandleBatchGenerateGeneralGuideProjectImagesTask)
	task.GlobalTaskManager.RegisterHandler("batch_generate_general_guide_project_videos", api.HandleBatchGenerateGeneralGuideProjectVideosTask)
	task.GlobalTaskManager.RegisterHandler("batch_generate_general_guide_project_transitions", api.HandleBatchGenerateGeneralGuideProjectTransitionsTask)
	task.GlobalTaskManager.RegisterHandler("batch_generate_general_guide_project_images_and_videos", api.HandleBatchGenerateGeneralGuideProjectImagesAndVideosTask)
	task.GlobalTaskManager.RegisterHandler("batch_generate_general_guide_project_images_videos_and_transitions", api.HandleBatchGenerateGeneralGuideProjectImagesVideosAndTransitionsTask)
	task.GlobalTaskManager.RegisterHandler("render_general_guide_scene_image", api.HandleRenderGeneralGuideSceneImageTask)
	task.GlobalTaskManager.RegisterHandler("render_general_guide_scene_video", api.HandleRenderGeneralGuideSceneVideoTask)
	task.GlobalTaskManager.RegisterHandler("render_general_guide_transition_video", api.HandleRenderGeneralGuideTransitionVideoTask)
	task.GlobalTaskManager.RegisterHandler("render_audio_clone_line", api.HandleRenderAudioCloneLineTask)
	task.GlobalTaskManager.RegisterHandler("recognize_audio_clone_character_reference", api.HandleRecognizeAudioCloneCharacterReferenceTask)
	task.GlobalTaskManager.RegisterHandler("render_qwen_tts_line", api.HandleRenderQwenTTSLineTask)
	task.GlobalTaskManager.RegisterHandler("recognize_qwen_tts_character_reference", api.HandleRecognizeQwenTTSCharacterReferenceTask)
	task.GlobalTaskManager.RegisterHandler("render_audio_production_line", api.HandleRenderAudioProductionLineTask)

	// 4. 设置路由
	r := gin.Default()

	// Serve static files from output directory
	// Access files via /output/...
	r.Static("/output", "./output")
	registerFrontendRoutes(r)

	// API 分组
	apiGroup := r.Group("/api")
	{
		apiGroup.GET("/workflows", func(c *gin.Context) {
			c.JSON(200, parsedWorkflows)
		})

		apiGroup.GET("/health", func(c *gin.Context) {
			c.JSON(200, gin.H{
				"status":   "ok",
				"timezone": cfg.Timezone.String(),
			})
		})

		// LLM Provider Routes
		apiGroup.GET("/llm", api.ListLLMProviders)
		apiGroup.POST("/llm", api.AddLLMProvider)
		apiGroup.PUT("/llm/:id", api.UpdateLLMProvider)
		apiGroup.PUT("/llm/:id/active", api.SetActiveLLMProvider)
		apiGroup.DELETE("/llm/:id", api.DeleteLLMProvider)
		apiGroup.POST("/llm/test", api.TestLLMConnection)
		apiGroup.GET("/llm/stats", api.GetLLMUsageSummary)
		apiGroup.POST("/llm/stats/refresh", api.ForceRefreshLLMUsage)
		apiGroup.POST("/llm/stats/reset", api.ResetAllLLMUsage)
		apiGroup.POST("/llm/:id/stats/reset", api.ResetLLMUsage)
		apiGroup.GET("/llm/stream/current", api.GetCurrentLLMStream)

		// System Logs Routes
		apiGroup.GET("/logs", api.ListLogs)
		apiGroup.DELETE("/logs", api.ClearLogs)

		// Art Style Routes
		apiGroup.GET("/styles", api.ListArtStyles)
		apiGroup.POST("/styles", api.AddArtStyle)
		apiGroup.PUT("/styles/:id", api.UpdateArtStyle)
		apiGroup.DELETE("/styles/:id", api.DeleteArtStyle)

		// Auto Generate Tag Routes
		apiGroup.GET("/auto-generate-tags", api.ListAutoGenerateTags)
		apiGroup.POST("/auto-generate-tags", api.AddAutoGenerateTag)
		apiGroup.PUT("/auto-generate-tags/:id", api.UpdateAutoGenerateTag)
		apiGroup.DELETE("/auto-generate-tags/:id", api.DeleteAutoGenerateTag)

		// Multi Visual Routes
		apiGroup.GET("/multi-visual-projects", api.ListMultiVisualProjects)
		apiGroup.POST("/multi-visual-projects", api.CreateMultiVisualProject)
		apiGroup.GET("/multi-visual-projects/:id", api.GetMultiVisualProject)
		apiGroup.PUT("/multi-visual-projects/:id", api.UpdateMultiVisualProject)
		apiGroup.DELETE("/multi-visual-projects/:id", api.DeleteMultiVisualProject)
		apiGroup.POST("/multi-visual-projects/:id/reset-state", api.ResetMultiVisualProjectState)
		apiGroup.GET("/multi-visual-projects/:id/images", api.ListMultiVisualImages)
		apiGroup.POST("/multi-visual-projects/:id/regenerate", api.RegenerateMultiVisualProject)
		apiGroup.DELETE("/multi-visual-projects/:id/images/:imageId", api.DeleteMultiVisualImage)
		apiGroup.POST("/multi-visual-projects/:id/images/delete-batch", api.BatchDeleteMultiVisualImages)
		apiGroup.POST("/multi-visual-projects/:id/export", api.ExportMultiVisualImages)

		// Store Visit Routes
		apiGroup.GET("/store-visits", api.ListStoreVisitProjects)
		apiGroup.POST("/store-visits", api.CreateStoreVisitProject)
		apiGroup.GET("/store-visits/:id", api.GetStoreVisitProject)
		apiGroup.PUT("/store-visits/:id", api.UpdateStoreVisitProject)
		apiGroup.DELETE("/store-visits/:id", api.DeleteStoreVisitProject)
		apiGroup.POST("/store-visits/:id/one-click-generate", api.StartStoreVisitProjectAutoGenerate)
		apiGroup.POST("/store-visits/:id/generate-all-images", api.StartStoreVisitProjectGenerateAllImages)
		apiGroup.POST("/store-visits/:id/generate-all-videos", api.StartStoreVisitProjectGenerateAllVideos)
		apiGroup.POST("/store-visits/:id/reset-all-images", api.ResetStoreVisitProjectAllImages)
		apiGroup.POST("/store-visits/:id/reset-all-videos", api.ResetStoreVisitProjectAllVideos)
		apiGroup.POST("/store-visits/:id/reset-all-states", api.ResetStoreVisitProjectAllStates)
		apiGroup.POST("/store-visits/:id/export", api.ExportStoreVisitProjectArchive)
		apiGroup.POST("/store-visits/:id/export-merged", api.ExportStoreVisitProjectMergedVideo)
		apiGroup.GET("/store-visits/:id/blogger-references", api.ListStoreVisitBloggerReferences)
		apiGroup.POST("/store-visits/:id/blogger-references/:referenceId/select", api.SelectStoreVisitBloggerReference)
		apiGroup.GET("/store-visits/:id/spots", api.ListStoreVisitSpots)
		apiGroup.PUT("/store-visit-spots/:spotId", api.UpdateStoreVisitSpot)
		apiGroup.GET("/store-visit-spots/:spotId/dish-generation-items", api.ListStoreVisitDishGenerationItems)
		apiGroup.POST("/store-visit-spots/:spotId/dish-generation-items", api.CreateStoreVisitDishGenerationItem)
		apiGroup.POST("/store-visit-spots/:spotId/generate-image", api.GenerateStoreVisitSpotImage)
		apiGroup.POST("/store-visit-spots/:spotId/reroll-image", api.RerollStoreVisitSpotImage)
		apiGroup.POST("/store-visit-spots/:spotId/generate-video", api.GenerateStoreVisitSpotVideo)
		apiGroup.POST("/store-visit-spots/:spotId/reroll-video", api.RerollStoreVisitSpotVideo)
		apiGroup.POST("/store-visit-spots/:spotId/interrupt", api.InterruptStoreVisitSpotGeneration)
		apiGroup.POST("/store-visit-spots/:spotId/reset-state", api.ResetStoreVisitSpotState)
		apiGroup.PUT("/store-visit-dish-generation-items/:itemId", api.UpdateStoreVisitDishGenerationItem)
		apiGroup.DELETE("/store-visit-dish-generation-items/:itemId", api.DeleteStoreVisitDishGenerationItem)
		apiGroup.POST("/store-visit-dish-generation-items/:itemId/generate-video", api.GenerateStoreVisitDishGenerationItemVideo)
		apiGroup.POST("/store-visit-dish-generation-items/:itemId/reset-state", api.ResetStoreVisitDishGenerationItemState)
		apiGroup.POST("/store-visit-dish-generation-items/:itemId/interrupt", api.InterruptStoreVisitDishGenerationItemGeneration)

		// General Guide Routes
		apiGroup.GET("/general-guide-tags", api.ListGeneralGuideTags)
		apiGroup.POST("/general-guide-tags", api.AddGeneralGuideTag)
		apiGroup.PUT("/general-guide-tags/:id", api.UpdateGeneralGuideTag)
		apiGroup.DELETE("/general-guide-tags/:id", api.DeleteGeneralGuideTag)
		apiGroup.GET("/general-guides", api.ListGeneralGuideProjects)
		apiGroup.POST("/general-guides", api.CreateGeneralGuideProject)
		apiGroup.GET("/general-guides/:id", api.GetGeneralGuideProject)
		apiGroup.PUT("/general-guides/:id", api.UpdateGeneralGuideProject)
		apiGroup.DELETE("/general-guides/:id", api.DeleteGeneralGuideProject)
		apiGroup.POST("/general-guides/:id/plan-scenes", api.StartGeneralGuideProjectPlanScenes)
		apiGroup.POST("/general-guides/:id/generate-all-images", api.StartGeneralGuideProjectGenerateAllImages)
		apiGroup.POST("/general-guides/:id/generate-all-videos", api.StartGeneralGuideProjectGenerateAllVideos)
		apiGroup.POST("/general-guides/:id/generate-all-transitions", api.StartGeneralGuideProjectGenerateAllTransitions)
		apiGroup.POST("/general-guides/:id/generate-all-images-and-videos", api.StartGeneralGuideProjectGenerateAllImagesAndVideos)
		apiGroup.POST("/general-guides/:id/generate-all-images-videos-and-transitions", api.StartGeneralGuideProjectGenerateAllImagesVideosAndTransitions)
		apiGroup.POST("/general-guides/:id/reset-state", api.ResetGeneralGuideProjectState)
		apiGroup.POST("/general-guides/:id/reset-project-status", api.ResetGeneralGuideProjectProcessingState)
		apiGroup.POST("/general-guides/:id/batch-update-video-size", api.BatchUpdateGeneralGuideVideoSize)
		apiGroup.GET("/general-guide-transition-presets", api.ListGeneralGuideTransitionPresetOptions)
		apiGroup.POST("/general-guides/:id/apply-transition-preset", api.ApplyGeneralGuideProjectTransitionPreset)
		apiGroup.POST("/general-guides/:id/export", api.ExportGeneralGuideProjectArchive)
		apiGroup.POST("/general-guides/:id/export-merged", api.ExportGeneralGuideProjectMergedVideo)
		apiGroup.GET("/general-guides/:id/references", api.ListGeneralGuideReferences)
		apiGroup.POST("/general-guides/:id/references/:referenceId/select", api.SelectGeneralGuideReference)
		apiGroup.GET("/general-guides/:id/scenes", api.ListGeneralGuideScenes)
		apiGroup.GET("/general-guides/:id/transitions", api.ListGeneralGuideTransitions)
		apiGroup.PUT("/general-guide-scenes/:sceneId", api.UpdateGeneralGuideScene)
		apiGroup.POST("/general-guide-scenes/:sceneId/generate-image", api.GenerateGeneralGuideSceneImage)
		apiGroup.POST("/general-guide-scenes/:sceneId/generate-video", api.GenerateGeneralGuideSceneVideo)
		apiGroup.POST("/general-guide-scenes/:sceneId/reset-state", api.ResetGeneralGuideSceneState)
		apiGroup.PUT("/general-guide-transitions/:transitionId", api.UpdateGeneralGuideTransition)
		apiGroup.POST("/general-guide-transitions/:transitionId/reset-state", api.ResetGeneralGuideTransitionState)
		apiGroup.POST("/general-guide-transitions/:transitionId/extract-tail-frame", api.ExtractGeneralGuideTransitionTailFrame)
		apiGroup.POST("/general-guide-transitions/:transitionId/generate-video", api.GenerateGeneralGuideTransitionVideo)

		// Audio Clone Routes
		apiGroup.GET("/audio-clone-projects", api.ListAudioCloneProjects)
		apiGroup.POST("/audio-clone-projects", api.CreateAudioCloneProject)
		apiGroup.GET("/audio-clone-projects/:id", api.GetAudioCloneProject)
		apiGroup.PUT("/audio-clone-projects/:id", api.UpdateAudioCloneProject)
		apiGroup.DELETE("/audio-clone-projects/:id", api.DeleteAudioCloneProject)
		apiGroup.GET("/audio-clone-projects/:id/characters", api.ListAudioCloneCharacters)
		apiGroup.POST("/audio-clone-projects/:id/characters", api.CreateAudioCloneCharacter)
		apiGroup.PUT("/audio-clone-characters/:characterId", api.UpdateAudioCloneCharacter)
		apiGroup.DELETE("/audio-clone-characters/:characterId", api.DeleteAudioCloneCharacter)
		apiGroup.POST("/audio-clone-characters/:characterId/recognize-reference", api.RecognizeAudioCloneCharacterReference)
		apiGroup.GET("/audio-clone-projects/:id/lines", api.ListAudioCloneLines)
		apiGroup.POST("/audio-clone-projects/:id/save-lines", api.SaveAudioCloneScriptLines)
		apiGroup.POST("/audio-clone-projects/:id/generate-lines", api.GenerateAudioCloneProjectLines)
		apiGroup.PUT("/audio-clone-lines/:lineId", api.UpdateAudioCloneLine)
		apiGroup.POST("/audio-clone-lines/:lineId/generate", api.GenerateAudioCloneLine)

		apiGroup.GET("/qwen-tts-projects", api.ListQwenTTSProjects)
		apiGroup.POST("/qwen-tts-projects", api.CreateQwenTTSProject)
		apiGroup.GET("/qwen-tts-projects/:id", api.GetQwenTTSProject)
		apiGroup.PUT("/qwen-tts-projects/:id", api.UpdateQwenTTSProject)
		apiGroup.DELETE("/qwen-tts-projects/:id", api.DeleteQwenTTSProject)
		apiGroup.GET("/qwen-tts-projects/:id/characters", api.ListQwenTTSCharacters)
		apiGroup.POST("/qwen-tts-projects/:id/characters", api.CreateQwenTTSCharacter)
		apiGroup.PUT("/qwen-tts-characters/:characterId", api.UpdateQwenTTSCharacter)
		apiGroup.DELETE("/qwen-tts-characters/:characterId", api.DeleteQwenTTSCharacter)
		apiGroup.POST("/qwen-tts-characters/:characterId/recognize-reference", api.RecognizeQwenTTSCharacterReference)
		apiGroup.GET("/qwen-tts-projects/:id/lines", api.ListQwenTTSLines)
		apiGroup.POST("/qwen-tts-projects/:id/import-prompt", api.BuildQwenTTSImportPrompt)
		apiGroup.POST("/qwen-tts-projects/:id/import-result", api.ImportQwenTTSResult)
		apiGroup.POST("/qwen-tts-projects/:id/export", api.ExportQwenTTSProjectArchive)
		apiGroup.POST("/qwen-tts-projects/:id/save-script", api.SaveQwenTTSScriptText)
		apiGroup.POST("/qwen-tts-projects/:id/save-lines", api.SaveQwenTTSScriptLines)
		apiGroup.POST("/qwen-tts-projects/:id/generate-lines", api.GenerateQwenTTSProjectLines)
		apiGroup.POST("/qwen-tts-projects/:id/reset-line-states", api.ResetQwenTTSProjectLineStates)
		apiGroup.POST("/qwen-tts-projects/:id/reset-lines", api.ResetQwenTTSProjectLines)
		apiGroup.POST("/qwen-tts-projects/:id/interrupt-generation", api.InterruptQwenTTSProjectGeneration)
		apiGroup.PUT("/qwen-tts-lines/:lineId", api.UpdateQwenTTSLine)
		apiGroup.POST("/qwen-tts-lines/:lineId/generate", api.GenerateQwenTTSLine)
		apiGroup.POST("/qwen-tts-lines/:lineId/reset-state", api.ResetQwenTTSLineState)

		apiGroup.GET("/audio-production-presets", api.ListAudioProductionPresets)
		apiGroup.GET("/audio-production-projects", api.ListAudioProductionProjects)
		apiGroup.POST("/audio-production-projects", api.CreateAudioProductionProject)
		apiGroup.GET("/audio-production-projects/:id", api.GetAudioProductionProject)
		apiGroup.PUT("/audio-production-projects/:id", api.UpdateAudioProductionProject)
		apiGroup.DELETE("/audio-production-projects/:id", api.DeleteAudioProductionProject)
		apiGroup.GET("/audio-production-projects/:id/lines", api.ListAudioProductionLines)
		apiGroup.POST("/audio-production-projects/:id/save-lines", api.SaveAudioProductionLines)
		apiGroup.POST("/audio-production-projects/:id/generate-lines", api.GenerateAudioProductionProjectLines)
		apiGroup.POST("/audio-production-lines/:lineId/generate", api.GenerateAudioProductionLine)

		// Project Routes
		apiGroup.GET("/projects", api.ListProjects)
		apiGroup.GET("/projects/:id", api.GetProject)
		apiGroup.POST("/projects", api.AddProject)
		apiGroup.PUT("/projects/:id", api.UpdateProject)
		apiGroup.POST("/projects/:id/reset-content", api.ResetProjectContent)
		apiGroup.DELETE("/projects/:id", api.DeleteProject)
		apiGroup.POST("/projects/:id/auto-generate", api.AutoGenerateProject)
		apiGroup.POST("/projects/:id/import-story-json", api.ImportStoryJSON)
		apiGroup.POST("/projects/:id/export-story-json", api.ExportStoryJSON)
		apiGroup.GET("/projects/:id/auto-generate-draft", api.GetAutoGenerateDraft)
		apiGroup.PUT("/projects/:id/auto-generate-draft", api.UpdateAutoGenerateDraft)
		apiGroup.DELETE("/projects/:id/auto-generate-draft", api.DeleteAutoGenerateDraft)
		apiGroup.POST("/projects/:id/auto-generate-draft/continue", api.ContinueAutoGenerateProject)

		// Character Routes (Nested under projects usually, but simple CRUD here)
		// Assuming we pass project_id in body for create, or filter by project_id in list
		apiGroup.GET("/projects/:id/characters", api.ListCharacters)
		apiGroup.POST("/characters", api.AddCharacter)
		apiGroup.PUT("/characters/:charId", api.UpdateCharacter)
		apiGroup.DELETE("/characters/:charId", api.DeleteCharacter)
		apiGroup.POST("/projects/:id/characters/:charId/auto-generate-prompt", api.AutoGenerateCharacterPrompt)
		apiGroup.POST("/projects/:id/characters/:charId/generate-image", api.GenerateCharacterImage)
		apiGroup.POST("/projects/:id/characters/:charId/reset-image", api.ResetCharacterImage)
		apiGroup.POST("/projects/:id/batch-generate-characters", api.AutoGenerateAllCharacters)
		apiGroup.POST("/projects/:id/batch-generate-characters-scenes", api.AutoGenerateCharactersAndScenes)
		apiGroup.POST("/projects/:id/batch-generate-all-media", api.AutoGenerateAllMedia)
		apiGroup.POST("/projects/:id/characters/delete-all-images", api.DeleteAllCharacterImages)
		apiGroup.POST("/projects/:id/episodes/reset-assets", api.EpisodeResetAssets)
		apiGroup.POST("/projects/:id/episodes/delete", api.DeleteEpisode)
		apiGroup.POST("/upload", api.UploadFile) // For reference images

		// Scene Routes
		apiGroup.GET("/projects/:id/scenes", api.ListScenes)
		apiGroup.POST("/scenes", api.AddScene)
		apiGroup.PUT("/scenes/:sceneId", api.UpdateScene)
		apiGroup.DELETE("/scenes/:sceneId", api.DeleteScene)
		apiGroup.POST("/projects/:id/scenes/:sceneId/repair-prompts", api.RepairScenePrompts)
		apiGroup.POST("/projects/:id/scenes/:sceneId/generate-image", api.GenerateSceneImage)
		apiGroup.POST("/projects/:id/scenes/:sceneId/reset-image", api.ResetSceneImage)
		apiGroup.POST("/projects/:id/batch-generate-scenes", api.AutoGenerateAllScenes)
		apiGroup.POST("/projects/:id/scenes/delete-all-images", api.DeleteAllSceneImages)

		// Video Routes
		apiGroup.GET("/projects/:id/videos", api.ListVideos)
		apiGroup.DELETE("/projects/:id/videos/:videoId", api.DeleteVideo)
		apiGroup.PUT("/projects/:id/videos/:videoId", api.UpdateVideo)
		apiGroup.POST("/projects/:id/videos/:videoId/repair-prompts", api.RepairVideoPrompts)
		apiGroup.POST("/projects/:id/videos/:videoId/reextract", api.ReextractVideo)
		apiGroup.POST("/projects/:id/videos/:videoId/generate-video", api.GenerateVideo)
		apiGroup.GET("/projects/:id/videos/:videoId/jimeng-status", api.GetJimengVideoStatus)
		apiGroup.POST("/projects/:id/videos/:videoId/jimeng-retrieve", api.RetrieveJimengVideoResult)
		apiGroup.POST("/projects/:id/videos/:videoId/reset-status", api.ResetVideo)
		apiGroup.POST("/projects/:id/videos/:videoId/segments/:segmentId/regenerate", api.RegenerateVideoSegment)
		apiGroup.POST("/projects/:id/videos/reset", api.ResetProjectVideos)
		apiGroup.POST("/projects/:id/videos/export", api.ExportEpisodeVideos)
		apiGroup.POST("/projects/:id/videos/export-merged", api.ExportMergedEpisodeVideo)
		apiGroup.POST("/projects/:id/batch-generate-videos", api.BatchGenerateVideos)

		// Settings Routes
		apiGroup.GET("/settings", api.GetSettings)
		apiGroup.PUT("/settings", api.UpdateSettings)

		// ComfyUI Routes
		apiGroup.GET("/comfyui/check_models", api.CheckWorkflowModels)
		apiGroup.GET("/comfyui/status", api.GetComfyUIStatus)

		// System Routes
		apiGroup.GET("/system/info", api.GetSystemInfo)
		apiGroup.GET("/system/monitor", api.GetSystemMonitor)
		apiGroup.GET("/tasks", api.ListTasks)
		apiGroup.GET("/tasks/:id", api.GetTask)
		apiGroup.GET("/tasks/:id/llm-stream", api.GetTaskLLMStream)
		apiGroup.DELETE("/tasks", api.ClearTasks)

		// SSE Endpoint for real-time updates
		apiGroup.GET("/events", api.SSEHandler)
	}

	// 5. 启动服务器
	log.Printf("服务器正在启动，端口 %s", cfg.AppPort)

	srv := &http.Server{
		Addr:    "0.0.0.0:" + cfg.AppPort,
		Handler: r,
	}

	// 监听所有网络接口，以便局域网访问
	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("服务器启动失败: %v", err)
		}
	}()
	openBrowserWhenReady("http://127.0.0.1:" + cfg.AppPort)

	// Wait for interrupt signal to gracefully shutdown the server with
	// a timeout of 5 seconds.
	quit := make(chan os.Signal, 1)
	// kill (no param) default send syscall.SIGTERM
	// kill -2 is syscall.SIGINT
	// kill -9 is syscall.SIGKILL but can't be catch, so don't need add it
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM, os.Interrupt)
	<-quit
	log.Println("Shutting down server...")

	// The context is used to inform the server it has 5 seconds to finish
	// the request it is currently handling
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		log.Fatal("Server forced to shutdown: ", err)
	}

	api.StopLLMUsageTracker()

	log.Println("Server exiting")
}

func registerFrontendRoutes(r *gin.Engine) {
	r.GET("/assets/*filepath", serveEmbeddedFrontendAsset)
	r.GET("/vite.svg", serveEmbeddedFrontendAsset)

	r.NoRoute(func(c *gin.Context) {
		if c.Request.Method != http.MethodGet ||
			strings.HasPrefix(c.Request.URL.Path, "/api/") ||
			strings.HasPrefix(c.Request.URL.Path, "/output/") {
			c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
			return
		}

		indexHTML, err := frontendDist.ReadFile("frontend/dist/index.html")
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "frontend index not found"})
			return
		}
		c.Data(http.StatusOK, "text/html; charset=utf-8", indexHTML)
	})
}

func serveEmbeddedFrontendAsset(c *gin.Context) {
	requestPath := strings.TrimPrefix(c.Request.URL.Path, "/")
	data, err := frontendDist.ReadFile("frontend/dist/" + requestPath)
	if err != nil {
		c.Status(http.StatusNotFound)
		return
	}

	contentType := mime.TypeByExtension(filepath.Ext(requestPath))
	if contentType == "" {
		contentType = http.DetectContentType(data)
	}
	c.Writer.Header().Set("Content-Type", contentType)
	http.ServeContent(c.Writer, c.Request, filepath.Base(requestPath), time.Time{}, bytes.NewReader(data))
}

func openBrowserWhenReady(url string) {
	go func() {
		client := http.Client{Timeout: 500 * time.Millisecond}
		for i := 0; i < 40; i++ {
			resp, err := client.Get(url + "/api/health")
			if err == nil {
				resp.Body.Close()
				openBrowser(url)
				return
			}
			time.Sleep(250 * time.Millisecond)
		}
	}()
}

func openBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	case "darwin":
		cmd = exec.Command("open", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	if err := cmd.Start(); err != nil {
		log.Printf("无法自动打开浏览器，请手动访问 %s: %v", url, err)
	}
}
