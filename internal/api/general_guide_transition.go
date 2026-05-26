package api

import (
	"encoding/json"
	"fmt"
	"math"
	"math/rand"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"kt-ai-studio/internal/db"
	"kt-ai-studio/internal/models"
	"kt-ai-studio/internal/task"

	"github.com/gin-gonic/gin"
)

const (
	generalGuideTransitionWorkflowPathLTX = "workflows/ltx2.3_nor_zhuanchang.json"
	generalGuideTransitionWorkflowPathWan = "workflows/video_wan2_2_14B_flf2v.json"
	generalGuideTransitionDefaultFrames   = 3
	generalGuideTransitionFFmpegFrames    = 1
	generalGuideTransitionDefaultSecond   = 2
	generalGuideTransitionFPS             = 25
	generalGuideTransitionSeedMax         = int64(1125899906842624)
)

const (
	generalGuideTransitionNegativePromptLTX = `cartoon, bad quality, subtitles, caption, text, text overlay, on-screen text, watermark, logo, lower-third, overlay effects, blurry, distorted, deformed, AI artifacts`
	generalGuideTransitionNegativePromptWan = `overexposed, static frame, frozen motion, blurry details, low quality, subtitles, caption, text overlay, watermark, logo, bad transition, broken motion continuity, extra people, crowd, distorted anatomy, deformed hands, AI artifacts`
	generalGuideTransitionLandingClauseLTX  = `The transition ends with a very brief cinematic dip into shadow, almost like a soft blackout, which masks the final handoff before the next scene takes over`
	generalGuideTransitionLandingClauseWan  = `The final moment gently dips through a very brief shadowed fade, almost like a soft blackout, so the handoff into the next scene feels smoother and less abrupt`
)

type generalGuideTransitionPreset struct {
	Key                        string `json:"key"`
	Label                      string `json:"label"`
	Description                string `json:"description"`
	Prompt                     string `json:"prompt"`
	RecommendedDurationSeconds int    `json:"recommended_duration_seconds"`
}

type generalGuideTransitionPresetListResponse struct {
	Engine  string                         `json:"engine"`
	Presets []generalGuideTransitionPreset `json:"presets"`
}

var generalGuideTransitionPresetsLTX = []generalGuideTransitionPreset{
	{
		Key:                        "soft_rebuild",
		Label:                      "柔光重构",
		Description:                "当前画面柔和消解，再平滑重构到下一场景，最稳。",
		Prompt:                     "A handheld medium shot begins on the current scene with slight natural camera breathing. The old image gradually dissolves into soft drifting light and subtle visual fragments, then smoothly reconstructs into the next scene with increasing clarity, depth, and texture. The transformation remains cinematic, elegant, and coherent, with refined highlights, soft atmospheric glow, realistic motion continuity, and a premium transition feel, zhuanchang",
		RecommendedDurationSeconds: 2,
	},
	{
		Key:                        "shadow_dip_landing",
		Label:                      "暗场收束",
		Description:                "结尾带一个很短的暗场收束，专门遮盖首尾帧出入。",
		Prompt:                     "A cinematic handheld shot begins on the current scene with subtle natural camera breathing and stable visual depth. The old image gradually dissolves into layered light, texture, and soft visual fragments, then reconstructs into the next scene with smooth motion continuity and premium detail. In the final moment, the image briefly dips into a controlled shadowed falloff, almost like a soft blackout, before the handoff completes, zhuanchang",
		RecommendedDurationSeconds: 2,
	},
	{
		Key:                        "particle_reform",
		Label:                      "粒子散逸",
		Description:                "画面分解成细微粒子，再聚合成下一场景。",
		Prompt:                     "A handheld medium shot starts on the current scene with a stable realistic perspective and slight camera breathing. The visible image gradually breaks apart into fine drifting particles and luminous fragments, while the old scene loses structure and fades away. Those particles gather again and slowly rebuild the next scene from the center outward with increasing clarity, spatial depth, and premium visual cohesion, zhuanchang",
		RecommendedDurationSeconds: 2,
	},
	{
		Key:                        "snap_dissolve_rebuild",
		Label:                      "响指消散重构",
		Description:                "像灭霸响指一样消散成灰，再以粒子重塑成下一场景。",
		Prompt:                     "A cinematic handheld shot begins on the current scene with subtle natural camera breathing. The visible image starts to break apart into fine drifting ash-like particles, dissolving smoothly from solid form into thousands of floating fragments, as if the entire scene is disintegrating into dust. The particles continue moving across the frame in a controlled swirling flow, then gradually gather again and reconstruct the next scene from the air, restoring shapes, textures, depth, and clarity until the new image is fully formed. The effect feels dramatic, elegant, and highly cinematic, with soft glowing particle edges, rich atmosphere, and a seamless sense of visual transformation, zhuanchang",
		RecommendedDurationSeconds: 3,
	},
	{
		Key:                        "light_flow",
		Label:                      "光流拉丝",
		Description:                "场景被拉成流动光带，再组织成下一场景。",
		Prompt:                     "A cinematic handheld shot begins in the current scene with subtle natural movement and realistic depth. The image stretches into flowing streaks of light and color, as if the old scene is being pulled forward in motion, then those luminous trails reorganize into the next scene with increasing clarity and structure. The transformation is smooth, glossy, dynamic, and visually rich, with refined highlights, elegant motion blur, layered depth, and a polished cinematic feel, zhuanchang",
		RecommendedDurationSeconds: 2,
	},
	{
		Key:                        "space_fold",
		Label:                      "空间折叠",
		Description:                "旧场景向中心折叠，下一场景从内部展开。",
		Prompt:                     "A medium cinematic shot begins with slight handheld breathing and realistic visual depth. The current scene starts to fold inward as if space itself is bending and collapsing in layers toward the center of the frame. As the old environment compresses and recedes, the next scene unfolds from within it, expanding outward in a smooth spatial transformation until the new view is fully reconstructed and stable. The effect feels sleek, immersive, and cinematic, with subtle dimensional distortion, refined lighting, and premium atmospheric depth, zhuanchang",
		RecommendedDurationSeconds: 3,
	},
	{
		Key:                        "mist_reveal",
		Label:                      "雾化显现",
		Description:                "旧场景融进柔雾里，下一场景从雾中显现。",
		Prompt:                     "A realistic handheld shot begins on the current scene with gentle camera breathing and stable framing. The image slowly softens and diffuses into a dreamy layer of mist, glow, and translucent texture, as if the current scene is dissolving into atmosphere. Within that haze, the next scene gradually emerges and sharpens little by little until its structure, lighting, and composition become fully visible. The transition feels elegant, immersive, and cinematic, with soft bloom, refined contrast, and a premium visual finish, zhuanchang",
		RecommendedDurationSeconds: 2,
	},
	{
		Key:                        "glass_shards",
		Label:                      "碎光折射",
		Description:                "画面裂成反光碎片，再拼成下一场景，比较炫。",
		Prompt:                     "A premium handheld cinematic shot starts in the current scene with subtle natural movement and realistic visual depth. The image fractures into elegant translucent shards and reflective fragments, as if the scene is turning into thin pieces of glass and light. These fragments drift, rotate, and then realign, gradually revealing the next scene until the new image is fully assembled and visually stable. The transition feels luxurious, polished, and cinematic, with reflective highlights, layered depth, and a strong sense of visual transformation, zhuanchang",
		RecommendedDurationSeconds: 2,
	},
	{
		Key:                        "energy_wave",
		Label:                      "能量涌动",
		Description:                "旧场景被光能量覆盖，再涌出下一场景。",
		Prompt:                     "A cinematic handheld shot begins on the current scene with slight natural breathing and realistic depth. A wave of luminous energy gradually spreads across the frame, washing over the existing image and dissolving it into a glowing layer of motion and light. As the energy settles, the next scene emerges from underneath with clearer edges, textures, and spatial definition until it fully takes over the frame. The overall look feels dynamic, premium, and coherent, with refined glow, controlled motion, and rich atmospheric depth, zhuanchang",
		RecommendedDurationSeconds: 2,
	},
	{
		Key:                        "premium_dissolve",
		Label:                      "高级溶解",
		Description:                "更克制的高端广告感转场。",
		Prompt:                     "A refined handheld medium shot begins on the current scene with subtle real camera breathing. The existing image gently dissolves into soft layers of glow, motion, and visual texture, then transitions into the next scene through a smooth premium reconstruction with increasing detail, depth, and clarity. The movement feels controlled, elegant, and cinematic, with polished highlights, soft atmospheric diffusion, and a luxurious commercial finish, zhuanchang",
		RecommendedDurationSeconds: 2,
	},
	{
		Key:                        "light_curtain",
		Label:                      "光幕揭示",
		Description:                "一道发光光幕横扫画面，带出下一场景。",
		Prompt:                     "A handheld cinematic shot begins on the current scene with slight natural camera breathing and realistic depth. A luminous curtain of soft moving light sweeps across the frame, gradually veiling the old image in glowing layers while preserving smooth motion continuity. As the curtain passes, the next scene is revealed behind it with increasing clarity, polished highlights, dimensional depth, and a sleek premium finish. The transition feels elegant, controlled, and visually rich, zhuanchang",
		RecommendedDurationSeconds: 2,
	},
	{
		Key:                        "water_refraction",
		Label:                      "水波折射",
		Description:                "像隔着流动水面折射，旧画面扭转成下一场景。",
		Prompt:                     "A realistic handheld shot starts on the current scene with subtle breathing and stable visual depth. The entire frame begins to ripple like light passing through moving water, bending reflections, edges, and textures into a fluid refracted surface. As those ripples travel across the image, the old scene gradually gives way to the next scene, which resolves into cleaner shapes, richer detail, and refined cinematic depth. The transformation feels smooth, immersive, and premium, zhuanchang",
		RecommendedDurationSeconds: 2,
	},
	{
		Key:                        "cloud_break",
		Label:                      "云层穿越",
		Description:                "旧场景被云雾覆盖，再穿出到下一场景。",
		Prompt:                     "A cinematic handheld shot begins in the current scene with slight natural breathing and realistic perspective. Soft rolling clouds and bright vapor gradually surge through the frame, obscuring the old image as if the camera is moving into a dense luminous cloud bank. Within that motion, the next scene starts to emerge from the mist with growing clarity, depth, and visual structure until the new view fully replaces the old one. The effect feels expansive, airy, and dramatic while remaining smooth and premium, zhuanchang",
		RecommendedDurationSeconds: 3,
	},
	{
		Key:                        "whip_reveal",
		Label:                      "甩镜揭示",
		Description:                "更有速度感的甩镜式揭示，适合节奏快一点的切换。",
		Prompt:                     "A handheld medium shot begins on the current scene with realistic camera breathing and stable framing. The camera motion suddenly accelerates into a refined whip-like sweep, smearing the old image into elegant directional motion blur and streaked detail rather than chaotic distortion. As the motion decelerates, the next scene locks into place with stronger clarity, polished highlights, and restored structure, creating a clean, energetic, cinematic reveal with controlled momentum, zhuanchang",
		RecommendedDurationSeconds: 2,
	},
	{
		Key:                        "page_turn",
		Label:                      "卷页重构",
		Description:                "像翻页一样把旧场景卷走，再展开下一场景。",
		Prompt:                     "A refined handheld cinematic shot begins on the current scene with slight natural breathing and realistic depth. The visible image starts to curl and fold like a premium page turning in space, carrying the old scene away in a smooth layered motion with soft highlights and dimensional texture. As the page-like surface completes its turn, the next scene unfolds beneath it with increasing detail, stable structure, and elegant visual continuity. The result feels polished, deliberate, and cinematic, zhuanchang",
		RecommendedDurationSeconds: 2,
	},
	{
		Key:                        "prism_flash",
		Label:                      "棱镜闪切",
		Description:                "通过棱镜般的折光闪切到下一场景，偏时尚广告感。",
		Prompt:                     "A premium handheld shot opens on the current scene with subtle real camera breathing and realistic depth. Bright prismatic flares and refracted shards of color sweep across the frame, splitting the old image into layered bands of light and soft spectral reflections. As those refracted bands pass, the next scene assembles from within them with clearer edges, refined depth, and a glossy cinematic finish. The transition feels stylish, luminous, and sharply polished without losing visual coherence, zhuanchang",
		RecommendedDurationSeconds: 2,
	},
	{
		Key:                        "comic_break",
		Label:                      "漫画破框",
		Description:                "旧画面像破框一样裂开，再显现下一场景，戏剧感更强。",
		Prompt:                     "A cinematic handheld shot begins on the current scene with slight natural breathing and clear visual depth. The image begins to fracture into layered graphic planes and broken frame edges, as if the current scene is tearing through its own boundaries. Those fractured planes slide and peel away in a controlled visual burst, revealing the next scene underneath with restored realism, stronger clarity, and dimensional structure. The transition feels bold, stylized, and dramatic while remaining smooth and readable, zhuanchang",
		RecommendedDurationSeconds: 2,
	},
	{
		Key:                        "style_shift",
		Label:                      "风格切换",
		Description:                "旧画面短暂进入更强风格化层次，再回到下一场景。",
		Prompt:                     "A handheld cinematic shot begins on the current scene with subtle natural breathing and realistic perspective. The old image gradually passes through a stylized intermediate layer of heightened contrast, softened texture, and graphic visual treatment, as if reality is briefly being reinterpreted before shifting forward. That stylized layer then resolves into the next scene with cleaner detail, restored realism, and polished cinematic depth. The transformation feels intentional, premium, and visually surprising while staying coherent, zhuanchang",
		RecommendedDurationSeconds: 3,
	},
}

var generalGuideTransitionPresetsWan = []generalGuideTransitionPreset{
	{
		Key:                        "wan_smooth_bridge",
		Label:                      "平滑桥接",
		Description:                "最稳的首尾帧平滑桥接，适合默认使用。",
		Prompt:                     "A smooth cinematic transition begins in the current scene and gradually bridges into the next scene with strong visual continuity. The camera remains stable with subtle natural motion, while the details of the first frame gently soften and give way to the second frame. Textures, lighting, and depth shift progressively and coherently until the next scene is fully established with a polished, natural finish.",
		RecommendedDurationSeconds: 2,
	},
	{
		Key:                        "wan_shadow_landing",
		Label:                      "暗场收束",
		Description:                "更稳的首尾帧桥接结尾，最后用很短的暗场柔化交接。",
		Prompt:                     "A smooth cinematic transition begins in the current scene and gradually bridges into the next scene with stable visual continuity. The first frame softens and gives way to the second frame through polished, coherent motion. In the final moment, the image passes through a very brief shadowed fade, almost like a soft blackout, so the handoff into the next scene feels smoother and less abrupt.",
		RecommendedDurationSeconds: 2,
	},
	{
		Key:                        "wan_soft_dissolve",
		Label:                      "柔和溶接",
		Description:                "旧画面柔和融入下一场景，过渡更自然。",
		Prompt:                     "The current scene transitions into the next scene through a soft cinematic dissolve with clear motion continuity. Visual details from the first frame gently fade and reorganize into the second frame while preserving stable composition, smooth timing, and natural depth. The result feels polished, seamless, and visually calm, with no abrupt jump between the two scenes.",
		RecommendedDurationSeconds: 2,
	},
	{
		Key:                        "wan_particle_bridge",
		Label:                      "粒子桥接",
		Description:                "轻粒子感过渡，但仍以稳定桥接为主。",
		Prompt:                     "The first scene gradually breaks into small drifting particles and soft luminous fragments, then those fragments flow forward and rebuild the second scene with smooth cinematic continuity. The transformation remains readable and stable, with clean timing, soft atmospheric detail, and a seamless bridge from the first frame to the last frame.",
		RecommendedDurationSeconds: 2,
	},
	{
		Key:                        "wan_light_ribbon",
		Label:                      "光带衔接",
		Description:                "以流动光带把首尾帧连接起来，适合更高级一点的广告感。",
		Prompt:                     "The current scene stretches into elegant flowing ribbons of light and color that gradually carry the viewer into the next scene. The transition stays stable and controlled, with the first frame progressively transforming into the last frame through smooth motion, refined highlights, and strong structural continuity.",
		RecommendedDurationSeconds: 2,
	},
	{
		Key:                        "wan_mist_bridge",
		Label:                      "薄雾显现",
		Description:                "旧场景融入薄雾，下一场景从雾中自然显现。",
		Prompt:                     "A layer of soft mist and glow slowly washes over the first scene, gently diffusing its details before revealing the next scene from within the haze. The transition is smooth, natural, and visually coherent, with gradual shifts in shape, light, and depth until the second scene becomes fully clear.",
		RecommendedDurationSeconds: 2,
	},
	{
		Key:                        "wan_refracted_flow",
		Label:                      "折射流转",
		Description:                "通过折射感的流动变化连接两个场景。",
		Prompt:                     "The first scene transitions into the second scene through a refined refracted flow, as if the image is passing through a moving transparent surface. Edges, colors, and textures bend smoothly and then settle into the next scene with clean timing, stable composition, and strong visual continuity.",
		RecommendedDurationSeconds: 2,
	},
}

var generalGuideTransitionPresetsFFmpeg = []generalGuideTransitionPreset{
	{Key: "ff_fade", Label: "普通淡入淡出", Description: "最基础也最稳的过渡。", Prompt: "fade", RecommendedDurationSeconds: 1},
	{Key: "ff_fadeblack", Label: "黑场淡出", Description: "先轻微压暗再进入下一场景，更能遮住接缝。", Prompt: "fadeblack", RecommendedDurationSeconds: 1},
	{Key: "ff_fadewhite", Label: "白场闪切", Description: "快速闪白后进入下一场景，节奏更亮。", Prompt: "fadewhite", RecommendedDurationSeconds: 1},
	{Key: "ff_dissolve", Label: "交叉溶解", Description: "两张画面柔和交叠，是最像剪辑软件的稳定效果。", Prompt: "dissolve", RecommendedDurationSeconds: 1},
	{Key: "ff_wipeleft", Label: "向左擦除", Description: "像从右向左推开画面。", Prompt: "wipeleft", RecommendedDurationSeconds: 1},
	{Key: "ff_wiperight", Label: "向右擦除", Description: "像从左向右推开画面。", Prompt: "wiperight", RecommendedDurationSeconds: 1},
	{Key: "ff_wipeup", Label: "向上擦除", Description: "由下往上擦除，适合竖屏。", Prompt: "wipeup", RecommendedDurationSeconds: 1},
	{Key: "ff_wipedown", Label: "向下擦除", Description: "由上往下擦除，节奏更明确。", Prompt: "wipedown", RecommendedDurationSeconds: 1},
	{Key: "ff_slideleft", Label: "向左滑动", Description: "旧画面滑走，新画面滑入。", Prompt: "slideleft", RecommendedDurationSeconds: 1},
	{Key: "ff_slideright", Label: "向右滑动", Description: "反方向滑动切换。", Prompt: "slideright", RecommendedDurationSeconds: 1},
	{Key: "ff_slideup", Label: "向上滑动", Description: "适合竖屏时往上切入。", Prompt: "slideup", RecommendedDurationSeconds: 1},
	{Key: "ff_slidedown", Label: "向下滑动", Description: "由上往下滑入下一场景。", Prompt: "slidedown", RecommendedDurationSeconds: 1},
	{Key: "ff_smoothleft", Label: "平滑左移", Description: "比普通擦除更柔和。", Prompt: "smoothleft", RecommendedDurationSeconds: 1},
	{Key: "ff_smoothright", Label: "平滑右移", Description: "比普通右移更细腻。", Prompt: "smoothright", RecommendedDurationSeconds: 1},
	{Key: "ff_smoothup", Label: "平滑上移", Description: "竖向过渡更自然。", Prompt: "smoothup", RecommendedDurationSeconds: 1},
	{Key: "ff_smoothdown", Label: "平滑下移", Description: "下移切换更柔和。", Prompt: "smoothdown", RecommendedDurationSeconds: 1},
	{Key: "ff_circleopen", Label: "圆形展开", Description: "从中心圆形展开到下一场景。", Prompt: "circleopen", RecommendedDurationSeconds: 1},
	{Key: "ff_circleclose", Label: "圆形收束", Description: "圆形收束后切入下一场景。", Prompt: "circleclose", RecommendedDurationSeconds: 1},
	{Key: "ff_circlecrop", Label: "圆形裁切", Description: "带明显中心感的圆形裁切。", Prompt: "circlecrop", RecommendedDurationSeconds: 1},
	{Key: "ff_rectcrop", Label: "矩形裁切", Description: "矩形框线式切换。", Prompt: "rectcrop", RecommendedDurationSeconds: 1},
	{Key: "ff_distance", Label: "距离扩散", Description: "旧画面向外扩散到下一场景。", Prompt: "distance", RecommendedDurationSeconds: 1},
	{Key: "ff_radial", Label: "径向扩散", Description: "有一点放射感的稳定过渡。", Prompt: "radial", RecommendedDurationSeconds: 1},
	{Key: "ff_pixelize", Label: "像素化重组", Description: "先像素化再切入下一场景。", Prompt: "pixelize", RecommendedDurationSeconds: 1},
	{Key: "ff_horzopen", Label: "横向展开", Description: "横向打开到下一场景。", Prompt: "horzopen", RecommendedDurationSeconds: 1},
	{Key: "ff_horzclose", Label: "横向收束", Description: "横向收束后进入下一场景。", Prompt: "horzclose", RecommendedDurationSeconds: 1},
	{Key: "ff_vertopen", Label: "纵向展开", Description: "纵向打开，更适合竖屏。", Prompt: "vertopen", RecommendedDurationSeconds: 1},
	{Key: "ff_vertclose", Label: "纵向收束", Description: "纵向收束过渡。", Prompt: "vertclose", RecommendedDurationSeconds: 1},
	{Key: "ff_diagtl", Label: "对角揭示（左上）", Description: "从左上角斜向揭示下一场景。", Prompt: "diagtl", RecommendedDurationSeconds: 1},
	{Key: "ff_diagtr", Label: "对角揭示（右上）", Description: "从右上角斜向揭示下一场景。", Prompt: "diagtr", RecommendedDurationSeconds: 1},
	{Key: "ff_diagbl", Label: "对角揭示（左下）", Description: "从左下角斜向揭示下一场景。", Prompt: "diagbl", RecommendedDurationSeconds: 1},
	{Key: "ff_diagbr", Label: "对角揭示（右下）", Description: "从右下角斜向揭示下一场景。", Prompt: "diagbr", RecommendedDurationSeconds: 1},
}

func generalGuideTransitionPresetsForEngine(engine string) []generalGuideTransitionPreset {
	switch normalizeGeneralGuideTransitionEngine(engine) {
	case GeneralGuideTransitionEngineWan22:
		return generalGuideTransitionPresetsWan
	case GeneralGuideTransitionEngineFFmpeg:
		return generalGuideTransitionPresetsFFmpeg
	default:
		return generalGuideTransitionPresetsLTX
	}
}

func randomGeneralGuideTransitionPreset(rng *rand.Rand, engine string) generalGuideTransitionPreset {
	presets := generalGuideTransitionPresetsForEngine(engine)
	if len(presets) == 0 {
		return generalGuideTransitionPreset{}
	}
	if rng == nil {
		rng = rand.New(rand.NewSource(time.Now().UnixNano()))
	}
	return presets[rng.Intn(len(presets))]
}

func generalGuideTransitionPresetByKey(key string, engine string) (generalGuideTransitionPreset, bool) {
	trimmed := strings.TrimSpace(key)
	for _, preset := range generalGuideTransitionPresetsForEngine(engine) {
		if preset.Key == trimmed {
			return preset, true
		}
	}
	return generalGuideTransitionPreset{}, false
}

type generalGuideTransitionVideoTaskPayload struct {
	ProjectID    uint  `json:"project_id"`
	TransitionID uint  `json:"transition_id"`
	Seed         int64 `json:"seed"`
}

type updateGeneralGuideTransitionRequest struct {
	TransitionPrompt string `json:"transition_prompt"`
	DurationSeconds  int    `json:"duration_seconds"`
	FramesFromEnd    int    `json:"frames_from_end"`
}

type applyGeneralGuideTransitionPresetRequest struct {
	PresetKey string `json:"preset_key"`
}

type extractGeneralGuideTransitionTailFrameRequest struct {
	FramesFromEnd int `json:"frames_from_end"`
}

type ffprobeVideoStreamInfo struct {
	NbFrames     string `json:"nb_frames"`
	AvgFrameRate string `json:"avg_frame_rate"`
	RFrameRate   string `json:"r_frame_rate"`
	Duration     string `json:"duration"`
}

type ffprobeVideoInfo struct {
	Streams []ffprobeVideoStreamInfo `json:"streams"`
}

func generalGuideTransitionsDir(code string) string {
	return filepath.Join(generalGuideProjectDir(code), "transitions")
}

func generalGuideTransitionFramesDir(code string) string {
	return filepath.Join(generalGuideTransitionsDir(code), "frames")
}

func generalGuideTransitionVideosDir(code string) string {
	return filepath.Join(generalGuideTransitionsDir(code), "videos")
}

func generalGuideTransitionFileKey(transition models.GeneralGuideTransition) string {
	return fmt.Sprintf("transition_%02d_%02d", transition.FromSortOrder, transition.ToSortOrder)
}

func loadGeneralGuideTransitionOr404(c *gin.Context) (*models.GeneralGuideTransition, error) {
	transitionID := strings.TrimSpace(c.Param("transitionId"))
	var transition models.GeneralGuideTransition
	if err := db.DB.First(&transition, transitionID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "讲解转场不存在"})
		return nil, err
	}
	return &transition, nil
}

func normalizeGeneralGuideTransitionPrompt(prompt string) string {
	return normalizeGeneralGuideTransitionPromptForEngine(prompt, getConfiguredGeneralGuideTransitionEngine())
}

func normalizeGeneralGuideTransitionPromptForEngine(prompt string, engine string) string {
	trimmed := strings.TrimSpace(prompt)
	if trimmed == "" {
		if preset := defaultGeneralGuideTransitionPresetForEngine(engine); preset.Key != "" {
			return preset.Prompt
		}
		return trimmed
	}
	switch normalizeGeneralGuideTransitionEngine(engine) {
	case GeneralGuideTransitionEngineFFmpeg:
		for _, preset := range generalGuideTransitionPresetsForEngine(engine) {
			if trimmed == preset.Prompt || trimmed == preset.Key || trimmed == preset.Label {
				return preset.Prompt
			}
		}
		if preset := defaultGeneralGuideTransitionPresetForEngine(engine); preset.Key != "" {
			return preset.Prompt
		}
		return trimmed
	case GeneralGuideTransitionEngineWan22:
		lower := strings.ToLower(trimmed)
		if idx := strings.LastIndex(lower, ", zhuanchang"); idx >= 0 && idx == len(lower)-len(", zhuanchang") {
			trimmed = strings.TrimSpace(trimmed[:idx])
		} else if idx := strings.LastIndex(lower, " zhuanchang"); idx >= 0 && idx == len(lower)-len(" zhuanchang") {
			trimmed = strings.TrimSpace(trimmed[:idx])
		}
		trimmed = strings.TrimRight(trimmed, ",.，。 ")
		return appendGeneralGuideTransitionLandingClause(trimmed, GeneralGuideTransitionEngineWan22)
	default:
		lower := strings.ToLower(trimmed)
		if idx := strings.LastIndex(lower, ", zhuanchang"); idx >= 0 && idx == len(lower)-len(", zhuanchang") {
			trimmed = strings.TrimSpace(trimmed[:idx])
		} else if idx := strings.LastIndex(lower, " zhuanchang"); idx >= 0 && idx == len(lower)-len(" zhuanchang") {
			trimmed = strings.TrimSpace(trimmed[:idx])
		}
		trimmed = strings.TrimRight(trimmed, ",.，。 ")
		trimmed = appendGeneralGuideTransitionLandingClause(trimmed, GeneralGuideTransitionEngineLTX23)
		if strings.HasSuffix(strings.ToLower(trimmed), "zhuanchang") {
			return trimmed
		}
		return trimmed + ", zhuanchang"
	}
}

func defaultGeneralGuideTransitionPresetForEngine(engine string) generalGuideTransitionPreset {
	presets := generalGuideTransitionPresetsForEngine(engine)
	if len(presets) == 0 {
		return generalGuideTransitionPreset{}
	}
	return presets[0]
}

func appendGeneralGuideTransitionLandingClause(prompt string, engine string) string {
	trimmed := strings.TrimSpace(strings.TrimRight(prompt, ",.，。 "))
	if trimmed == "" {
		return trimmed
	}
	lower := strings.ToLower(trimmed)
	if strings.Contains(lower, "soft blackout") || strings.Contains(lower, "dip into shadow") || strings.Contains(lower, "shadowed fade") {
		return trimmed
	}
	clause := generalGuideTransitionLandingClauseLTX
	if normalizeGeneralGuideTransitionEngine(engine) == GeneralGuideTransitionEngineWan22 {
		clause = generalGuideTransitionLandingClauseWan
	}
	return trimmed + ". " + clause
}

func sanitizeGeneralGuideTransitionFramesFromEnd(value int) int {
	if value <= 0 {
		return generalGuideTransitionDefaultFrames
	}
	if value > 60 {
		return 60
	}
	return value
}

func sanitizeGeneralGuideTransitionFramesFromEndForEngine(value int, engine string) int {
	if normalizeGeneralGuideTransitionEngine(engine) == GeneralGuideTransitionEngineFFmpeg {
		return generalGuideTransitionFFmpegFrames
	}
	return sanitizeGeneralGuideTransitionFramesFromEnd(value)
}

func sanitizeGeneralGuideTransitionDurationSeconds(value int) int {
	if value <= 0 {
		return generalGuideTransitionDefaultSecond
	}
	if value > 4 {
		return 4
	}
	return value
}

func randomGeneralGuideTransitionSeed() int64 {
	seed := time.Now().UnixNano()
	if seed < 0 {
		seed = -seed
	}
	if generalGuideTransitionSeedMax > 0 {
		seed = seed % generalGuideTransitionSeedMax
	}
	if seed <= 0 {
		seed = 1
	}
	return seed
}

func generalGuideTransitionNegativePromptForEngine(engine string) string {
	switch normalizeGeneralGuideTransitionEngine(engine) {
	case GeneralGuideTransitionEngineWan22:
		return generalGuideTransitionNegativePromptWan
	case GeneralGuideTransitionEngineFFmpeg:
		return ""
	default:
		return generalGuideTransitionNegativePromptLTX
	}
}

func ListGeneralGuideTransitionPresetOptions(c *gin.Context) {
	engine := getConfiguredGeneralGuideTransitionEngine()
	c.JSON(http.StatusOK, generalGuideTransitionPresetListResponse{
		Engine:  engine,
		Presets: generalGuideTransitionPresetsForEngine(engine),
	})
}

func shouldApplyGeneralGuideTransitionVideoTaskResult(transitionID uint, taskID string) bool {
	var current models.GeneralGuideTransition
	if err := db.DB.Select("video_current_task_id").First(&current, transitionID).Error; err != nil {
		return false
	}
	return strings.TrimSpace(current.VideoCurrentTaskID) == strings.TrimSpace(taskID)
}

func ListGeneralGuideTransitions(c *gin.Context) {
	project, err := loadGeneralGuideProjectOr404(c)
	if err != nil {
		return
	}
	var transitions []models.GeneralGuideTransition
	if err := db.DB.Where("project_id = ?", project.ID).Order("from_sort_order asc, to_sort_order asc, id asc").Find(&transitions).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "读取讲解转场失败"})
		return
	}
	c.JSON(http.StatusOK, transitions)
}

func ApplyGeneralGuideProjectTransitionPreset(c *gin.Context) {
	project, err := loadGeneralGuideProjectOr404(c)
	if err != nil {
		return
	}

	var req applyGeneralGuideTransitionPresetRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求参数格式不正确"})
		return
	}
	engine := getConfiguredGeneralGuideTransitionEngine()
	preset, ok := generalGuideTransitionPresetByKey(req.PresetKey, engine)
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": "未找到对应的转场预设"})
		return
	}

	updates := map[string]interface{}{
		"transition_prompt": normalizeGeneralGuideTransitionPromptForEngine(preset.Prompt, engine),
		"updated_at":        time.Now(),
	}
	if preset.RecommendedDurationSeconds > 0 {
		updates["duration_seconds"] = sanitizeGeneralGuideTransitionDurationSeconds(preset.RecommendedDurationSeconds)
	}
	updates["frames_from_end"] = sanitizeGeneralGuideTransitionFramesFromEndForEngine(generalGuideTransitionDefaultFrames, engine)
	if normalizeGeneralGuideTransitionEngine(engine) == GeneralGuideTransitionEngineFFmpeg {
		updates["tail_frame_image"] = ""
		updates["tail_frame_source_video"] = ""
	}
	if err := db.DB.Model(&models.GeneralGuideTransition{}).
		Where("project_id = ?", project.ID).
		Updates(updates).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "批量应用转场预设失败"})
		return
	}

	var transitions []models.GeneralGuideTransition
	if err := db.DB.Where("project_id = ?", project.ID).Order("from_sort_order asc, to_sort_order asc, id asc").Find(&transitions).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "读取更新后的转场失败"})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"message":     "已覆盖全部转场预设",
		"engine":      engine,
		"preset_key":  preset.Key,
		"preset_name": preset.Label,
		"transitions": transitions,
	})
}

func UpdateGeneralGuideTransition(c *gin.Context) {
	transition, err := loadGeneralGuideTransitionOr404(c)
	if err != nil {
		return
	}
	if transition.VideoStatus == "generating" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "当前转场正在生成，请稍后再改"})
		return
	}

	var req updateGeneralGuideTransitionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求参数格式不正确"})
		return
	}
	engine := getConfiguredGeneralGuideTransitionEngine()
	prompt := normalizeGeneralGuideTransitionPromptForEngine(req.TransitionPrompt, engine)
	if strings.TrimSpace(prompt) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请先填写转场提示词"})
		return
	}
	updates := map[string]interface{}{
		"transition_prompt": prompt,
		"duration_seconds":  sanitizeGeneralGuideTransitionDurationSeconds(req.DurationSeconds),
		"frames_from_end":   sanitizeGeneralGuideTransitionFramesFromEndForEngine(req.FramesFromEnd, engine),
		"updated_at":        time.Now(),
	}
	if normalizeGeneralGuideTransitionEngine(engine) == GeneralGuideTransitionEngineFFmpeg {
		updates["tail_frame_image"] = ""
		updates["tail_frame_source_video"] = ""
	}
	if err := db.DB.Model(&models.GeneralGuideTransition{}).Where("id = ?", transition.ID).Updates(updates).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "保存转场失败"})
		return
	}
	var updated models.GeneralGuideTransition
	if err := db.DB.First(&updated, transition.ID).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "读取最新转场失败"})
		return
	}
	c.JSON(http.StatusOK, updated)
}

func ResetGeneralGuideTransitionState(c *gin.Context) {
	transition, err := loadGeneralGuideTransitionOr404(c)
	if err != nil {
		return
	}
	if err := removeGeneralGuideAsset(transition.TailFrameImage); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "删除旧尾帧失败"})
		return
	}
	if err := removeGeneralGuideAsset(transition.GeneratedVideo); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "删除旧转场视频失败"})
		return
	}
	if err := db.DB.Model(&models.GeneralGuideTransition{}).Where("id = ?", transition.ID).Updates(map[string]interface{}{
		"tail_frame_image":         "",
		"tail_frame_source_video":  "",
		"video_status":             "draft",
		"video_current_task_id":    "",
		"video_last_error":         "",
		"generated_video":          "",
		"video_generated_workflow": "",
		"updated_at":               time.Now(),
	}).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "重置转场状态失败"})
		return
	}
	var updated models.GeneralGuideTransition
	if err := db.DB.First(&updated, transition.ID).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "读取重置后的转场失败"})
		return
	}
	c.JSON(http.StatusOK, updated)
}

func resolveFFprobeBinary() (string, error) {
	var setting models.SystemSettings
	if err := db.DB.Where("key = ?", KeyFFmpegPath).First(&setting).Error; err == nil {
		path := strings.TrimSpace(strings.Trim(setting.Value, `"`))
		if path != "" {
			dir := filepath.Dir(path)
			base := filepath.Base(path)
			candidates := []string{"ffprobe", "ffprobe.exe"}
			if strings.HasSuffix(strings.ToLower(base), ".exe") {
				candidates = []string{"ffprobe.exe", "ffprobe"}
			}
			for _, name := range candidates {
				candidate := filepath.Join(dir, name)
				if _, err := os.Stat(candidate); err == nil {
					return candidate, nil
				}
			}
		}
	}
	ffprobePath, err := exec.LookPath("ffprobe")
	if err != nil {
		return "", fmt.Errorf("ffprobe not found; please configure ffmpeg_path in settings")
	}
	return ffprobePath, nil
}

func ffprobeVideoFramesAndFPS(videoPath string) (int, float64, error) {
	ffprobePath, err := resolveFFprobeBinary()
	if err != nil {
		return 0, 0, err
	}
	cmd := exec.Command(
		ffprobePath,
		"-v", "error",
		"-select_streams", "v:0",
		"-show_entries", "stream=nb_frames,avg_frame_rate,r_frame_rate,duration",
		"-of", "json",
		videoPath,
	)
	output, err := cmd.Output()
	if err != nil {
		return 0, 0, fmt.Errorf("ffprobe failed: %w", err)
	}
	var info ffprobeVideoInfo
	if err := json.Unmarshal(output, &info); err != nil {
		return 0, 0, fmt.Errorf("invalid ffprobe json: %w", err)
	}
	if len(info.Streams) == 0 {
		return 0, 0, fmt.Errorf("video stream missing")
	}
	stream := info.Streams[0]
	parseRate := func(raw string) float64 {
		parts := strings.Split(strings.TrimSpace(raw), "/")
		if len(parts) == 2 {
			num, _ := strconv.ParseFloat(parts[0], 64)
			den, _ := strconv.ParseFloat(parts[1], 64)
			if den != 0 {
				return num / den
			}
		}
		value, _ := strconv.ParseFloat(strings.TrimSpace(raw), 64)
		return value
	}
	fps := parseRate(stream.AvgFrameRate)
	if fps <= 0 {
		fps = parseRate(stream.RFrameRate)
	}
	if fps <= 0 {
		fps = float64(generalGuideTransitionFPS)
	}
	totalFrames := 0
	if strings.TrimSpace(stream.NbFrames) != "" {
		if parsed, parseErr := strconv.Atoi(strings.TrimSpace(stream.NbFrames)); parseErr == nil && parsed > 0 {
			totalFrames = parsed
		}
	}
	if totalFrames <= 0 && strings.TrimSpace(stream.Duration) != "" {
		if seconds, parseErr := strconv.ParseFloat(strings.TrimSpace(stream.Duration), 64); parseErr == nil && seconds > 0 {
			totalFrames = int(math.Round(seconds * fps))
		}
	}
	if totalFrames <= 0 {
		return 0, fps, fmt.Errorf("unable to determine total frames")
	}
	return totalFrames, fps, nil
}

func extractGeneralGuideTransitionTailFrameAsset(project models.GeneralGuideProject, transition models.GeneralGuideTransition, sourceVideoPath string, framesFromEnd int) (string, error) {
	absVideoPath, err := assetWebPathToAbs(sourceVideoPath)
	if err != nil {
		return "", err
	}
	totalFrames, _, err := ffprobeVideoFramesAndFPS(absVideoPath)
	if err != nil {
		return "", err
	}
	targetFrame := totalFrames - sanitizeGeneralGuideTransitionFramesFromEnd(framesFromEnd)
	if targetFrame < 0 {
		targetFrame = 0
	}
	if err := os.MkdirAll(generalGuideTransitionFramesDir(project.Code), 0755); err != nil {
		return "", err
	}
	savePath := filepath.Join(
		generalGuideTransitionFramesDir(project.Code),
		fmt.Sprintf("%s_tail_%d_%d.png", generalGuideTransitionFileKey(transition), targetFrame, time.Now().UnixNano()),
	)
	filter := fmt.Sprintf("select=eq(n\\,%d)", targetFrame)
	if err := runFFmpeg("-i", absVideoPath, "-vf", filter, "-vsync", "vfr", "-frames:v", "1", savePath, "-y"); err != nil {
		return "", err
	}
	return "/" + filepath.ToSlash(savePath), nil
}

func ExtractGeneralGuideTransitionTailFrame(c *gin.Context) {
	if normalizeGeneralGuideTransitionEngine(getConfiguredGeneralGuideTransitionEngine()) == GeneralGuideTransitionEngineFFmpeg {
		c.JSON(http.StatusBadRequest, gin.H{"error": "FFmpeg 转场固定使用上一行最后一帧，无需手动抽尾帧"})
		return
	}
	transition, err := loadGeneralGuideTransitionOr404(c)
	if err != nil {
		return
	}
	var req extractGeneralGuideTransitionTailFrameRequest
	if err := c.ShouldBindJSON(&req); err != nil && err.Error() != "EOF" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求参数格式不正确"})
		return
	}
	var project models.GeneralGuideProject
	if err := db.DB.First(&project, transition.ProjectID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "综合讲解项目不存在"})
		return
	}
	var fromScene models.GeneralGuideScene
	if err := db.DB.First(&fromScene, transition.FromSceneID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "转场起始场景不存在"})
		return
	}
	if strings.TrimSpace(fromScene.GeneratedVideo) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请先生成上一行的视频，再抽取尾帧"})
		return
	}
	framesFromEnd := sanitizeGeneralGuideTransitionFramesFromEndForEngine(req.FramesFromEnd, getConfiguredGeneralGuideTransitionEngine())
	if req.FramesFromEnd == 0 {
		framesFromEnd = sanitizeGeneralGuideTransitionFramesFromEndForEngine(transition.FramesFromEnd, getConfiguredGeneralGuideTransitionEngine())
	}
	webPath, err := extractGeneralGuideTransitionTailFrameAsset(project, *transition, fromScene.GeneratedVideo, framesFromEnd)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("抽取尾帧失败：%v", err)})
		return
	}
	if err := removeGeneralGuideAsset(transition.TailFrameImage); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "清理旧尾帧失败"})
		return
	}
	if err := db.DB.Model(&models.GeneralGuideTransition{}).Where("id = ?", transition.ID).Updates(map[string]interface{}{
		"frames_from_end":         framesFromEnd,
		"tail_frame_image":        webPath,
		"tail_frame_source_video": fromScene.GeneratedVideo,
		"updated_at":              time.Now(),
	}).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "写回尾帧失败"})
		return
	}
	var updated models.GeneralGuideTransition
	if err := db.DB.First(&updated, transition.ID).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "读取最新转场失败"})
		return
	}
	c.JSON(http.StatusOK, updated)
}

func generalGuideTransitionNextImage(scene models.GeneralGuideScene) string {
	return strings.TrimSpace(scene.GeneratedImage)
}

func setGeneralGuideTransitionConstantByTitle(workflowJSON map[string]interface{}, title string, value interface{}) {
	for _, nodeRaw := range workflowJSON {
		node, ok := nodeRaw.(map[string]interface{})
		if !ok {
			continue
		}
		classType, _ := node["class_type"].(string)
		if classType != "INTConstant" && classType != "PrimitiveFloat" {
			continue
		}
		meta, _ := node["_meta"].(map[string]interface{})
		nodeTitle, _ := meta["title"].(string)
		if strings.TrimSpace(nodeTitle) != title {
			continue
		}
		inputs, _ := node["inputs"].(map[string]interface{})
		if inputs == nil {
			continue
		}
		inputs["value"] = value
	}
}

func buildGeneralGuideTransitionWorkflowLTX(project models.GeneralGuideProject, transition models.GeneralGuideTransition, fromScene models.GeneralGuideScene, toScene models.GeneralGuideScene, seed int64) (map[string]interface{}, string, error) {
	raw, err := os.ReadFile(generalGuideTransitionWorkflowPathLTX)
	if err != nil {
		return nil, "", err
	}
	var workflowJSON map[string]interface{}
	if err := json.Unmarshal(raw, &workflowJSON); err != nil {
		return nil, "", err
	}
	clone, err := cloneStoreVisitWorkflow(workflowJSON)
	if err != nil {
		return nil, "", err
	}
	workflowJSON = clone

	tailImage := strings.TrimSpace(transition.TailFrameImage)
	if tailImage == "" {
		tailImage, err = extractGeneralGuideTransitionTailFrameAsset(project, transition, fromScene.GeneratedVideo, transition.FramesFromEnd)
		if err != nil {
			return nil, "", err
		}
		if shouldApplyGeneralGuideTransitionVideoTaskResult(transition.ID, transition.VideoCurrentTaskID) || strings.TrimSpace(transition.VideoCurrentTaskID) == "" {
			_ = db.DB.Model(&models.GeneralGuideTransition{}).Where("id = ?", transition.ID).Updates(map[string]interface{}{
				"tail_frame_image":        tailImage,
				"tail_frame_source_video": fromScene.GeneratedVideo,
				"updated_at":              time.Now(),
			}).Error
		}
	}
	nextImage := generalGuideTransitionNextImage(toScene)
	if nextImage == "" {
		return nil, "", fmt.Errorf("下一行缺少已生成图片，请先生成下一行图片")
	}

	tailAbs, err := assetWebPathToAbs(tailImage)
	if err != nil {
		return nil, "", err
	}
	nextAbs, err := assetWebPathToAbs(nextImage)
	if err != nil {
		return nil, "", err
	}
	tailName, err := UploadToComfyUIInput(tailAbs)
	if err != nil {
		return nil, "", err
	}
	nextName, err := UploadToComfyUIInput(nextAbs)
	if err != nil {
		return nil, "", err
	}

	prompt := normalizeGeneralGuideTransitionPromptForEngine(transition.TransitionPrompt, GeneralGuideTransitionEngineLTX23)
	if err := setStoreVisitWorkflowInput(workflowJSON, "16", "text", prompt); err != nil {
		return nil, "", err
	}
	if err := setStoreVisitWorkflowInput(workflowJSON, "11", "text", generalGuideTransitionNegativePromptForEngine(GeneralGuideTransitionEngineLTX23)); err != nil {
		return nil, "", err
	}
	if err := setStoreVisitWorkflowInput(workflowJSON, "45", "image", tailName); err != nil {
		return nil, "", err
	}
	if err := setStoreVisitWorkflowInput(workflowJSON, "56", "image", nextName); err != nil {
		return nil, "", err
	}
	if node, ok := workflowJSON["197"].(map[string]interface{}); ok {
		if inputs, ok := node["inputs"].(map[string]interface{}); ok {
			inputs["model"] = []interface{}{"243", float64(0)}
		}
	}
	if seed <= 0 {
		seed = randomGeneralGuideTransitionSeed()
	}
	if err := setStoreVisitWorkflowInput(workflowJSON, "214", "seed", seed); err != nil {
		return nil, "", err
	}
	setGeneralGuideTransitionConstantByTitle(workflowJSON, "FPS", float64(generalGuideTransitionFPS))
	setGeneralGuideTransitionConstantByTitle(workflowJSON, "LENGTH (in seconds)", sanitizeGeneralGuideTransitionDurationSeconds(transition.DurationSeconds))
	if err := setStoreVisitWorkflowInput(workflowJSON, "43", "filename_prefix", fmt.Sprintf("%s_%s", project.Code, generalGuideTransitionFileKey(transition))); err != nil {
		return nil, "", err
	}
	_ = setStoreVisitWorkflowInput(workflowJSON, "43", "save_output", true)
	if node, ok := workflowJSON["43"].(map[string]interface{}); ok {
		if inputs, ok := node["inputs"].(map[string]interface{}); ok {
			delete(inputs, "audio")
			inputs["trim_to_audio"] = false
		}
	}
	return workflowJSON, workflowDisplayNameFromPath(generalGuideTransitionWorkflowPathLTX), nil
}

func buildGeneralGuideTransitionWorkflowWan(project models.GeneralGuideProject, transition models.GeneralGuideTransition, fromScene models.GeneralGuideScene, toScene models.GeneralGuideScene, seed int64) (map[string]interface{}, string, error) {
	raw, err := os.ReadFile(generalGuideTransitionWorkflowPathWan)
	if err != nil {
		return nil, "", err
	}
	var workflowJSON map[string]interface{}
	if err := json.Unmarshal(raw, &workflowJSON); err != nil {
		return nil, "", err
	}
	clone, err := cloneStoreVisitWorkflow(workflowJSON)
	if err != nil {
		return nil, "", err
	}
	workflowJSON = clone

	tailImage := strings.TrimSpace(transition.TailFrameImage)
	if tailImage == "" {
		tailImage, err = extractGeneralGuideTransitionTailFrameAsset(project, transition, fromScene.GeneratedVideo, transition.FramesFromEnd)
		if err != nil {
			return nil, "", err
		}
		if shouldApplyGeneralGuideTransitionVideoTaskResult(transition.ID, transition.VideoCurrentTaskID) || strings.TrimSpace(transition.VideoCurrentTaskID) == "" {
			_ = db.DB.Model(&models.GeneralGuideTransition{}).Where("id = ?", transition.ID).Updates(map[string]interface{}{
				"tail_frame_image":        tailImage,
				"tail_frame_source_video": fromScene.GeneratedVideo,
				"updated_at":              time.Now(),
			}).Error
		}
	}
	nextImage := generalGuideTransitionNextImage(toScene)
	if nextImage == "" {
		return nil, "", fmt.Errorf("下一行缺少已生成图片，请先生成下一行图片")
	}

	tailAbs, err := assetWebPathToAbs(tailImage)
	if err != nil {
		return nil, "", err
	}
	nextAbs, err := assetWebPathToAbs(nextImage)
	if err != nil {
		return nil, "", err
	}
	tailName, err := UploadToComfyUIInput(tailAbs)
	if err != nil {
		return nil, "", err
	}
	nextName, err := UploadToComfyUIInput(nextAbs)
	if err != nil {
		return nil, "", err
	}

	prompt := normalizeGeneralGuideTransitionPromptForEngine(transition.TransitionPrompt, GeneralGuideTransitionEngineWan22)
	if err := setStoreVisitWorkflowInput(workflowJSON, "6", "text", prompt); err != nil {
		return nil, "", err
	}
	if err := setStoreVisitWorkflowInput(workflowJSON, "7", "text", generalGuideTransitionNegativePromptForEngine(GeneralGuideTransitionEngineWan22)); err != nil {
		return nil, "", err
	}
	if err := setStoreVisitWorkflowInput(workflowJSON, "68", "image", tailName); err != nil {
		return nil, "", err
	}
	if err := setStoreVisitWorkflowInput(workflowJSON, "62", "image", nextName); err != nil {
		return nil, "", err
	}
	if seed <= 0 {
		seed = randomGeneralGuideTransitionSeed()
	}
	_ = setStoreVisitWorkflowInput(workflowJSON, "57", "noise_seed", seed)
	_ = setStoreVisitWorkflowInput(workflowJSON, "58", "noise_seed", seed)
	_ = setStoreVisitWorkflowInput(workflowJSON, "60", "fps", generalGuideTransitionFPS)
	if err := setStoreVisitWorkflowInput(workflowJSON, "61", "filename_prefix", fmt.Sprintf("video/%s_%s", project.Code, generalGuideTransitionFileKey(transition))); err != nil {
		return nil, "", err
	}
	_ = setStoreVisitWorkflowInput(workflowJSON, "61", "format", "auto")
	_ = setStoreVisitWorkflowInput(workflowJSON, "61", "codec", "auto")

	width := toScene.VideoWidth
	height := toScene.VideoHeight
	if width <= 0 || height <= 0 {
		width, height = generalGuideDefaultVideoSize(toScene.SceneType, toScene.NeedPresenter)
	}
	frameCount := generalGuideTransitionFPS*sanitizeGeneralGuideTransitionDurationSeconds(transition.DurationSeconds) + 1
	_ = setStoreVisitWorkflowInput(workflowJSON, "67", "width", width)
	_ = setStoreVisitWorkflowInput(workflowJSON, "67", "height", height)
	_ = setStoreVisitWorkflowInput(workflowJSON, "67", "length", frameCount)

	return workflowJSON, workflowDisplayNameFromPath(generalGuideTransitionWorkflowPathWan), nil
}

func buildGeneralGuideTransitionVideoFFmpeg(project models.GeneralGuideProject, transition models.GeneralGuideTransition, fromScene models.GeneralGuideScene, toScene models.GeneralGuideScene) (string, string, error) {
	preparedTransition, err := ensureGeneralGuideTransitionTailFramePrepared(project, transition, fromScene)
	if err != nil {
		return "", "", err
	}
	tailAbs, err := assetWebPathToAbs(preparedTransition.TailFrameImage)
	if err != nil {
		return "", "", err
	}
	nextImage := generalGuideTransitionNextImage(toScene)
	if nextImage == "" {
		return "", "", fmt.Errorf("下一行缺少已生成图片，请先生成下一行图片")
	}
	nextAbs, err := assetWebPathToAbs(nextImage)
	if err != nil {
		return "", "", err
	}

	width := toScene.VideoWidth
	height := toScene.VideoHeight
	if width <= 0 || height <= 0 {
		width, height = generalGuideDefaultVideoSize(toScene.SceneType, toScene.NeedPresenter)
	}
	durationSeconds := sanitizeGeneralGuideTransitionDurationSeconds(preparedTransition.DurationSeconds)
	preset, ok := generalGuideTransitionPresetByKey(strings.TrimSpace(preparedTransition.TransitionPrompt), GeneralGuideTransitionEngineFFmpeg)
	if !ok {
		for _, item := range generalGuideTransitionPresetsForEngine(GeneralGuideTransitionEngineFFmpeg) {
			if item.Prompt == strings.TrimSpace(preparedTransition.TransitionPrompt) {
				preset = item
				ok = true
				break
			}
		}
	}
	if !ok {
		preset = defaultGeneralGuideTransitionPresetForEngine(GeneralGuideTransitionEngineFFmpeg)
	}

	if err := os.MkdirAll(generalGuideTransitionVideosDir(project.Code), 0755); err != nil {
		return "", "", err
	}
	savePath := filepath.Join(generalGuideTransitionVideosDir(project.Code), fmt.Sprintf("%s_%d.mp4", generalGuideTransitionFileKey(preparedTransition), time.Now().UnixNano()))
	durationText := fmt.Sprintf("%.3f", float64(durationSeconds))
	filter := fmt.Sprintf(
		"[0:v]fps=%d,scale=%d:%d:force_original_aspect_ratio=decrease,pad=%d:%d:(ow-iw)/2:(oh-ih)/2:color=black,setsar=1,format=yuv420p[v0];"+
			"[1:v]fps=%d,scale=%d:%d:force_original_aspect_ratio=decrease,pad=%d:%d:(ow-iw)/2:(oh-ih)/2:color=black,setsar=1,format=yuv420p[v1];"+
			"[v0][v1]xfade=transition=%s:duration=%s:offset=0,format=yuv420p[v]",
		generalGuideTransitionFPS, width, height, width, height,
		generalGuideTransitionFPS, width, height, width, height,
		preset.Prompt, durationText,
	)
	if err := runFFmpeg(
		"-y",
		"-v", "error",
		"-loop", "1",
		"-t", durationText,
		"-i", tailAbs,
		"-loop", "1",
		"-t", durationText,
		"-i", nextAbs,
		"-filter_complex", filter,
		"-map", "[v]",
		"-r", strconv.Itoa(generalGuideTransitionFPS),
		"-pix_fmt", "yuv420p",
		"-movflags", "+faststart",
		savePath,
	); err != nil {
		return "", "", err
	}
	return "/" + filepath.ToSlash(savePath), fmt.Sprintf("FFmpeg xfade（%s）", preset.Label), nil
}

func buildGeneralGuideTransitionWorkflow(project models.GeneralGuideProject, transition models.GeneralGuideTransition, fromScene models.GeneralGuideScene, toScene models.GeneralGuideScene, seed int64) (map[string]interface{}, string, error) {
	switch getConfiguredGeneralGuideTransitionEngine() {
	case GeneralGuideTransitionEngineWan22:
		return buildGeneralGuideTransitionWorkflowWan(project, transition, fromScene, toScene, seed)
	case GeneralGuideTransitionEngineFFmpeg:
		return nil, "", fmt.Errorf("ffmpeg transition does not use a comfy workflow")
	default:
		return buildGeneralGuideTransitionWorkflowLTX(project, transition, fromScene, toScene, seed)
	}
}

func waitForGeneralGuideTransitionVideoOutput(promptID string, projectCode string, key string) (string, error) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		history, err := GetComfyHistory(promptID)
		if err != nil {
			continue
		}
		outputs, ok := history["outputs"].(map[string]interface{})
		if !ok {
			continue
		}
		for _, nodeOutput := range outputs {
			outputMap, ok := nodeOutput.(map[string]interface{})
			if !ok {
				continue
			}
			var fileData map[string]interface{}
			if gifs, ok := outputMap["gifs"].([]interface{}); ok && len(gifs) > 0 {
				fileData, _ = gifs[0].(map[string]interface{})
			} else if images, ok := outputMap["images"].([]interface{}); ok && len(images) > 0 {
				fileData, _ = images[0].(map[string]interface{})
			}
			if fileData == nil {
				continue
			}
			filename, _ := fileData["filename"].(string)
			subfolder, _ := fileData["subfolder"].(string)
			typeStr, _ := fileData["type"].(string)
			if filename == "" {
				continue
			}
			saveDir := generalGuideTransitionVideosDir(projectCode)
			if err := os.MkdirAll(saveDir, 0755); err != nil {
				return "", err
			}
			ext := filepath.Ext(filename)
			if ext == "" {
				ext = ".mp4"
			}
			savePath := filepath.Join(saveDir, fmt.Sprintf("%s_%d%s", key, time.Now().UnixNano(), ext))
			if err := DownloadComfyImage(filename, subfolder, typeStr, savePath); err != nil {
				return "", err
			}
			return "/" + filepath.ToSlash(savePath), nil
		}
	}
	return "", nil
}

func GenerateGeneralGuideTransitionVideo(c *gin.Context) {
	transition, err := loadGeneralGuideTransitionOr404(c)
	if err != nil {
		return
	}
	var project models.GeneralGuideProject
	if err := db.DB.First(&project, transition.ProjectID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "综合讲解项目不存在"})
		return
	}
	var fromScene models.GeneralGuideScene
	if err := db.DB.First(&fromScene, transition.FromSceneID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "上一行场景不存在"})
		return
	}
	var toScene models.GeneralGuideScene
	if err := db.DB.First(&toScene, transition.ToSceneID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "下一行场景不存在"})
		return
	}
	if strings.TrimSpace(fromScene.GeneratedVideo) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请先生成上一行视频"})
		return
	}
	if strings.TrimSpace(generalGuideTransitionNextImage(toScene)) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "下一行缺少已生成图片，请先生成下一行图片"})
		return
	}
	taskID, err := startGeneralGuideTransitionVideoTask(transition, &project)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "提交转场生成任务失败"})
		return
	}
	c.JSON(http.StatusAccepted, gin.H{
		"message": "转场生成任务已提交",
		"task_id": taskID,
	})
}

func startGeneralGuideTransitionVideoTask(transition *models.GeneralGuideTransition, project *models.GeneralGuideProject) (string, error) {
	payload := generalGuideTransitionVideoTaskPayload{
		ProjectID:    project.ID,
		TransitionID: transition.ID,
		Seed:         randomGeneralGuideTransitionSeed(),
	}
	taskRecord, err := task.GlobalTaskManager.AddTask("render_general_guide_transition_video", payload)
	if err != nil {
		return "", err
	}
	if err := db.DB.Model(&models.GeneralGuideTransition{}).Where("id = ?", transition.ID).Updates(map[string]interface{}{
		"video_status":          "generating",
		"video_current_task_id": taskRecord.ID,
		"video_last_error":      "",
		"updated_at":            time.Now(),
	}).Error; err != nil {
		return "", err
	}
	return taskRecord.ID, nil
}

func HandleRenderGeneralGuideTransitionVideoTask(t *models.Task) (interface{}, error) {
	var payload generalGuideTransitionVideoTaskPayload
	if err := json.Unmarshal([]byte(t.Payload), &payload); err != nil {
		return nil, fmt.Errorf("invalid payload: %v", err)
	}
	var project models.GeneralGuideProject
	if err := db.DB.First(&project, payload.ProjectID).Error; err != nil {
		return nil, fmt.Errorf("综合讲解项目不存在")
	}
	var transition models.GeneralGuideTransition
	if err := db.DB.First(&transition, payload.TransitionID).Error; err != nil {
		return nil, fmt.Errorf("讲解转场不存在")
	}
	var fromScene models.GeneralGuideScene
	if err := db.DB.First(&fromScene, transition.FromSceneID).Error; err != nil {
		return nil, fmt.Errorf("上一行场景不存在")
	}
	var toScene models.GeneralGuideScene
	if err := db.DB.First(&toScene, transition.ToSceneID).Error; err != nil {
		return nil, fmt.Errorf("下一行场景不存在")
	}

	engine := normalizeGeneralGuideTransitionEngine(getConfiguredGeneralGuideTransitionEngine())
	var workflowLabel string
	var webPath string
	var err error
	if engine == GeneralGuideTransitionEngineFFmpeg {
		webPath, workflowLabel, err = buildGeneralGuideTransitionVideoFFmpeg(project, transition, fromScene, toScene)
	} else {
		workflowJSON, label, buildErr := buildGeneralGuideTransitionWorkflow(project, transition, fromScene, toScene, payload.Seed)
		if buildErr != nil {
			err = buildErr
		} else {
			workflowLabel = label
			logComfyWorkflowPayload("General Guide Transition ComfyUI Payload", workflowLabel, workflowJSON)
			var promptID string
			promptID, err = QueueComfyPrompt(workflowJSON)
			if err == nil {
				webPath, err = waitForGeneralGuideTransitionVideoOutput(promptID, project.Code, generalGuideTransitionFileKey(transition))
			}
		}
	}
	if err != nil {
		if shouldApplyGeneralGuideTransitionVideoTaskResult(transition.ID, t.ID) {
			_ = db.DB.Model(&models.GeneralGuideTransition{}).Where("id = ?", transition.ID).Updates(map[string]interface{}{
				"video_status":          "failed",
				"video_current_task_id": "",
				"video_last_error":      err.Error(),
				"updated_at":            time.Now(),
			}).Error
		}
		return nil, err
	}
	if !shouldApplyGeneralGuideTransitionVideoTaskResult(transition.ID, t.ID) {
		return gin.H{"transition_id": transition.ID, "ignored": true}, nil
	}
	if err := removeGeneralGuideAsset(transition.GeneratedVideo); err != nil {
		return nil, err
	}
	if err := db.DB.Model(&models.GeneralGuideTransition{}).Where("id = ?", transition.ID).Updates(map[string]interface{}{
		"generated_video":          webPath,
		"video_generated_workflow": workflowLabel,
		"video_status":             "generated",
		"video_current_task_id":    "",
		"video_last_error":         "",
		"updated_at":               time.Now(),
	}).Error; err != nil {
		return nil, err
	}
	return gin.H{
		"transition_id": transition.ID,
		"generated":     true,
	}, nil
}
