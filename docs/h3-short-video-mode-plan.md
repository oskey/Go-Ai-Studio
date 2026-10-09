# H3 短视频模式升级计划（替代 R2V LLM 模式）

> 状态：待评审。评审通过后再动代码。
> 目标：移除问题较多的 `r2v` LLM 生成模式，改为新的 `h3_short`（H3 短视频）模式，一键 LLM 生成剧本/分镜，输出不等长的 MiniMax H3 视频段（LLM 按剧情节奏自由分配 scene 时长、官方下限 4s，不感知用户阈值；实际段长由用户阈值设置的全局多段拼接控制），以参考图资产绑定驱动角色一致性，并引入量化标尺、台词铁律、自检审核、全局位置台账、三帧职责化等六大质量机制。范围 = 全链路（提示词 + 视频段规划 + 渲染适配 + 前端 UI）。

## 红线：H3 超阈值多段拼接功能不动（硬件约束）

用户明确：**系统设置的「Minimax H3 超阈值自动多段拼接」已跑通，不得影响**。该功能 = 生成长视频时，超过 `settings.go:188` 设置的秒数阈值后自动走 H3 ref2v 多段拼接。

**硬件事实**：长视频只能一次生成几秒再拼接（显存限制），否则爆显存、非常慢或失败。**该阈值是全局值：只要走 H3 模型生成就生效，为 0 则关闭拼接功能**。每段时长与触发阈值都由用户通过 `KeyH3AutoSegmentThresholdSeconds` 设置（设备显存大的可调高，小的调低），并非写死；该功能已跑通。h3_short 不再自设段长上限，段长一律由该全局阈值决定。

- **拼接机制本身不改**：`video_segments.go` 的 `isH3R2VWorkflow`(999)、`resolveH3R2VLastFrameImage`(1015)、H3 多段拼接逻辑、首尾帧衔接、阈值判定，以及 `settings.go:188` 设置项——全部保留原样。
- **h3_short 与红线的协作边界**：
  - scene `DurationSeconds ≤ 阈值设置值` → 单段一次生成，**不触发红线**。
  - scene `DurationSeconds > 阈值设置值`（LLM 自由分镜的正常结果，分镜时长只是参考） → 红线按用户阈值自动切段拼接，每段 = 阈值秒、共用该 scene 同一完整 prompt，总时长不再等于 LLM 分镜时长。与原 R2V 行为完全一致。
- **h3_short 的价值不与红线冲突**：切点语义化（红线按用户设定的阈值等分 → h3_short LLM 按剧情节奏切分）+ 每段独立 prompt 与参考图（红线段共用同一 prompt）。

## 已确认决策（用户）
1. 新模式形态：H3 短视频升级版 —— 保留 H3 视频链路与红线多段拼接，**不等长**（LLM 按剧情节奏给各 scene 自由分配时长，官方下限 4s；超出全局阈值即由红线分段拼接，分镜时长只是参考，最终总时长 = 段数×阈值），角色用**参考图资产绑定**驱动一致性。

2. 阈值语义：`KeyH3AutoSegmentThresholdSeconds` 是**全局值**，只要用 H3 模型生成就生效，设为 0 则关闭多段拼接。段长、触发线都由它决定；h3_short 侧不复制、不覆盖这一语义。

3. R2V 痛点（本次要解决）：提示词生成质量差、`video_prompt.json` 三段式未用、default mode 参考图不推 ref2v、video_prompt 缺时长字段。
4. 范围：全链路。
5. 开源调研结论（Toonflow-app / Jellyfish 两大项目）：六项质量机制**全部纳入**。

## 质量差距调研：六项机制（来源：Toonflow-app 15.8k⭐ / Jellyfish 6.4k⭐）

| # | 机制 | 来源 | 我们现状 |
|---|---|---|---|
| A | **参考图资产绑定**：角色卡四视图先行，分镜 prompt 用 `@图N` 引用资产、不再重写外表 | Toonflow（`@图N`）、Jellyfish（实体库 ID 引用闭合） | ❌ 提示词还在让 LLM 重写外表锚点 → 漂移源 |
| B | **量化硬阈值**：台词 ≤4 字/秒、单句 ≤20 字、黄金 6 秒（无新信息即切断）、自查清单 | Toonflow（3-15-45 秒节奏、三大密度） | ❌ 只有"禁止/必须"式规则，无可自检标尺 |
| C | **台词零删改 + 在场人物不能消失**（分镜铁律，优先级最高） | Toonflow 分镜表 | ❌ 未见硬约束 |
| D | **监督/审核层**：生成后 A/B/C/D 评分 + 问题回灌重生成 | Toonflow（三层监督）、Jellyfish（retry_guidance 回灌） | ⚠️ 只有结构校验，无语义自检/重生成 |
| E | **全局位置/朝向基准表**：分镜前先建全局台账，跨镜锁定 | Toonflow | ⚠️ 只有"当前可见状态继承"，缺全局空间台账 |
| F | **三帧职责化 + trigger/peak/aftermath 时间切片** | Jellyfish | ❌ 只有单首帧 |

我们已有优势（保留）：连续性账本、可见锚点复用、群像/站位/朝向控制、正向锁定、breakdown 前置——规则密度高于两家，缺的是**结构层手段**而非更多规则。

## 现状核查事实

### 官方规格（MiniMax H3 README）
- 输出时长 **4-15 秒**（模型理论范围；实际单段由用户设置的阈值限定）。
- 帧率 24 FPS、原生 32kHz 立体声、分辨率 768p（短边）。
- Ref2VA（全参考模式）：最多 **9 张参考图** + ≤3 视频/≤3 音频，混合最多 12 文件。多参考图正是角色一致性正路。
- 官方提示词格式为连续三段式：
  - `integrated_multimodal_description:`（含 `[Shot 1]`/`[Shot 2]` 分镜标记、时间码 `At 00:04.500`）
  - `overall_soundscape:`（环境/物体/空间声音）
  - `non_diegetic_music:`（配乐）
- 官方 prompt-writing skill 自带 `ref-en.txt`（Ref2VA 专用），可供镜像抓取参照。

### 现有代码约束（行号已核对）
- `auto_generate_modes.go:5-10`：`AutoGenerateModeR2V = "r2v"` 常量；`:29/:43` `autoGenerateModeAllowsCharacterSpeech`、`autoGenerateModeUsesFlowingVideoPrompt` 含 r2v（`autoGenerateModeRequiresEmptyNarration` 恒 false 不含）。
- `lightweight_story_generation.go:1995`：R2V 专属前置 `runLightweightStoryBreakdown`（进度 20「R2V 前置分镜节点清单」），拆出 `total_nodes` + `narrative_nodes` 作为 scene 数量下限锚点（`:1624` 报错）。
- `lightweight_story_prompts_r2v.go`：全套 R2V 提示词，硬编码"5 秒"（`buildH3R2VShotPlanningInstruction`、`buildR2VLightweightStorySceneSegmentationGuidance`、规则 11「duration_seconds 只能返回 5」、规则 25 自创 `[MODE]/[TOPIC]/[REFERENCE]` + `Audio:` 行——**未对齐官方三段式**）。
- `lightweight_story_prompts_storyboard.go` / `lightweight_story_prompts_high_quality.go`：flowing video_prompt 沿用（H3 三段式骨架可从这里对齐改造）。
- `lightweight_story_prompt_continuity.go`：角色连续状态账本可复用，可扩展出全局位置台账（机制 E）。
- `video_segments.go`：红线中列明——拼接核心不动。
- `h3_video_frame.go`：`minimax_h3_ref2v-gguf-api.json`(16)、`stripH3Ref2VExampleAssets` 已能剥离官方模板示例素材节点（LoadAudio/LoadVideo/GetVideoComponents）。
- `scenes.go:952-956`：首帧生成时 `scene.UseRefImage` → 选 ref2v 工作流；`:1046-1070` 场景参考图已注入 LoadImage 并上传。
- `characters.go:467-499`：角色参考图注入先例（非 h3VideoFrameMode 时，把 `char.RefImage` 转绝对路径上传后注入首个 LoadImage）；参考图存于 `output/<project_code>/ref_images/`。
- `lightweight_story_generation.go` 已有结构校验 `validateLightweightStoryResponse`（对象级），可在此之上挂语义自检。
- 前端：`types/index.ts:479` union 含 "r2v"；`AutoSeries.tsx:509-512`、`Projects.tsx` R2V 单选按钮。

## 设计：新模式 `h3_short`

### 1. 模式名与分发
- `auto_generate_modes.go`：删 `AutoGenerateModeR2V`，增 `AutoGenerateModeH3Short = "h3_short"`。
- `normalizeAutoGenerateGenerationMode` 等三处 switch 同步替换；`autoGenerateModeAllowsCharacterSpeech` / `autoGenerateModeUsesFlowingVideoPrompt` 对 `h3_short` 均返回 true。
- 分发 `lightweight_story_generation.go:746`（r2v case 所在行，switch 在 :739）、:1995 改用新模式名。

### 2. 提示词（机制 A/B/C/E + 官方三段式，P1）
以 `lightweight_story_prompts_storyboard.go` 为模板复制出 `lightweight_story_prompts_h3_short.go`：

**(A) 参考图资产绑定（@图N）**：前置注入"参考图角色索引块"——列出本批次有 `RefImage` 的角色与参考图编号（`@图1=角色甲`…）。规则：凡是索引块内的角色，其外表一律用「参考图编号」指代，**禁止在 image_prompt/video_prompt 里重写五官/发型/服装全量描述**；只允许写当前镜头状态变化（表情、姿态、服装临时状态）。无参考图的角色才写全量外表。与渲染侧（机制 A 注入 `ref_images.*` 通道）配套，生产端只认参考图。

**(B) 量化硬阈值**：新增可自检标尺规则——
- 台词 ≤4 字/秒：`duration_seconds` 必须能容纳该镜台词量（LLM 先做内部试讲再定罪长）；台词超出的拆成多镜。
- 单句 ≤20 字；单镜一段对白 ≤50 字。
- 黄金 6 秒：无声/无新信息镜段不得超过 6 秒，超过必须补充新信息（动作/环境/情绪变化）。
- 自查清单（内部试讲 + 三大密度评级）**只自检不许输出**，避免污染 JSON。
- `duration_seconds` **不再由阈值钳制**：LLM 按剧情节奏自由分配（官方下限 4s），时长只是参考；超出全局阈值时由红线按阈值自动切段拼接（每段=阈值），h3_short 提示词层**不感知、不输出拼接语义**，拼接层也不读新模式的任何字段。

**(C) 台词零删改 + 在场人物不能消失**（铁律，优先级最高）：
- 分镜只设计画面与节奏，**引号台词逐字保留**，不改写不缩写（LLM 不得重写对白）。
- 剧本里在场的人物，分镜必须给出可见痕迹（主体/背景/局部的保留），不得无声消失。

**(E) 全局位置/朝向基准表**：在 `buildCharacterContinuityLedgerRule` 基础上扩展——LLM 生成前先静默建立全局「角色位置/朝向台账」（谁在画面左/右、朝向、与关键物体相对关系），跨镜锁定；转身/走位变化在对应镜标记，之后从新状态续锁。跨场景才允许重置。

**官方三段式对齐**：
- 规则 25 重写：video_prompt 用 `integrated_multimodal_description:`（含 `[Shot N]` 按时间码顺序，时间码与 `duration_seconds`（scene 分镜时长）对齐，红线切段共用同一完整 prompt，各段按各自时间段演绎）+ `overall_soundscape:` + `non_diegetic_music:` 三段骨架；取消自创 `[MODE]/[TOPIC]/[REFERENCE]` 与 `Audio:` 行。
- `buildH3R2VShotPlanningInstruction` → `buildH3ShortShotPlanningInstruction`，删除全部"固定约 5 秒"表述，改为"按剧情节奏不等长分配 scene 时长，≥4s（官方下限）；超出全局阈值即由红线拼接，时长仅作参考"。块内不写拼接/阈值字样。
- 保留 flowing 单段连续叙事结构、首尾帧衔接逻辑（H3 拼接仍自动首尾帧连续）。
- 保留既有高价值约束：30 秒承载规则、主位人物、三人以上仅一人联动、说话者口型/嘴部、场景一致性、`existing_characters` 锁资产、`characters` 只返回新角色、JSON 纯合法规则等。

### 3. 生成后自检审核层（机制 D，P1）
在 `validateLightweightStoryResponse` 结构校验之后新增**语义自检回灌**：
- 新增 `lightweight_story_selfreview.go`：把生成的 scenes（含 narration/duration/video_prompt）喂回 LLM，逐镜审查——台词是否超时、首尾帧是否真的连续、是否有空镜/抽象词、@图N 绑定角色是否有漂移、仿真人物是否消失、位置台账是否被打破。输出修订项列表。
- 有修订项 → 将修订项作为 `retry_guidance` 回灌，重新生成一次 scenes（复用 Jellyfish 回灌思路）；无修订项才落库。
- 不回灌超阈值拼接层；自检只作用于分镜/提示词产物。

### 4. 视频段规划
- **红线不动（硬件约束）**：超阈值拼接、等分逻辑、阈值判定、设置项全部原样（注意现提交 f411c18 已将段长与切段除数改用用户阈值，拼出的总时长 = 段数×阈值，可略大于分镜时长，属预期）。
- h3_short 场景：既有 R2V 分支保留作兜底；LLM 自由分配不等长 `scene.DurationSeconds`（官方下限 4s），≤阈值 → 单段一次生成（天然不触发红线），>阈值 → 红线按阈值切段拼接。**不新增任何分段/拼接代码、不读取阈值。**

### 5. 渲染适配（机制 A 落地 + F，P1/P2）
- **(A) 参考图资产绑定落地**：`scene.UseRefImage` + ref2v 工作流路径已存在（`scenes.go:952`）。补：h3VideoFrameMode 下把角色参考图（`Character.RefImage`）也经 `UploadToComfyUIInput` 注入 `MiniMaxH3ReferenceToVideo` 的 `ref_images.*` 通道（`stripH3Ref2VExampleAssets` 已保留该输入）。参考图索引块与 @图N 编号与渲染注入顺序对应。
- **(F, P2) 三帧职责化**：H3 段新增尾帧生成——分镜末镜尾帧强调"情绪余韵/动作收束"态，与首帧职责分离（首帧=事件触发瞬间，尾帧=收束）。视频生成时尾帧作为 ref2v 末帧参考。时间切片：`[Shot]` 内动作按 trigger→peak→aftermath 顺序组织，与 `duration_seconds` 对齐。
- 可选 P2：一键生成角色四视图（3 全身 + 1 脸部特写），存 `ref_images/`，再接入 ref2v。不阻塞 P1。

### 6. 前端 UI
- `types/index.ts:479`：union 中 "r2v" → "h3_short"。
- `AutoSeries.tsx:509-512`、`Projects.tsx`：R2V 单选改为「H3 短视频（不等长分镜 + 角色参考图）」文案：面向小说原文，按剧情节奏拆不等长段（官方下限 4s；超出用户设置阈值时长时由 H3 自动多段拼接，总时长以拼接结果为准），自动使用角色参考图保证一致性，需在设置中启用 H3 短视频抽帧、配置本地 H3 视频模型与 H3 多段拼接阈值。

### 7. 数据兼容
- 历史 `generation_mode="r2v"` 草稿/项目：读取时 `normalizeAutoGenerateGenerationMode` 把 "r2v" → "h3_short" 归并，避免旧数据失效。
- 旧 scenes 无 `DurationSeconds`：段时长回退 5s（兼容超阈值拼接既有数据）。

## 落点清单
| 文件 | 改动 |
|---|---|
| `internal/api/auto_generate_modes.go` | 常量 + 3 个判定函数 r2v→h3_short，旧值兼容归并 |
| `internal/api/lightweight_story_prompts_h3_short.go`（新建） | 官方三段式 + 机制 A/B/C/E（@图N 索引、量化标尺、台词铁律、位置台账） |
| `internal/api/lightweight_story_prompts_r2v.go`（删除） | 移除旧提示词 |
| `internal/api/lightweight_story_selfreview.go`（新建） | 语义自检回灌（机制 D） |
| `internal/api/lightweight_story_generation.go` | 分发(:746)、前置 breakdown 判定(:1995)、:1624 文案、挂载自检 |
| `internal/api/video_segments.go` | **零改动（红线）**：超阈值拼接/等分/衔接/判定全保留 |
| `internal/api/scenes.go` / `characters.go` | 角色参考图接入 ref2v 参考通道（h3 模式下） |
| `frontend/src/types/index.ts` | union 改值 |
| `frontend/src/pages/AutoSeries.tsx`、`Projects.tsx` | 单选与文案 |

## 风险
- H3 对不等长段（4s 起）阈值内单段生成的兼容需实测（不触发拼接时）；超阈值拼接后的总时长 = 段数×阈值，可大于分镜时长（预期行为，LLM 时长仅为参考）。
- 官方三段式 prompt 在本地 Ref2VA 工作流上能否按期望演绎，需 1 个样例验证；失败时保留 flowing 单段结构仅改 duration。
- 角色参考图注入 `ref_images` 通道的节点 key 需按实际 workflow JSON 确认（现有 strip 逻辑保留了该输入，实现阶段核对）。
- 自检回灌增加一次 LLM 调用与延迟，用单轮回灌封顶避免死循环。
- 台词零删改与 narration 旁白体系（既有 R2V 用 narration 承载口播）需兼容：H3 段内对白=台词逐字保留，narration 旁白的改写权保留（台词 vs 旁白分轨）。

## 验证（P1 完成后）
1. 以 `h3_short` 模式跑一次全链路：入参 → 分镜 → 各 scene `duration_seconds ≥ 4`（官方下限，自由不等长）→ 单段生成时长与 JSON 一致（≤阈值时）或拼接后总时长 = 段数×阈值（>阈值时，属预期，不要求等于 JSON）。
2. 带参考图角色连续两镜生成，肉眼确认外观不漂移。
3. 触发一次**超阈值多段拼接**，确认拼接功能与升级前行为一致（红线验收）。
4. 自检环节触发一次修订回灌，确认修订项生效。
5. 旧 `r2v` 草稿打开不报错（归并到 h3_short）。

## 后续 P2
- 三帧职责化 + trigger/peak/aftermath 时间切片（机制 F）。
- 角色四视图一键生成接入 ref2v 参考通道。
- breakdown 节点清单改为全模式可选（当前仅 h3 系使用）。