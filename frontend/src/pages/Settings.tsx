import { useEffect, useState } from "react";
import axios from "axios";
import type { Workflow } from "@/types";
import { Input } from "@/components/ui/input";
import { Combobox } from "@/components/ui/combobox";
import { Save, CheckCircle2, XCircle, ExternalLink, FolderSearch } from "lucide-react";
import { toast } from "sonner";
import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogDescription } from "@/components/ui/dialog";

interface ModelCheckResult {
    file_name: string;
    type: string;
    expected_path: string;
    exists: boolean;
    download_urls: string[];
}

export default function Settings() {
    const [workflows, setWorkflows] = useState<Workflow[]>([]);
    const jimengAspectRatioOptions = [
        { value: "21:9", label: "21:9（2176 × 928）" },
        { value: "16:9", label: "16:9（1920 × 1088）" },
        { value: "4:3", label: "4:3（1664 × 1248）" },
        { value: "1:1", label: "1:1（1440 × 1440）" },
        { value: "3:4", label: "3:4（1248 × 1664）" },
        { value: "9:16", label: "9:16（1088 × 1920）" },
    ];
    
    // Settings State
    const [settings, setSettings] = useState({
        image_height: "1344",
        image_width: "768",
        character_image_height: "1344",
        character_image_width: "768",
        optimize_clothing: false,
        video_height: "640",
        video_width: "640",
        video_generation_provider: "local",
        jimeng_api_base: "https://visual.volcengineapi.com",
        jimeng_access_key: "",
        jimeng_secret_key: "",
        jimeng_req_key: "jimeng_ti2v_v30_pro",
        jimeng_aspect_ratio: "16:9",
        default_image_model: "",
        default_video_model: "",
        global_seed: "264590",
        store_visit_image_reference_order: "blogger_first",
        general_guide_transition_engine: "ltx2_3",
        llm_timeout_minutes: "30",
        comfyui_api_address: "127.0.0.1:8188",
        comfyui_models_dir: "",
        ffmpeg_path: "",
    });

    const [checkResults, setCheckResults] = useState<ModelCheckResult[]>([]);
    const [isCheckModalOpen, setIsCheckModalOpen] = useState(false);
    const [checkingWorkflow, setCheckingWorkflow] = useState("");

    useEffect(() => {
        // Fetch Workflows
        axios.get("/api/workflows")
            .then(res => setWorkflows(res.data))
            .catch(err => console.error(err));

        // Fetch Settings
        axios.get("/api/settings")
            .then(res => {
                const nextSettings = { ...res.data } as Record<string, any>;
                if (
                    !nextSettings.jimeng_aspect_ratio &&
                    nextSettings.jimeng_video_width &&
                    nextSettings.jimeng_video_height
                ) {
                    const legacyPreset = `${nextSettings.jimeng_video_width}x${nextSettings.jimeng_video_height}`;
                    const legacyMapping: Record<string, string> = {
                        "2176x928": "21:9",
                        "1920x1088": "16:9",
                        "1664x1248": "4:3",
                        "1440x1440": "1:1",
                        "1248x1664": "3:4",
                        "1088x1920": "9:16",
                    };
                    nextSettings.jimeng_aspect_ratio = legacyMapping[legacyPreset] || "16:9";
                }
                delete nextSettings.jimeng_video_width;
                delete nextSettings.jimeng_video_height;
                delete nextSettings.jimeng_video_duration_seconds;
                delete nextSettings.jimeng_video_frames;
                // Merge with defaults to ensure all keys exist
                setSettings(prev => ({ ...prev, ...nextSettings }));
            })
            .catch(err => {
                console.error(err);
                toast.error("获取系统设置失败");
            });
    }, []);

    const handleSave = () => {
        axios.put("/api/settings", settings)
            .then(() => {
                toast.success("系统设置已保存");
            })
            .catch(err => {
                console.error(err);
                toast.error("保存失败");
            });
    };

    const updateSetting = (key: string, value: any) => {
        setSettings(prev => ({ ...prev, [key]: value }));
    };

    const checkModels = (workflowName: string) => {
        if (!settings.comfyui_models_dir) {
            toast.error("请先设置 ComfyUI Models 目录并保存");
            return;
        }
        setCheckingWorkflow(workflowName);
        axios.get(`/api/comfyui/check_models?workflow=${encodeURIComponent(workflowName)}`)
            .then(res => {
                setCheckResults(res.data);
                setIsCheckModalOpen(true);
            })
            .catch(err => {
                console.error(err);
                toast.error(err.response?.data?.error || "检测失败");
            });
    };

    const imageWorkflows = workflows.filter(w => w.type === "image" && w.workflow_name !== "Qwen-Image-Edit");
    const videoWorkflows = workflows.filter(w => w.type === "video");
    return (
        <div className="space-y-6">
            <div className="flex justify-between items-center">
                <h1 className="text-3xl font-bold">系统设置</h1>
                <button
                    onClick={handleSave}
                    className="flex items-center gap-2 bg-primary text-primary-foreground px-4 py-2 rounded-md hover:bg-primary/90 transition-colors"
                >
                    <Save className="w-4 h-4" /> 保存设置
                </button>
            </div>
            
            {/* LLM 设置 */}
            <div className="bg-card p-6 rounded-lg border border-border shadow-sm">
                <h2 className="text-xl font-semibold mb-4 text-primary">LLM 设置</h2>
                <div className="grid grid-cols-1 md:grid-cols-2 gap-6">
                    <div>
                        <label className="block text-sm font-medium mb-2">LLM 请求超时（分钟）</label>
                        <Input
                            type="number"
                            min="1"
                            value={settings.llm_timeout_minutes}
                            onChange={e => updateSetting("llm_timeout_minutes", e.target.value)}
                            placeholder="30"
                        />
                        <p className="text-xs text-muted-foreground mt-1">
                            自动剧情请求 LLM 时的最长等待时间。适合长文本、慢模型或拥堵时增大。
                        </p>
                    </div>
                </div>
            </div>

            <div className="bg-card p-6 rounded-lg border border-border shadow-sm">
                <h2 className="text-xl font-semibold mb-4 text-primary">综合讲解转场</h2>
                <div className="grid grid-cols-1 md:grid-cols-2 gap-6">
                    <div>
                        <label className="block text-sm font-medium mb-2">转场引擎</label>
                        <select
                            value={settings.general_guide_transition_engine}
                            onChange={e => updateSetting("general_guide_transition_engine", e.target.value)}
                            className="w-full rounded-md border border-input bg-background px-3 py-2 text-sm"
                        >
                            <option value="ltx2_3">LTX2.3（特效更炫）</option>
                            <option value="wan2_2">Wan2.2（首尾帧更稳）</option>
                            <option value="ffmpeg">FFmpeg（稳定硬转场）</option>
                        </select>
                        <p className="text-xs text-muted-foreground mt-1">
                            LTX2.3 更适合粒子消散、重构、空间折叠这类特效化转场；Wan2.2 更适合静态首尾帧之间更平滑的桥接过渡；FFmpeg 更适合完全稳定、可控的传统转场。
                        </p>
                    </div>
                </div>
            </div>

            {/* ComfyUI 设置 */}
            <div className="bg-card p-6 rounded-lg border border-border shadow-sm">
                <h2 className="text-xl font-semibold mb-4 text-primary">ComfyUI 设置</h2>
                <div className="grid grid-cols-1 md:grid-cols-2 gap-6">
                    <div>
                        <label className="block text-sm font-medium mb-2">ComfyUI API 地址</label>
                        <Input 
                            value={settings.comfyui_api_address}
                            onChange={e => updateSetting("comfyui_api_address", e.target.value)}
                            placeholder="127.0.0.1:8188" 
                        />
                        <p className="text-xs text-muted-foreground mt-1">通常无需修改，程序会自动处理 HTTP/WS 协议。</p>
                    </div>
                    <div>
                        <label className="block text-sm font-medium mb-2">ComfyUI Models 目录</label>
                        <Input 
                            value={settings.comfyui_models_dir}
                            onChange={e => updateSetting("comfyui_models_dir", e.target.value)}
                            placeholder="例如: D:\ComfyUI\models" 
                        />
                        <p className="text-xs text-muted-foreground mt-1">用于检测模型文件是否存在，请填写绝对路径。</p>
                    </div>
                    <div>
                        <label className="block text-sm font-medium mb-2">FFmpeg 可执行文件</label>
                        <Input 
                            value={settings.ffmpeg_path}
                            onChange={e => updateSetting("ffmpeg_path", e.target.value)}
                            placeholder="例如: C:\ffmpeg\bin\ffmpeg.exe（留空则尝试 PATH）" 
                        />
                        <p className="text-xs text-muted-foreground mt-1">用于分段视频抽帧与合并。Windows 建议填写 ffmpeg.exe 绝对路径。</p>
                    </div>
                </div>
            </div>

            <div className="bg-card p-6 rounded-lg border border-border shadow-sm">
                <h2 className="text-xl font-semibold mb-4 text-primary">视频生成接入</h2>
                <div className="space-y-6">
                    <div>
                        <label className="block text-sm font-medium mb-2">视频生成方式</label>
                        <div className="grid grid-cols-1 md:grid-cols-2 gap-3">
                            <button
                                type="button"
                                onClick={() => updateSetting("video_generation_provider", "local")}
                                className={`rounded-md border px-4 py-3 text-left transition-colors ${
                                    settings.video_generation_provider === "local"
                                        ? "border-primary bg-primary/10 text-primary"
                                        : "border-border hover:bg-accent"
                                }`}
                            >
                                <div className="font-medium">本地 ComfyUI / LTX</div>
                                <div className="text-xs text-muted-foreground mt-1">继续走当前本地工作流、分段规划和 ComfyUI 出图链路。</div>
                            </button>
                            <button
                                type="button"
                                onClick={() => updateSetting("video_generation_provider", "jimeng")}
                                className={`rounded-md border px-4 py-3 text-left transition-colors ${
                                    settings.video_generation_provider === "jimeng"
                                        ? "border-primary bg-primary/10 text-primary"
                                        : "border-border hover:bg-accent"
                                }`}
                            >
                                <div className="font-medium">即梦在线模型</div>
                                <div className="text-xs text-muted-foreground mt-1">使用即梦视频 3.0 Pro 图生视频接口，提交首帧图与视频提示词后在线生成并自动回收结果。</div>
                            </button>
                        </div>
                    </div>

                    <div className="grid grid-cols-1 md:grid-cols-2 gap-6">
                        <div>
                            <label className="block text-sm font-medium mb-2">即梦 API 地址</label>
                            <Input
                                value={settings.jimeng_api_base}
                                onChange={e => updateSetting("jimeng_api_base", e.target.value)}
                                placeholder="https://visual.volcengineapi.com"
                            />
                            <p className="text-xs text-muted-foreground mt-1">默认官方地址即可，只有代理转发或网关改造时才需要修改。</p>
                        </div>
                        <div>
                            <label className="block text-sm font-medium mb-2">即梦 req_key</label>
                            <Input
                                value={settings.jimeng_req_key}
                                onChange={e => updateSetting("jimeng_req_key", e.target.value)}
                                placeholder="jimeng_ti2v_v30_pro"
                            />
                            <p className="text-xs text-muted-foreground mt-1">默认即梦视频 3.0 Pro：`jimeng_ti2v_v30_pro`。</p>
                        </div>
                        <div>
                            <label className="block text-sm font-medium mb-2">即梦 AccessKey</label>
                            <Input
                                value={settings.jimeng_access_key}
                                onChange={e => updateSetting("jimeng_access_key", e.target.value)}
                                placeholder="请输入 AccessKey"
                            />
                        </div>
                        <div>
                            <label className="block text-sm font-medium mb-2">即梦 SecretKey</label>
                            <Input
                                type="password"
                                value={settings.jimeng_secret_key}
                                onChange={e => updateSetting("jimeng_secret_key", e.target.value)}
                                placeholder="请输入 SecretKey"
                            />
                        </div>
                        <div>
                            <label className="block text-sm font-medium mb-2">即梦输出画幅比例</label>
                            <select
                                value={settings.jimeng_aspect_ratio}
                                onChange={e => updateSetting("jimeng_aspect_ratio", e.target.value)}
                                className="w-full rounded-md border border-input bg-background px-3 py-2 text-sm"
                            >
                                {jimengAspectRatioOptions.map((option) => (
                                    <option key={option.value} value={option.value}>
                                        {option.label}
                                    </option>
                                ))}
                            </select>
                            <p className="text-xs text-muted-foreground mt-1">这里只决定即梦在线模型的官方画幅预设，不影响本地 ComfyUI 视频宽高。视频时长不在这里配置，提交时会直接使用当前镜头的 duration_seconds，并按 24fps 自动换算成 frames。</p>
                        </div>
                    </div>
                </div>
            </div>

            <div className="bg-card p-6 rounded-lg border border-border shadow-sm">
                <h2 className="text-xl font-semibold mb-4 text-primary">全局默认值</h2>
                <div className="grid grid-cols-1 md:grid-cols-2 gap-6">
                     <div className="space-y-4">
                        <h3 className="text-lg font-medium text-muted-foreground">场景图片生成</h3>
                        <div className="grid grid-cols-2 gap-4">
                             <div>
                                <label className="block text-sm font-medium mb-1">高度</label>
                                <Input 
                                    type="number" 
                                    value={settings.image_height}
                                    onChange={e => updateSetting("image_height", e.target.value)}
                                    placeholder="1344" 
                                />
                            </div>
                            <div>
                                <label className="block text-sm font-medium mb-1">宽度</label>
                                <Input 
                                    type="number" 
                                    value={settings.image_width}
                                    onChange={e => updateSetting("image_width", e.target.value)}
                                    placeholder="768" 
                                />
                            </div>
                        </div>
                        <p className="text-xs text-muted-foreground -mt-2">
                            用于场景图生成与自动剧情落库时的场景默认尺寸。
                        </p>

                        <div className="pt-2">
                            <h3 className="text-lg font-medium text-muted-foreground">角色预览图片</h3>
                            <div className="grid grid-cols-2 gap-4 mt-4">
                                <div>
                                    <label className="block text-sm font-medium mb-1">高度</label>
                                    <Input 
                                        type="number" 
                                        value={settings.character_image_height}
                                        onChange={e => updateSetting("character_image_height", e.target.value)}
                                        placeholder="1344" 
                                    />
                                </div>
                                <div>
                                    <label className="block text-sm font-medium mb-1">宽度</label>
                                    <Input 
                                        type="number" 
                                        value={settings.character_image_width}
                                        onChange={e => updateSetting("character_image_width", e.target.value)}
                                        placeholder="768" 
                                    />
                                </div>
                            </div>
                            <p className="text-xs text-muted-foreground mt-1">
                                用于角色预览基图生成与角色默认宽高落库。
                            </p>
                        </div>
                        
                        <div>
                            <label className="block text-sm font-medium mb-1">全局种子 (Seed)</label>
                            <Input 
                                type="number" 
                                value={settings.global_seed}
                                onChange={e => updateSetting("global_seed", e.target.value)}
                                placeholder="264590" 
                            />
                            <p className="text-xs text-muted-foreground mt-1">同时作用于图片和视频生成。</p>
                        </div>

                        <div>
                            <label className="block text-sm font-medium mb-1">探店参考图生成顺序</label>
                            <select
                                value={settings.store_visit_image_reference_order}
                                onChange={e => updateSetting("store_visit_image_reference_order", e.target.value)}
                                className="w-full rounded-md border border-input bg-background px-3 py-2 text-sm"
                            >
                                <option value="blogger_first">人物为主（image1=人物，image2=场景）</option>
                                <option value="scene_first">场景为主（image1=场景，image2=人物）</option>
                            </select>
                            <p className="text-xs text-muted-foreground mt-1">
                                只影响博主探店图片生成的 Qwen Image Edit 注入顺序。默认是人物图作为主图；如果你觉得场景约束更重要，可以切到场景为主。
                            </p>
                        </div>
                     </div>
                     
                     <div className="space-y-4">
                        <h3 className="text-lg font-medium text-muted-foreground">本地视频生成</h3>
                        <div className="grid grid-cols-2 gap-4">
                             <div>
                                <label className="block text-sm font-medium mb-1">高度</label>
                                <Input 
                                    type="number" 
                                    value={settings.video_height}
                                    onChange={e => updateSetting("video_height", e.target.value)}
                                    placeholder="640" 
                                />
                            </div>
                            <div>
                                <label className="block text-sm font-medium mb-1">宽度</label>
                                <Input 
                                    type="number" 
                                    value={settings.video_width}
                                    onChange={e => updateSetting("video_width", e.target.value)}
                                    placeholder="640" 
                                />
                            </div>
                        </div>
                        <p className="text-xs text-muted-foreground mt-1">
                            这里只影响本地 ComfyUI / LTX 视频链路。若切换到即梦在线模型，会改用上面的即梦画幅比例预设。
                        </p>
                     </div>
                </div>
            </div>

            <div className="bg-card p-6 rounded-lg border border-border shadow-sm">
                <h2 className="text-xl font-semibold mb-4 text-primary">模型选择</h2>
                <div className="grid grid-cols-1 md:grid-cols-2 gap-6">
                    <div className="space-y-2">
                        <label className="block text-sm font-medium mb-2">默认图片模型</label>
                        <div className="flex gap-2">
                            <div className="flex-1">
                                <Combobox
                                    items={imageWorkflows}
                                    value={settings.default_image_model}
                                    onChange={(val) => updateSetting("default_image_model", String(val))}
                                    placeholder="选择图片模型..."
                                    searchPlaceholder="搜索模型..."
                                    emptyText="未找到模型"
                                    getItemValue={(w) => w.workflow_name}
                                    getItemLabel={(w) => w.workflow_name}
                                    renderItem={(w) => (
                                        <span>{w.workflow_name}</span>
                                    )}
                                />
                            </div>
                            {settings.default_image_model && (
                                <button 
                                    onClick={() => checkModels(settings.default_image_model)}
                                    className="px-3 py-2 bg-secondary hover:bg-secondary/80 text-secondary-foreground rounded-md transition-colors"
                                    title="检测模型文件"
                                >
                                    <FolderSearch className="w-4 h-4" />
                                </button>
                            )}
                        </div>
                         <p className="text-xs text-muted-foreground mt-1">解析自 workflows/ 目录</p>
                    </div>
                     <div className="space-y-2">
                        <label className="block text-sm font-medium mb-2">本地默认视频模型</label>
                        <div className="flex gap-2">
                            <div className="flex-1">
                                <Combobox
                                    items={videoWorkflows}
                                    value={settings.default_video_model}
                                    onChange={(val) => updateSetting("default_video_model", String(val))}
                                    placeholder="选择视频模型..."
                                    searchPlaceholder="搜索模型..."
                                    emptyText="未找到模型"
                                    getItemValue={(w) => w.workflow_name}
                                    getItemLabel={(w) => w.workflow_name}
                                    renderItem={(w) => (
                                        <span>{w.workflow_name}</span>
                                    )}
                                />
                            </div>
                            {settings.default_video_model && (
                                <button 
                                    onClick={() => checkModels(settings.default_video_model)}
                                    className="px-3 py-2 bg-secondary hover:bg-secondary/80 text-secondary-foreground rounded-md transition-colors"
                                    title="检测模型文件"
                                >
                                    <FolderSearch className="w-4 h-4" />
                                </button>
                            )}
                        </div>
                        <p className="text-xs text-muted-foreground mt-1">仅本地 ComfyUI / LTX 链路使用，解析自 workflows/ 目录。</p>
                    </div>
                </div>
            </div>

            {/* Check Result Modal */}
            <Dialog open={isCheckModalOpen} onOpenChange={setIsCheckModalOpen}>
                <DialogContent className="max-w-3xl max-h-[80vh] overflow-y-auto">
                    <DialogHeader>
                        <DialogTitle>模型检测结果: {checkingWorkflow}</DialogTitle>
                        <DialogDescription>
                            检测 ComfyUI Models 目录下是否存在所需模型文件。
                        </DialogDescription>
                    </DialogHeader>
                    <div className="py-4">
                        <div className="border rounded-md overflow-hidden">
                            <table className="w-full text-sm text-left">
                                <thead className="bg-secondary/50 text-muted-foreground">
                                    <tr>
                                        <th className="px-4 py-2">状态</th>
                                        <th className="px-4 py-2">类型</th>
                                        <th className="px-4 py-2">文件名</th>
                                        <th className="px-4 py-2">路径 / 操作</th>
                                    </tr>
                                </thead>
                                <tbody className="divide-y divide-border">
                                    {checkResults.map((res, i) => (
                                        <tr key={i} className="hover:bg-accent/10">
                                            <td className="px-4 py-3">
                                                {res.exists ? (
                                                    <CheckCircle2 className="w-5 h-5 text-green-500" />
                                                ) : (
                                                    <XCircle className="w-5 h-5 text-red-500" />
                                                )}
                                            </td>
                                            <td className="px-4 py-3 font-mono text-xs">{res.type}</td>
                                            <td className="px-4 py-3 font-medium">{res.file_name}</td>
                                            <td className="px-4 py-3">
                                                <div className="space-y-1">
                                                    <p className="text-xs text-muted-foreground break-all">{res.expected_path}</p>
                                                    {!res.exists && (
                                                        <div className="flex gap-2 mt-1">
                                                            <a href={res.download_urls[0]} target="_blank" rel="noreferrer" className="flex items-center gap-1 text-xs text-blue-500 hover:underline">
                                                                <ExternalLink className="w-3 h-3" /> HuggingFace
                                                            </a>
                                                            <a href={res.download_urls[1]} target="_blank" rel="noreferrer" className="flex items-center gap-1 text-xs text-purple-500 hover:underline">
                                                                <ExternalLink className="w-3 h-3" /> ModelScope
                                                            </a>
                                                        </div>
                                                    )}
                                                </div>
                                            </td>
                                        </tr>
                                    ))}
                                    {checkResults.length === 0 && (
                                        <tr>
                                            <td colSpan={4} className="px-4 py-8 text-center text-muted-foreground">
                                                该工作流似乎没有引用任何外部模型文件。
                                            </td>
                                        </tr>
                                    )}
                                </tbody>
                            </table>
                        </div>
                    </div>
                </DialogContent>
            </Dialog>
        </div>
    )
}
