package api

import (
	"strings"
	"testing"

	"kt-ai-studio/internal/db"
	"kt-ai-studio/internal/models"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func TestBuildH3ShortLightweightStoryPromptsPlaceholders(t *testing.T) {
	ctx := lightweightStoryPromptContext{
		Project: models.Project{
			Code:        "TEST",
			Name:        "测试项目",
			Description: "测试描述",
		},
		ReferenceCharactersJSON: "@图1=萧景珩\n@图2=沈知微",
		SelectedTagRules:        "题材标签：宫斗",
		SceneImageWidth:         768,
		SceneImageHeight:        1344,
		SceneImageFrameType:     "竖屏 9:16",
		FixedVideoFPS:           24,
	}

	systemPrompt, _ := buildH3ShortLightweightStoryPrompts(ctx)

	for _, want := range []string{
		"integrated_multimodal_description:",
		"overall_soundscape:",
		"non_diegetic_music:",
		"参考图@图N",
		"@图1=萧景珩",
		"4 字/秒",
		"20 字",
		"50 字",
		"黄金 6 秒",
		"台词零删改铁律",
		"全局位置/朝向基准表",
		"题材标签：宫斗",
	} {
		if !strings.Contains(systemPrompt, want) {
			t.Errorf("h3_short systemPrompt missing %q", want)
		}
	}
	if strings.Contains(systemPrompt, "%!") {
		t.Errorf("h3_short systemPrompt has unmatched format verb")
	}
	if strings.Contains(systemPrompt, "R2V") || strings.Contains(systemPrompt, "固定 5 秒") {
		t.Errorf("h3_short systemPrompt should not reference R2V or fixed 5s segments")
	}
}

func TestBuildReferenceCharactersIndexBlockNumbering(t *testing.T) {
	// 空名角色不得造成 @图N 跳号；编号只对有名字的角色连续递增。
	ttDB, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open in-memory sqlite: %v", err)
	}
	if err := ttDB.AutoMigrate(&models.Character{}); err != nil {
		t.Fatalf("auto migrate: %v", err)
	}
	if err := ttDB.Create([]models.Character{
		{ID: 1, ProjectID: 1, Name: "甲", RefImage: "/output/p/a.png"},
		{ID: 2, ProjectID: 1, Name: "", RefImage: "/output/p/b.png"},
		{ID: 3, ProjectID: 1, Name: "丙", RefImage: "/output/p/c.png"},
	}).Error; err != nil {
		t.Fatalf("seed characters: %v", err)
	}

	origDB := db.DB
	db.DB = ttDB
	defer func() { db.DB = origDB }()

	block, err := buildReferenceCharactersIndexBlock(1)
	if err != nil {
		t.Fatalf("buildReferenceCharactersIndexBlock error: %v", err)
	}
	if !strings.Contains(block, "@图1=甲") {
		t.Errorf("index block should contain @图1=甲, got: %s", block)
	}
	if !strings.Contains(block, "@图2=丙") {
		t.Errorf("index block should contain @图2=丙 (no skip), got: %s", block)
	}
	if strings.Contains(block, "@图3") {
		t.Errorf("index block should not emit @图3, got: %s", block)
	}
}