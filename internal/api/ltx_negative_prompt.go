package api

import "strings"

const fixedLTXVideoNegativePromptEN = "cartoon, still image, bad quality, subtitles, caption, text, text overlay, on-screen text, watermark, logo, lower-third, speech bubble, dialogue box, readable signage, ui elements, overlay, overlay effects, bad hands, malformed hands, deformed hands, extra hands, duplicate hands, missing hands, fused hands, merged hands, interlocked fingers, fused fingers, merged fingers, extra fingers, missing fingers, malformed fingers, deformed fingers, broken fingers, twisted fingers, deformed thumbs, malformed thumbs, extra thumbs, missing thumbs, merged limbs, fused arms, extra arms, malformed limbs"

var fixedLTXPositiveForbiddenMarkers = []string{
	"禁止字幕",
	"禁止画面文字",
	"禁止水印",
	"禁止界面元素",
	"禁止采访感",
	"人物脸部不可改变",
	"人物年龄感不可改变",
	"人物体型不可改变",
	"禁止变成另一个人",
}

func getFixedLTXVideoNegativePromptEN() string {
	return strings.TrimSpace(fixedLTXVideoNegativePromptEN)
}

func getFixedLTXPositiveForbiddenMarkers() []string {
	return fixedLTXPositiveForbiddenMarkers
}
