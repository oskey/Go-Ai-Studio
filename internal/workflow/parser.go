package workflow

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"kt-ai-studio/internal/models"
)

// ParseWorkflow 解析 ComfyUI 工作流 JSON 文件并提取元数据
func ParseWorkflow(filePath string) (*models.WorkflowMetadata, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, err
	}

	var wf models.WorkflowJSON
	if err := json.Unmarshal(data, &wf); err != nil {
		return nil, err
	}

	meta := &models.WorkflowMetadata{
		FilePath:       filePath,
		FileName:       filepath.Base(filePath),
		Type:           "unknown",
		SeedInputKey:   "seed",
		WidthInputKey:  "width",
		HeightInputKey: "height",
		FPSInputKey:    "fps",
		LengthInputKey: "frame_count",
	}

	var ksamplerIDs []string
	// var outputNodeID string // 暂时未使用

	for id, node := range wf {
		// 检测输出节点和工作流名称
		if node.ClassType == "SaveImage" || node.ClassType == "SaveImageWebsocket" {
			meta.Type = "image"
			// outputNodeID = id
			if prefix, ok := node.Inputs["filename_prefix"].(string); ok {
				meta.WorkflowName = prefix
			}
		} else if node.ClassType == "SaveVideo" || node.ClassType == "VHS_VideoCombine" {
			meta.Type = "video"
			// outputNodeID = id
			if prefix, ok := node.Inputs["filename_prefix"].(string); ok {
				meta.WorkflowName = prefix
			}
		}

		// 检测种子 (Seed)
		if _, ok := node.Inputs["seed"]; ok {
			meta.SeedNodeID = id
			meta.SeedInputKey = "seed"
		} else if _, ok := node.Inputs["noise_seed"]; ok {
			meta.SeedNodeID = id
			meta.SeedInputKey = "noise_seed"
		}

		// 检测尺寸 (宽度/高度)
		// 优先考虑 EmptyLatentImage 或类似节点
		if _, ok := node.Inputs["width"]; ok {
			if _, ok2 := node.Inputs["height"]; ok2 {
				// 尽量避免 "Resize" 节点，首选 "EmptyLatent"
				isResize := strings.Contains(strings.ToLower(node.ClassType), "resize")
				if !isResize || meta.WidthNodeID == "" {
					meta.WidthNodeID = id
					meta.HeightNodeID = id
					meta.WidthInputKey = "width"
					meta.HeightInputKey = "height"
				}
			}
		}

		// 检测 FPS / 帧率
		if _, ok := node.Inputs["fps"]; ok {
			meta.FPSNodeID = id
			meta.FPSInputKey = "fps"
		} else if _, ok := node.Inputs["frame_rate"]; ok {
			meta.FPSNodeID = id
			meta.FPSInputKey = "frame_rate"
		}

		// 检测长度 / 帧数
		if _, ok := node.Inputs["length"]; ok {
			meta.LengthNodeID = id
			meta.LengthInputKey = "length"
		} else if _, ok := node.Inputs["frames_number"]; ok {
			// 针对 LTXVEmptyLatentAudio 或类似节点
			meta.LengthNodeID = id
			meta.LengthInputKey = "frames_number"
		} else if _, ok := node.Inputs["frame_count"]; ok {
			meta.LengthNodeID = id
			meta.LengthInputKey = "frame_count"
		}

		// 检测 KSampler (用于查找提示词)
		if strings.Contains(node.ClassType, "KSampler") || strings.Contains(node.ClassType, "SamplerCustom") {
			ksamplerIDs = append(ksamplerIDs, id)
		}
	}

	// 如果在 filename_prefix 中未找到，则回退到文件名作为工作流名称
	if meta.WorkflowName == "" {
		name := strings.TrimSuffix(meta.FileName, filepath.Ext(meta.FileName))
		meta.WorkflowName = name
	}

	// 从 KSampler 追踪提示词
	// Iterate ALL KSamplers until we find valid Prompt nodes
	for _, ksamplerID := range ksamplerIDs {
		sampler := wf[ksamplerID]

		resolveBranchFromSource := func(sourceID string, branch string) {
			nodeID, key := findPromptNodeByBranch(wf, sourceID, branch, 0)
			if nodeID == "" {
				return
			}
			if branch == "positive" && meta.PositiveNodeID == "" {
				meta.PositiveNodeID = nodeID
				meta.PositiveInputKey = key
			}
			if branch == "negative" && meta.NegativeNodeID == "" {
				meta.NegativeNodeID = nodeID
				meta.NegativeInputKey = key
			}
		}

		// 正向提示词
		if meta.PositiveNodeID == "" {
			if posInput, ok := sampler.Inputs["positive"].([]interface{}); ok && len(posInput) > 0 {
				if sourceID, ok := posInput[0].(string); ok {
					resolveBranchFromSource(sourceID, "positive")
				}
			}
		}

		// 负向提示词
		if meta.NegativeNodeID == "" {
			if negInput, ok := sampler.Inputs["negative"].([]interface{}); ok && len(negInput) > 0 {
				if sourceID, ok := negInput[0].(string); ok {
					resolveBranchFromSource(sourceID, "negative")
				}
			}
		}

		if guiderInput, ok := sampler.Inputs["guider"].([]interface{}); ok && len(guiderInput) > 0 {
			if guiderID, ok := guiderInput[0].(string); ok {
				if meta.PositiveNodeID == "" {
					resolveBranchFromSource(guiderID, "positive")
				}
				if meta.NegativeNodeID == "" {
					resolveBranchFromSource(guiderID, "negative")
				}
			}
		}

		// If both found, we can stop
		if meta.PositiveNodeID != "" && meta.NegativeNodeID != "" {
			break
		}
	}

	return meta, nil
}

// GetDependencies 解析工作流文件并提取模型依赖
func GetDependencies(filePath string) ([]models.ModelDependency, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, err
	}

	var wf models.WorkflowJSON
	if err := json.Unmarshal(data, &wf); err != nil {
		return nil, err
	}

	var deps []models.ModelDependency

	for id, node := range wf {
		classType := node.ClassType

		// 检查 Checkpoint
		if classType == "CheckpointLoaderSimple" || classType == "CheckpointLoader" {
			if name, ok := node.Inputs["ckpt_name"].(string); ok {
				deps = append(deps, models.ModelDependency{
					FileName:  name,
					Type:      "checkpoints",
					NodeID:    id,
					ClassType: classType,
				})
			}
		}

		// 检查 LoRA
		if classType == "LoraLoader" || classType == "LoraLoaderModelOnly" {
			if name, ok := node.Inputs["lora_name"].(string); ok {
				deps = append(deps, models.ModelDependency{
					FileName:  name,
					Type:      "loras",
					NodeID:    id,
					ClassType: classType,
				})
			}
		}

		// 检查 VAE
		if classType == "VAELoader" {
			if name, ok := node.Inputs["vae_name"].(string); ok {
				deps = append(deps, models.ModelDependency{
					FileName:  name,
					Type:      "vae",
					NodeID:    id,
					ClassType: classType,
				})
			}
		}

		// 检查 ControlNet
		if classType == "ControlNetLoader" || classType == "ControlNetLoaderAdvanced" {
			if name, ok := node.Inputs["control_net_name"].(string); ok {
				deps = append(deps, models.ModelDependency{
					FileName:  name,
					Type:      "controlnet",
					NodeID:    id,
					ClassType: classType,
				})
			}
		}

		// 检查 CLIP Vision
		if classType == "CLIPVisionLoader" {
			if name, ok := node.Inputs["clip_name"].(string); ok {
				deps = append(deps, models.ModelDependency{
					FileName:  name,
					Type:      "clip_vision",
					NodeID:    id,
					ClassType: classType,
				})
			}
		}

		// 检查 Upscale Models
		if classType == "UpscaleModelLoader" {
			if name, ok := node.Inputs["model_name"].(string); ok {
				deps = append(deps, models.ModelDependency{
					FileName:  name,
					Type:      "upscale_models",
					NodeID:    id,
					ClassType: classType,
				})
			}
		}

		// 检查 UNET
		if classType == "UNETLoader" {
			if name, ok := node.Inputs["unet_name"].(string); ok {
				deps = append(deps, models.ModelDependency{
					FileName:  name,
					Type:      "unet",
					NodeID:    id,
					ClassType: classType,
				})
			}
		}
	}

	return deps, nil
}

func findPromptNodeByBranch(wf models.WorkflowJSON, currentID string, branch string, depth int) (string, string) {
	if depth > 20 {
		return "", ""
	}

	node, exists := wf[currentID]
	if !exists {
		return "", ""
	}

	if branch == "positive" || branch == "negative" {
		if branchInput, ok := node.Inputs[branch].([]interface{}); ok && len(branchInput) > 0 {
			if sourceID, ok := branchInput[0].(string); ok {
				return findPromptNodeByBranch(wf, sourceID, branch, depth+1)
			}
		}
	}

	return findCLIPTextEncode(wf, currentID, depth)
}

// findCLIPTextEncode 递归辅助函数，用于查找 CLIPTextEncode 节点
// 限制深度以避免死循环
// Returns: NodeID, InputKey
func findCLIPTextEncode(wf models.WorkflowJSON, currentID string, depth int) (string, string) {
	if depth > 20 { // Increased depth limit to handle deeper nested workflows
		return "", ""
	}

	node, exists := wf[currentID]
	if !exists {
		return "", ""
	}

	// Primitive text holders should be preferred when prompt text is generated through helper nodes.
	if node.ClassType == "PrimitiveStringMultiline" || node.ClassType == "PrimitiveString" {
		return currentID, "value"
	}

	// Support standard CLIPTextEncode and custom prompt generator chains.
	if node.ClassType == "CLIPTextEncode" || node.ClassType == "ShowText" {
		if textInput, ok := node.Inputs["text"].([]interface{}); ok && len(textInput) > 0 {
			if sourceID, ok := textInput[0].(string); ok {
				nodeID, key := findCLIPTextEncode(wf, sourceID, depth+1)
				if nodeID != "" {
					return nodeID, key
				}
			}
		}
		return currentID, "text"
	}
	// Qwen Image Edit / LTX prompt-generation nodes use "prompt" as key
	if node.ClassType == "TextEncodeQwenImageEditPlus" || node.ClassType == "TextEncode" || node.ClassType == "QwenImageEditPlus" || node.ClassType == "TextGenerateLTX2Prompt" {
		if promptInput, ok := node.Inputs["prompt"].([]interface{}); ok && len(promptInput) > 0 {
			if sourceID, ok := promptInput[0].(string); ok {
				nodeID, key := findCLIPTextEncode(wf, sourceID, depth+1)
				if nodeID != "" {
					return nodeID, key
				}
			}
		}
		return currentID, "prompt"
	}

	// 如果遇到 ConditioningZeroOut 或其他明确表示空的节点，停止追踪并返回空
	// 这样可以避免将这些节点误认为是连接路径的一部分，虽然它们通常没有 conditioning 输入，
	// 但明确处理更安全。
	if node.ClassType == "ConditioningZeroOut" {
		return "", ""
	}

	// 首先检查 "conditioning" 输入 (最常见)
	if condInput, ok := node.Inputs["conditioning"].([]interface{}); ok && len(condInput) > 0 {
		if sourceID, ok := condInput[0].(string); ok {
			nodeID, key := findCLIPTextEncode(wf, sourceID, depth+1)
			if nodeID != "" {
				return nodeID, key
			}
		}
	}

	// 回退：检查所有是连接（数组）的输入
	for _, val := range node.Inputs {
		if inputLink, ok := val.([]interface{}); ok && len(inputLink) > 0 {
			if sourceID, ok := inputLink[0].(string); ok {
				// 如果与 currentID 相同则跳过（DAG 中不应发生，但为了安全起见）
				if sourceID == currentID {
					continue
				}

				nodeID, key := findCLIPTextEncode(wf, sourceID, depth+1)
				if nodeID != "" {
					return nodeID, key
				}
			}
		}
	}

	return "", ""
}
