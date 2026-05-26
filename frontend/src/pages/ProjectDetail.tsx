import { useParams, useNavigate } from "react-router-dom";
import { useEffect, useRef, useState } from "react";
import axios from "axios";
import {
  ArrowLeft,
  Users,
  Clapperboard,
  Film,
  Wand2,
  Image as ImageIcon,
  ChevronRight,
  ChevronDown,
  Eye,
  X,
  Download,
  RotateCcw,
  Pencil,
  Trash2,
} from "lucide-react";
import type {
  Project,
  Character,
  Scene,
  Video,
  LocalizedPromptText,
} from "@/types";
import { toast } from "sonner";
import { Input } from "@/components/ui/input";
import { Textarea } from "@/components/ui/textarea";
import { Combobox } from "@/components/ui/combobox";
import { Switch } from "@/components/ui/switch";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogFooter,
  DialogDescription,
} from "@/components/ui/dialog";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog";
import { RefreshCw, PlayCircle } from "lucide-react";

interface VideoFingerprintPhaseEditor {
  index: number;
  time_range: string;
  content: string;
  audio: string;
}

interface VideoFingerprintEditorPayload {
  recommended_fps?: number;
  total_duration_seconds?: number;
  prompt_neg_zh?: string;
  style_zh?: string;
  player_desc_zh?: string;
  phases_zh?: VideoFingerprintPhaseEditor[];
}

interface EpisodeSummary {
  episode: number;
  count: number;
  all_generated?: boolean;
}

interface EpisodePageResponse<T> {
  items: T[];
  total: number;
  offset: number;
  limit: number;
  has_more: boolean;
}

interface JimengStatusDialogData {
  task_id: string;
  http_status: number;
  code: number;
  message: string;
  request_id: string;
  status: string;
  video_url: string;
  aigc_meta_tagged: boolean;
  can_retrieve: boolean;
  already_fetched: boolean;
  pretty_json: string;
}

interface PromptRepairDialogState {
  targetType: "scene" | "video";
  targetId?: number;
  title: string;
}

interface PromptRepairTaskState {
  taskId: string;
  shotId: number;
  targetType: "scene" | "video";
  title: string;
  status: "pending" | "running" | "completed" | "failed";
  toastId: string;
}

interface BackgroundTaskRecord {
  id: string;
  status: "pending" | "running" | "completed" | "failed";
  progress: number;
  result?: string;
  error?: string;
}

interface ScenePromptEditorState {
  id?: number;
  title: string;
  imagePrompt: string;
  scene?: Scene;
}

const EPISODE_PAGE_SIZE = 5;

const createEmptyVideoFingerprintPayload =
  (): VideoFingerprintEditorPayload => ({
    recommended_fps: 24,
    total_duration_seconds: 3,
    prompt_neg_zh: "",
    style_zh: "",
    player_desc_zh: "",
    phases_zh: [{ index: 1, time_range: "0-3s", content: "", audio: "" }],
  });

const parseVideoFingerprintPayload = (
  raw?: string,
): VideoFingerprintEditorPayload => {
  if (!raw?.trim()) return createEmptyVideoFingerprintPayload();
  try {
    const parsed = JSON.parse(raw) as {
      recommended_fps?: number;
      total_duration_seconds?: number;
      prompt_neg_zh?: string;
      style_zh?: string;
      player_desc_zh?: string;
      phases_zh?: Array<Partial<VideoFingerprintPhaseEditor>>;
    };
    const {
      recommended_fps,
      total_duration_seconds,
      prompt_neg_zh,
      style_zh,
      player_desc_zh,
      phases_zh,
    } = parsed;
    const sanitizedPhases =
      phases_zh && phases_zh.length > 0
        ? phases_zh.map((phase) => ({
            index: Number(phase.index || 1),
            time_range: String(phase.time_range || ""),
            content: String(phase.content || ""),
            audio: String(phase.audio || ""),
          }))
        : createEmptyVideoFingerprintPayload().phases_zh;
    return {
      ...createEmptyVideoFingerprintPayload(),
      recommended_fps,
      total_duration_seconds,
      prompt_neg_zh,
      style_zh,
      player_desc_zh,
      phases_zh: sanitizedPhases,
    };
  } catch {
    return createEmptyVideoFingerprintPayload();
  }
};

const buildVideoPositivePromptPreview = (
  payload: VideoFingerprintEditorPayload,
): string => {
  const style = payload.style_zh?.trim() || "";
  const phases = payload.phases_zh || [];

  const parts: string[] = [];
  if (style) {
    parts.push(`Style: ${style}`);
  }

  [...phases]
    .sort((a, b) => (a.index || 0) - (b.index || 0))
    .forEach((phase, idx) => {
      const content = phase.content?.trim() || "";
      const timeRange = phase.time_range?.trim() || "";
      const audio = phase.audio?.trim() || "";
      if (!content) return;
      parts.push(
        `Phase ${phase.index || idx + 1}${timeRange ? ` (${timeRange})` : ""}: ${content}`,
      );
      if (audio) {
        parts.push(`Audio: ${audio}`);
      }
    });

  return parts.join("\n\n").trim();
};

const buildVideoIssueReportPrompt = (
  video: Video,
  payload: VideoFingerprintEditorPayload,
): string => {
  const positive = buildVideoPositivePromptPreview(payload);
  const negative = payload.prompt_neg_zh?.trim() || "";

  return [
    "我发现生成效果不理想，我现在把相关的内容发给你，你帮我研判，具体原因见最后面",
    "",
    "说明：上面的“场景基础描述 / 旁白”是当前镜头记录的生成上下文，用于问题定位。",
    "说明：下面的“正向提示词 / 负向提示词”是按当前系统固定 Style / Phase / Audio 路径，从 video_fingerprint 中提取并最终实际提交给 ComfyUI LTX2.3 的内容。",
    "",
    `场景基础描述：${video.scene?.description?.trim() || ""}`,
    `旁白：${video.narration?.trim() || ""}`,
    `正向提示词：${positive}`,
    `负向提示词：${negative}`,
    "",
    "目前遇到的问题是：",
    "",
  ].join("\n");
};

const extractDownloadFilename = (
  contentDisposition: string | undefined,
  fallback: string,
): string => {
  if (!contentDisposition) return fallback;
  const utfMatch = contentDisposition.match(/filename\*=UTF-8''([^;]+)/i);
  if (utfMatch?.[1]) {
    try {
      return decodeURIComponent(utfMatch[1]);
    } catch {
      return utfMatch[1];
    }
  }
  const plainMatch = contentDisposition.match(/filename="?([^"]+)"?/i);
  return plainMatch?.[1] || fallback;
};

const triggerBlobDownload = (blob: Blob, filename: string) => {
  const url = window.URL.createObjectURL(blob);
  const link = document.createElement("a");
  link.href = url;
  link.download = filename;
  document.body.appendChild(link);
  link.click();
  link.remove();
  window.setTimeout(() => window.URL.revokeObjectURL(url), 1000);
};

const resolveSceneStylePrompt = (project?: Project): string => {
  const styleDescription = project?.art_style?.description?.trim() || "";
  return styleDescription;
};

const appendSceneStylePrompt = (prompt: string, project?: Project): string => {
  const base = prompt.trim();
  const stylePrompt = resolveSceneStylePrompt(project);
  if (!stylePrompt) return base;
  if (!base) return stylePrompt;
  if (base.includes(stylePrompt)) return base;
  return `${stylePrompt}\n${base}`;
};

const orderedSceneCharactersForPreview = (scene: Scene): Character[] => {
  const chars = [...(scene.characters || [])];
  if (chars.length <= 1) return chars;

  const context = [scene.description?.trim() || "", scene.narration?.trim() || ""].join("\n");

  const getIndex = (char: Character) => {
    const candidates = [char.name?.trim() || ""].filter(Boolean);
    let index = -1;
    candidates.forEach((candidate) => {
      const pos = context.indexOf(candidate);
      if (pos >= 0 && (index === -1 || pos < index)) {
        index = pos;
      }
    });
    return index;
  };

  chars.sort((a, b) => {
    const ia = getIndex(a);
    const ib = getIndex(b);
    if ((ia >= 0) !== (ib >= 0)) return ia >= 0 ? -1 : 1;
    if (ia >= 0 && ib >= 0 && ia !== ib) return ia - ib;
    return a.id - b.id;
  });

  return chars;
};

const buildSceneIssueReportPrompt = (
  scene: Scene,
  project: Project | null,
): string => {
  const positivePrompt = parseLocalizedPromptText(scene.positive_prompt);
  const negativePrompt = parseLocalizedPromptText(scene.negative_prompt);
  const positive = positivePrompt.zh.trim();
  const negative = negativePrompt.zh.trim();
  const finalPositive = appendSceneStylePrompt(positive, project || undefined);
  const finalNegative = negative;
  const orderedChars = orderedSceneCharactersForPreview(scene);
  const promptOnlySceneGeneration = !!project?.disable_reference_images;
  const workflowName = promptOnlySceneGeneration
    ? "项目已启用禁用参考图模式，场景统一走系统默认文生图工作流"
    : orderedChars.length === 1
      ? "a_qwen_Image_edit_subgraphed"
      : orderedChars.length >= 2
        ? "b_qwen_Image_edit_subgraphed"
        : "默认空镜工作流/当前系统默认图像工作流";
  const referenceOrderText = promptOnlySceneGeneration
    ? "本项目当前已禁用场景参考图，本次不会向工作流提交角色参考图。"
    : orderedChars.length > 0
      ? orderedChars
          .slice(0, 2)
          .map((char, idx) => `${idx + 1}. ${char.name}`)
          .join("\n")
      : "无参考角色";
  const characterAnchorText =
    orderedChars.length > 0
      ? orderedChars
          .map((char) =>
            [
              `角色名：${char.name}`,
              `FaceFingerprint：${char.face_fingerprint?.trim() || ""}`,
              `Fingerprint：${char.fingerprint?.trim() || ""}`,
            ].join("\n"),
          )
          .join("\n\n")
      : "无绑定角色（空镜或纯场景镜头）";

  return [
    "我发现生成效果不理想，我现在把相关的内容发给你，你帮我研判，具体原因见最后面",
    "",
    "说明：上面的“场景基础描述 / 绑定角色视觉锚点”是当前场景记录的生成上下文，用于问题定位。",
    "说明：face_fingerprint 与 fingerprint 都会参与场景生成；其中 face_fingerprint 主要锁脸，fingerprint 主要锁体态、服装、装备与整体身份。",
    "说明：下面的“正向提示词 / 负向提示词”先是数据库里保存的场景中文提示词，再往下是最终实际提交给 ComfyUI Qwen Image / Z-Image 的版本；若项目启用了禁用参考图模式，则不会额外提交角色参考图。",
    "",
    `场景基础描述：${scene.description?.trim() || ""}`,
    `绑定角色视觉锚点：\n${characterAnchorText}`,
    `数据库正向提示词：${positive}`,
    `数据库负向提示词：${negative}`,
    `最终工作流：${workflowName}`,
    `参考角色顺序（提交给工作流）：\n${referenceOrderText}`,
    "场景尺寸：由系统设置中的“场景图片生成”统一控制，不取场景记录单独字段",
    `最终提交正向提示词：${finalPositive}`,
    `最终提交负向提示词：${finalNegative}`,
    "",
    "目前遇到的问题是：",
    "",
  ].join("\n");
};

const buildCharacterIssueReportPrompt = (
  char: Character,
): string => {
  const positivePrompt = parseLocalizedPromptText(char.positive_prompt);
  const negativePrompt = parseLocalizedPromptText(char.negative_prompt);
  const positive = positivePrompt.zh.trim();
  const negative = negativePrompt.zh.trim();
  const mode = char.use_ref_image
    ? "角色参考图强化模式"
    : char.optimize_clothing
      ? "服装优化强化模式"
      : "标准提示词强化模式";
  const refImageNote = char.use_ref_image
    ? `已启用角色参考图：是\n参考图路径：${char.ref_image?.trim() || "已启用，但当前未记录路径"}\n说明：参考图会在后续人物生图时传给 ComfyUI，本次 LLM 强化不会直接读取图片像素内容。`
    : "已启用角色参考图：否";

  return [
    "我发现人物生成效果不理想，我现在把相关的内容发给你，你帮我研判，具体原因见最后面",
    "",
    "说明：下面的“人物基础描述 / 已锁定角色指纹 / 角色名 / 性别 / 当前强化模式”是 LLM 提示词强化时使用的输入上下文。",
    "说明：face_fingerprint 不会单独传给这次 LLM 强化；人物脸部锚点主要来自人物基础描述和已锁定角色指纹。",
    "说明：若启用了角色参考图，参考图会在后续生图时传给 ComfyUI，但不属于本次 LLM 强化直接输入。",
    "说明：下面的“正向提示词 / 负向提示词”是当前数据库里保存、并最终实际提交给 ComfyUI 的人物基图提示词。",
    "",
    `当前强化模式：${mode}`,
    `人物基础描述：${char.description?.trim() || ""}`,
    `已锁定角色指纹：${char.fingerprint?.trim() || ""}`,
    `角色名：${char.name?.trim() || ""}`,
    `性别：${char.gender?.trim() || ""}`,
    `身高：${char.body_height?.trim() || ""}`,
    refImageNote,
    `正向提示词：${positive}`,
    `负向提示词：${negative}`,
    "",
    "目前遇到的问题是：",
    "",
  ].join("\n");
};

const createEmptyLocalizedPromptText = (): LocalizedPromptText => ({
  zh: "",
  en: "",
});

const parseLocalizedPromptText = (raw?: string): LocalizedPromptText => {
  if (!raw?.trim()) return createEmptyLocalizedPromptText();
  try {
    const parsed = JSON.parse(raw) as Partial<LocalizedPromptText>;
    if (typeof parsed === "object" && parsed !== null) {
      return {
        zh: typeof parsed.zh === "string" ? parsed.zh : "",
        en: typeof parsed.en === "string" ? parsed.en : "",
      };
    }
  } catch {
    return {
      zh: raw,
      en: "",
    };
  }
  return createEmptyLocalizedPromptText();
};

const stringifyLocalizedPromptText = (value: LocalizedPromptText): string => {
  if (!value.zh.trim() && !value.en.trim()) {
    return "";
  }
  return JSON.stringify(
    {
      zh: value.zh.trim(),
      en: value.en.trim(),
    },
    null,
    2,
  );
};

const resolveGeneratedWorkflowLabel = (workflow?: string): string => {
  const trimmed = workflow?.trim() || "";
  if (!trimmed) return "";
  return trimmed.replace(/\.json$/i, "");
};

const isSeniorStageCharacter = (char?: Partial<Character>) => {
  const source = [
    char?.name,
    char?.description,
    char?.fingerprint,
  ]
    .filter(Boolean)
    .join(" ");

  if (!source) return false;

  return [
    "老年",
    "晚年",
    "暮年",
    "年老",
    "老妇",
    "老妪",
    "老太",
    "老媪",
    "婆婆",
    "奶奶",
    "祖母",
    "外婆",
  ].some((keyword) => source.includes(keyword));
};

const getCharacterAppearanceText = (char: Character): string => {
  return (
    char.appearance?.trim() ||
    char.description?.trim() ||
    "（暂无角色外观描述）"
  );
};

const getSceneImagePromptText = (scene: Scene): string => {
  return (
    scene.image_prompt?.trim() ||
    parseLocalizedPromptText(scene.positive_prompt).zh.trim() ||
    ""
  );
};

const getSceneVideoPromptText = (scene: Scene): string => {
  if (scene.video_prompt?.trim()) {
    return scene.video_prompt.trim();
  }
  return buildVideoPositivePromptPreview(
    parseVideoFingerprintPayload(scene.video_fingerprint),
  );
};

export default function ProjectDetail() {
  const { id } = useParams();
  const navigate = useNavigate();
  const lightweightAutoStoryReadonlyView = true;
  // State for tabs with localStorage persistence
  const [activeTab, setActiveTab] = useState<
    "characters" | "scenes" | "videos"
  >(() => {
    const savedTab = localStorage.getItem(`project_tab_${id}`);
    return (savedTab as "characters" | "scenes" | "videos") || "characters";
  });

  // Restore Project State
  const [project, setProject] = useState<Project | null>(null);

  useEffect(() => {
    if (id) {
      localStorage.setItem(`project_tab_${id}`, activeTab);
    }
  }, [activeTab, id]);

  // Characters State
  const [characters, setCharacters] = useState<Character[]>([]);
  const [isCharModalOpen, setIsCharModalOpen] = useState(false);
  const [currentChar, setCurrentChar] = useState<Partial<Character>>({});
  const [currentCharPositivePrompt, setCurrentCharPositivePrompt] =
    useState<LocalizedPromptText>(createEmptyLocalizedPromptText());
  const [currentCharNegativePrompt, setCurrentCharNegativePrompt] =
    useState<LocalizedPromptText>(createEmptyLocalizedPromptText());

  // Scenes State
  const [isSceneModalOpen, setIsSceneModalOpen] = useState(false);
  const [currentScene, setCurrentScene] = useState<Partial<Scene>>({});
  const [currentScenePositivePrompt, setCurrentScenePositivePrompt] =
    useState<LocalizedPromptText>(createEmptyLocalizedPromptText());
  const [currentSceneNegativePrompt, setCurrentSceneNegativePrompt] =
    useState<LocalizedPromptText>(createEmptyLocalizedPromptText());
  const [sceneGrouped, setSceneGrouped] = useState<Record<number, Scene[]>>({});
  const sceneGroupedRef = useRef<Record<number, Scene[]>>({});
  const [sceneEpisodeSummaries, setSceneEpisodeSummaries] = useState<
    EpisodeSummary[]
  >([]);
  const [expandedSceneEpisodes, setExpandedSceneEpisodes] = useState<
    Record<number, boolean>
  >({});
  const [visibleSceneEpisodes, setVisibleSceneEpisodes] = useState<
    Record<number, boolean>
  >({});
  const [loadingSceneEpisodes, setLoadingSceneEpisodes] = useState<
    Record<number, boolean>
  >({});
  const [sceneEpisodeHasMore, setSceneEpisodeHasMore] = useState<
    Record<number, boolean>
  >({});

  // Videos State
  const [videoGrouped, setVideoGrouped] = useState<Record<number, Video[]>>({});
  const videoGroupedRef = useRef<Record<number, Video[]>>({});
  const [videoEpisodeSummaries, setVideoEpisodeSummaries] = useState<
    EpisodeSummary[]
  >([]);
  const [expandedVideoEpisodes, setExpandedVideoEpisodes] = useState<
    Record<number, boolean>
  >({});
  const [visibleVideoEpisodes, setVisibleVideoEpisodes] = useState<
    Record<number, boolean>
  >({});
  const [loadingVideoEpisodes, setLoadingVideoEpisodes] = useState<
    Record<number, boolean>
  >({});
  const [videoEpisodeHasMore, setVideoEpisodeHasMore] = useState<
    Record<number, boolean>
  >({});
  const [isVideoModalOpen, setIsVideoModalOpen] = useState(false);
  const [currentVideo] = useState<Partial<Video>>({});
  const [videoFingerprintEditor, setVideoFingerprintEditor] =
    useState<VideoFingerprintEditorPayload>(
      createEmptyVideoFingerprintPayload(),
    );
  const [isVideoPromptPreviewOpen, setIsVideoPromptPreviewOpen] =
    useState(false);
  const [videoPromptPreview, setVideoPromptPreview] = useState("");
  const [isVideoDurationDialogOpen, setIsVideoDurationDialogOpen] =
    useState(false);
  const [videoDurationEditor, setVideoDurationEditor] = useState<{
    id?: number;
    title: string;
    durationSeconds: string;
    videoPrompt: string;
    videoFingerprint: string;
    positivePrompt: string;
    negativePrompt: string;
  }>({
    title: "",
    durationSeconds: "",
    videoPrompt: "",
    videoFingerprint: "",
    positivePrompt: "",
    negativePrompt: "",
  });
  const [isScenePromptEditDialogOpen, setIsScenePromptEditDialogOpen] =
    useState(false);
  const [scenePromptEditor, setScenePromptEditor] =
    useState<ScenePromptEditorState>({
      title: "",
      imagePrompt: "",
    });
  const [isCharacterPromptPreviewOpen, setIsCharacterPromptPreviewOpen] =
    useState(false);
  const [characterPromptPreview, setCharacterPromptPreview] = useState("");
  const [isScenePromptPreviewOpen, setIsScenePromptPreviewOpen] =
    useState(false);
  const [scenePromptPreview, setScenePromptPreview] = useState("");
  const [showFloatingRefresh, setShowFloatingRefresh] = useState(false);

  // Image Preview State
  const [previewImage, setPreviewImage] = useState<string | null>(null);
  const [previewVideo, setPreviewVideo] = useState<string | null>(null);
  const [isJimengStatusDialogOpen, setIsJimengStatusDialogOpen] =
    useState(false);
  const [jimengStatusVideo, setJimengStatusVideo] = useState<Video | null>(null);
  const [jimengStatusData, setJimengStatusData] =
    useState<JimengStatusDialogData | null>(null);
  const [isJimengStatusLoading, setIsJimengStatusLoading] = useState(false);
  const [isJimengRetrieveLoading, setIsJimengRetrieveLoading] = useState(false);
  const [isPromptRepairDialogOpen, setIsPromptRepairDialogOpen] =
    useState(false);
  const [promptRepairDialog, setPromptRepairDialog] =
    useState<PromptRepairDialogState>({
      targetType: "scene",
      title: "",
    });
  const [promptRepairReason, setPromptRepairReason] = useState("");
  const [isPromptRepairSubmitting, setIsPromptRepairSubmitting] = useState(false);
  const [activePromptRepairTasks, setActivePromptRepairTasks] = useState<
    Record<string, PromptRepairTaskState>
  >({});
  const promptRepairPollersRef = useRef<Record<string, number>>({});

  // Default Settings (fetched on mount)
  const [defaultSettings, setDefaultSettings] = useState<any>({});

  useEffect(() => {
    sceneGroupedRef.current = sceneGrouped;
  }, [sceneGrouped]);

  useEffect(() => {
    videoGroupedRef.current = videoGrouped;
  }, [videoGrouped]);

  useEffect(() => {
    const handleKeyDown = (e: KeyboardEvent) => {
      if (e.key === "Escape") {
        if (previewImage) {
          e.preventDefault();
          e.stopPropagation();
          setPreviewImage(null);
        }
        if (previewVideo) {
          e.preventDefault();
          e.stopPropagation();
          setPreviewVideo(null);
        }
      }
    };

    window.addEventListener("keydown", handleKeyDown, true); // Capture phase
    return () => window.removeEventListener("keydown", handleKeyDown, true);
  }, [previewImage, previewVideo]);

  useEffect(() => {
    if (id) {
      fetchProject(id);
      fetchCharacters(id);
      fetchScenes(id);
      fetchVideos(id);
      fetchSettings();
    }

    // Subscribe to SSE for real-time updates
    const eventSource = new EventSource("/api/events");

    eventSource.onmessage = (event) => {
      try {
        const data = JSON.parse(event.data);
        if (data.type === "character" && id) {
          fetchCharacters(id);
        }
        if (data.type === "scene" && id) {
          fetchScenes(id);
          fetchVideos(id); // Scene generation triggers video creation
        }
        if (data.type === "video" && id) {
          fetchVideos(id);
        }
      } catch (e) {
        console.error("SSE Parse Error", e);
      }
    };

    return () => {
      eventSource.close();
    };
  }, [id]);

  useEffect(() => {
    const handleScroll = () => {
      setShowFloatingRefresh(window.scrollY > 320);
    };

    handleScroll();
    window.addEventListener("scroll", handleScroll, { passive: true });
    return () => window.removeEventListener("scroll", handleScroll);
  }, []);

  useEffect(() => {
    return () => {
      Object.values(promptRepairPollersRef.current).forEach((timerId) =>
        window.clearInterval(timerId),
      );
      promptRepairPollersRef.current = {};
    };
  }, []);

  const getPromptRepairShotKey = (shotId?: number) =>
    shotId ? String(shotId) : "";

  const getActivePromptRepairTask = (shotId?: number) => {
    const key = getPromptRepairShotKey(shotId);
    return key ? activePromptRepairTasks[key] : undefined;
  };

  const isShotPromptRepairBusy = (shotId?: number) => {
    const task = getActivePromptRepairTask(shotId);
    return task?.status === "pending" || task?.status === "running";
  };

  const clearPromptRepairTaskTracking = (taskId: string, shotId: number) => {
    const poller = promptRepairPollersRef.current[taskId];
    if (poller) {
      window.clearInterval(poller);
      delete promptRepairPollersRef.current[taskId];
    }
    setActivePromptRepairTasks((prev) => {
      const next = { ...prev };
      delete next[String(shotId)];
      return next;
    });
  };

  const startPromptRepairTaskTracking = (
    taskId: string,
    shotId: number,
    targetType: "scene" | "video",
    title: string,
  ) => {
    const toastId = `prompt-repair-${taskId}`;
    setActivePromptRepairTasks((prev) => ({
      ...prev,
      [String(shotId)]: {
        taskId,
        shotId,
        targetType,
        title,
        status: "pending",
        toastId,
      },
    }));

    toast("请等待修复提示词中...", {
      id: toastId,
      description: `${title} 正在等待 LLM 返回并写回数据库`,
      duration: Infinity,
    });

    const pollTask = () => {
      axios
        .get(`/api/tasks/${taskId}`)
        .then((res) => {
          const task = res.data as BackgroundTaskRecord;
          setActivePromptRepairTasks((prev) => {
            const current = prev[String(shotId)];
            if (!current || current.taskId !== taskId) return prev;
            return {
              ...prev,
              [String(shotId)]: {
                ...current,
                status: task.status,
              },
            };
          });

          if (task.status === "completed") {
            clearPromptRepairTaskTracking(taskId, shotId);
            fetchScenes(id!);
            fetchVideos(id!);
            toast.success("已成功修复，请自行生成测试。", {
              id: toastId,
            });
            return;
          }

          if (task.status === "failed") {
            clearPromptRepairTaskTracking(taskId, shotId);
            toast.error(task.error || "修复失败", {
              id: toastId,
            });
          }
        })
        .catch((err) => {
          console.error(err);
        });
    };

    pollTask();
    promptRepairPollersRef.current[taskId] = window.setInterval(pollTask, 2000);
  };

  const fetchProject = (projectId: string) => {
    axios
      .get(`/api/projects/${projectId}`)
      .then((res) => setProject(res.data))
      .catch((err) => {
        console.error(err);
        toast.error("获取项目详情失败");
        navigate("/projects");
      });
  };

  const fetchCharacters = (projectId: string) => {
    axios
      .get(`/api/projects/${projectId}/characters`)
      .then((res) => setCharacters(res.data))
      .catch((err) => console.error(err));
  };

  const fetchSceneEpisodeSummaries = (projectId: string) => {
    axios
      .get(`/api/projects/${projectId}/scenes`, {
        params: { summary: "episodes" },
      })
      .then((res) => {
        const summaries = (res.data as EpisodeSummary[]).sort(
          (a, b) => a.episode - b.episode,
        );
        setSceneEpisodeSummaries(summaries);
        setSceneGrouped((prev) => {
          const next: Record<number, Scene[]> = {};
          summaries.forEach((item) => {
            if (prev[item.episode]) {
              next[item.episode] = prev[item.episode];
            }
          });
          return next;
        });
        setExpandedSceneEpisodes((prev) => {
          const next: Record<number, boolean> = {};
          summaries.forEach((item) => {
            next[item.episode] = prev[item.episode] ?? false;
          });
          return next;
        });
        setVisibleSceneEpisodes((prev) => {
          const next: Record<number, boolean> = {};
          summaries.forEach((item) => {
            next[item.episode] = prev[item.episode] ?? false;
          });
          return next;
        });
        setLoadingSceneEpisodes((prev) => {
          const next: Record<number, boolean> = {};
          summaries.forEach((item) => {
            next[item.episode] = prev[item.episode] ?? false;
          });
          return next;
        });
        setSceneEpisodeHasMore((prev) => {
          const next: Record<number, boolean> = {};
          summaries.forEach((item) => {
            next[item.episode] =
              prev[item.episode] ?? item.count > EPISODE_PAGE_SIZE;
          });
          return next;
        });
      })
      .catch((err) => console.error(err));
  };

  const fetchSceneEpisode = (
    projectId: string,
    episode: number,
    offset = 0,
    append = false,
    limit = EPISODE_PAGE_SIZE,
  ) => {
    setLoadingSceneEpisodes((prev) => ({ ...prev, [episode]: true }));
    return axios
      .get(`/api/projects/${projectId}/scenes`, {
        params: { episode, offset, limit },
      })
      .then((res) => {
        const data = res.data as EpisodePageResponse<Scene>;
        setSceneGrouped((prev) => ({
          ...prev,
          [episode]: append
            ? [...(prev[episode] || []), ...data.items]
            : data.items,
        }));
        setSceneEpisodeHasMore((prev) => ({
          ...prev,
          [episode]: data.has_more,
        }));
      })
      .catch((err) => console.error(err))
      .finally(() => {
        setLoadingSceneEpisodes((prev) => ({ ...prev, [episode]: false }));
      });
  };

  const refreshLoadedSceneEpisodes = (projectId: string) => {
    const grouped = sceneGroupedRef.current;
    const episodes = Object.keys(grouped)
      .map(Number)
      .filter((episode) => grouped[episode]);
    if (episodes.length === 0) return;
    Promise.all(
      episodes.map((episode) =>
        fetchSceneEpisode(
          projectId,
          episode,
          0,
          false,
          Math.max(grouped[episode]?.length || EPISODE_PAGE_SIZE, EPISODE_PAGE_SIZE),
        ),
      ),
    ).catch((err) => console.error(err));
  };

  const fetchScenes = (projectId: string) => {
    fetchSceneEpisodeSummaries(projectId);
    refreshLoadedSceneEpisodes(projectId);
  };

  const fetchVideoEpisodeSummaries = (projectId: string) => {
    axios
      .get(`/api/projects/${projectId}/videos`, {
        params: { summary: "episodes" },
      })
      .then((res) => {
        const summaries = (res.data as EpisodeSummary[]).sort(
          (a, b) => a.episode - b.episode,
        );
        setVideoEpisodeSummaries(summaries);
        setVideoGrouped((prev) => {
          const next: Record<number, Video[]> = {};
          summaries.forEach((item) => {
            if (prev[item.episode]) {
              next[item.episode] = prev[item.episode];
            }
          });
          return next;
        });
        setExpandedVideoEpisodes((prev) => {
          const next: Record<number, boolean> = {};
          summaries.forEach((item) => {
            next[item.episode] = prev[item.episode] ?? false;
          });
          return next;
        });
        setVisibleVideoEpisodes((prev) => {
          const next: Record<number, boolean> = {};
          summaries.forEach((item) => {
            next[item.episode] = prev[item.episode] ?? false;
          });
          return next;
        });
        setLoadingVideoEpisodes((prev) => {
          const next: Record<number, boolean> = {};
          summaries.forEach((item) => {
            next[item.episode] = prev[item.episode] ?? false;
          });
          return next;
        });
        setVideoEpisodeHasMore((prev) => {
          const next: Record<number, boolean> = {};
          summaries.forEach((item) => {
            next[item.episode] =
              prev[item.episode] ?? item.count > EPISODE_PAGE_SIZE;
          });
          return next;
        });
      })
      .catch((err) => console.error(err));
  };

  const fetchVideoEpisode = (
    projectId: string,
    episode: number,
    offset = 0,
    append = false,
    limit = EPISODE_PAGE_SIZE,
  ) => {
    setLoadingVideoEpisodes((prev) => ({ ...prev, [episode]: true }));
    return axios
      .get(`/api/projects/${projectId}/videos`, {
        params: { episode, offset, limit },
      })
      .then((res) => {
        const data = (res.data as EpisodePageResponse<Video>).items.map((video) => ({
          ...video,
          segments: [...(video.segments || [])].sort(
            (a, b) => a.segment_index - b.segment_index,
          ),
        }));
        data.sort(
          (a, b) => (a.scene?.scene_number || 0) - (b.scene?.scene_number || 0),
        );
        setVideoGrouped((prev) => ({
          ...prev,
          [episode]: append ? [...(prev[episode] || []), ...data] : data,
        }));
        setVideoEpisodeHasMore((prev) => ({
          ...prev,
          [episode]: (res.data as EpisodePageResponse<Video>).has_more,
        }));
      })
      .catch((err) => console.error(err))
      .finally(() => {
        setLoadingVideoEpisodes((prev) => ({ ...prev, [episode]: false }));
      });
  };

  const refreshLoadedVideoEpisodes = (projectId: string) => {
    const grouped = videoGroupedRef.current;
    const episodes = Object.keys(grouped)
      .map(Number)
      .filter((episode) => grouped[episode]);
    if (episodes.length === 0) return;
    Promise.all(
      episodes.map((episode) =>
        fetchVideoEpisode(
          projectId,
          episode,
          0,
          false,
          Math.max(grouped[episode]?.length || EPISODE_PAGE_SIZE, EPISODE_PAGE_SIZE),
        ),
      ),
    ).catch((err) => console.error(err));
  };

  const fetchVideos = (projectId: string) => {
    fetchVideoEpisodeSummaries(projectId);
    refreshLoadedVideoEpisodes(projectId);
  };

  useEffect(() => {
    if (activeTab !== "videos") return;

    const observer = new IntersectionObserver(
      (entries) => {
        entries.forEach((entry) => {
          if (!entry.isIntersecting) return;
          const episodeAttr = entry.target.getAttribute("data-video-episode");
          const episode = Number(episodeAttr);
          if (!Number.isFinite(episode)) return;
          setVisibleVideoEpisodes((prev) =>
            prev[episode] ? prev : { ...prev, [episode]: true },
          );
        });
      },
      { rootMargin: "320px 0px" },
    );

    const elements = document.querySelectorAll("[data-video-episode]");
    elements.forEach((element) => observer.observe(element));

    return () => observer.disconnect();
  }, [activeTab, videoGrouped]);

  useEffect(() => {
    if (activeTab !== "videos" || !id) return;

    const observer = new IntersectionObserver(
      (entries) => {
        entries.forEach((entry) => {
          if (!entry.isIntersecting) return;
          const episode = Number(
            entry.target.getAttribute("data-video-page-sentinel"),
          );
          if (
            !Number.isFinite(episode) ||
            !expandedVideoEpisodes[episode] ||
            !videoEpisodeHasMore[episode] ||
            loadingVideoEpisodes[episode]
          ) {
            return;
          }
          fetchVideoEpisode(
            id,
            episode,
            videoGrouped[episode]?.length || 0,
            true,
          );
        });
      },
      { rootMargin: "240px 0px" },
    );

    const elements = document.querySelectorAll("[data-video-page-sentinel]");
    elements.forEach((element) => observer.observe(element));

    return () => observer.disconnect();
  }, [
    activeTab,
    id,
    expandedVideoEpisodes,
    videoEpisodeHasMore,
    loadingVideoEpisodes,
    videoGrouped,
  ]);

  useEffect(() => {
    if (activeTab !== "scenes") return;

    const observer = new IntersectionObserver(
      (entries) => {
        entries.forEach((entry) => {
          if (!entry.isIntersecting) return;
          const episodeAttr = entry.target.getAttribute("data-scene-episode");
          const episode = Number(episodeAttr);
          if (!Number.isFinite(episode)) return;
          setVisibleSceneEpisodes((prev) =>
            prev[episode] ? prev : { ...prev, [episode]: true },
          );
        });
      },
      { rootMargin: "320px 0px" },
    );

    const elements = document.querySelectorAll("[data-scene-episode]");
    elements.forEach((element) => observer.observe(element));

    return () => observer.disconnect();
  }, [activeTab, sceneGrouped]);

  useEffect(() => {
    if (activeTab !== "scenes" || !id) return;

    const observer = new IntersectionObserver(
      (entries) => {
        entries.forEach((entry) => {
          if (!entry.isIntersecting) return;
          const episode = Number(
            entry.target.getAttribute("data-scene-page-sentinel"),
          );
          if (
            !Number.isFinite(episode) ||
            !expandedSceneEpisodes[episode] ||
            !sceneEpisodeHasMore[episode] ||
            loadingSceneEpisodes[episode]
          ) {
            return;
          }
          fetchSceneEpisode(
            id,
            episode,
            sceneGrouped[episode]?.length || 0,
            true,
          );
        });
      },
      { rootMargin: "240px 0px" },
    );

    const elements = document.querySelectorAll("[data-scene-page-sentinel]");
    elements.forEach((element) => observer.observe(element));

    return () => observer.disconnect();
  }, [
    activeTab,
    id,
    expandedSceneEpisodes,
    sceneEpisodeHasMore,
    loadingSceneEpisodes,
    sceneGrouped,
  ]);

  useEffect(() => {
    if (activeTab !== "scenes" || !id) return;
    sceneEpisodeSummaries.forEach((item) => {
      const episode = item.episode;
      if (
        expandedSceneEpisodes[episode] &&
        visibleSceneEpisodes[episode] &&
        !sceneGrouped[episode] &&
        !loadingSceneEpisodes[episode]
      ) {
        fetchSceneEpisode(id, episode, 0, false);
      }
    });
  }, [
    activeTab,
    id,
    sceneEpisodeSummaries,
    expandedSceneEpisodes,
    visibleSceneEpisodes,
    sceneGrouped,
    loadingSceneEpisodes,
  ]);

  useEffect(() => {
    if (activeTab !== "videos" || !id) return;
    videoEpisodeSummaries.forEach((item) => {
      const episode = item.episode;
      if (
        expandedVideoEpisodes[episode] &&
        visibleVideoEpisodes[episode] &&
        !videoGrouped[episode] &&
        !loadingVideoEpisodes[episode]
      ) {
        fetchVideoEpisode(id, episode, 0, false);
      }
    });
  }, [
    activeTab,
    id,
    videoEpisodeSummaries,
    expandedVideoEpisodes,
    visibleVideoEpisodes,
    videoGrouped,
    loadingVideoEpisodes,
  ]);

  const fetchSettings = () => {
    axios
      .get("/api/settings")
      .then((res) => setDefaultSettings(res.data))
      .catch((err) => console.error(err));
  };

  // Character Actions
  const handleSaveCharacter = () => {
    if (!currentChar.name || !currentChar.gender || !currentChar.description) {
      toast.error("请填写必填项 (名称, 性别, 外貌描述)");
      return;
    }

    const { width: _width, height: _height, seed: _seed, ...characterPayload } =
      currentChar;
    void _width;
    void _height;
    void _seed;
    const payload = {
      ...characterPayload,
      project_id: Number(id),
      positive_prompt: stringifyLocalizedPromptText(currentCharPositivePrompt),
      negative_prompt: stringifyLocalizedPromptText(currentCharNegativePrompt),
      optimize_clothing:
        currentChar.optimize_clothing ?? defaultSettings.optimize_clothing,
    };

    const req = currentChar.id
      ? axios.put(`/api/characters/${currentChar.id}`, payload)
      : axios.post("/api/characters", payload);

    req
      .then(() => {
        setIsCharModalOpen(false);
        fetchCharacters(id!);
        toast.success(currentChar.id ? "角色已更新" : "角色已创建");
      })
      .catch((err) => {
        console.error(err);
        toast.error("保存角色失败");
      });
  };

  const handleDeleteCharacter = (charId: number) => {
    toast("确定要删除该角色吗？", {
      action: {
        label: "删除",
        onClick: () => {
          axios
            .delete(`/api/characters/${charId}`)
            .then(() => {
              fetchCharacters(id!);
              toast.success("角色已删除");
            })
            .catch(() => toast.error("删除失败"));
        },
      },
      cancel: {
        label: "取消",
        onClick: () => {},
      },
    });
  };

  const handleResetCharacter = (char: Character) => {
    toast("确定要重置该角色状态吗？", {
      description: "会物理删除当前人物图片，并清空图片字段，状态恢复为草稿",
      action: {
        label: "重置",
        onClick: () => {
          axios
            .post(`/api/projects/${id}/characters/${char.id}/reset-image`)
            .then(() => {
              fetchCharacters(id!);
              toast.success("角色状态已重置");
            })
            .catch((err) => {
              console.error(err);
              toast.error("重置失败");
            });
        },
      },
      cancel: {
        label: "取消",
        onClick: () => {},
      },
    });
  };

  // Scene Actions
  const handleSaveScene = () => {
    if (
      !currentScene.name ||
      !currentScene.description ||
      !currentScene.episode ||
      !currentScene.scene_number
    ) {
      toast.error("请填写必填项 (集数, 场次, 名称, 基础描述)");
      return;
    }

    const { width: _width, height: _height, seed: _seed, ...scenePayload } =
      currentScene;
    void _width;
    void _height;
    void _seed;
    const payload = {
      scene: {
        ...scenePayload,
        project_id: Number(id),
        positive_prompt: stringifyLocalizedPromptText(
          currentScenePositivePrompt,
        ),
        negative_prompt: stringifyLocalizedPromptText(
          currentSceneNegativePrompt,
        ),
      },
      character_ids: currentScene.character_ids || [],
    };

    const req = currentScene.id
      ? axios.put(`/api/scenes/${currentScene.id}`, payload)
      : axios.post("/api/scenes", payload);

    req
      .then(() => {
        setIsSceneModalOpen(false);
        fetchScenes(id!);
        toast.success(currentScene.id ? "场景已更新" : "场景已创建");
      })
      .catch((err) => {
        console.error(err);
        toast.error("保存场景失败");
      });
  };

  const handleDeleteScene = (sceneId: number) => {
    if (isShotPromptRepairBusy(sceneId)) {
      toast.error("当前镜头正在修复提示词，请等待修复完成后再删除");
      return;
    }
    toast("确定要删除该场景吗？", {
      description: "会物理删除当前首帧图、关联视频、分段视频与过渡帧，并删除这条镜头记录。",
      action: {
        label: "删除",
        onClick: () => {
          axios
            .delete(`/api/scenes/${sceneId}`)
            .then(() => {
              Promise.all([fetchScenes(id!), fetchVideos(id!)]);
              toast.success("场景已删除");
            })
            .catch(() => toast.error("删除失败"));
        },
      },
      cancel: {
        label: "取消",
        onClick: () => {},
      },
    });
  };

  const handleDeleteVideo = (videoId: number) => {
    if (isShotPromptRepairBusy(videoId)) {
      toast.error("当前镜头正在修复提示词，请等待修复完成后再删除");
      return;
    }
    toast("确定要删除该视频吗？", {
      description:
        "会物理删除当前完整视频、分段视频、过渡帧以及关联首帧图，并删除这条镜头记录。",
      action: {
        label: "删除",
        onClick: () => {
          axios
            .delete(`/api/projects/${id}/videos/${videoId}`)
            .then(() => {
              Promise.all([fetchScenes(id!), fetchVideos(id!)]);
              toast.success("视频已删除");
            })
            .catch((err) =>
              toast.error(err.response?.data?.error || "删除失败"),
            );
        },
      },
      cancel: {
        label: "取消",
        onClick: () => {},
      },
    });
  };

  const handleFileUpload = async (e: React.ChangeEvent<HTMLInputElement>) => {
    if (e.target.files && e.target.files[0] && project) {
      const file = e.target.files[0];
      const formData = new FormData();
      formData.append("file", file);
      // Pass project code to organize files
      formData.append("project_code", project.code);

      try {
        const res = await axios.post("/api/upload", formData);
        // Ensure we get a usable URL
        const path = res.data.path;
        // Since we serve static files at root /output, the path returned (e.g. /output/...) should work directly
        // If it's a relative path without slash, add it
        const fullPath = path.startsWith("/") ? path : `/${path}`;

        setCurrentChar((prev) => ({ ...prev, ref_image: fullPath }));
        toast.success("图片上传成功");
      } catch (err) {
        console.error(err);
        toast.error("上传失败");
      }
    }
  };

  const handleAutoGeneratePrompt = (charId: number) => {
    const targetChar = characters.find((char) => char.id === charId);
    if (targetChar && isSeniorStageCharacter(targetChar) && targetChar.optimize_clothing) {
      toast.error("老年或晚年阶段不启用服装优化");
      return;
    }
    const toastId = toast("正在启动 LLM 提示词强化...", {
      description:
        "将根据当前角色描述与人物指纹重写提示词，并直接提交人物生图任务",
    });
    axios
      .post(`/api/projects/${id}/characters/${charId}/auto-generate-prompt`)
      .then(() => {
        toast.success("提示词强化与生图任务已提交，请在系统日志中查看进度", {
          id: toastId,
        });
      })
      .catch((err) => {
        console.error(err);
        toast.error("提交任务失败", { id: toastId });
      });
  };

  const handleGenerateImageOnly = (char: Character) => {
    if (!char.appearance?.trim()) {
      toast.error("该角色缺少外观锚点", {
        description: "请先确认自动剧情已经生成完整角色设定",
      });
      return;
    }

    const toastId = toast("正在请求生成图片...", {
      description: "将根据锁定角色设定直接生成角色预览图",
    });

    axios
      .post(`/api/projects/${id}/characters/${char.id}/generate-image`)
      .then(() => {
        toast.success("图片生成任务已提交", { id: toastId });
      })
      .catch((err) => {
        console.error(err);
        toast.error("提交任务失败", { id: toastId });
      });
  };

  const handleBatchGenerateCharacters = () => {
    const toastId = toast("正在启动批量生成人物...", {
      description: "会根据锁定角色设定直接生成预览图，已生成的不覆盖",
    });
    axios
      .post(`/api/projects/${id}/batch-generate-characters`)
      .then(() => {
        toast.success("批量任务已提交", { id: toastId });
      })
      .catch((err) => {
        console.error(err);
        toast.error("提交任务失败", { id: toastId });
      });
  };

  const handleBatchGenerateScenes = () => {
    const toastId = toast("正在启动批量生成场景...", {
      description: "只会生成当前未生成场景图的镜头，已生成的不覆盖",
    });
    axios
      .post(`/api/projects/${id}/batch-generate-scenes`)
      .then(() => {
        toast.success("批量场景任务已提交", { id: toastId });
      })
      .catch((err) => {
        console.error(err);
        toast.error("提交任务失败", { id: toastId });
      });
  };

  const handleGenerateSceneImageOnly = (scene: Scene) => {
    if (isShotPromptRepairBusy(scene.id)) {
      toast.error("当前镜头正在修复提示词，请等待修复完成后再生成");
      return;
    }
    if (!scene.image_prompt?.trim()) {
      toast.error("该场景还未生成首帧图提示词", {
        description: "请先确认自动剧情已经生成完整 image_prompt",
      });
      return;
    }

    const toastId = toast("正在请求生成场景图片...", {
      description: "将直接使用当前 image_prompt 提交到 ComfyUI",
    });

    axios
      .post(`/api/projects/${id}/scenes/${scene.id}/generate-image`)
      .then(() => {
        toast.success("图片生成任务已提交", { id: toastId });
      })
      .catch((err) => {
        console.error(err);
        toast.error("提交任务失败", { id: toastId });
      });
  };

  const handleDeleteAllCharacterImages = () => {
    toast("确定要重置所有角色状态吗？", {
      description: "会物理删除角色预览图、清空生成字段，并把状态改回草稿。",
      action: {
        label: "重置",
        onClick: () => {
          const toastId = toast("正在重置所有角色状态...", {
            description: "会清理角色预览图并将状态改回草稿",
          });
          axios
            .post(`/api/projects/${id}/characters/delete-all-images`)
            .then((res) => {
              fetchCharacters(id!);
              toast.success(`已重置 ${res.data.count || 0} 个角色`, {
                id: toastId,
              });
            })
            .catch((err) => {
              console.error(err);
              toast.error(err.response?.data?.error || "删除失败", {
                id: toastId,
              });
            });
        },
      },
      cancel: {
        label: "取消",
        onClick: () => {},
      },
    });
  };

  const handleDeleteAllSceneImages = () => {
    toast("确定要重置所有场景状态吗？", {
      description: "会物理删除场景图、清空生成字段，并把状态改回草稿。",
      action: {
        label: "重置",
        onClick: () => {
          const toastId = toast("正在重置所有场景状态...", {
            description: "会清理场景图并将状态改回草稿",
          });
          axios
            .post(`/api/projects/${id}/scenes/delete-all-images`)
            .then((res) => {
              fetchScenes(id!);
              toast.success(`已重置 ${res.data.count || 0} 个场景`, {
                id: toastId,
              });
            })
            .catch((err) => {
              console.error(err);
              toast.error(err.response?.data?.error || "删除失败", {
                id: toastId,
              });
            });
        },
      },
      cancel: {
        label: "取消",
        onClick: () => {},
      },
    });
  };

  const handleResetScene = (scene: Scene) => {
    if (isShotPromptRepairBusy(scene.id)) {
      toast.error("当前镜头正在修复提示词，请等待修复完成后再重置");
      return;
    }
    toast("确定要重置该场景状态吗？", {
      description: "会物理删除当前场景图，清空生成字段，并把状态恢复为草稿。",
      action: {
        label: "重置",
        onClick: () => {
          axios
            .post(`/api/projects/${id}/scenes/${scene.id}/reset-image`)
            .then(() => {
              fetchScenes(id!);
              toast.success("场景状态已重置");
            })
            .catch((err) => {
              console.error(err);
              toast.error(err.response?.data?.error || "重置失败");
            });
        },
      },
      cancel: {
        label: "取消",
        onClick: () => {},
      },
    });
  };

  const updateVideoCardState = (videoId: number, patch: Partial<Video>) => {
    setVideoGrouped((prev) => {
      const next: Record<number, Video[]> = {};
      Object.entries(prev).forEach(([episodeKey, videos]) => {
        next[Number(episodeKey)] = (videos || []).map((video) =>
          video.id === videoId ? { ...video, ...patch } : video,
        );
      });
      return next;
    });
  };

  const handleGenerateVideo = (video: Video) => {
    if (isShotPromptRepairBusy(video.id)) {
      toast.error("当前镜头正在修复提示词，请等待修复完成后再生成");
      return;
    }
    const hasSceneImage = Boolean(video.scene?.generated_image?.trim());
    if (!hasSceneImage) {
      toast.error("请先生成场景图，再生成视频");
      return;
    }

    const hasVideoSource = Boolean(
      video.video_prompt?.trim() ||
        video.scene?.video_prompt?.trim() ||
        video.video_fingerprint?.trim(),
    );
    if (!hasVideoSource) {
      toast.error("当前视频缺少可用的视频提示词");
      return;
    }

    const toastId = toast("正在启动视频生成...", {
      description:
        defaultSettings.video_generation_provider === "jimeng"
          ? "将使用即梦在线模型提交首帧图和视频提示词生成视频"
          : "将使用当前本地视频链路生成视频",
    });
    axios
      .post(`/api/projects/${id}/videos/${video.id}/generate-video`)
      .then(() => {
        updateVideoCardState(video.id, {
          status: "generating",
          generated_video: "",
        });
        fetchVideos(id!);
        toast.success("视频生成任务已提交，请查看进度", {
          id: toastId,
        });
      })
      .catch((err) => {
        console.error(err);
        // Show detailed error if available
        const errorMessage = err.response?.data?.error || "提交任务失败";
        toast.error(errorMessage, { id: toastId });
      });
  };

  const handleOpenSceneRepairDialog = (scene: Scene) => {
    setPromptRepairDialog({
      targetType: "scene",
      targetId: scene.id,
      title:
        scene.name || `第 ${scene.episode}-${scene.scene_id || scene.scene_number} 镜`,
    });
    setPromptRepairReason("");
    setIsPromptRepairDialogOpen(true);
  };

  const handleOpenVideoRepairDialog = (video: Video) => {
    setPromptRepairDialog({
      targetType: "video",
      targetId: video.id,
      title:
        video.scene?.name ||
        `第 ${video.scene?.episode}-${video.scene?.scene_number} 镜`,
    });
    setPromptRepairReason("");
    setIsPromptRepairDialogOpen(true);
  };

  const handleSubmitPromptRepair = () => {
    if (!promptRepairDialog.targetId) return;
    const reason = promptRepairReason.trim();
    if (!reason) {
      toast.error("请先填写你看到的问题");
      return;
    }

    const endpoint =
      promptRepairDialog.targetType === "scene"
        ? `/api/projects/${id}/scenes/${promptRepairDialog.targetId}/repair-prompts`
        : `/api/projects/${id}/videos/${promptRepairDialog.targetId}/repair-prompts`;

    setIsPromptRepairSubmitting(true);
    axios
      .post(endpoint, { reason })
      .then((res) => {
        const taskID = String(res.data?.task_id || "").trim();
        setIsPromptRepairDialogOpen(false);
        setPromptRepairReason("");
        if (taskID) {
          startPromptRepairTaskTracking(
            taskID,
            promptRepairDialog.targetId!,
            promptRepairDialog.targetType,
            promptRepairDialog.title,
          );
        } else {
          toast("请等待修复提示词中...", {
            description: "修复完成后会自动回写到当前镜头",
          });
        }
      })
      .catch((err) => {
        console.error(err);
        toast.error(err.response?.data?.error || "提交修复任务失败");
      })
      .finally(() => setIsPromptRepairSubmitting(false));
  };

  const handleOpenVideoDurationDialog = (video: Video) => {
    const displayTitle =
      video.scene?.name ||
      `镜头 ${video.scene?.scene_id || video.scene?.scene_number || video.scene_id}`;
    setVideoDurationEditor({
      id: video.id,
      title: displayTitle,
      durationSeconds: String(
        video.duration_seconds || video.scene?.duration_seconds || "",
      ),
      videoPrompt: video.video_prompt || "",
      videoFingerprint: video.video_fingerprint || "",
      positivePrompt: video.positive_prompt || "",
      negativePrompt: video.negative_prompt || "",
    });
    setIsVideoDurationDialogOpen(true);
  };

  const handleOpenScenePromptEditDialog = (scene: Scene) => {
    const displayTitle =
      scene.name || `镜头 ${scene.scene_id || scene.scene_number}`;
    setScenePromptEditor({
      id: scene.id,
      title: displayTitle,
      imagePrompt: getSceneImagePromptText(scene),
      scene,
    });
    setIsScenePromptEditDialogOpen(true);
  };

  const handleSaveScenePromptEdit = async () => {
    if (!scenePromptEditor.id || !scenePromptEditor.scene) return;
    const nextImagePrompt = scenePromptEditor.imagePrompt.trim();
    if (!nextImagePrompt) {
      toast.error("场景提示词不能为空");
      return;
    }

    const scene = scenePromptEditor.scene;
    const toastId = toast("正在保存场景提示词并重置旧资产...", {
      description: "会删除当前首帧图与关联视频，方便按新提示词重新生成",
    });

    const payload = {
      scene: {
        ...scene,
        image_prompt: nextImagePrompt,
      },
      character_ids:
        scene.character_ids ||
        (scene.characters || []).map((character) => character.id),
    };

    try {
      await axios.put(`/api/scenes/${scenePromptEditor.id}`, payload);
      await axios.post(`/api/projects/${id}/scenes/${scenePromptEditor.id}/reset-image`);
      await axios.post(`/api/projects/${id}/videos/${scenePromptEditor.id}/reset-status`);
      setIsScenePromptEditDialogOpen(false);
      fetchScenes(id!);
      fetchVideos(id!);
      toast.success("场景提示词已更新，首帧图与视频已重置", { id: toastId });
    } catch (err: any) {
      console.error(err);
      toast.error(err.response?.data?.error || "更新场景提示词失败", {
        id: toastId,
      });
    }
  };

  const handleSaveVideoDuration = async () => {
    if (!videoDurationEditor.id) return;
    const nextDuration = Number(videoDurationEditor.durationSeconds);
    if (!Number.isFinite(nextDuration) || nextDuration < 3) {
      toast.error("视频时长必须至少为 3 秒");
      return;
    }
    if (!videoDurationEditor.videoPrompt.trim()) {
      toast.error("视频提示词不能为空");
      return;
    }

    const toastId = toast("正在保存视频设置并重置旧视频...", {
      description: "会删除当前已生成视频并把状态恢复为草稿，方便重新生成",
    });

    try {
      const res = await axios.put(`/api/projects/${id}/videos/${videoDurationEditor.id}`, {
        video_prompt: videoDurationEditor.videoPrompt,
        video_fingerprint: videoDurationEditor.videoFingerprint,
        positive_prompt: videoDurationEditor.positivePrompt,
        negative_prompt: videoDurationEditor.negativePrompt,
        duration_seconds: nextDuration,
      });
      await axios.post(`/api/projects/${id}/videos/${videoDurationEditor.id}/reset-status`);
      setIsVideoDurationDialogOpen(false);
      updateVideoCardState(videoDurationEditor.id!, {
        duration_seconds: res.data?.duration_seconds || nextDuration,
        video_fingerprint:
          res.data?.video_fingerprint || videoDurationEditor.videoFingerprint,
        video_prompt: res.data?.video_prompt || videoDurationEditor.videoPrompt,
        status: "draft",
        generated_video: "",
      });
      fetchVideos(id!);
      toast.success("视频设置已更新，旧视频已重置", { id: toastId });
    } catch (err: any) {
      console.error(err);
      toast.error(err.response?.data?.error || "更新视频设置失败", {
        id: toastId,
      });
    }
  };

  const handleViewJimengStatus = (video: Video) => {
    const taskID = video.jm_task_id?.trim();
    if (!taskID) {
      toast.error("当前视频还没有 jm_task_id，无法查看即梦状态");
      return;
    }
    setJimengStatusVideo(video);
    setIsJimengStatusDialogOpen(true);
    setIsJimengStatusLoading(true);
    axios
      .get(`/api/projects/${id}/videos/${video.id}/jimeng-status`)
      .then((res) => {
        setJimengStatusData(res.data);
      })
      .catch((err) => {
        console.error(err);
        toast.error(err.response?.data?.error || "查看即梦状态失败");
      })
      .finally(() => setIsJimengStatusLoading(false));
  };

  const handleRetrieveJimengVideo = () => {
    if (!jimengStatusVideo) return;
    setIsJimengRetrieveLoading(true);
    axios
      .post(`/api/projects/${id}/videos/${jimengStatusVideo.id}/jimeng-retrieve`)
      .then((res) => {
        const generatedVideo = res.data?.generated_video;
        updateVideoCardState(jimengStatusVideo.id, {
          status: res.data?.status || "generated",
          generated_video: generatedVideo || "",
        });
        setJimengStatusData((prev) =>
          prev
            ? {
                ...prev,
                already_fetched: true,
              }
            : prev,
        );
        fetchVideos(id!);
        toast.success("即梦视频已取回到本地");
      })
      .catch((err) => {
        console.error(err);
        toast.error(err.response?.data?.error || "取回即梦视频失败");
      })
      .finally(() => setIsJimengRetrieveLoading(false));
  };

  const handleReextractVideo = (videoId: number) => {
    const toastId = toast("正在重新提取数据...", {
      description: "将从同一条镜头记录重新提取指纹、旁白、对白等数据覆盖到视频视图",
    });
    axios
      .post(`/api/projects/${id}/videos/${videoId}/reextract`)
      .then(() => {
        fetchVideos(id!);
        toast.success("数据已重置", { id: toastId });
      })
      .catch((err) => {
        console.error(err);
        toast.error("重置失败", { id: toastId });
      });
  };

  const handleSaveVideo = () => {
    const { prompt_pos_zh, ...nextFingerprint } =
      videoFingerprintEditor as VideoFingerprintEditorPayload & {
        prompt_pos_zh?: string;
      };
    const { width: _width, height: _height, seed: _seed, ...videoPayload } =
      currentVideo;
    void _width;
    void _height;
    void _seed;
    const payload = {
      ...videoPayload,
      video_fingerprint: JSON.stringify(nextFingerprint, null, 2),
    };
    axios
      .put(`/api/projects/${id}/videos/${currentVideo.id}`, payload)
      .then(() => {
        setIsVideoModalOpen(false);
        fetchVideos(id!);
        toast.success("视频设置已更新");
      })
      .catch((err) => {
        console.error(err);
        toast.error("更新失败");
      });
  };

  const handleOpenVideoPromptPreview = (video: Video) => {
    const payload = parseVideoFingerprintPayload(video.video_fingerprint);
    setVideoPromptPreview(
      buildVideoIssueReportPrompt(video, payload),
    );
    setIsVideoPromptPreviewOpen(true);
  };

  const handleOpenCharacterPromptPreview = (char: Character) => {
    setCharacterPromptPreview(
      buildCharacterIssueReportPrompt(char),
    );
    setIsCharacterPromptPreviewOpen(true);
  };

  const handleOpenScenePromptPreview = (scene: Scene) => {
    setScenePromptPreview(buildSceneIssueReportPrompt(scene, project));
    setIsScenePromptPreviewOpen(true);
  };

  const handleCopyPromptPreview = async (text: string, label: string) => {
    if (!text.trim()) {
      toast.error(`${label}为空，暂无可复制内容`);
      return;
    }
    const fallbackCopy = () => {
      const textarea = document.createElement("textarea");
      textarea.value = text;
      textarea.setAttribute("readonly", "true");
      textarea.style.position = "fixed";
      textarea.style.top = "0";
      textarea.style.left = "0";
      textarea.style.width = "1px";
      textarea.style.height = "1px";
      textarea.style.opacity = "0";
      textarea.style.pointerEvents = "none";
      document.body.appendChild(textarea);
      textarea.focus();
      textarea.select();
      textarea.setSelectionRange(0, textarea.value.length);
      const copied = document.execCommand("copy");
      document.body.removeChild(textarea);
      return copied;
    };

    try {
      const copied = fallbackCopy();
      if (!copied) {
        if (navigator.clipboard?.writeText) {
          await navigator.clipboard.writeText(text);
        } else {
          throw new Error("copy command failed");
        }
      }
      toast.success(`${label}已复制`);
    } catch (err) {
      console.error(err);
      toast.error("复制失败");
    }
  };

  const currentPromptLang: "zh" = "zh";
  const currentLanguagePhases = videoFingerprintEditor.phases_zh || [];
  const setCurrentLanguagePhases = (phases: VideoFingerprintPhaseEditor[]) => {
    setVideoFingerprintEditor((prev) => ({ ...prev, phases_zh: phases }));
  };

  const handleBatchGenerateVideos = () => {
    if (videoEpisodeSummaries.length === 0) {
      toast.error("当前还没有可生成的视频，请先完成镜头入库");
      return;
    }
    const toastId = toast("正在启动批量生成视频...", {
      description: "将依次执行视频生成，不进行额外 LLM 推理",
    });
    axios
      .post(`/api/projects/${id}/batch-generate-videos`)
      .then(() => {
        fetchVideos(id!);
        toast.success("批量任务已提交", { id: toastId });
      })
      .catch((err) => {
        console.error(err);
        toast.error(err.response?.data?.error || "提交任务失败", {
          id: toastId,
        });
      });
  };

  const handleBatchGenerateCharactersAndScenes = async () => {
    const toastId = toast("正在提交人物+场景生成任务...", {
      description: "会先等待人物基础图完成，再批量提交场景图生成",
    });
    try {
      await axios.post(`/api/projects/${id}/batch-generate-characters-scenes`);
      toast.success("人物+场景任务已加入队列", { id: toastId });
    } catch (err) {
      console.error(err);
      toast.error("提交任务失败", { id: toastId });
    }
  };

  const handleBatchGenerateAllMedia = async () => {
    const toastId = toast("正在提交人物+场景+视频生成任务...", {
      description: "会先等待人物图完成，再等待场景图完成，最后提交视频批量生成",
    });
    try {
      await axios.post(`/api/projects/${id}/batch-generate-all-media`);
      toast.success("人物+场景+视频任务已加入队列", { id: toastId });
    } catch (err) {
      console.error(err);
      toast.error("提交任务失败", { id: toastId });
    }
  };

  const [isResetVideosConfirmOpen, setIsResetVideosConfirmOpen] =
    useState(false);
  const [isExportDialogOpen, setIsExportDialogOpen] = useState(false);
  const [exportEpisode, setExportEpisode] = useState<number | "">("");
  const [isEpisodeAssetResetDialogOpen, setIsEpisodeAssetResetDialogOpen] =
    useState(false);
  const [episodeAssetResetTarget, setEpisodeAssetResetTarget] = useState<
    number | ""
  >("");
  const [isDeleteEpisodeDialogOpen, setIsDeleteEpisodeDialogOpen] =
    useState(false);
  const [deleteEpisodeTarget, setDeleteEpisodeTarget] = useState<number | "">(
    "",
  );
  const [deleteEpisodeConfirmText, setDeleteEpisodeConfirmText] = useState("");

  const handleResetVideos = () => {
    setIsResetVideosConfirmOpen(true);
  };

  const confirmResetVideos = () => {
    setIsResetVideosConfirmOpen(false);
    const toastId = toast("正在重置视频状态...", {
      description: "将删除完整视频、分段视频、过渡帧，清空生成字段并把状态重置为草稿",
    });
    axios
      .post(`/api/projects/${id}/videos/reset`)
      .then((res) => {
        fetchVideos(id!);
        toast.success(`已重置 ${res.data.count} 条视频记录`, {
          id: toastId,
        });
      })
      .catch((err) => {
        console.error(err);
        toast.error("重置失败", { id: toastId });
      });
  };

  const handleResetVideo = (video: Video) => {
    if (isShotPromptRepairBusy(video.id)) {
      toast.error("当前镜头正在修复提示词，请等待修复完成后再重置");
      return;
    }
    toast("确定要重置该视频状态吗？", {
      description:
        "会物理删除当前完整视频、分段视频与过渡帧，清空生成字段，并把状态恢复为草稿。",
      action: {
        label: "重置",
        onClick: () => {
          axios
            .post(`/api/projects/${id}/videos/${video.id}/reset-status`)
            .then(() => {
              fetchVideos(id!);
              toast.success("视频状态已重置");
            })
            .catch((err) => {
              console.error(err);
              toast.error(err.response?.data?.error || "重置失败");
            });
        },
      },
      cancel: {
        label: "取消",
        onClick: () => {},
      },
    });
  };

  const exportableEpisodes = videoEpisodeSummaries
    .filter((item) => item.all_generated)
    .map((item) => item.episode);
  const availableEpisodes = Array.from(
    new Set([
      ...sceneEpisodeSummaries.map((item) => item.episode),
      ...videoEpisodeSummaries.map((item) => item.episode),
    ]),
  ).sort((a, b) => a - b);
  const deletableEpisodes = availableEpisodes.filter((episode) => episode > 1);

  const handleOpenExportDialog = () => {
    if (exportableEpisodes.length === 0) {
      toast.error("暂无可导出的完整剧集");
      return;
    }
    if (exportEpisode === "") {
      setExportEpisode(exportableEpisodes[0]);
    }
    setIsExportDialogOpen(true);
  };

  const handleOpenEpisodeAssetResetDialog = () => {
    if (availableEpisodes.length === 0) {
      toast.error("暂无可操作的集数");
      return;
    }
    setEpisodeAssetResetTarget((prev) =>
      prev === "" ? availableEpisodes[0] : prev,
    );
    setIsEpisodeAssetResetDialogOpen(true);
  };

  const handleConfirmEpisodeAssetReset = () => {
    if (episodeAssetResetTarget === "") {
      toast.error("请选择要重置的集数");
      return;
    }
    const toastId = toast("正在按集重置资产...", {
      description:
        "会重置该集涉及角色的人物基础图、场景图，以及该集视频与分段文件",
    });
    axios
      .post(`/api/projects/${id}/episodes/reset-assets`, {
        episode: episodeAssetResetTarget,
      })
      .then((res) => {
        setIsEpisodeAssetResetDialogOpen(false);
        fetchCharacters(id!);
        fetchScenes(id!);
        fetchVideos(id!);
        toast.success(
          `第 ${res.data.episode} 集已重置：角色 ${res.data.characters_reset || 0} 个，场景 ${res.data.scenes_reset || 0} 个，视频 ${res.data.videos_reset || 0} 个`,
          { id: toastId },
        );
      })
      .catch((err) => {
        console.error(err);
        toast.error(err.response?.data?.error || "按集重置失败", {
          id: toastId,
        });
      });
  };

  const handleOpenDeleteEpisodeDialog = () => {
    if (deletableEpisodes.length === 0) {
      toast.error("当前没有可删除的集数，第 1 集不允许删除");
      return;
    }
    setDeleteEpisodeTarget((prev) =>
      prev === "" || prev === 1 ? deletableEpisodes[0] : prev,
    );
    setDeleteEpisodeConfirmText("");
    setIsDeleteEpisodeDialogOpen(true);
  };

  const handleConfirmDeleteEpisode = () => {
    if (deleteEpisodeTarget === "") {
      toast.error("请选择要删除的集数");
      return;
    }
    if (deleteEpisodeConfirmText.trim() !== "我同意删除") {
      toast.error("请输入“我同意删除”后才能继续");
      return;
    }
    const toastId = toast("正在删除整集...", {
      description:
        "会删除该集所有场景、视频，以及删除后不再被其他集引用的角色",
    });
    axios
      .post(`/api/projects/${id}/episodes/delete`, {
        episode: deleteEpisodeTarget,
      })
      .then((res) => {
        setIsDeleteEpisodeDialogOpen(false);
        setDeleteEpisodeConfirmText("");
        fetchCharacters(id!);
        fetchScenes(id!);
        fetchVideos(id!);
        toast.success(
          `第 ${res.data.episode} 集已删除：角色 ${res.data.characters_deleted || 0} 个，场景 ${res.data.scenes_deleted || 0} 个，视频 ${res.data.videos_deleted || 0} 个`,
          { id: toastId },
        );
      })
      .catch((err) => {
        console.error(err);
        toast.error(err.response?.data?.error || "删除整集失败", {
          id: toastId,
        });
      });
  };

  const handleExportEpisodeVideos = async () => {
    if (exportEpisode === "") {
      toast.error("请选择要导出的集数");
      return;
    }
    const toastId = toast("正在打包下载剧集文件...", {
      description: `正在整理第 ${exportEpisode} 集的镜头文件`,
    });
    try {
      const res = await axios.post(`/api/projects/${id}/videos/export`, {
        episode: exportEpisode,
      }, {
        responseType: "blob",
      });
      const filename = extractDownloadFilename(
        res.headers["content-disposition"],
        `${exportEpisode}-export.zip`,
      );
      triggerBlobDownload(res.data, filename);
      setIsExportDialogOpen(false);
      toast.success("导出包已开始下载", { id: toastId });
    } catch (err: any) {
      console.error(err);
      let message = "导出失败";
      if (err?.response?.data instanceof Blob) {
        try {
          const text = await err.response.data.text();
          const parsed = JSON.parse(text);
          message = parsed?.error || message;
        } catch {}
      } else if (err?.response?.data?.error) {
        message = err.response.data.error;
      }
      toast.error(message, { id: toastId });
    }
  };

  const handleMergeEpisodeVideos = async () => {
    if (exportEpisode === "") {
      toast.error("请选择要合并的集数");
      return;
    }
    const toastId = toast("正在合并导出成片...", {
      description: `正在合并第 ${exportEpisode} 集，并生成时间轴旁白文稿`,
    });
    try {
      const res = await axios.post(`/api/projects/${id}/videos/export-merged`, {
        episode: exportEpisode,
      }, {
        responseType: "blob",
      });
      const filename = extractDownloadFilename(
        res.headers["content-disposition"],
        `${exportEpisode}-merged.zip`,
      );
      triggerBlobDownload(res.data, filename);
      setIsExportDialogOpen(false);
      toast.success("成片导出包已开始下载", { id: toastId });
    } catch (err: any) {
      console.error(err);
      let message = "合并失败";
      if (err?.response?.data instanceof Blob) {
        try {
          const text = await err.response.data.text();
          const parsed = JSON.parse(text);
          message = parsed?.error || message;
        } catch {}
      } else if (err?.response?.data?.error) {
        message = err.response.data.error;
      }
      toast.error(message, { id: toastId });
    }
  };

  const handleRefreshCharacters = () => {
    if (id) fetchCharacters(id);
  };

  const handleRefreshScenes = () => {
    if (id) fetchScenes(id);
  };

  const handleRefreshVideos = () => {
    if (id) fetchVideos(id);
  };

  const handleFloatingRefresh = () => {
    if (!id) return;
    if (activeTab === "characters") {
      fetchCharacters(id);
      return;
    }
    if (activeTab === "scenes") {
      fetchScenes(id);
      return;
    }
    fetchVideos(id);
  };

  // Read-only auto-story mode keeps legacy edit handlers dormant while still
  // allowing deterministic preview generation actions.
  void [
    handleDeleteCharacter,
    handleResetCharacter,
    handleDeleteScene,
    handleAutoGeneratePrompt,
    handleDeleteAllCharacterImages,
    handleDeleteAllSceneImages,
    handleReextractVideo,
    handleOpenVideoPromptPreview,
    handleOpenCharacterPromptPreview,
    handleOpenScenePromptPreview,
    handleBatchGenerateCharactersAndScenes,
    handleBatchGenerateAllMedia,
    handleResetVideos,
    handleOpenEpisodeAssetResetDialog,
    handleOpenDeleteEpisodeDialog,
  ];

  if (!project) {
    return <div className="p-8 text-center">加载中...</div>;
  }

  return (
    <div className="space-y-6">
      {/* Header */}
      <div className="flex flex-col gap-4 md:flex-row md:items-center md:justify-between border-b border-border pb-4">
        <div className="flex items-center gap-4">
          <button
            onClick={() => navigate("/projects")}
            className="p-2 hover:bg-accent rounded-full transition-colors"
          >
            <ArrowLeft className="w-5 h-5" />
          </button>
          <div>
            <h1 className="text-2xl font-bold flex items-center gap-2">
              {project.name}
              <span className="text-sm font-normal text-muted-foreground bg-accent px-2 py-0.5 rounded">
                {project.code}
              </span>
            </h1>
            <p className="text-sm text-muted-foreground mt-1">
              画风:{" "}
              <span className="text-purple-500">{project.art_style?.name}</span>
              {project.description && <span className="mx-2">|</span>}
              {project.description}
            </p>
          </div>
        </div>
      </div>

      {lightweightAutoStoryReadonlyView && (
        <div className="rounded-lg border border-amber-500/20 bg-amber-500/5 px-4 py-3 text-sm">
          <p className="font-medium text-amber-700 dark:text-amber-400">
            当前页面已切换为自动剧情只读视图
          </p>
          <p className="mt-1 text-muted-foreground">
            这里仅展示自动剧情一次性生成的角色、旁白、首帧图提示词和视频提示词；不再支持手动新增、编辑、删除、参考图模式或服装优化。
          </p>
        </div>
      )}

      <div className="flex flex-col lg:flex-row gap-6">
        {/* Left Sidebar Actions */}
        <div className="w-full lg:w-64 space-y-3 shrink-0">
          <div className="p-4 bg-card border border-border rounded-lg shadow-sm space-y-3">
            <h3 className="font-semibold text-sm text-muted-foreground uppercase tracking-wider mb-2">
              一键操作
            </h3>
            {activeTab !== "videos" ? (
              <>
                {activeTab === "characters" && (
                  <>
                    <div className="rounded-md border border-border/60 bg-accent/20 px-3 py-3 text-sm text-muted-foreground">
                      角色页现在只展示自动剧情生成的人物资产，用于跨集记忆与查看，不再提供手动改写或人物强化入口。
                    </div>
                    <button
                      onClick={handleBatchGenerateCharacters}
                      className="w-full flex items-center gap-2 bg-secondary/50 hover:bg-secondary text-secondary-foreground px-4 py-3 rounded-md transition-colors text-sm"
                    >
                      <Wand2 className="w-4 h-4 text-blue-500" />
                      生成所有角色预览图
                    </button>
                  </>
                )}
                {activeTab === "scenes" && (
                  <>
                    <button
                      onClick={handleBatchGenerateScenes}
                      className="w-full flex items-center gap-2 bg-secondary/50 hover:bg-secondary text-secondary-foreground px-4 py-3 rounded-md transition-colors text-sm"
                    >
                      <Clapperboard className="w-4 h-4 text-green-500" />
                      生成所有场景图
                    </button>
                  </>
                )}
              </>
            ) : (
              <>
                <button
                  onClick={handleBatchGenerateVideos}
                  className="w-full flex items-center gap-2 bg-secondary/50 hover:bg-secondary text-secondary-foreground px-4 py-3 rounded-md transition-colors text-sm"
                >
                  <Film className="w-4 h-4 text-orange-500" />
                  生成所有视频
                </button>
              </>
            )}
          </div>
        </div>

        {/* Main Content Area */}
        <div className="flex-1 space-y-6">
          {/* Tabs */}
          <div className="flex border-b border-border justify-between items-center">
            <div className="flex">
              <button
                onClick={() => setActiveTab("characters")}
                className={`px-6 py-3 text-sm font-medium border-b-2 transition-colors ${
                  activeTab === "characters"
                    ? "border-primary text-primary"
                    : "border-transparent text-muted-foreground hover:text-foreground"
                }`}
              >
                角色预览
              </button>
              <button
                onClick={() => setActiveTab("scenes")}
                className={`px-6 py-3 text-sm font-medium border-b-2 transition-colors ${
                  activeTab === "scenes"
                    ? "border-primary text-primary"
                    : "border-transparent text-muted-foreground hover:text-foreground"
                }`}
              >
                场景列表
              </button>
              <button
                onClick={() => setActiveTab("videos")}
                className={`px-6 py-3 text-sm font-medium border-b-2 transition-colors ${
                  activeTab === "videos"
                    ? "border-primary text-primary"
                    : "border-transparent text-muted-foreground hover:text-foreground"
                }`}
              >
                视频列表
              </button>
            </div>
            <div className="pr-2 pb-2 flex items-center gap-2">
              {activeTab === "characters" && (
                <>
                  <button
                    onClick={handleRefreshCharacters}
                    className="flex items-center gap-2 border border-border bg-background px-3 py-1.5 rounded-md text-sm hover:bg-accent transition-colors"
                  >
                    <RefreshCw className="w-4 h-4" /> 刷新
                  </button>
                  <button
                    onClick={handleDeleteAllCharacterImages}
                    className="flex items-center gap-2 border border-destructive/40 bg-background px-3 py-1.5 rounded-md text-sm text-destructive hover:bg-destructive/5 transition-colors"
                  >
                    <RotateCcw className="w-4 h-4" /> 重置状态
                  </button>
                </>
              )}
              {activeTab === "scenes" && (
                <>
                  <button
                    onClick={handleRefreshScenes}
                    className="flex items-center gap-2 border border-border bg-background px-3 py-1.5 rounded-md text-sm hover:bg-accent transition-colors"
                  >
                    <RefreshCw className="w-4 h-4" /> 刷新
                  </button>
                  <button
                    onClick={handleDeleteAllSceneImages}
                    className="flex items-center gap-2 border border-destructive/40 bg-background px-3 py-1.5 rounded-md text-sm text-destructive hover:bg-destructive/5 transition-colors"
                  >
                    <RotateCcw className="w-4 h-4" /> 重置状态
                  </button>
                </>
              )}
              {activeTab === "videos" && (
                <>
                  <button
                    onClick={handleRefreshVideos}
                    className="flex items-center gap-2 border border-border bg-background px-3 py-1.5 rounded-md text-sm hover:bg-accent transition-colors"
                  >
                    <RefreshCw className="w-4 h-4" /> 刷新
                  </button>
                  <button
                    onClick={handleResetVideos}
                    className="flex items-center gap-2 border border-destructive/40 bg-background px-3 py-1.5 rounded-md text-sm text-destructive hover:bg-destructive/5 transition-colors"
                  >
                    <RotateCcw className="w-4 h-4" /> 重置状态
                  </button>
                  {exportableEpisodes.length > 0 && (
                    <button
                      onClick={handleOpenExportDialog}
                      className="flex items-center gap-2 bg-primary text-primary-foreground px-3 py-1.5 rounded-md text-sm hover:bg-primary/90 transition-colors"
                    >
                      <Download className="w-4 h-4" /> 导出/合并剧集
                    </button>
                  )}
                </>
              )}
            </div>
          </div>

          {/* Tab Content */}
          <div className="bg-card border border-border rounded-lg min-h-[400px]">
            {activeTab === "characters" && (
              <div className="p-4 space-y-4">
                {characters.length === 0 ? (
                  <div className="text-center py-12 text-muted-foreground">
                    暂无自动剧情角色结果
                  </div>
                ) : (
                  characters.map((char) => (
                    <div
                      key={char.id}
                      className="border border-border rounded-lg p-4 flex gap-4 hover:bg-accent/30 transition-colors"
                    >
                      {/* Preview Image (Generated) */}
                      <div
                        className="w-24 h-32 bg-secondary rounded-md shrink-0 cursor-pointer overflow-hidden relative group flex items-center justify-center"
                        onClick={() =>
                          char.generated_image &&
                          setPreviewImage(char.generated_image)
                        }
                      >
                        {char.generated_image ? (
                          <img
                            src={char.generated_image}
                            alt={char.name}
                            className="w-full h-full object-contain bg-black/5 transition-transform group-hover:scale-105"
                          />
                        ) : (
                          <div className="w-full h-full flex items-center justify-center text-muted-foreground">
                            <ImageIcon className="w-8 h-8 opacity-20" />
                          </div>
                        )}
                        {char.generated_image && (
                          <div className="absolute inset-0 bg-black/30 opacity-0 group-hover:opacity-100 transition-opacity flex items-center justify-center">
                            <Eye className="w-6 h-6 text-white" />
                          </div>
                        )}
                      </div>

                      {/* Content */}
                      <div className="flex-1 space-y-2">
                        <div className="flex justify-between items-start">
                          <div className="flex items-center gap-3">
                            <h3 className="font-bold text-lg">{char.name}</h3>
                            <span
                              className={`text-xs px-2 py-0.5 rounded-full ${char.status === "generated" ? "bg-green-500/10 text-green-500" : "bg-yellow-500/10 text-yellow-500"}`}
                            >
                              {char.status === "generated" ? "已生成" : "草稿"}
                            </span>
                            {char.status === "generated" &&
                              resolveGeneratedWorkflowLabel(
                                char.generated_workflow,
                              ) && (
                                <span className="text-xs px-2 py-0.5 rounded-full bg-secondary text-muted-foreground font-mono">
                                  {resolveGeneratedWorkflowLabel(
                                    char.generated_workflow,
                                  )}
                                </span>
                              )}
                            {char.is_locked && (
                              <span className="text-xs px-2 py-0.5 rounded-full bg-amber-500/10 text-amber-600">
                                已锁定
                              </span>
                            )}
                          </div>
                          <span className="text-xs px-2 py-0.5 rounded-full bg-secondary text-muted-foreground">
                            自动剧情角色
                          </span>
                        </div>

                        <div className="flex justify-end">
                          <button
                            onClick={() => handleGenerateImageOnly(char)}
                            className="p-1.5 hover:bg-accent rounded text-blue-500"
                            title="生成角色预览图"
                          >
                            <Wand2 className="w-4 h-4" />
                          </button>
                          <button
                            onClick={() => handleResetCharacter(char)}
                            className="p-1.5 hover:bg-accent rounded text-destructive"
                            title="重置角色状态"
                          >
                            <RotateCcw className="w-4 h-4" />
                          </button>
                        </div>

                        <div className="flex flex-wrap items-center gap-2 text-sm text-muted-foreground">
                          <span className="rounded-full bg-secondary px-2 py-0.5">
                            {char.gender || "未标注性别"}
                          </span>
                          {char.age && (
                            <span className="rounded-full bg-secondary px-2 py-0.5">
                              {char.age}
                            </span>
                          )}
                          {char.body_height && (
                            <span className="rounded-full bg-secondary px-2 py-0.5">
                              {char.body_height}
                            </span>
                          )}
                          {char.era && (
                            <span className="rounded-full bg-secondary px-2 py-0.5">
                              {char.era}
                            </span>
                          )}
                          {char.country && (
                            <span className="rounded-full bg-secondary px-2 py-0.5">
                              {char.country}
                            </span>
                          )}
                        </div>

                        <div className="rounded-md border border-border/60 bg-accent/20 px-3 py-2">
                          <p className="text-xs font-medium text-foreground/80 mb-1">
                            外观设定
                          </p>
                          <p className="text-sm text-muted-foreground whitespace-pre-wrap">
                            {getCharacterAppearanceText(char)}
                          </p>
                        </div>
                      </div>
                    </div>
                  ))
                )}
              </div>
            )}

            {activeTab === "scenes" && (
              <div className="p-4 space-y-4">
                {sceneEpisodeSummaries.length === 0 ? (
                  <div className="text-center py-12 text-muted-foreground">
                    暂无自动剧情镜头结果
                  </div>
                ) : (
                  sceneEpisodeSummaries.map(({ episode, count }) => (
                      <div
                        key={episode}
                        data-scene-episode={episode}
                        className="border border-border rounded-lg overflow-hidden"
                      >
                        <div
                          className="bg-accent/50 px-4 py-2 flex items-center gap-2 cursor-pointer hover:bg-accent transition-colors"
                          onClick={() =>
                            {
                              const episodeNumber = Number(episode);
                              setVisibleSceneEpisodes((prev) => ({
                                ...prev,
                                [episodeNumber]: true,
                              }));
                              setExpandedSceneEpisodes((prev) => ({
                                ...prev,
                                [episodeNumber]: !prev[episodeNumber],
                              }));
                            }
                          }
                        >
                          {expandedSceneEpisodes[Number(episode)] ? (
                            <ChevronDown className="w-4 h-4" />
                          ) : (
                            <ChevronRight className="w-4 h-4" />
                          )}
                          <span className="font-medium">第 {episode} 集</span>
                          <span className="text-xs text-muted-foreground ml-auto">
                            {count} 个场景
                          </span>
                        </div>

                        {expandedSceneEpisodes[Number(episode)] && (
                          sceneGrouped[Number(episode)] ? (
                            <div className="divide-y divide-border">
                              {sceneGrouped[Number(episode)].map((scene) => (
                              (() => {
                                const isRepairing = isShotPromptRepairBusy(scene.id);
                                return (
                              <div
                                key={scene.id}
                                className="p-4 flex gap-4 hover:bg-accent/10 transition-colors"
                              >
                                {/* Preview Image */}
                                <div
                                  className="w-32 h-20 bg-secondary rounded-md shrink-0 cursor-pointer overflow-hidden relative group"
                                  onClick={() =>
                                    scene.generated_image &&
                                    setPreviewImage(scene.generated_image)
                                  }
                                >
                                  {scene.generated_image ? (
                                    <img
                                      src={scene.generated_image}
                                      alt={
                                        scene.name ||
                                        `scene-${scene.scene_id || scene.scene_number}`
                                      }
                                      className="w-full h-full object-contain bg-black/5 transition-transform group-hover:scale-105"
                                    />
                                  ) : (
                                    <div className="w-full h-full flex items-center justify-center text-muted-foreground">
                                      <ImageIcon className="w-6 h-6 opacity-20" />
                                    </div>
                                  )}
                                </div>

                                <div className="flex-1 space-y-2">
                                    <div className="flex justify-between items-start">
                                      <div className="flex items-center gap-2">
                                        <span className="bg-primary/10 text-primary px-2 py-0.5 rounded text-xs font-mono">
                                        {scene.episode}-{scene.scene_id || scene.scene_number}
                                        </span>
                                      <h3 className="font-bold">
                                        {scene.name ||
                                          `镜头 ${scene.scene_id || scene.scene_number}`}
                                      </h3>
                                      {scene.duration_seconds ? (
                                        <span className="text-xs px-2 py-0.5 rounded-full bg-secondary text-muted-foreground">
                                          约 {scene.duration_seconds} 秒
                                        </span>
                                      ) : null}
                                      <span
                                        className={`text-xs px-2 py-0.5 rounded-full ${scene.status === "generated" ? "bg-green-500/10 text-green-500" : "bg-yellow-500/10 text-yellow-500"}`}
                                      >
                                        {scene.status === "generated"
                                          ? "已生成"
                                          : "草稿"}
                                      </span>
                                      {scene.status === "generated" &&
                                        resolveGeneratedWorkflowLabel(
                                          scene.generated_workflow,
                                        ) && (
                                          <span className="text-xs px-2 py-0.5 rounded-full bg-secondary text-muted-foreground font-mono">
                                            {resolveGeneratedWorkflowLabel(
                                              scene.generated_workflow,
                                            )}
                                          </span>
                                        )}
                                    </div>
                                    <div className="flex gap-2">
                                      <button
                                        onClick={() => handleOpenSceneRepairDialog(scene)}
                                        disabled={isRepairing}
                                        className={`px-2 py-1 text-xs rounded border border-border ${
                                          isRepairing
                                            ? "text-muted-foreground/50 cursor-not-allowed"
                                            : "hover:bg-accent text-amber-600"
                                        }`}
                                        title={
                                          isRepairing
                                            ? "修复提示词中，请等待结果回写"
                                            : "修复当前镜头的首帧图和视频提示词"
                                        }
                                      >
                                        {isRepairing ? "修复中..." : "修复"}
                                      </button>
                                      <button
                                        type="button"
                                        onClick={() =>
                                          handleOpenScenePromptEditDialog(scene)
                                        }
                                        className="p-1.5 hover:bg-accent rounded text-amber-500"
                                        title="编辑场景提示词"
                                      >
                                        <Pencil className="w-4 h-4" />
                                      </button>
                                      <button
                                        onClick={() =>
                                          handleGenerateSceneImageOnly(scene)
                                        }
                                        disabled={isRepairing}
                                        className={`p-1.5 rounded ${
                                          isRepairing
                                            ? "text-muted-foreground/50 cursor-not-allowed"
                                            : "hover:bg-accent text-blue-500"
                                        }`}
                                        title={
                                          isRepairing
                                            ? "修复提示词中，请等待结果回写"
                                            : "生成图片"
                                        }
                                      >
                                        <Wand2 className="w-4 h-4" />
                                      </button>
                                      <button
                                        onClick={() => handleResetScene(scene)}
                                        disabled={isRepairing}
                                        className={`p-1.5 rounded ${
                                          isRepairing
                                            ? "text-muted-foreground/50 cursor-not-allowed"
                                            : "hover:bg-accent text-destructive"
                                        }`}
                                        title={
                                          isRepairing
                                            ? "修复提示词中，请等待结果回写"
                                            : "重置场景状态"
                                        }
                                      >
                                        <RotateCcw className="w-4 h-4" />
                                      </button>
                                      <button
                                        type="button"
                                        onClick={() => handleDeleteScene(scene.id)}
                                        disabled={isRepairing}
                                        className={`p-1.5 rounded ${
                                          isRepairing
                                            ? "text-muted-foreground/50 cursor-not-allowed"
                                            : "hover:bg-accent text-destructive"
                                        }`}
                                        title={
                                          isRepairing
                                            ? "修复提示词中，请等待结果回写"
                                            : "删除当前场景记录"
                                        }
                                      >
                                        <Trash2 className="w-4 h-4" />
                                      </button>
                                    </div>
                                  </div>

                                  {scene.narration && (
                                    <div className="rounded-md border border-border/60 bg-accent/20 px-3 py-2">
                                      <p className="text-xs font-medium text-foreground/80 mb-1">
                                        旁白
                                      </p>
                                      <p className="text-sm text-muted-foreground whitespace-pre-wrap">
                                        {scene.narration}
                                      </p>
                                    </div>
                                  )}

                                  <div className="rounded-md border border-border/60 bg-background px-3 py-2">
                                    <p className="text-xs font-medium text-foreground/80 mb-1">
                                      首帧图提示词
                                    </p>
                                    <p className="text-sm text-muted-foreground whitespace-pre-wrap">
                                      {getSceneImagePromptText(scene) || "(暂无 image_prompt)"}
                                    </p>
                                  </div>

                                  <div className="rounded-md border border-border/60 bg-background px-3 py-2">
                                    <p className="text-xs font-medium text-foreground/80 mb-1">
                                      视频提示词
                                    </p>
                                    <p className="text-sm text-muted-foreground whitespace-pre-wrap">
                                      {getSceneVideoPromptText(scene) || "(暂无 video_prompt)"}
                                    </p>
                                  </div>

                                  {scene.characters &&
                                    scene.characters.length > 0 && (
                                      <div className="flex flex-wrap gap-1 mt-2">
                                        {scene.characters.map((c) => (
                                          <span
                                            key={c.id}
                                            className="text-xs bg-secondary text-secondary-foreground px-2 py-0.5 rounded-full flex items-center gap-1"
                                          >
                                            <Users className="w-3 h-3" />{" "}
                                            {c.name}
                                          </span>
                                        ))}
                                      </div>
                                    )}
                                </div>
                              </div>
                                );
                              })()
                              ))}
                              {sceneEpisodeHasMore[Number(episode)] && (
                                <div
                                  data-scene-page-sentinel={episode}
                                  className="px-4 py-4 text-center text-sm text-muted-foreground"
                                >
                                  {loadingSceneEpisodes[Number(episode)]
                                    ? "正在继续加载更多场景..."
                                    : "继续滚动将自动加载更多场景"}
                                </div>
                              )}
                            </div>
                          ) : loadingSceneEpisodes[Number(episode)] ? (
                            <div className="px-4 py-6 text-sm text-muted-foreground">
                              正在加载本集场景列表...
                            </div>
                          ) : (
                            <div className="px-4 py-6 text-sm text-muted-foreground">
                              正在按需加载本集场景列表...
                            </div>
                          )
                        )}
                      </div>
                    ))
                )}
              </div>
            )}

            {activeTab === "videos" && (
              <div className="p-4 space-y-4">
                {videoEpisodeSummaries.length === 0 ? (
                  <div className="text-center py-12 text-muted-foreground">
                    暂无视频数据，请先生成场景，系统将自动同步创建视频任务。
                  </div>
                ) : (
                  videoEpisodeSummaries.map(({ episode, count }) => (
                      <div
                        key={episode}
                        data-video-episode={episode}
                        className="border border-border rounded-lg overflow-hidden"
                      >
                        <div
                          className="bg-accent/50 px-4 py-2 flex items-center gap-2 cursor-pointer hover:bg-accent transition-colors"
                          onClick={() =>
                            {
                              const episodeNumber = Number(episode);
                              setVisibleVideoEpisodes((prev) => ({
                                ...prev,
                                [episodeNumber]: true,
                              }));
                              setExpandedVideoEpisodes((prev) => ({
                                ...prev,
                                [episodeNumber]: !prev[episodeNumber],
                              }));
                            }
                          }
                        >
                          {expandedVideoEpisodes[Number(episode)] ? (
                            <ChevronDown className="w-4 h-4" />
                          ) : (
                            <ChevronRight className="w-4 h-4" />
                          )}
                          <span className="font-medium">第 {episode} 集</span>
                          <span className="text-xs text-muted-foreground ml-auto">
                            {count} 个视频
                          </span>
                        </div>

                        {expandedVideoEpisodes[Number(episode)] && (
                          videoGrouped[Number(episode)] ? (
                            <div className="divide-y divide-border">
                              {videoGrouped[Number(episode)].map((video) => {
                                const isRepairing = isShotPromptRepairBusy(video.id);
                                const syncedScene = video.scene;
                                const videoPromptSummary =
                                  video.video_prompt?.trim() ||
                                  syncedScene?.video_prompt?.trim() ||
                                  buildVideoPositivePromptPreview(
                                    parseVideoFingerprintPayload(
                                      video.video_fingerprint,
                                    ),
                                  );
                                const imagePromptSummary =
                                  syncedScene ? getSceneImagePromptText(syncedScene) : "";
                                const canGenerateVideo = Boolean(
                                  syncedScene?.generated_image?.trim() &&
                                    (video.video_prompt?.trim() ||
                                      syncedScene?.video_prompt?.trim() ||
                                      video.video_fingerprint?.trim()) &&
                                    !isRepairing &&
                                    video.status !== "generating",
                                );
                                const generateVideoTitle = !syncedScene?.generated_image?.trim()
                                  ? "请先生成场景图"
                                  : isRepairing
                                    ? "修复提示词中，请等待结果回写"
                                  : !(
                                        video.video_prompt?.trim() ||
                                        syncedScene?.video_prompt?.trim() ||
                                        video.video_fingerprint?.trim()
                                      )
                                    ? "当前视频缺少可用的视频提示词"
                                    : video.status === "generating"
                                      ? "视频生成中"
                                      : "生成视频";
                                const canInspectJimengTask = Boolean(video.jm_task_id?.trim());

                                return (
                              <div
                                key={video.id}
                                className="p-4 flex gap-4 hover:bg-accent/10 transition-colors"
                              >
                                {/* Preview */}
                                <div
                                  className="w-32 h-20 bg-secondary rounded-md shrink-0 cursor-pointer overflow-hidden relative group"
                                  onClick={() =>
                                    video.generated_video &&
                                    setPreviewVideo(video.generated_video)
                                  }
                                >
                                  {video.generated_video ? (
                                    <div className="w-full h-full relative">
                                      {syncedScene?.generated_image ? (
                                        <img
                                          src={syncedScene.generated_image}
                                          className="w-full h-full object-contain bg-black/5"
                                        />
                                      ) : (
                                        <div className="w-full h-full flex items-center justify-center text-muted-foreground bg-black/5">
                                          <Film className="w-6 h-6 opacity-30" />
                                        </div>
                                      )}
                                      <div className="absolute inset-0 flex items-center justify-center bg-black/20 group-hover:bg-black/10 transition-colors">
                                        <PlayCircle className="w-8 h-8 text-white opacity-80 group-hover:opacity-100" />
                                      </div>
                                    </div>
                                  ) : (
                                    <div className="w-full h-full flex items-center justify-center text-muted-foreground relative">
                                      {syncedScene?.generated_image ? (
                                        <img
                                          src={syncedScene.generated_image}
                                          className="w-full h-full object-contain opacity-50"
                                        />
                                      ) : (
                                        <Film className="w-6 h-6 opacity-20" />
                                      )}
                                      {video.status === "generating" && (
                                        <div className="absolute inset-0 flex items-center justify-center bg-black/10">
                                          <RefreshCw className="w-5 h-5 animate-spin text-primary" />
                                        </div>
                                      )}
                                    </div>
                                  )}
                                </div>

                                <div className="flex-1 space-y-2">
                                  <div className="flex justify-between items-start">
                                    <div className="flex items-center gap-2">
                                      <span className="bg-primary/10 text-primary px-2 py-0.5 rounded text-xs font-mono">
                                        {syncedScene?.episode}-
                                        {syncedScene?.scene_number}
                                      </span>
                                      <h3 className="font-bold text-sm">
                                        {syncedScene?.name ||
                                          `镜头 ${syncedScene?.scene_id || syncedScene?.scene_number || video.scene_id}`}
                                        <span className="ml-2 font-normal text-muted-foreground text-xs">
                                          (约{" "}
                                          {video.duration_seconds ||
                                            syncedScene?.duration_seconds ||
                                            "-"}{" "}
                                          秒)
                                        </span>
                                      </h3>
                                      <span
                                        className={`text-xs px-2 py-0.5 rounded-full ${
                                          video.status === "generated"
                                            ? "bg-green-500/10 text-green-500"
                                            : video.status === "generating"
                                              ? "bg-blue-500/10 text-blue-500"
                                              : video.status === "failed"
                                                ? "bg-red-500/10 text-red-500"
                                                : "bg-yellow-500/10 text-yellow-500"
                                        }`}
                                      >
                                        {video.status === "generated"
                                          ? "已生成"
                                          : video.status === "generating"
                                            ? "生成中"
                                            : video.status === "failed"
                                              ? "失败"
                                              : "待处理"}
                                      </span>
                                      {video.status === "generated" &&
                                        resolveGeneratedWorkflowLabel(
                                          video.generated_workflow,
                                        ) && (
                                          <span className="text-xs px-2 py-0.5 rounded-full bg-secondary text-muted-foreground font-mono">
                                            {resolveGeneratedWorkflowLabel(
                                              video.generated_workflow,
                                            )}
                                          </span>
                                        )}
                                    </div>
                                    <div className="flex gap-2">
                                      <button
                                        type="button"
                                        onClick={() => handleOpenVideoRepairDialog(video)}
                                        disabled={isRepairing}
                                        className={`px-2 py-1 text-xs rounded border border-border ${
                                          isRepairing
                                            ? "text-muted-foreground/50 cursor-not-allowed"
                                            : "hover:bg-accent text-amber-600"
                                        }`}
                                        title={
                                          isRepairing
                                            ? "修复提示词中，请等待结果回写"
                                            : "修复当前镜头的视频提示词"
                                        }
                                      >
                                        {isRepairing ? "修复中..." : "修复"}
                                      </button>
                                      <button
                                        type="button"
                                        onClick={() => handleOpenVideoDurationDialog(video)}
                                        className="p-1.5 rounded transition-colors hover:bg-accent text-amber-500"
                                        title="编辑视频时长和提示词"
                                      >
                                        <Pencil className="w-4 h-4" />
                                      </button>
                                      <button
                                        type="button"
                                        onClick={() => handleViewJimengStatus(video)}
                                        disabled={!canInspectJimengTask}
                                        className={`p-1.5 rounded transition-colors ${
                                          canInspectJimengTask
                                            ? "hover:bg-accent text-violet-500"
                                            : "text-muted-foreground/40 cursor-not-allowed"
                                        }`}
                                        title={
                                          canInspectJimengTask
                                            ? "查看即梦任务状态"
                                            : "当前视频还没有 jm_task_id"
                                        }
                                      >
                                        <Eye className="w-4 h-4" />
                                      </button>
                                      <button
                                        type="button"
                                        onClick={() => handleGenerateVideo(video)}
                                        disabled={!canGenerateVideo}
                                        className={`p-1.5 rounded transition-colors ${
                                          canGenerateVideo
                                            ? "hover:bg-accent text-blue-500"
                                            : "text-muted-foreground/50 cursor-not-allowed"
                                        }`}
                                        title={generateVideoTitle}
                                      >
                                        <Film className="w-4 h-4" />
                                      </button>
                                      <button
                                        type="button"
                                        onClick={() => handleResetVideo(video)}
                                        disabled={isRepairing}
                                        className={`p-1.5 rounded transition-colors ${
                                          isRepairing
                                            ? "text-muted-foreground/50 cursor-not-allowed"
                                            : "hover:bg-accent text-destructive"
                                        }`}
                                        title={
                                          isRepairing
                                            ? "修复提示词中，请等待结果回写"
                                            : "重置视频状态"
                                        }
                                      >
                                        <RotateCcw className="w-4 h-4" />
                                      </button>
                                      <button
                                        type="button"
                                        onClick={() => handleDeleteVideo(video.id)}
                                        disabled={isRepairing}
                                        className={`p-1.5 rounded transition-colors ${
                                          isRepairing
                                            ? "text-muted-foreground/50 cursor-not-allowed"
                                            : "hover:bg-accent text-destructive"
                                        }`}
                                        title={
                                          isRepairing
                                            ? "修复提示词中，请等待结果回写"
                                            : "删除当前视频记录"
                                        }
                                      >
                                        <Trash2 className="w-4 h-4" />
                                      </button>
                                    </div>
                                  </div>

                                  {(video.narration ||
                                    syncedScene?.narration) && (
                                    <div className="rounded-md border border-border/60 bg-accent/20 px-3 py-2">
                                      <p className="text-xs font-medium text-foreground/80 mb-1">
                                        旁白
                                      </p>
                                      <p className="text-sm text-muted-foreground whitespace-pre-wrap">
                                        {video.narration ||
                                          syncedScene?.narration}
                                      </p>
                                    </div>
                                  )}

                                  <div className="rounded-md border border-border/60 bg-background px-3 py-2">
                                    <p className="text-xs font-medium text-foreground/80 mb-1">
                                      首帧图提示词
                                    </p>
                                    <p
                                      className="text-sm text-muted-foreground whitespace-pre-wrap"
                                      title={imagePromptSummary}
                                    >
                                      {imagePromptSummary || "(暂无 image_prompt)"}
                                    </p>
                                  </div>

                                  <div className="rounded-md border border-border/60 bg-background px-3 py-2">
                                    <p className="text-xs font-medium text-foreground/80 mb-1">
                                      视频提示词
                                    </p>
                                    <p
                                      className="text-sm text-muted-foreground whitespace-pre-wrap"
                                      title={videoPromptSummary}
                                    >
                                      {videoPromptSummary || "(视频指纹未生成)"}
                                    </p>
                                  </div>

                                  {syncedScene?.characters &&
                                    syncedScene.characters.length > 0 && (
                                      <div className="flex flex-wrap gap-1 mt-2">
                                        {syncedScene.characters.map((c) => (
                                          <span
                                            key={c.id}
                                            className="text-xs bg-secondary text-secondary-foreground px-2 py-0.5 rounded-full flex items-center gap-1"
                                          >
                                            <Users className="w-3 h-3" />{" "}
                                            {c.name}
                                          </span>
                                        ))}
                                      </div>
                                    )}
                                </div>
                              </div>
                                );
                              })}
                              {videoEpisodeHasMore[Number(episode)] && (
                                <div
                                  data-video-page-sentinel={episode}
                                  className="px-4 py-4 text-center text-sm text-muted-foreground"
                                >
                                  {loadingVideoEpisodes[Number(episode)]
                                    ? "正在继续加载更多视频..."
                                    : "继续滚动将自动加载更多视频"}
                                </div>
                              )}
                            </div>
                          ) : loadingVideoEpisodes[Number(episode)] ? (
                            <div className="px-4 py-6 text-sm text-muted-foreground">
                              正在加载本集视频列表...
                            </div>
                          ) : (
                            <div className="px-4 py-6 text-sm text-muted-foreground">
                              正在按需加载本集视频列表...
                            </div>
                          )
                        )}
                      </div>
                    ))
                )}
              </div>
            )}
          </div>
        </div>
      </div>

      {/* Character Modal */}
      <Dialog
        open={isCharModalOpen}
        onOpenChange={(open) => {
          // Only allow closing via X button or Cancel button (which call setIsCharModalOpen(false) directly)
          // We ignore the `onOpenChange` event from Radix UI which is triggered by ESC or clicking outside
          if (!open) return;
          setIsCharModalOpen(open);
        }}
      >
        <DialogContent
          className="max-w-2xl max-h-[90vh] overflow-y-auto"
          onInteractOutside={(e) => e.preventDefault()} // Prevent closing on click outside
          onEscapeKeyDown={(e) => e.preventDefault()} // Prevent closing on ESC
        >
          <DialogHeader>
            <DialogTitle>
              {currentChar.id ? "编辑角色" : "新增角色"}
            </DialogTitle>
            <DialogDescription className="sr-only">
              编辑或新增角色信息
            </DialogDescription>
            {/* Custom Close Button since we disabled default behaviors */}
            <button
              onClick={() => setIsCharModalOpen(false)}
              className="absolute right-4 top-4 rounded-sm opacity-70 ring-offset-background transition-opacity hover:opacity-100 focus:outline-none focus:ring-2 focus:ring-ring focus:ring-offset-2 disabled:pointer-events-none data-[state=open]:bg-accent data-[state=open]:text-muted-foreground"
            >
              <X className="w-4 h-4" />
              <span className="sr-only">Close</span>
            </button>
          </DialogHeader>
          <div className="grid gap-4 py-4">
            <div className="grid grid-cols-2 gap-4">
              <div>
                <label className="text-sm font-medium mb-1 block">
                  角色名称 <span className="text-red-500">*</span>
                </label>
                <Input
                  value={currentChar.name || ""}
                  onChange={(e) =>
                    setCurrentChar({ ...currentChar, name: e.target.value })
                  }
                />
              </div>
              <div>
                <label className="text-sm font-medium mb-1 block">
                  性别 <span className="text-red-500">*</span>
                </label>
                <Combobox
                  items={[
                    { id: "男性", name: "男性" },
                    { id: "女性", name: "女性" },
                    { id: "其他", name: "其他" },
                  ]}
                  value={currentChar.gender}
                  onChange={(val) =>
                    setCurrentChar({ ...currentChar, gender: String(val) })
                  }
                  placeholder="选择性别"
                  getItemValue={(i) => i.id}
                  getItemLabel={(i) => i.name}
                  renderItem={(i) => <span>{i.name}</span>}
                />
              </div>
            </div>

            <div>
              <label className="text-sm font-medium mb-1 block">
                人脸锚点 (face_fingerprint)
              </label>
              <Textarea
                className="h-20"
                value={currentChar.face_fingerprint || ""}
                onChange={(e) =>
                  setCurrentChar({
                    ...currentChar,
                    face_fingerprint: e.target.value,
                  })
                }
                placeholder="例如：长脸，细眉，眼尾微垂，鼻梁细直，右眼下浅小泪痣..."
              />
            </div>

            <div>
              <label className="text-sm font-medium mb-1 block">
                外貌/预览说明 <span className="text-red-500">*</span>
              </label>
              <Textarea
                value={currentChar.description || ""}
                onChange={(e) =>
                  setCurrentChar({
                    ...currentChar,
                    description: e.target.value,
                  })
                }
                placeholder="例如：黑发低髻，素青广袖长裙，身形清瘦，站姿端稳..."
              />
            </div>

            <div>
              <label className="text-sm font-medium mb-1 block">
                体态/服装/装备锚点 (fingerprint)
              </label>
              <Textarea
                className="h-24"
                value={currentChar.fingerprint || ""}
                onChange={(e) =>
                  setCurrentChar({
                    ...currentChar,
                    fingerprint: e.target.value,
                  })
                }
              />
            </div>

            <div className="rounded-md border border-border/60 bg-accent/20 px-3 py-2">
              <p className="text-sm font-medium">人物中文提示词</p>
              <p className="mt-1 text-xs text-muted-foreground">
                数据库仍按 JSON 结构兼容保存，但当前系统只使用中文提示词。
              </p>
            </div>

            <div className="grid gap-4 md:grid-cols-2">
              <div>
                <label className="text-sm font-medium mb-1 block">
                  正向提示词
                </label>
                <Textarea
                  className="h-24"
                  value={currentCharPositivePrompt.zh}
                  onChange={(e) =>
                    setCurrentCharPositivePrompt({
                      ...currentCharPositivePrompt,
                      zh: e.target.value,
                    })
                  }
                />
              </div>
              <div>
                <label className="text-sm font-medium mb-1 block">
                  反向提示词
                </label>
                <Textarea
                  className="h-24"
                  value={currentCharNegativePrompt.zh}
                  onChange={(e) =>
                    setCurrentCharNegativePrompt({
                      ...currentCharNegativePrompt,
                      zh: e.target.value,
                    })
                  }
                />
              </div>
            </div>

            <div className="flex items-center justify-between p-3 border rounded-md">
              <label className="text-sm font-medium">服装优化</label>
              <Switch
                checked={!!currentChar.optimize_clothing}
                disabled={isSeniorStageCharacter(currentChar)}
                onCheckedChange={(v) =>
                  setCurrentChar({ ...currentChar, optimize_clothing: v })
                }
              />
            </div>
            {isSeniorStageCharacter(currentChar) && (
              <p className="text-xs text-muted-foreground -mt-2">
                老年或晚年阶段不启用服装优化。
              </p>
            )}

            <div className="border-t pt-4 mt-2">
              <div className="flex items-center justify-between mb-4">
                <label className="text-sm font-medium">启用角色参考图</label>
                <Switch
                  checked={currentChar.use_ref_image}
                  onCheckedChange={(v) =>
                    setCurrentChar({ ...currentChar, use_ref_image: v })
                  }
                />
              </div>

              {currentChar.use_ref_image && (
                <div className="space-y-4">
                  {currentChar.ref_image && (
                    <div className="flex justify-center">
                      <div
                        className="relative w-full max-w-[300px] aspect-[2/3] border rounded bg-secondary overflow-hidden cursor-pointer group"
                        onClick={() => setPreviewImage(currentChar.ref_image!)}
                      >
                        <img
                          src={currentChar.ref_image}
                          className="w-full h-full object-cover"
                        />
                        <div className="absolute inset-0 bg-black/30 opacity-0 group-hover:opacity-100 transition-opacity flex items-center justify-center">
                          <Eye className="w-8 h-8 text-white" />
                        </div>
                      </div>
                    </div>
                  )}

                  <div className="space-y-2">
                    <label className="text-sm font-medium block">
                      上传参考图
                    </label>
                    <Input
                      type="file"
                      accept="image/*"
                      onChange={handleFileUpload}
                      className="cursor-pointer"
                    />
                  </div>
                </div>
              )}
            </div>
          </div>
          <DialogFooter>
            <button
              onClick={() => setIsCharModalOpen(false)}
              className="px-4 py-2 hover:bg-accent rounded-md"
            >
              取消
            </button>
            <button
              onClick={handleSaveCharacter}
              className="px-4 py-2 bg-primary text-primary-foreground rounded-md hover:bg-primary/90"
            >
              保存
            </button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* Scene Modal */}
      <Dialog
        open={isSceneModalOpen}
        onOpenChange={(open) => {
          if (!open) return;
          setIsSceneModalOpen(open);
        }}
      >
        <DialogContent
          className="max-w-2xl max-h-[90vh] overflow-y-auto"
          onInteractOutside={(e) => e.preventDefault()}
          onEscapeKeyDown={(e) => e.preventDefault()}
        >
          <DialogHeader>
            <DialogTitle>
              {currentScene.id ? "编辑场景" : "新增场景"}
            </DialogTitle>
            <DialogDescription className="sr-only">
              编辑或新增场景信息
            </DialogDescription>
            <button
              onClick={() => setIsSceneModalOpen(false)}
              className="absolute right-4 top-4 rounded-sm opacity-70 ring-offset-background transition-opacity hover:opacity-100 focus:outline-none focus:ring-2 focus:ring-ring focus:ring-offset-2 disabled:pointer-events-none data-[state=open]:bg-accent data-[state=open]:text-muted-foreground"
            >
              <X className="w-4 h-4" />
              <span className="sr-only">Close</span>
            </button>
          </DialogHeader>
          <div className="grid gap-4 py-4">
            <div className="grid grid-cols-2 gap-4">
              <div>
                <label className="text-sm font-medium mb-1 block">
                  第几集 <span className="text-red-500">*</span>
                </label>
                <Input
                  type="number"
                  value={currentScene.episode}
                  onChange={(e) =>
                    setCurrentScene({
                      ...currentScene,
                      episode: Number(e.target.value),
                    })
                  }
                />
              </div>
              <div>
                <label className="text-sm font-medium mb-1 block">
                  第几镜 <span className="text-red-500">*</span>
                </label>
                <Input
                  type="number"
                  value={currentScene.scene_number}
                  onChange={(e) =>
                    setCurrentScene({
                      ...currentScene,
                      scene_number: Number(e.target.value),
                    })
                  }
                />
              </div>
            </div>

            <div>
              <label className="text-sm font-medium mb-1 block">
                场景名称 <span className="text-red-500">*</span>
              </label>
              <Input
                value={currentScene.name || ""}
                onChange={(e) =>
                  setCurrentScene({ ...currentScene, name: e.target.value })
                }
              />
            </div>

            <div>
              <label className="text-sm font-medium mb-1 block">
                绑定角色 (多选)
              </label>
              <div className="border rounded-md p-3 max-h-40 overflow-y-auto space-y-2">
                {characters.map((char) => (
                  <div key={char.id} className="flex items-center gap-2">
                    <input
                      type="checkbox"
                      id={`char-${char.id}`}
                      checked={
                        currentScene.character_ids?.includes(char.id) || false
                      }
                      onChange={(e) => {
                        const ids = currentScene.character_ids || [];
                        if (e.target.checked) {
                          setCurrentScene({
                            ...currentScene,
                            character_ids: [...ids, char.id],
                          });
                        } else {
                          setCurrentScene({
                            ...currentScene,
                            character_ids: ids.filter((id) => id !== char.id),
                          });
                        }
                      }}
                      className="w-4 h-4"
                    />
                    <label
                      htmlFor={`char-${char.id}`}
                      className="text-sm cursor-pointer select-none flex-1"
                    >
                      {char.name}
                    </label>
                  </div>
                ))}
                {characters.length === 0 && (
                  <p className="text-xs text-muted-foreground">暂无角色可选</p>
                )}
              </div>
            </div>

            <div>
              <label className="text-sm font-medium mb-1 block">
                场景基础描述 <span className="text-red-500">*</span>
              </label>
              <Textarea
                value={currentScene.description || ""}
                onChange={(e) =>
                  setCurrentScene({
                    ...currentScene,
                    description: e.target.value,
                  })
                }
              />
            </div>

            <div>
              <label className="text-sm font-medium mb-1 block">
                旁白 / 解说词（仅无对白镜头）
              </label>
              <Textarea
                value={currentScene.narration || ""}
                onChange={(e) =>
                  setCurrentScene({
                    ...currentScene,
                    narration: e.target.value,
                  })
                }
                placeholder="仅完全无对白的镜头才填写，供剪辑师直接使用"
              />
            </div>

            <div className="rounded-md border border-border/60 bg-accent/20 px-3 py-2">
              <p className="text-sm font-medium">场景中文提示词</p>
              <p className="mt-1 text-xs text-muted-foreground">
                数据库仍按 JSON 结构兼容保存，但当前系统只使用中文提示词。
              </p>
            </div>

            <div className="grid gap-4 md:grid-cols-2">
              <div>
                <label className="text-sm font-medium mb-1 block">
                  正向提示词
                </label>
                <Textarea
                  className="h-24"
                  value={currentScenePositivePrompt.zh}
                  onChange={(e) =>
                    setCurrentScenePositivePrompt({
                      ...currentScenePositivePrompt,
                      zh: e.target.value,
                    })
                  }
                />
              </div>
              <div>
                <label className="text-sm font-medium mb-1 block">
                  反向提示词
                </label>
                <Textarea
                  className="h-24"
                  value={currentSceneNegativePrompt.zh}
                  onChange={(e) =>
                    setCurrentSceneNegativePrompt({
                      ...currentSceneNegativePrompt,
                      zh: e.target.value,
                    })
                  }
                />
              </div>
            </div>

          </div>
          <DialogFooter>
            <button
              onClick={() => setIsSceneModalOpen(false)}
              className="px-4 py-2 hover:bg-accent rounded-md"
            >
              取消
            </button>
            <button
              onClick={handleSaveScene}
              className="px-4 py-2 bg-primary text-primary-foreground rounded-md hover:bg-primary/90"
            >
              保存
            </button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <AlertDialog
        open={isResetVideosConfirmOpen}
        onOpenChange={setIsResetVideosConfirmOpen}
      >
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>重置视频状态</AlertDialogTitle>
            <AlertDialogDescription>
              此操作会删除当前项目已生成的完整视频、分段视频和过渡帧，清空生成字段，并把视频状态恢复为草稿。
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>取消</AlertDialogCancel>
            <AlertDialogAction
              onClick={confirmResetVideos}
              className="bg-destructive text-destructive-foreground hover:bg-destructive/90"
            >
              确认重置
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>


      <Dialog open={isExportDialogOpen} onOpenChange={setIsExportDialogOpen}>
        <DialogContent className="max-w-lg">
          <DialogHeader>
            <DialogTitle>导出或合并剧集视频</DialogTitle>
            <DialogDescription>
              可按集下载单镜头文件压缩包，包内文件名形如 `1-1.mp4`、`1-1.txt`；也可以直接下载该集合并成片的压缩包，包内包含 `1-merged.mp4` 和按时间轴排列的 `1-merged.txt`。
            </DialogDescription>
          </DialogHeader>
          <div className="grid gap-4 py-4">
            <div>
              <label className="text-sm font-medium mb-1 block">导出集数</label>
              <select
                value={exportEpisode}
                onChange={(e) => setExportEpisode(Number(e.target.value))}
                className="w-full rounded-md border border-input bg-background px-3 py-2 text-sm"
              >
                {exportableEpisodes.map((ep) => (
                  <option key={ep} value={ep}>
                    第 {ep} 集
                  </option>
                ))}
              </select>
            </div>
            <div className="rounded-md border border-border/60 bg-accent/20 px-3 py-2 text-xs text-muted-foreground">
              导出会直接走浏览器下载，不再要求手动填写 WSL/Windows 路径。保存位置由浏览器或桌面端下载设置决定。
            </div>
          </div>
          <DialogFooter>
            <button
              onClick={() => setIsExportDialogOpen(false)}
              className="px-4 py-2 hover:bg-accent rounded-md"
            >
              取消
            </button>
            <button
              onClick={handleExportEpisodeVideos}
              className="px-4 py-2 border border-border rounded-md hover:bg-accent"
            >
              导出单镜头
            </button>
            <button
              onClick={handleMergeEpisodeVideos}
              className="px-4 py-2 bg-primary text-primary-foreground rounded-md hover:bg-primary/90"
            >
              合并成片
            </button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <Dialog
        open={isEpisodeAssetResetDialogOpen}
        onOpenChange={setIsEpisodeAssetResetDialogOpen}
      >
        <DialogContent className="max-w-lg">
          <DialogHeader>
            <DialogTitle>按集重置资产</DialogTitle>
            <DialogDescription>
              会重置该集涉及角色的人物基础图、该集场景图，以及该集视频与分段文件。若这些角色也被其他集复用，其人物基础图也会一起受影响。
            </DialogDescription>
          </DialogHeader>
          <div className="grid gap-4 py-4">
            <div>
              <label className="text-sm font-medium mb-1 block">
                选择集数
              </label>
              <select
                value={episodeAssetResetTarget}
                onChange={(e) => setEpisodeAssetResetTarget(Number(e.target.value))}
                className="w-full rounded-md border border-input bg-background px-3 py-2 text-sm"
              >
                {availableEpisodes.map((ep) => (
                  <option key={ep} value={ep}>
                    第 {ep} 集
                  </option>
                ))}
              </select>
            </div>
          </div>
          <DialogFooter>
            <button
              onClick={() => setIsEpisodeAssetResetDialogOpen(false)}
              className="px-4 py-2 hover:bg-accent rounded-md"
            >
              取消
            </button>
            <button
              onClick={handleConfirmEpisodeAssetReset}
              className="px-4 py-2 bg-destructive text-destructive-foreground rounded-md hover:bg-destructive/90"
            >
              确认重置
            </button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <Dialog
        open={isDeleteEpisodeDialogOpen}
        onOpenChange={setIsDeleteEpisodeDialogOpen}
      >
        <DialogContent className="max-w-lg">
          <DialogHeader>
            <DialogTitle className="text-destructive text-xl">
              危险操作：删除整集
            </DialogTitle>
            <DialogDescription className="text-destructive/90">
              这个操作会物理删除该集视频、场景、相关图片，并删除删除后不再被其他集引用的角色记录。第 1 集永远不允许删除。
            </DialogDescription>
          </DialogHeader>
          <div className="grid gap-4 py-4">
            <div>
              <label className="text-sm font-medium mb-1 block">
                选择要删除的集数
              </label>
              <select
                value={deleteEpisodeTarget}
                onChange={(e) => setDeleteEpisodeTarget(Number(e.target.value))}
                className="w-full rounded-md border border-input bg-background px-3 py-2 text-sm"
              >
                {deletableEpisodes.map((ep) => (
                  <option key={ep} value={ep}>
                    第 {ep} 集
                  </option>
                ))}
              </select>
            </div>
            <div className="rounded-md border border-destructive/40 bg-destructive/5 px-3 py-3 text-sm text-destructive">
              请输入 <span className="font-bold">我同意删除</span> 才能继续。
            </div>
            <div>
              <Input
                value={deleteEpisodeConfirmText}
                onChange={(e) => setDeleteEpisodeConfirmText(e.target.value)}
                placeholder="请输入：我同意删除"
              />
            </div>
          </div>
          <DialogFooter>
            <button
              onClick={() => {
                setIsDeleteEpisodeDialogOpen(false);
                setDeleteEpisodeConfirmText("");
              }}
              className="px-4 py-2 hover:bg-accent rounded-md"
            >
              取消
            </button>
            <button
              onClick={handleConfirmDeleteEpisode}
              className="px-4 py-2 bg-destructive text-destructive-foreground rounded-md hover:bg-destructive/90"
            >
              确认删除
            </button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <Dialog
        open={isVideoPromptPreviewOpen}
        onOpenChange={setIsVideoPromptPreviewOpen}
      >
        <DialogContent className="max-w-4xl max-h-[90vh] overflow-y-auto">
          <DialogHeader>
            <DialogTitle>问题模板预览</DialogTitle>
            <DialogDescription>
              这里展示的是按当前系统固定 Style / Phase / Audio 路径组装的问题排查模板，方便直接复制给我分析，不可编辑。
            </DialogDescription>
          </DialogHeader>
          <div className="grid gap-4 py-2">
            <div className="rounded-md border border-border/60 bg-accent/20 px-3 py-2 text-xs text-muted-foreground">
              当前模板包含：场景基础描述、旁白、对白，以及当前系统固定 Style / Phase / Audio 路径提取出的正负向提示词。你只需要在末尾补充遇到的问题，再发给我即可。
            </div>
            <div className="grid gap-4">
              <div className="space-y-2">
                <div className="flex items-center justify-between">
                  <label className="text-sm font-medium">问题排查模板</label>
                  <button
                    onClick={() =>
                      handleCopyPromptPreview(
                        videoPromptPreview || "",
                        "提示词问题模板",
                      )
                    }
                    className="px-2 py-1 text-xs rounded border border-border hover:bg-accent"
                  >
                    复制
                  </button>
                </div>
                <Textarea
                  className="h-[32rem] w-full resize-none overflow-y-auto whitespace-pre-wrap font-mono text-xs"
                  readOnly
                  value={videoPromptPreview}
                />
              </div>
            </div>
          </div>
          <DialogFooter>
            <button
              onClick={() => setIsVideoPromptPreviewOpen(false)}
              className="px-4 py-2 hover:bg-accent rounded-md"
            >
              关闭
            </button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <Dialog
        open={isJimengStatusDialogOpen}
        onOpenChange={setIsJimengStatusDialogOpen}
      >
        <DialogContent className="max-w-4xl max-h-[90vh] overflow-y-auto">
          <DialogHeader>
            <DialogTitle>即梦任务状态</DialogTitle>
            <DialogDescription>
              这里显示即梦查询接口返回的完整 JSON，方便你判断当前任务是否已完成、是否可取回。
            </DialogDescription>
          </DialogHeader>
          <div className="grid gap-4 py-2">
            <div className="rounded-md border border-border/60 bg-accent/20 px-3 py-2 text-xs text-muted-foreground">
              {jimengStatusVideo ? (
                <span>
                  当前镜头：
                  第 {jimengStatusVideo.scene?.episode}-{jimengStatusVideo.scene?.scene_number} 镜，
                  jm_task_id：{jimengStatusVideo.jm_task_id || "-"}
                </span>
              ) : (
                <span>未选择视频</span>
              )}
            </div>
            {jimengStatusData && (
              <div className="grid grid-cols-2 md:grid-cols-4 gap-3 text-sm">
                <div className="rounded-md border px-3 py-2">
                  <div className="text-xs text-muted-foreground">任务状态</div>
                  <div className="font-medium">{jimengStatusData.status || "-"}</div>
                </div>
                <div className="rounded-md border px-3 py-2">
                  <div className="text-xs text-muted-foreground">业务 Code</div>
                  <div className="font-medium">{jimengStatusData.code}</div>
                </div>
                <div className="rounded-md border px-3 py-2">
                  <div className="text-xs text-muted-foreground">可取回</div>
                  <div className="font-medium">{jimengStatusData.can_retrieve ? "是" : "否"}</div>
                </div>
                <div className="rounded-md border px-3 py-2">
                  <div className="text-xs text-muted-foreground">已取回</div>
                  <div className="font-medium">{jimengStatusData.already_fetched ? "是" : "否"}</div>
                </div>
              </div>
            )}
            <div className="space-y-2">
              <div className="flex items-center justify-between">
                <label className="text-sm font-medium">完整返回 JSON</label>
                <button
                  onClick={() =>
                    handleCopyPromptPreview(
                      jimengStatusData?.pretty_json || "",
                      "即梦状态 JSON",
                    )
                  }
                  className="px-2 py-1 text-xs rounded border border-border hover:bg-accent"
                >
                  复制
                </button>
              </div>
              <Textarea
                className="h-[28rem] w-full resize-none overflow-y-auto whitespace-pre-wrap font-mono text-xs"
                readOnly
                value={
                  isJimengStatusLoading
                    ? "正在查询即梦状态..."
                    : jimengStatusData?.pretty_json || ""
                }
              />
            </div>
          </div>
          <DialogFooter>
            <button
              onClick={() => jimengStatusVideo && handleViewJimengStatus(jimengStatusVideo)}
              className="px-4 py-2 border border-border rounded-md hover:bg-accent disabled:opacity-50"
              disabled={isJimengStatusLoading || !jimengStatusVideo}
            >
              {isJimengStatusLoading ? "查询中..." : "刷新状态"}
            </button>
            <button
              onClick={handleRetrieveJimengVideo}
              className="px-4 py-2 bg-primary text-primary-foreground rounded-md hover:bg-primary/90 disabled:opacity-50"
              disabled={
                isJimengRetrieveLoading ||
                !jimengStatusData?.can_retrieve ||
                jimengStatusData?.already_fetched
              }
            >
              {isJimengRetrieveLoading
                ? "取回中..."
                : jimengStatusData?.already_fetched
                  ? "已取回"
                  : "取回结果"}
            </button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <Dialog
        open={isScenePromptPreviewOpen}
        onOpenChange={setIsScenePromptPreviewOpen}
      >
        <DialogContent className="max-w-4xl max-h-[90vh] overflow-y-auto">
          <DialogHeader>
            <DialogTitle>问题模板预览</DialogTitle>
            <DialogDescription>
              这里展示的是按当前系统固定中文路径组装的场景问题排查模板，方便直接复制给我分析，不可编辑。
            </DialogDescription>
          </DialogHeader>
          <div className="grid gap-4 py-2">
            <div className="rounded-md border border-border/60 bg-accent/20 px-3 py-2 text-xs text-muted-foreground">
              当前模板包含：场景基础描述、绑定角色视觉锚点，以及当前系统固定中文路径提取出的正负向提示词。你只需要在末尾补充遇到的问题，再发给我即可。
            </div>
            <div className="grid gap-4">
              <div className="space-y-2">
                <div className="flex items-center justify-between">
                  <label className="text-sm font-medium">问题排查模板</label>
                  <button
                    onClick={() =>
                      handleCopyPromptPreview(
                        scenePromptPreview || "",
                        "场景提示词问题模板",
                      )
                    }
                    className="px-2 py-1 text-xs rounded border border-border hover:bg-accent"
                  >
                    复制
                  </button>
                </div>
                <Textarea
                  className="h-[32rem] w-full resize-none overflow-y-auto whitespace-pre-wrap font-mono text-xs"
                  readOnly
                  value={scenePromptPreview}
                />
              </div>
            </div>
          </div>
          <DialogFooter>
            <button
              onClick={() => setIsScenePromptPreviewOpen(false)}
              className="px-4 py-2 hover:bg-accent rounded-md"
            >
              关闭
            </button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* Video Modal */}
      <Dialog
        open={isPromptRepairDialogOpen}
        onOpenChange={(open) => {
          setIsPromptRepairDialogOpen(open);
          if (!open) {
            setPromptRepairReason("");
            setIsPromptRepairSubmitting(false);
          }
        }}
      >
        <DialogContent className="max-w-2xl">
          <DialogHeader>
            <DialogTitle>修复提示词</DialogTitle>
            <DialogDescription>
              这里直接告诉 LLM 当前镜头哪里有问题。它会基于现有 image_prompt、video_prompt 和 duration_seconds 修复并回写。
            </DialogDescription>
          </DialogHeader>
          <div className="space-y-4 py-2">
            <div className="rounded-md border border-border/60 bg-accent/20 px-3 py-2">
              <p className="text-sm font-medium">{promptRepairDialog.title}</p>
              <p className="mt-1 text-xs text-muted-foreground">
                修复目标：z-image 首帧图提示词 + LTX2.3 视频提示词 + 视频时长
              </p>
            </div>
            <div>
              <label className="text-sm font-medium mb-1 block">
                当前存在的问题
              </label>
              <Textarea
                className="min-h-[12rem] w-full resize-y"
                placeholder="例如：人物衣服前后不一致；首帧里没有建立接触点；视频里动作不成立；LTX2.3 无法根据当前 video_prompt 表现出针飞出后命中的过程……"
                value={promptRepairReason}
                onChange={(e) => setPromptRepairReason(e.target.value)}
              />
            </div>
          </div>
          <DialogFooter>
            <button
              type="button"
              onClick={() => setIsPromptRepairDialogOpen(false)}
              className="px-4 py-2 rounded-md border border-border hover:bg-accent"
              disabled={isPromptRepairSubmitting}
            >
              取消
            </button>
            <button
              type="button"
              onClick={handleSubmitPromptRepair}
              className="px-4 py-2 rounded-md bg-primary text-primary-foreground hover:opacity-90 disabled:opacity-50"
              disabled={isPromptRepairSubmitting}
            >
              {isPromptRepairSubmitting ? "提交中..." : "提交修复"}
            </button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <Dialog
        open={isScenePromptEditDialogOpen}
        onOpenChange={setIsScenePromptEditDialogOpen}
      >
        <DialogContent className="max-w-3xl max-h-[90vh] overflow-y-auto">
          <DialogHeader>
            <DialogTitle>编辑场景提示词</DialogTitle>
            <DialogDescription>
              这里只改首帧图提示词。保存后会物理删除当前首帧图和关联视频，并把场景与视频状态重置为草稿，方便重新生成。
            </DialogDescription>
          </DialogHeader>
          <div className="space-y-4 py-2">
            <div className="rounded-md border border-border/60 bg-accent/20 px-3 py-2">
              <p className="text-sm font-medium">{scenePromptEditor.title}</p>
              <p className="mt-1 text-xs text-muted-foreground">
                当前保存逻辑会同步清理旧首帧图和旧视频，避免你改完提示词后仍然沿用旧资产。
              </p>
            </div>
            <div>
              <label className="text-sm font-medium mb-1 block">
                首帧图提示词
              </label>
              <Textarea
                className="min-h-[18rem] w-full resize-y"
                value={scenePromptEditor.imagePrompt}
                onChange={(e) =>
                  setScenePromptEditor((prev) => ({
                    ...prev,
                    imagePrompt: e.target.value,
                  }))
                }
              />
            </div>
          </div>
          <DialogFooter>
            <button
              type="button"
              onClick={() => setIsScenePromptEditDialogOpen(false)}
              className="px-4 py-2 rounded-md border border-border hover:bg-accent"
            >
              取消
            </button>
            <button
              type="button"
              onClick={handleSaveScenePromptEdit}
              className="px-4 py-2 rounded-md bg-primary text-primary-foreground hover:opacity-90"
            >
              保存
            </button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <Dialog
        open={isVideoDurationDialogOpen}
        onOpenChange={setIsVideoDurationDialogOpen}
      >
        <DialogContent className="max-w-3xl max-h-[90vh] overflow-y-auto">
          <DialogHeader>
            <DialogTitle>编辑视频</DialogTitle>
            <DialogDescription>
              可以直接修改当前视频的时长和 video_prompt。保存后会物理删除当前视频并把状态重置为草稿，方便重新提交测试。
            </DialogDescription>
          </DialogHeader>
          <div className="space-y-4 py-2">
            <div className="rounded-md border border-border/60 bg-accent/20 px-3 py-2">
              <p className="text-sm font-medium">{videoDurationEditor.title}</p>
              <p className="mt-1 text-xs text-muted-foreground">
                当前手动测试链路不再限制 13 秒上限；本地 LTX 会按你填写的秒数换算帧数提交。
              </p>
            </div>
            <div>
              <label className="text-sm font-medium mb-1 block">
                视频时长（秒）
              </label>
              <Input
                type="number"
                min={3}
                value={videoDurationEditor.durationSeconds}
                onChange={(e) =>
                  setVideoDurationEditor((prev) => ({
                    ...prev,
                    durationSeconds: e.target.value,
                  }))
                }
              />
            </div>
            <div>
              <label className="text-sm font-medium mb-1 block">
                视频提示词
              </label>
              <Textarea
                className="min-h-[16rem] w-full resize-y"
                value={videoDurationEditor.videoPrompt}
                onChange={(e) =>
                  setVideoDurationEditor((prev) => ({
                    ...prev,
                    videoPrompt: e.target.value,
                  }))
                }
              />
            </div>
          </div>
          <DialogFooter>
            <button
              type="button"
              onClick={() => setIsVideoDurationDialogOpen(false)}
              className="px-4 py-2 rounded-md border border-border hover:bg-accent"
            >
              取消
            </button>
            <button
              type="button"
              onClick={handleSaveVideoDuration}
              className="px-4 py-2 rounded-md bg-primary text-primary-foreground hover:opacity-90"
            >
              保存
            </button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <Dialog
        open={isVideoModalOpen}
        onOpenChange={(open) => {
          if (!open) return;
          setIsVideoModalOpen(open);
        }}
      >
        <DialogContent
          className="max-w-2xl max-h-[90vh] overflow-y-auto"
          onInteractOutside={(e) => e.preventDefault()}
          onEscapeKeyDown={(e) => e.preventDefault()}
        >
          <DialogHeader>
                    <DialogTitle>编辑视频指纹</DialogTitle>
                    <DialogDescription className="sr-only">
                      编辑视频指纹和生成参数
                    </DialogDescription>
            <button
              onClick={() => setIsVideoModalOpen(false)}
              className="absolute right-4 top-4 rounded-sm opacity-70 ring-offset-background transition-opacity hover:opacity-100 focus:outline-none focus:ring-2 focus:ring-ring focus:ring-offset-2 disabled:pointer-events-none data-[state=open]:bg-accent data-[state=open]:text-muted-foreground"
            >
              <X className="w-4 h-4" />
              <span className="sr-only">Close</span>
            </button>
          </DialogHeader>
          <div className="grid gap-4 py-4">
            <div className="rounded-md border border-border/60 bg-accent/20 px-3 py-2">
              <p className="text-sm font-medium">视频指纹编辑</p>
              <p className="mt-1 text-xs text-muted-foreground">
                当前编辑的是视频指纹。系统固定使用中文视频提示词与中文分段内容。
              </p>
            </div>

            <div className="grid grid-cols-2 gap-4">
              <div>
                <label className="text-sm font-medium mb-1 block">FPS</label>
                <Input
                  type="number"
                  value={videoFingerprintEditor.recommended_fps || 24}
                  onChange={(e) =>
                    setVideoFingerprintEditor({
                      ...videoFingerprintEditor,
                      recommended_fps: Number(e.target.value),
                    })
                  }
                />
              </div>
              <div>
                <label className="text-sm font-medium mb-1 block">
                  总时长（秒）
                </label>
                <Input
                  type="number"
                  value={videoFingerprintEditor.total_duration_seconds || 3}
                  onChange={(e) =>
                    setVideoFingerprintEditor({
                      ...videoFingerprintEditor,
                      total_duration_seconds: Number(e.target.value),
                    })
                  }
                />
              </div>
            </div>

            <div>
              <label className="text-sm font-medium mb-1 block">Style</label>
              <Textarea
                className="h-20"
                value={
                  videoFingerprintEditor.style_zh || ""
                }
                onChange={(e) =>
                  setVideoFingerprintEditor(
                    { ...videoFingerprintEditor, style_zh: e.target.value },
                  )
                }
              />
            </div>

            <div>
              <label className="text-sm font-medium mb-1 block">
                整段负向提示词
              </label>
              <Textarea
                className="h-20"
                value={
                  videoFingerprintEditor.prompt_neg_zh || ""
                }
                onChange={(e) =>
                  setVideoFingerprintEditor(
                    {
                      ...videoFingerprintEditor,
                      prompt_neg_zh: e.target.value,
                    },
                  )
                }
              />
            </div>

            <div className="space-y-3">
              <div className="flex items-center justify-between">
                <label className="text-sm font-medium">分段内容</label>
                <button
                  onClick={() => {
                    const next = [
                      ...currentLanguagePhases,
                      {
                        index: currentLanguagePhases.length + 1,
                        time_range: "",
                        content: "",
                        audio: "",
                      },
                    ];
                    setCurrentLanguagePhases(
                      next.map((phase, idx) => ({ ...phase, index: idx + 1 })),
                    );
                  }}
                  className="px-2 py-1 text-xs rounded border border-border hover:bg-accent"
                >
                  新增阶段
                </button>
              </div>
              {currentLanguagePhases.map((phase, idx) => (
                <div
                  key={`${currentPromptLang}-${idx}`}
                  className="rounded-md border border-border/60 bg-background px-3 py-3 space-y-3"
                >
                  <div className="flex items-center justify-between">
                    <p className="text-sm font-medium">第{idx + 1}段</p>
                    <button
                      onClick={() => {
                        const next = currentLanguagePhases
                          .filter((_, phaseIdx) => phaseIdx !== idx)
                          .map((item, order) => ({
                            ...item,
                            index: order + 1,
                          }));
                        setCurrentLanguagePhases(
                          next.length > 0
                            ? next
                            : [
                                {
                                  index: 1,
                                  time_range: "",
                                  content: "",
                                  audio: "",
                                },
                              ],
                        );
                      }}
                      className="px-2 py-1 text-xs rounded border border-border hover:bg-accent"
                    >
                      删除
                    </button>
                  </div>
                  <div>
                    <label className="text-xs font-medium mb-1 block">
                      时间
                    </label>
                    <Input
                      value={phase.time_range || ""}
                      onChange={(e) => {
                        const next = [...currentLanguagePhases];
                        next[idx] = { ...phase, time_range: e.target.value };
                        setCurrentLanguagePhases(next);
                      }}
                    />
                  </div>
                  <div>
                    <label className="text-xs font-medium mb-1 block">
                      content
                    </label>
                    <Textarea
                      className="h-24"
                      value={phase.content || ""}
                      onChange={(e) => {
                        const next = [...currentLanguagePhases];
                        next[idx] = { ...phase, content: e.target.value };
                        setCurrentLanguagePhases(next);
                      }}
                    />
                  </div>
                  <div>
                    <label className="text-xs font-medium mb-1 block">
                      audio
                    </label>
                    <Textarea
                      className="h-20"
                      value={phase.audio || ""}
                      onChange={(e) => {
                        const next = [...currentLanguagePhases];
                        next[idx] = { ...phase, audio: e.target.value };
                        setCurrentLanguagePhases(next);
                      }}
                    />
                  </div>
                </div>
              ))}
            </div>

          </div>
          <DialogFooter>
            <button
              onClick={() => setIsVideoModalOpen(false)}
              className="px-4 py-2 hover:bg-accent rounded-md"
            >
              取消
            </button>
            <button
              onClick={handleSaveVideo}
              className="px-4 py-2 bg-primary text-primary-foreground rounded-md hover:bg-primary/90"
            >
              保存
            </button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <Dialog
        open={isCharacterPromptPreviewOpen}
        onOpenChange={setIsCharacterPromptPreviewOpen}
      >
        <DialogContent className="max-w-4xl max-h-[90vh] overflow-y-auto">
          <DialogHeader>
            <DialogTitle>问题模板预览</DialogTitle>
            <DialogDescription>
              这里展示的是按当前系统固定中文路径组装的角色问题排查模板，方便直接复制给我分析，不可编辑。
            </DialogDescription>
          </DialogHeader>
          <div className="grid gap-4 py-2">
            <div className="rounded-md border border-border/60 bg-accent/20 px-3 py-2 text-xs text-muted-foreground">
              当前模板包含：人物基础描述、已锁定角色指纹、角色名、性别、当前强化模式，以及当前系统固定中文路径提取出的正负向提示词。你只需要在末尾补充遇到的问题，再发给我即可。
            </div>
            <div className="grid gap-4">
              <div className="space-y-2">
                <div className="flex items-center justify-between">
                  <label className="text-sm font-medium">问题排查模板</label>
                  <button
                    onClick={() =>
                      handleCopyPromptPreview(
                        characterPromptPreview || "",
                        "角色提示词问题模板",
                      )
                    }
                    className="px-2 py-1 text-xs rounded border border-border hover:bg-accent"
                  >
                    复制
                  </button>
                </div>
                <Textarea
                  className="h-[32rem] w-full resize-none overflow-y-auto whitespace-pre-wrap font-mono text-xs"
                  readOnly
                  value={characterPromptPreview}
                />
              </div>
            </div>
          </div>
          <DialogFooter>
            <button
              onClick={() => setIsCharacterPromptPreviewOpen(false)}
              className="px-4 py-2 hover:bg-accent rounded-md"
            >
              关闭
            </button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* Video Preview Modal */}
      {previewVideo && (
        <div
          className="fixed inset-0 z-[100] bg-black/90 flex items-center justify-center p-4 cursor-pointer"
          onClick={() => setPreviewVideo(null)}
        >
          <div
            className="relative max-w-4xl w-full aspect-video bg-black rounded overflow-hidden shadow-2xl"
            onClick={(e) => e.stopPropagation()}
          >
            <video
              src={previewVideo}
              controls
              autoPlay
              className="w-full h-full object-contain"
            />
            <button
              className="absolute top-4 right-4 text-white hover:text-gray-300 z-[101] p-2 bg-black/50 rounded-full transition-colors"
              onClick={() => setPreviewVideo(null)}
            >
              <X className="w-6 h-6" />
            </button>
          </div>
        </div>
      )}

      {/* Image Preview Modal */}
      {previewImage && (
        <div
          className="fixed inset-0 z-[100] bg-black/90 flex items-center justify-center p-4 cursor-pointer"
          onClick={(e) => {
            e.preventDefault();
            e.stopPropagation();
            setPreviewImage(null);
          }}
          onMouseDown={(e) => {
            e.preventDefault();
            e.stopPropagation();
          }}
        >
          <div
            className="relative flex max-w-[calc(100vw-2rem)] max-h-[calc(100vh-2rem)] items-center justify-center"
            onClick={(e) => e.stopPropagation()} // Prevent click on container closing it
          >
            <img
              src={previewImage}
              alt="Preview"
              className="block max-w-[calc(100vw-2rem)] max-h-[calc(100vh-2rem)] object-contain rounded shadow-2xl pointer-events-auto"
              onClick={(e) => {
                e.preventDefault();
                e.stopPropagation();
              }}
            />
            <button
              className="absolute -top-12 -right-12 text-white hover:text-gray-300 z-[101] p-2 bg-white/10 rounded-full hover:bg-white/20 transition-colors pointer-events-auto cursor-pointer"
              onClick={(e) => {
                e.preventDefault();
                e.stopPropagation();
                setPreviewImage(null);
              }}
              onMouseDown={(e) => e.stopPropagation()} // Prevent drag/selection
            >
              <span className="sr-only">Close</span>
              <X className="w-8 h-8 pointer-events-none" />
            </button>
          </div>
        </div>
      )}

      {showFloatingRefresh && (
        <button
          onClick={handleFloatingRefresh}
          className="fixed bottom-6 right-6 z-40 flex items-center gap-2 rounded-full bg-primary px-4 py-3 text-sm font-medium text-primary-foreground shadow-lg transition-colors hover:bg-primary/90"
        >
          <RefreshCw className="h-4 w-4" />
          刷新当前列表
        </button>
      )}
    </div>
  );
}
