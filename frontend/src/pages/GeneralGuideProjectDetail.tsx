import { useEffect, useMemo, useRef, useState } from "react";
import { useNavigate, useParams } from "react-router-dom";
import axios from "axios";
import { ArrowLeft, CheckCircle2, ChevronDown, Clapperboard, FolderInput, GitBranch, ImagePlus, Loader2, Megaphone, RefreshCw, Save, Scissors, Sparkles, Tags, UploadCloud, UserRound, Video } from "lucide-react";
import { toast } from "sonner";

import type { GeneralGuideProject, GeneralGuideReference, GeneralGuideScene, GeneralGuideTag, GeneralGuideTransition, GeneralGuideTransitionPresetListResponse, GeneralGuideTransitionPresetOption, LLMStreamState } from "@/types";
import { Checkbox } from "@/components/ui/checkbox";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";
import { Textarea } from "@/components/ui/textarea";

const withAssetVersion = (url?: string, version?: string) => {
  const trimmed = (url || "").trim();
  if (!trimmed) return "";
  if (!version) return trimmed;
  return `${trimmed}${trimmed.includes("?") ? "&" : "?"}v=${encodeURIComponent(version)}`;
};

const getSceneTypeLabel = (sceneType: GeneralGuideScene["scene_type"] | string) => {
  switch ((sceneType || "").trim()) {
    case "material_scene":
      return "纯素材场景";
    case "closing_scene":
      return "收尾场景";
    default:
      return "人物讲解场景";
  }
};

const getImagePresetLabel = (preset: GeneralGuideScene["image_preset"] | string) => {
  switch ((preset || "").trim()) {
    case "presenter_front_halfbody":
      return "室外";
    case "presenter_seated_table":
      return "桌边坐姿讲解";
    case "material_only":
      return "纯素材";
    default:
      return "室内";
  }
};

const getEnvironmentTypeLabel = (environmentType: GeneralGuideScene["environment_type"] | string) => {
  switch ((environmentType || "").trim()) {
    case "outdoor":
      return "室外";
    default:
      return "室内";
  }
};

const getUploadGuideHeadline = (draft: Pick<SceneDraft, "upload_headline" | "image_preset" | "environment_type" | "need_presenter" | "scene_type">) => {
  if ((draft.upload_headline || "").trim()) {
    return draft.upload_headline.trim();
  }
  if (!draft.need_presenter || draft.image_preset === "material_only" || draft.scene_type === "material_scene") {
    return "请上传：纯素材主体图";
  }
  if (draft.image_preset === "presenter_seated_table") {
    return "请上传：桌边坐姿讲解图";
  }
  if (draft.environment_type === "outdoor") {
    return "请上传：室外人物讲解图";
  }
  return "请上传：室内人物讲解图";
};

const getUploadGuideBadges = (draft: Pick<SceneDraft, "image_preset" | "environment_type" | "need_presenter" | "scene_type">) => {
  const badges = ["白天/明亮", "画面完整", "无遮挡"];
  if (!draft.need_presenter || draft.image_preset === "material_only" || draft.scene_type === "material_scene") {
    return [...badges, "无需讲解人", "主体清晰"];
  }
  if (draft.image_preset === "presenter_seated_table") {
    return [...badges, "人物坐姿", "桌面在前"];
  }
  return [...badges, "前景留站位", draft.environment_type === "outdoor" ? "室外半身" : "室内半身"];
};

interface BackgroundTaskRecord {
  id: string;
  status: "pending" | "running" | "completed" | "failed";
  progress: number;
  result?: string;
  error?: string;
}

type SceneDraft = {
  title: string;
  scene_type: GeneralGuideScene["scene_type"];
  environment_type: GeneralGuideScene["environment_type"];
  need_presenter: boolean;
  image_preset: GeneralGuideScene["image_preset"];
  upload_headline: string;
  upload_requirement: string;
  intro_text: string;
  video_positive_prompt: string;
  video_duration_seconds: string;
  video_width: string;
  video_height: string;
};

type TransitionDraft = {
  transition_prompt: string;
  duration_seconds: string;
  frames_from_end: string;
};

type ImageGenerateDialogState = {
  scene: GeneralGuideScene;
  randomSeed: boolean;
} | null;

type ProjectImageBatchGenerateDialogState = {
  action: "images" | "images_and_videos" | "images_videos_and_transitions";
} | null;

type MissingSceneImageDialogState = {
  action: "images" | "images_and_videos" | "images_videos_and_transitions";
  sceneIds: number[];
} | null;

type GeneralGuideProjectExportAction = "archive" | "merged";

const triggerBlobDownload = (blob: Blob, filename: string) => {
  const objectUrl = window.URL.createObjectURL(blob);
  const link = document.createElement("a");
  link.href = objectUrl;
  link.download = filename;
  document.body.appendChild(link);
  link.click();
  link.remove();
  window.URL.revokeObjectURL(objectUrl);
};

const getFilenameFromDisposition = (contentDisposition?: string, fallbackName = "download.bin") => {
  const raw = contentDisposition || "";
  const utf8Match = raw.match(/filename\*=UTF-8''([^;]+)/i);
  if (utf8Match?.[1]) {
    return decodeURIComponent(utf8Match[1]);
  }
  const plainMatch = raw.match(/filename="?([^"]+)"?/i);
  if (plainMatch?.[1]) {
    return plainMatch[1];
  }
  return fallbackName;
};

type ResetDialogState =
  | { kind: "scene"; target: GeneralGuideScene }
  | { kind: "transition"; target: GeneralGuideTransition }
  | { kind: "project_full" }
  | { kind: "project_status" }
  | null;

const videoSizePresets = [
  { id: "portrait_720", label: "竖屏 720 × 1280", width: 720, height: 1280 },
  { id: "portrait_1080", label: "竖屏 1080 × 1920", width: 1080, height: 1920 },
  { id: "landscape_1280", label: "横屏 1280 × 720", width: 1280, height: 720 },
  { id: "landscape_1920", label: "横屏 1920 × 1080", width: 1920, height: 1080 },
  { id: "square_640", label: "方图 640 × 640", width: 640, height: 640 },
];

const getTransitionPresetByPrompt = (options: GeneralGuideTransitionPresetOption[], prompt?: string) =>
  options.find((item) => item.prompt.trim() === (prompt || "").trim()) || null;

const presenterPersonaOptions = {
  female: [
    { value: "female_natural", label: "自然女性", description: "自然、真实、生活化，最通用。" },
    { value: "female_playful", label: "俏皮女性", description: "更灵动、机灵、轻快一点。" },
    { value: "female_sexy", label: "性感女性", description: "更成熟、自信、有魅力。" },
    { value: "female_gentle", label: "温柔女性", description: "更柔和、亲和、细腻。" },
  ],
  male: [
    { value: "male_natural", label: "自然男性", description: "自然、真实、生活化，最通用。" },
    { value: "male_steady", label: "稳重男性", description: "更沉着、可靠、成熟。" },
    { value: "male_confident", label: "自信男性", description: "更利落、干脆、有掌控感。" },
    { value: "male_warm", label: "温和男性", description: "更温和、友好、有亲近感。" },
  ],
} as const;

const emptyDraftFromScene = (scene: GeneralGuideScene): SceneDraft => ({
  title: scene.title || "",
  scene_type: scene.scene_type || "presenter_scene",
  environment_type: scene.environment_type || "indoor",
  need_presenter: !!scene.need_presenter,
  image_preset: scene.image_preset || "presenter_room_foreground",
  upload_headline: scene.upload_headline || "",
  upload_requirement: scene.upload_requirement || "",
  intro_text: scene.intro_text || "",
  video_positive_prompt: scene.video_positive_prompt || "",
  video_duration_seconds: `${scene.video_duration_seconds || 6}`,
  video_width: `${scene.video_width || 720}`,
  video_height: `${scene.video_height || 1280}`,
});

const emptyDraftFromTransition = (transition: GeneralGuideTransition): TransitionDraft => ({
  transition_prompt: transition.transition_prompt || "",
  duration_seconds: `${transition.duration_seconds || 2}`,
  frames_from_end: `${transition.frames_from_end || 3}`,
});

export default function GeneralGuideProjectDetail() {
  const { id } = useParams<{ id: string }>();
  const navigate = useNavigate();

  const [project, setProject] = useState<GeneralGuideProject | null>(null);
  const [references, setReferences] = useState<GeneralGuideReference[]>([]);
  const [scenes, setScenes] = useState<GeneralGuideScene[]>([]);
  const [transitions, setTransitions] = useState<GeneralGuideTransition[]>([]);
  const [tags, setTags] = useState<GeneralGuideTag[]>([]);
  const [loading, setLoading] = useState(false);
  const [savingProject, setSavingProject] = useState(false);
  const [planning, setPlanning] = useState(false);
  const [selectingReferenceID, setSelectingReferenceID] = useState<number | null>(null);
  const [savingSceneID, setSavingSceneID] = useState<number | null>(null);
  const [resettingSceneID, setResettingSceneID] = useState<number | null>(null);
  const [clearingSceneReferenceID, setClearingSceneReferenceID] = useState<number | null>(null);
  const [planningTaskId, setPlanningTaskId] = useState("");
  const [planningTask, setPlanningTask] = useState<BackgroundTaskRecord | null>(null);
  const [planningTaskStream, setPlanningTaskStream] = useState<LLMStreamState | null>(null);
  const [planningDialogOpen, setPlanningDialogOpen] = useState(false);
  const [previewImageUrl, setPreviewImageUrl] = useState("");
  const [previewVideoUrl, setPreviewVideoUrl] = useState("");
  const [projectName, setProjectName] = useState("");
  const [projectCode, setProjectCode] = useState("");
  const [projectDescription, setProjectDescription] = useState("");
  const [presenterGender, setPresenterGender] = useState<"male" | "female">("female");
  const [presenterPersona, setPresenterPersona] = useState<
    | "female_natural"
    | "female_playful"
    | "female_sexy"
    | "female_gentle"
    | "male_natural"
    | "male_steady"
    | "male_confident"
    | "male_warm"
  >("female_natural");
  const [planningContent, setPlanningContent] = useState("");
  const [selectedTagIDs, setSelectedTagIDs] = useState<number[]>([]);
  const [sceneDrafts, setSceneDrafts] = useState<Record<number, SceneDraft>>({});
  const [transitionDrafts, setTransitionDrafts] = useState<Record<number, TransitionDraft>>({});
  const [imageGenerateScene, setImageGenerateScene] = useState<ImageGenerateDialogState>(null);
  const [submittingImageSceneID, setSubmittingImageSceneID] = useState<number | null>(null);
  const [submittingVideoSceneID, setSubmittingVideoSceneID] = useState<number | null>(null);
  const [savingTransitionID, setSavingTransitionID] = useState<number | null>(null);
  const [extractingTransitionID, setExtractingTransitionID] = useState<number | null>(null);
  const [submittingTransitionID, setSubmittingTransitionID] = useState<number | null>(null);
  const [resettingTransitionID, setResettingTransitionID] = useState<number | null>(null);
  const [resettingProject, setResettingProject] = useState(false);
  const [resettingProjectStatus, setResettingProjectStatus] = useState(false);
  const [resetDialog, setResetDialog] = useState<ResetDialogState>(null);
  const [videoSizeDialogOpen, setVideoSizeDialogOpen] = useState(false);
  const [selectedVideoSizePreset, setSelectedVideoSizePreset] = useState("portrait_720");
  const [batchVideoWidth, setBatchVideoWidth] = useState("720");
  const [batchVideoHeight, setBatchVideoHeight] = useState("1280");
  const [savingBatchVideoSize, setSavingBatchVideoSize] = useState(false);
  const [planVideoSizeDialogOpen, setPlanVideoSizeDialogOpen] = useState(false);
  const [selectedPlanVideoSizePreset, setSelectedPlanVideoSizePreset] = useState("portrait_720");
  const [planVideoWidth, setPlanVideoWidth] = useState("720");
  const [planVideoHeight, setPlanVideoHeight] = useState("1280");
  const [projectGenerateMenuOpen, setProjectGenerateMenuOpen] = useState(false);
  const [projectResetMenuOpen, setProjectResetMenuOpen] = useState(false);
  const [projectTransitionPresetMenuOpen, setProjectTransitionPresetMenuOpen] = useState(false);
  const [projectExportMenuOpen, setProjectExportMenuOpen] = useState(false);
  const [transitionPresetEngine, setTransitionPresetEngine] = useState<"ltx2_3" | "wan2_2" | "ffmpeg">("ltx2_3");
  const [transitionPresetOptions, setTransitionPresetOptions] = useState<GeneralGuideTransitionPresetOption[]>([]);
  const [projectBatchTaskId, setProjectBatchTaskId] = useState("");
  const [projectBatchTask, setProjectBatchTask] = useState<BackgroundTaskRecord | null>(null);
  const [submittingProjectBatch, setSubmittingProjectBatch] = useState(false);
  const [projectImageBatchDialog, setProjectImageBatchDialog] = useState<ProjectImageBatchGenerateDialogState>(null);
  const [missingSceneImageDialog, setMissingSceneImageDialog] = useState<MissingSceneImageDialogState>(null);
  const [applyingTransitionPreset, setApplyingTransitionPreset] = useState(false);
  const [exportingProjectAction, setExportingProjectAction] = useState<GeneralGuideProjectExportAction | null>(null);
  const [imageGenerateMenuSceneID, setImageGenerateMenuSceneID] = useState<number | null>(null);
  const [videoGenerateMenuSceneID, setVideoGenerateMenuSceneID] = useState<number | null>(null);

  const appendReferenceInputRef = useRef<HTMLInputElement | null>(null);
  const sceneReferenceInputRefs = useRef<Record<number, HTMLInputElement | null>>({});
  const batchMissingUploadInputRef = useRef<HTMLInputElement | null>(null);
  const sceneCardRefs = useRef<Record<number, HTMLDivElement | null>>({});
  const planningStreamRef = useRef<HTMLDivElement | null>(null);

  const selectedReference = useMemo(
    () => references.find((item) => item.id === project?.selected_reference_id) || references.find((item) => item.is_selected) || references[0] || null,
    [references, project?.selected_reference_id],
  );

  const selectedTagObjects = useMemo(
    () => tags.filter((tag) => selectedTagIDs.includes(tag.id)),
    [tags, selectedTagIDs],
  );
  const hasRunningSceneTask = useMemo(
    () => scenes.some((scene) => scene.image_status === "generating" || scene.video_status === "generating"),
    [scenes],
  );
  const hasRunningTransitionTask = useMemo(
    () => transitions.some((transition) => transition.video_status === "generating"),
    [transitions],
  );
  const sceneById = useMemo(() => {
    const map = new Map<number, GeneralGuideScene>();
    scenes.forEach((scene) => map.set(scene.id, scene));
    return map;
  }, [scenes]);
  const transitionByFromSceneID = useMemo(() => {
    const map = new Map<number, GeneralGuideTransition>();
    transitions.forEach((transition) => map.set(transition.from_scene_id, transition));
    return map;
  }, [transitions]);
  const batchTransitionState = useMemo(() => {
    let runnable = 0;
    let blocked = 0;
    transitions.forEach((transition) => {
      if (transition.generated_video) {
        return;
      }
      const fromScene = sceneById.get(transition.from_scene_id);
      const toScene = sceneById.get(transition.to_scene_id);
      const ready =
        !!fromScene?.generated_video &&
        !!toScene?.generated_image &&
        !!String(transition.transition_prompt || "").trim();
      if (ready) {
        runnable += 1;
      } else {
        blocked += 1;
      }
    });
    return {
      runnable,
      blocked,
      allReady: transitions.length > 0 && runnable > 0 && blocked === 0,
    };
  }, [sceneById, transitions]);
  const transitionEngineLabel = transitionPresetEngine === "wan2_2" ? "Wan2.2" : transitionPresetEngine === "ffmpeg" ? "FFmpeg" : "LTX2.3";
  const isFFmpegTransitionEngine = transitionPresetEngine === "ffmpeg";

  const getBatchMissingSceneImages = (
    action: "images" | "images_and_videos" | "images_videos_and_transitions",
  ) =>
    scenes.filter((scene) => {
      if (action !== "images" && action !== "images_and_videos" && action !== "images_videos_and_transitions") {
        return false;
      }
      if (scene.generated_image) return false;
      return !scene.reference_image;
    });

  const missingSceneDialogScenes = useMemo(() => {
    if (!missingSceneImageDialog) return [];
    return missingSceneImageDialog.sceneIds
      .map((sceneId) => scenes.find((scene) => scene.id === sceneId) || null)
      .filter((scene): scene is GeneralGuideScene => !!scene && !scene.reference_image && !scene.generated_image);
  }, [missingSceneImageDialog, scenes]);

  const currentMissingScene = missingSceneDialogScenes[0] || null;

  const fetchTransitionPresets = async () => {
    const res = await axios.get("/api/general-guide-transition-presets");
    const payload = res.data as GeneralGuideTransitionPresetListResponse;
    setTransitionPresetEngine(payload.engine || "ltx2_3");
    setTransitionPresetOptions(Array.isArray(payload.presets) ? payload.presets : []);
  };

  const scrollToSceneCard = (sceneID?: number) => {
    if (!sceneID) return;
    sceneCardRefs.current[sceneID]?.scrollIntoView({ behavior: "smooth", block: "center" });
  };

  const needsPresenterForDraft = (draft: SceneDraft) => draft.image_preset !== "material_only" && draft.scene_type !== "material_scene";

  const fetchProject = async () => {
    if (!id) return;
    const res = await axios.get(`/api/general-guides/${id}`);
    const projectData = res.data as GeneralGuideProject;
    setProject(projectData);
    setProjectName(projectData.name || "");
    setProjectCode(projectData.code || "");
    setProjectDescription(projectData.description || "");
    setPresenterGender(projectData.presenter_gender === "male" ? "male" : "female");
    setPresenterPersona(
      projectData.presenter_persona && projectData.presenter_gender === "male"
        ? ((["male_natural", "male_steady", "male_confident", "male_warm"] as const).includes(projectData.presenter_persona as any)
            ? (projectData.presenter_persona as any)
            : "male_natural")
        : projectData.presenter_persona && projectData.presenter_gender !== "male"
          ? ((["female_natural", "female_playful", "female_sexy", "female_gentle"] as const).includes(projectData.presenter_persona as any)
              ? (projectData.presenter_persona as any)
              : "female_natural")
          : projectData.presenter_gender === "male"
            ? "male_natural"
            : "female_natural",
    );
    setPlanningContent(projectData.auto_generate_content || "");
    setSelectedTagIDs(projectData.tag_ids || []);
    setPlanningTaskId(String(projectData.current_planning_task_id || "").trim());
  };

  const fetchReferences = async () => {
    if (!id) return;
    const res = await axios.get(`/api/general-guides/${id}/references`);
    setReferences(Array.isArray(res.data) ? res.data : []);
  };

  const fetchScenes = async () => {
    if (!id) return;
    const res = await axios.get(`/api/general-guides/${id}/scenes`);
    const sceneList = Array.isArray(res.data) ? (res.data as GeneralGuideScene[]) : [];
    setScenes(sceneList);
    setSceneDrafts(
      sceneList.reduce<Record<number, SceneDraft>>((acc, scene) => {
        acc[scene.id] = emptyDraftFromScene(scene);
        return acc;
      }, {}),
    );
  };

  const fetchTransitions = async () => {
    if (!id) return;
    const res = await axios.get(`/api/general-guides/${id}/transitions`);
    const transitionList = Array.isArray(res.data) ? (res.data as GeneralGuideTransition[]) : [];
    setTransitions(transitionList);
    setTransitionDrafts(
      transitionList.reduce<Record<number, TransitionDraft>>((acc, transition) => {
        acc[transition.id] = emptyDraftFromTransition(transition);
        return acc;
      }, {}),
    );
  };

  const fetchTags = async () => {
    const res = await axios.get("/api/general-guide-tags");
    setTags(Array.isArray(res.data) ? res.data : []);
  };

  const fetchAll = async () => {
    if (!id) return;
    setLoading(true);
    try {
      await Promise.all([fetchProject(), fetchReferences(), fetchScenes(), fetchTransitions(), fetchTags(), fetchTransitionPresets()]);
    } catch (err) {
      console.error(err);
      toast.error("读取综合讲解项目失败");
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    fetchAll();
  }, [id]);

  useEffect(() => {
    if (!planningDialogOpen) return;
    const streamEl = planningStreamRef.current;
    if (!streamEl) return;
    const rafId = window.requestAnimationFrame(() => {
      streamEl.scrollTop = streamEl.scrollHeight;
    });
    return () => window.cancelAnimationFrame(rafId);
  }, [planningDialogOpen, planningTaskStream?.content]);

  useEffect(() => {
    if (!videoSizeDialogOpen || scenes.length === 0) return;
    const firstScene = scenes[0];
    const matchedPreset =
      videoSizePresets.find((preset) => preset.width === firstScene.video_width && preset.height === firstScene.video_height)?.id || "custom";
    setSelectedVideoSizePreset(matchedPreset);
    setBatchVideoWidth(`${firstScene.video_width || 720}`);
    setBatchVideoHeight(`${firstScene.video_height || 1280}`);
  }, [videoSizeDialogOpen, scenes]);

  useEffect(() => {
    if (!planVideoSizeDialogOpen) return;
    const firstScene = scenes[0];
    const width = firstScene?.video_width || Number.parseInt(batchVideoWidth, 10) || 720;
    const height = firstScene?.video_height || Number.parseInt(batchVideoHeight, 10) || 1280;
    const matchedPreset = videoSizePresets.find((preset) => preset.width === width && preset.height === height)?.id || "custom";
    setSelectedPlanVideoSizePreset(matchedPreset);
    setPlanVideoWidth(`${width}`);
    setPlanVideoHeight(`${height}`);
  }, [planVideoSizeDialogOpen, scenes, batchVideoWidth, batchVideoHeight]);

  useEffect(() => {
    if (!planningTaskId) return;
    let stopped = false;
    let taskTimer = 0;
    let streamTimer = 0;

    const stopPolling = () => {
      if (taskTimer) window.clearInterval(taskTimer);
      if (streamTimer) window.clearInterval(streamTimer);
    };

    const fetchTaskState = () => {
      axios
        .get(`/api/tasks/${planningTaskId}`)
        .then(async (res) => {
          if (stopped) return;
          const taskRecord = res.data as BackgroundTaskRecord;
          setPlanningTask(taskRecord);
          if (taskRecord.status === "completed" || taskRecord.status === "failed") {
            stopPolling();
            setPlanning(false);
            try {
              await Promise.all([fetchProject(), fetchScenes(), fetchTransitions()]);
            } catch (err) {
              console.error(err);
            }
            if (taskRecord.status === "completed") {
              toast.success("综合讲解场景规划完成");
            } else {
              toast.error(taskRecord.error || "综合讲解场景规划失败");
            }
          }
        })
        .catch((err) => {
          if (!stopped) console.error(err);
        });
    };

    const fetchTaskStream = () => {
      axios
        .get(`/api/tasks/${planningTaskId}/llm-stream`)
        .then((res) => {
          if (!stopped) {
            setPlanningTaskStream(res.data?.stream || null);
          }
        })
        .catch((err) => {
          if (!stopped) console.error(err);
        });
    };

    fetchTaskState();
    fetchTaskStream();
    taskTimer = window.setInterval(fetchTaskState, 1500);
    streamTimer = window.setInterval(fetchTaskStream, 700);

    return () => {
      stopped = true;
      stopPolling();
    };
  }, [planningTaskId]);

  useEffect(() => {
    if (!id || (!hasRunningSceneTask && !hasRunningTransitionTask)) return;
    const timer = window.setInterval(() => {
      void Promise.all([fetchScenes(), fetchTransitions()]);
    }, 2000);
    return () => window.clearInterval(timer);
  }, [id, hasRunningSceneTask, hasRunningTransitionTask]);

  const saveProjectSettings = async (appendFiles?: File[]) => {
    if (!project) return false;
    if (!projectName.trim() || !projectCode.trim()) {
      toast.error("请先填写项目名称和项目文件名");
      return false;
    }

    setSavingProject(true);
    try {
      const formData = new FormData();
      formData.append("name", projectName.trim());
      formData.append("code", projectCode.trim());
      formData.append("description", projectDescription.trim());
      formData.append("presenter_gender", presenterGender);
      formData.append("presenter_persona", presenterPersona);
      formData.append("auto_generate_content", planningContent.trim());
      formData.append("tag_ids_json", JSON.stringify(selectedTagIDs));
      (appendFiles || []).forEach((file) => {
        formData.append("presenter_reference_images", file);
      });
      const res = await axios.put(`/api/general-guides/${project.id}`, formData, {
        headers: { "Content-Type": "multipart/form-data" },
      });
      const updated = res.data as GeneralGuideProject;
      setProject(updated);
      setProjectName(updated.name || "");
      setProjectCode(updated.code || "");
      setProjectDescription(updated.description || "");
      setPresenterGender(updated.presenter_gender === "male" ? "male" : "female");
      setPresenterPersona(
        updated.presenter_persona && updated.presenter_gender === "male"
          ? ((["male_natural", "male_steady", "male_confident", "male_warm"] as const).includes(updated.presenter_persona as any)
              ? (updated.presenter_persona as any)
              : "male_natural")
          : updated.presenter_persona && updated.presenter_gender !== "male"
            ? ((["female_natural", "female_playful", "female_sexy", "female_gentle"] as const).includes(updated.presenter_persona as any)
                ? (updated.presenter_persona as any)
                : "female_natural")
            : updated.presenter_gender === "male"
              ? "male_natural"
              : "female_natural",
      );
      setPlanningContent(updated.auto_generate_content || "");
      setSelectedTagIDs(updated.tag_ids || []);
      await fetchReferences();
      toast.success(appendFiles?.length ? "参考图已追加" : "项目设置已保存");
      return true;
    } catch (err: any) {
      console.error(err);
      toast.error(err.response?.data?.error || "保存综合讲解项目失败");
      return false;
    } finally {
      setSavingProject(false);
      if (appendReferenceInputRef.current) {
        appendReferenceInputRef.current.value = "";
      }
    }
  };

  const handleAppendPresenterReferences = async (files: FileList | null) => {
    const nextFiles = Array.from(files || []);
    if (nextFiles.length === 0) return;
    await saveProjectSettings(nextFiles);
  };

  const handleSelectReference = async (referenceID: number) => {
    if (!project) return;
    setSelectingReferenceID(referenceID);
    try {
      await axios.post(`/api/general-guides/${project.id}/references/${referenceID}/select`);
      await Promise.all([fetchProject(), fetchReferences()]);
      toast.success("主讲解人参考图已切换");
    } catch (err: any) {
      console.error(err);
      toast.error(err.response?.data?.error || "切换参考图失败");
    } finally {
      setSelectingReferenceID(null);
    }
  };

  const openPlanScenesDialog = () => {
    if (!planningContent.trim()) {
      toast.error("请先填写项目总文案");
      return;
    }
    setPlanVideoSizeDialogOpen(true);
  };

  const applyPlanVideoSizePreset = (presetId: string) => {
    setSelectedPlanVideoSizePreset(presetId);
    const preset = videoSizePresets.find((item) => item.id === presetId);
    if (!preset) return;
    setPlanVideoWidth(`${preset.width}`);
    setPlanVideoHeight(`${preset.height}`);
  };

  const handlePlanScenes = async () => {
    if (!project) return;
    if (!planningContent.trim()) {
      toast.error("请先填写项目总文案");
      return;
    }
    const width = Number.parseInt(planVideoWidth, 10) || 0;
    const height = Number.parseInt(planVideoHeight, 10) || 0;
    if (width <= 0 || height <= 0) {
      toast.error("请先选择正确的视频尺寸");
      return;
    }
    const saved = await saveProjectSettings();
    if (!saved) return;
    setPlanning(true);
    setPlanningDialogOpen(true);
    setPlanVideoSizeDialogOpen(false);
    setPlanningTask(null);
    setPlanningTaskStream(null);
    try {
      const res = await axios.post(`/api/general-guides/${project.id}/plan-scenes`, {
        content: planningContent.trim(),
        width,
        height,
      });
      const taskId = String(res.data?.task_id || "").trim();
      if (!taskId) {
        throw new Error("任务 ID 缺失");
      }
      setPlanningTaskId(taskId);
    } catch (err: any) {
      console.error(err);
      setPlanning(false);
      toast.error(err.response?.data?.error || "启动综合讲解场景规划失败");
    }
  };

  const applyVideoSizePreset = (presetId: string) => {
    setSelectedVideoSizePreset(presetId);
    const preset = videoSizePresets.find((item) => item.id === presetId);
    if (!preset) return;
    setBatchVideoWidth(`${preset.width}`);
    setBatchVideoHeight(`${preset.height}`);
  };

  const handleBatchReplaceVideoSize = async () => {
    if (!project) return;
    const width = Number.parseInt(batchVideoWidth, 10) || 0;
    const height = Number.parseInt(batchVideoHeight, 10) || 0;
    if (width <= 0 || height <= 0) {
      toast.error("请先填写正确的视频宽高");
      return;
    }
    setSavingBatchVideoSize(true);
    try {
      const res = await axios.post(`/api/general-guides/${project.id}/batch-update-video-size`, {
        width,
        height,
      });
      const nextScenes = Array.isArray(res.data?.scenes) ? (res.data.scenes as GeneralGuideScene[]) : [];
      if (nextScenes.length > 0) {
        setScenes(nextScenes);
        setSceneDrafts(
          nextScenes.reduce<Record<number, SceneDraft>>((acc, scene) => {
            acc[scene.id] = emptyDraftFromScene(scene);
            return acc;
          }, {}),
        );
      } else {
        await fetchScenes();
      }
      setVideoSizeDialogOpen(false);
      toast.success(`已批量替换 ${res.data?.count || scenes.length} 行视频尺寸`);
    } catch (err: any) {
      console.error(err);
      toast.error(err.response?.data?.error || "批量替换视频尺寸失败");
    } finally {
      setSavingBatchVideoSize(false);
    }
  };

  const updateSceneDraft = (sceneID: number, updater: (draft: SceneDraft) => SceneDraft) => {
    setSceneDrafts((prev) => {
      const current = prev[sceneID];
      if (!current) return prev;
      return {
        ...prev,
        [sceneID]: updater(current),
      };
    });
  };

  const updateTransitionDraft = (transitionID: number, updater: (draft: TransitionDraft) => TransitionDraft) => {
    setTransitionDrafts((prev) => {
      const current = prev[transitionID];
      if (!current) return prev;
      return {
        ...prev,
        [transitionID]: updater(current),
      };
    });
  };

  const buildSceneFormData = (sceneID: number, file?: File | null, clearReference?: boolean) => {
    const draft = sceneDrafts[sceneID];
    const needPresenter = needsPresenterForDraft(draft);
    const formData = new FormData();
    formData.append("title", draft.title.trim());
    formData.append("scene_type", draft.scene_type);
    formData.append("environment_type", draft.environment_type);
    formData.append("need_presenter", needPresenter ? "1" : "0");
    formData.append("image_preset", draft.image_preset);
    formData.append("upload_headline", draft.upload_headline.trim());
    formData.append("upload_requirement", draft.upload_requirement.trim());
    formData.append("intro_text", draft.intro_text.trim());
    formData.append("video_positive_prompt", draft.video_positive_prompt.trim());
    formData.append("video_duration_seconds", `${Math.max(3, Number.parseInt(draft.video_duration_seconds, 10) || 6)}`);
    formData.append("video_width", `${Math.max(1, Number.parseInt(draft.video_width, 10) || 720)}`);
    formData.append("video_height", `${Math.max(1, Number.parseInt(draft.video_height, 10) || 1280)}`);
    if (clearReference) {
      formData.append("clear_reference_image", "1");
    }
    if (file) {
      formData.append("reference_image", file);
    }
    return formData;
  };

  const saveScene = async (sceneID: number, file?: File | null, clearReference?: boolean) => {
    setSavingSceneID(sceneID);
    try {
      const formData = buildSceneFormData(sceneID, file, clearReference);
      const res = await axios.put(`/api/general-guide-scenes/${sceneID}`, formData, {
        headers: { "Content-Type": "multipart/form-data" },
      });
      const updatedScene = res.data as GeneralGuideScene;
      setScenes((prev) => prev.map((scene) => (scene.id === sceneID ? updatedScene : scene)));
      setSceneDrafts((prev) => ({
        ...prev,
        [sceneID]: emptyDraftFromScene(updatedScene),
      }));
      if (clearReference) {
        toast.success("场景参考图已清空");
      } else if (file) {
        toast.success("场景参考图已更新");
      } else {
        toast.success("场景内容已保存");
      }
      return true;
    } catch (err: any) {
      console.error(err);
      toast.error(err.response?.data?.error || "保存场景失败");
      return false;
    } finally {
      setSavingSceneID(null);
      setClearingSceneReferenceID(null);
      const inputRef = sceneReferenceInputRefs.current[sceneID];
      if (inputRef) inputRef.value = "";
    }
  };

  const handleClearSceneReference = async (sceneID: number) => {
    setClearingSceneReferenceID(sceneID);
    await saveScene(sceneID, null, true);
  };

  const handleUploadSceneReference = async (sceneID: number, file: File | null) => {
    if (!file) return;
    await saveScene(sceneID, file, false);
  };

  useEffect(() => {
    if (!missingSceneImageDialog) return;
    if (missingSceneDialogScenes.length > 0) return;
    const nextAction = missingSceneImageDialog.action;
    setMissingSceneImageDialog(null);
    setProjectImageBatchDialog({ action: nextAction });
    toast.success("缺少的场景图已补齐，可以继续一键生成");
  }, [missingSceneDialogScenes.length, missingSceneImageDialog]);

  const triggerSceneImageGeneration = async (scene: GeneralGuideScene, useLightningLoRA: boolean, randomSeed = false) => {
    setSubmittingImageSceneID(scene.id);
    try {
      const saved = await saveScene(scene.id);
      if (!saved) return;
      await axios.post(`/api/general-guide-scenes/${scene.id}/generate-image`, {
        use_lightning_lora: useLightningLoRA,
        random_seed: randomSeed,
      });
      toast.success(
        randomSeed
          ? useLightningLoRA
            ? "图片抽卡任务已提交（Lightning LoRA）"
            : "图片抽卡任务已提交（No Lightning LoRA）"
          : useLightningLoRA
            ? "图片生成任务已提交（Lightning LoRA）"
            : "图片生成任务已提交（No Lightning LoRA）",
      );
      setImageGenerateScene(null);
      await fetchScenes();
    } catch (err: any) {
      console.error(err);
      toast.error(err.response?.data?.error || "提交图片生成任务失败");
    } finally {
      setSubmittingImageSceneID(null);
    }
  };

  const handleGenerateImageClick = async (scene: GeneralGuideScene, randomSeed = false) => {
    setImageGenerateMenuSceneID(null);
    const draft = sceneDrafts[scene.id] || emptyDraftFromScene(scene);
    if (!scene.reference_image) {
      toast.error("请先上传这一行的场景图片");
      return;
    }
    if (draft.image_preset === "material_only" || !needsPresenterForDraft(draft)) {
      await triggerSceneImageGeneration(scene, false, randomSeed);
      return;
    }
    setImageGenerateScene({ scene, randomSeed });
  };

  const handleGenerateVideoClick = async (scene: GeneralGuideScene, randomSeed = false) => {
    setVideoGenerateMenuSceneID(null);
    setSubmittingVideoSceneID(scene.id);
    try {
      const saved = await saveScene(scene.id);
      if (!saved) return;
      await axios.post(`/api/general-guide-scenes/${scene.id}/generate-video`, {
        random_seed: randomSeed,
      });
      toast.success(randomSeed ? "视频抽卡任务已提交" : "视频生成任务已提交");
      await fetchScenes();
    } catch (err: any) {
      console.error(err);
      toast.error(err.response?.data?.error || "提交视频生成任务失败");
    } finally {
      setSubmittingVideoSceneID(null);
    }
  };

  const handleResetSceneState = async (scene: GeneralGuideScene) => {
    setResetDialog({ kind: "scene", target: scene });
  };

  const confirmResetSceneState = async (scene: GeneralGuideScene) => {
    setResettingSceneID(scene.id);
    try {
      const res = await axios.post(`/api/general-guide-scenes/${scene.id}/reset-state`);
      const updatedScene = res.data as GeneralGuideScene;
      setScenes((prev) => prev.map((item) => (item.id === scene.id ? updatedScene : item)));
      setSceneDrafts((prev) => ({
        ...prev,
        [scene.id]: emptyDraftFromScene(updatedScene),
      }));
      await fetchTransitions();
      toast.success("这一行已重置");
    } catch (err: any) {
      console.error(err);
      toast.error(err.response?.data?.error || "重置这一行失败");
    } finally {
      setResettingSceneID(null);
      setResetDialog(null);
    }
  };

  const saveTransition = async (transitionID: number) => {
    const draft = transitionDrafts[transitionID];
    if (!draft) return false;
    setSavingTransitionID(transitionID);
    try {
      const res = await axios.put(`/api/general-guide-transitions/${transitionID}`, {
        transition_prompt: draft.transition_prompt.trim(),
        duration_seconds: Math.max(1, Number.parseInt(draft.duration_seconds, 10) || 2),
        frames_from_end: Math.max(1, Number.parseInt(draft.frames_from_end, 10) || 3),
      });
      const updated = res.data as GeneralGuideTransition;
      setTransitions((prev) => prev.map((item) => (item.id === transitionID ? updated : item)));
      setTransitionDrafts((prev) => ({
        ...prev,
        [transitionID]: emptyDraftFromTransition(updated),
      }));
      toast.success("转场内容已保存");
      return true;
    } catch (err: any) {
      console.error(err);
      toast.error(err.response?.data?.error || "保存转场失败");
      return false;
    } finally {
      setSavingTransitionID(null);
    }
  };

  const handleExtractTailFrame = async (transition: GeneralGuideTransition, scene?: GeneralGuideScene | null) => {
    if (!scene?.generated_video) {
      toast.error("请先生成上一行视频，再抽取尾帧");
      return;
    }
    setExtractingTransitionID(transition.id);
    try {
      const saved = await saveTransition(transition.id);
      if (!saved) return;
      const draft = transitionDrafts[transition.id] || emptyDraftFromTransition(transition);
      const res = await axios.post(`/api/general-guide-transitions/${transition.id}/extract-tail-frame`, {
        frames_from_end: Math.max(1, Number.parseInt(draft.frames_from_end, 10) || 3),
      });
      const updated = res.data as GeneralGuideTransition;
      setTransitions((prev) => prev.map((item) => (item.id === transition.id ? updated : item)));
      setTransitionDrafts((prev) => ({
        ...prev,
        [transition.id]: emptyDraftFromTransition(updated),
      }));
      toast.success("尾帧已抽取");
    } catch (err: any) {
      console.error(err);
      toast.error(err.response?.data?.error || "抽取尾帧失败");
    } finally {
      setExtractingTransitionID(null);
    }
  };

  const handleGenerateTransitionVideo = async (transition: GeneralGuideTransition) => {
    setSubmittingTransitionID(transition.id);
    try {
      const saved = await saveTransition(transition.id);
      if (!saved) return;
      await axios.post(`/api/general-guide-transitions/${transition.id}/generate-video`);
      toast.success("转场生成任务已提交");
      await fetchTransitions();
    } catch (err: any) {
      console.error(err);
      toast.error(err.response?.data?.error || "提交转场生成任务失败");
    } finally {
      setSubmittingTransitionID(null);
    }
  };

  const handleResetTransitionState = async (transition: GeneralGuideTransition) => {
    setResetDialog({ kind: "transition", target: transition });
  };

  const confirmResetTransitionState = async (transition: GeneralGuideTransition) => {
    setResettingTransitionID(transition.id);
    try {
      const res = await axios.post(`/api/general-guide-transitions/${transition.id}/reset-state`);
      const updated = res.data as GeneralGuideTransition;
      setTransitions((prev) => prev.map((item) => (item.id === transition.id ? updated : item)));
      setTransitionDrafts((prev) => ({
        ...prev,
        [transition.id]: emptyDraftFromTransition(updated),
      }));
      toast.success("转场已重置");
    } catch (err: any) {
      console.error(err);
      toast.error(err.response?.data?.error || "重置转场失败");
    } finally {
      setResettingTransitionID(null);
      setResetDialog(null);
    }
  };

  const submitProjectBatchGenerate = async (
    action: "images" | "videos" | "transitions" | "images_and_videos" | "images_videos_and_transitions",
    useLightningLoRA?: boolean,
  ) => {
    if (!project) return;

    const endpointMap = {
      images: "generate-all-images",
      videos: "generate-all-videos",
      transitions: "generate-all-transitions",
      images_and_videos: "generate-all-images-and-videos",
      images_videos_and_transitions: "generate-all-images-videos-and-transitions",
    } as const;
    const loadingMessageMap = {
      images: "综合讲解批量图片任务已提交",
      videos: "综合讲解批量视频任务已提交",
      transitions: "综合讲解批量转场任务已提交",
      images_and_videos: "综合讲解批量图片和视频任务已提交",
      images_videos_and_transitions: "综合讲解批量图片、视频和转场任务已提交",
    } as const;

    try {
      setSubmittingProjectBatch(true);
      const payload =
        action === "images" || action === "images_and_videos" || action === "images_videos_and_transitions"
          ? { use_lightning_lora: !!useLightningLoRA }
          : undefined;
      const res = await axios.post(`/api/general-guides/${project.id}/${endpointMap[action]}`, payload);
      const taskId = String(res.data?.task_id || "").trim();
      if (!taskId) {
        throw new Error("任务 ID 缺失");
      }
      setProjectBatchTaskId(taskId);
      setProjectBatchTask({
        id: taskId,
        status: "pending",
        progress: 0,
      });
      toast.success(res.data?.message || loadingMessageMap[action]);
    } catch (err: any) {
      console.error(err);
      toast.error(err.response?.data?.error || "提交项目级批量任务失败");
    } finally {
      setSubmittingProjectBatch(false);
    }
  };

  const handleStartProjectBatchGenerate = async (
    action: "images" | "videos" | "transitions" | "images_and_videos" | "images_videos_and_transitions",
  ) => {
    if (!project) return;
    setProjectGenerateMenuOpen(false);
    if (action === "images" || action === "images_and_videos" || action === "images_videos_and_transitions") {
      const missingScenes = getBatchMissingSceneImages(action);
      if (missingScenes.length > 0) {
        setMissingSceneImageDialog({
          action,
          sceneIds: missingScenes.map((scene) => scene.id),
        });
        return;
      }
      setProjectImageBatchDialog({ action });
      return;
    }
    await submitProjectBatchGenerate(action);
  };

  const handleApplyTransitionPresetToAll = async (presetKey: string) => {
    if (!project) return;
    setProjectTransitionPresetMenuOpen(false);
    setApplyingTransitionPreset(true);
    try {
      const res = await axios.post(`/api/general-guides/${project.id}/apply-transition-preset`, {
        preset_key: presetKey,
      });
      const updated = Array.isArray(res.data?.transitions) ? (res.data.transitions as GeneralGuideTransition[]) : [];
      setTransitions(updated);
      setTransitionDrafts(
        updated.reduce<Record<number, TransitionDraft>>((acc, transition) => {
          acc[transition.id] = emptyDraftFromTransition(transition);
          return acc;
        }, {}),
      );
      toast.success(res.data?.message || "已覆盖全部转场预设");
    } catch (err: any) {
      console.error(err);
      toast.error(err.response?.data?.error || "批量应用转场预设失败");
    } finally {
      setApplyingTransitionPreset(false);
    }
  };

  const handleExportProject = async (action: GeneralGuideProjectExportAction) => {
    if (!project) return;
    setProjectExportMenuOpen(false);
    setExportingProjectAction(action);
    try {
      const endpoint = action === "archive" ? "export" : "export-merged";
      const fallbackName = action === "archive" ? `${project.code || "general_guide"}_export.zip` : `${project.code || "general_guide"}_merged.mp4`;
      const res = await axios.post(`/api/general-guides/${project.id}/${endpoint}`, {}, { responseType: "blob" });
      const filename = getFilenameFromDisposition(res.headers["content-disposition"], fallbackName);
      triggerBlobDownload(res.data, filename);
      toast.success(action === "archive" ? "导出压缩包已开始下载" : "合并导出视频已开始下载");
    } catch (err: any) {
      console.error(err);
      toast.error(err.response?.data?.error || "导出失败");
    } finally {
      setExportingProjectAction(null);
    }
  };

  const handleResetProjectState = async () => {
    if (!project) return;
    setResettingProject(true);
    try {
      await axios.post(`/api/general-guides/${project.id}/reset-state`);
      setPlanning(false);
      setPlanningTaskId("");
      setPlanningTask(null);
      setPlanningTaskStream(null);
      setProjectBatchTaskId("");
      setProjectBatchTask(null);
      await fetchAll();
      toast.success("本项目场景、转场和对应资产已清空");
    } catch (err: any) {
      console.error(err);
      toast.error(err.response?.data?.error || "重置本项目失败");
    } finally {
      setResettingProject(false);
      setResetDialog(null);
    }
  };

  const handleResetProjectProcessingState = async () => {
    if (!project) return;
    setResettingProjectStatus(true);
    try {
      await axios.post(`/api/general-guides/${project.id}/reset-project-status`);
      setPlanning(false);
      setPlanningTaskId("");
      setPlanningTask(null);
      setPlanningTaskStream(null);
      setProjectBatchTaskId("");
      setProjectBatchTask(null);
      await fetchAll();
      toast.success("项目状态已重置，可重新执行一键生成");
    } catch (err: any) {
      console.error(err);
      toast.error(err.response?.data?.error || "重置项目状态失败");
    } finally {
      setResettingProjectStatus(false);
      setResetDialog(null);
    }
  };

  const planningRunning = planning || !!planningTaskId || !!project?.current_planning_task_id;
  const projectBatchRunning =
    submittingProjectBatch ||
    !!projectBatchTaskId ||
    projectBatchTask?.status === "pending" ||
    projectBatchTask?.status === "running";

  useEffect(() => {
    if (!projectBatchTaskId) return;
    let stopped = false;
    let timer = 0;

    const fetchTaskState = () => {
      axios
        .get(`/api/tasks/${projectBatchTaskId}`)
        .then(async (res) => {
          if (stopped) return;
          const taskRecord = res.data as BackgroundTaskRecord;
          setProjectBatchTask(taskRecord);
          if (taskRecord.status === "completed" || taskRecord.status === "failed") {
            if (timer) window.clearInterval(timer);
            setProjectBatchTaskId("");
            try {
              await Promise.all([fetchScenes(), fetchTransitions()]);
            } catch (err) {
              console.error(err);
            }
            if (taskRecord.status === "completed") {
              toast.success("项目级批量任务已完成");
            } else {
              toast.error(taskRecord.error || "项目级批量任务失败");
            }
          }
        })
        .catch((err) => {
          if (!stopped) console.error(err);
        });
    };

    fetchTaskState();
    timer = window.setInterval(fetchTaskState, 1500);

    return () => {
      stopped = true;
      if (timer) window.clearInterval(timer);
    };
  }, [projectBatchTaskId]);

  return (
    <div className="space-y-6">
      <div className="flex items-start justify-between gap-4">
        <div className="flex items-start gap-3">
          <button
            type="button"
            onClick={() => navigate("/general-guides")}
            className="mt-1 rounded-md border border-border p-2 hover:bg-accent transition-colors"
          >
            <ArrowLeft className="w-4 h-4" />
          </button>
          <div className="space-y-1">
            <h1 className="text-3xl font-bold">{projectName || "综合讲解"}</h1>
            <p className="text-sm text-muted-foreground">
              用尽可能少的项目总文案，自动规划讲解场景，再按每行要求上传对应图片即可。
            </p>
            {project?.last_planning_error && (
              <p className="text-sm text-destructive">最近一次规划失败：{project.last_planning_error}</p>
            )}
          </div>
        </div>

        <div className="flex items-center gap-2">
          <Popover open={projectExportMenuOpen} onOpenChange={setProjectExportMenuOpen}>
            <PopoverTrigger asChild>
              <button
                type="button"
                disabled={exportingProjectAction !== null || scenes.length === 0}
                className="flex items-center gap-2 rounded-md border border-border px-4 py-2 text-sm hover:bg-accent transition-colors disabled:opacity-50"
              >
                {exportingProjectAction ? <Loader2 className="w-4 h-4 animate-spin" /> : <FolderInput className="w-4 h-4" />}
                导出
                <ChevronDown className="w-4 h-4" />
              </button>
            </PopoverTrigger>
            <PopoverContent align="end" className="w-56 p-2">
              <button
                type="button"
                onClick={() => void handleExportProject("archive")}
                className="flex w-full items-center gap-2 rounded-md px-3 py-2 text-left text-sm hover:bg-accent transition-colors"
              >
                <FolderInput className="w-4 h-4" />
                按片段导出
              </button>
              <button
                type="button"
                onClick={() => void handleExportProject("merged")}
                className="mt-1 flex w-full items-center gap-2 rounded-md px-3 py-2 text-left text-sm hover:bg-accent transition-colors"
              >
                <Video className="w-4 h-4" />
                合并导出
              </button>
            </PopoverContent>
          </Popover>
          <button
            type="button"
            onClick={fetchAll}
            disabled={loading}
            className="flex items-center gap-2 rounded-md border border-border px-4 py-2 text-sm hover:bg-accent transition-colors disabled:opacity-50"
          >
            <RefreshCw className={`w-4 h-4 ${loading ? "animate-spin" : ""}`} />
            刷新
          </button>
          <Popover open={projectTransitionPresetMenuOpen} onOpenChange={setProjectTransitionPresetMenuOpen}>
            <PopoverTrigger asChild>
              <button
                type="button"
                disabled={applyingTransitionPreset || transitions.length === 0 || transitionPresetOptions.length === 0}
                className="flex items-center gap-2 rounded-md border border-border px-4 py-2 text-sm hover:bg-accent transition-colors disabled:opacity-50"
              >
                {applyingTransitionPreset ? <Loader2 className="w-4 h-4 animate-spin" /> : <GitBranch className="w-4 h-4" />}
                转场预设
                <ChevronDown className="w-4 h-4" />
              </button>
            </PopoverTrigger>
            <PopoverContent align="end" className="w-72 p-2">
              <div className="px-3 pb-2 text-[11px] leading-5 text-muted-foreground">
                当前引擎：{transitionEngineLabel}
              </div>
              {transitionPresetOptions.map((item) => (
                <button
                  key={item.key}
                  type="button"
                  onClick={() => void handleApplyTransitionPresetToAll(item.key)}
                  className="flex w-full flex-col items-start rounded-md px-3 py-2 text-left hover:bg-accent transition-colors"
                >
                  <span className="text-sm font-medium">{item.label}</span>
                  <span className="text-xs leading-5 text-muted-foreground">{item.description}</span>
                </button>
              ))}
            </PopoverContent>
          </Popover>
          <Popover open={projectGenerateMenuOpen} onOpenChange={setProjectGenerateMenuOpen}>
            <PopoverTrigger asChild>
              <button
                type="button"
                disabled={projectBatchRunning || planningRunning || scenes.length === 0 || hasRunningSceneTask || hasRunningTransitionTask}
                className="flex items-center gap-2 rounded-md border border-border px-4 py-2 text-sm hover:bg-accent transition-colors disabled:opacity-50"
              >
                {projectBatchRunning ? <Loader2 className="w-4 h-4 animate-spin" /> : <Sparkles className="w-4 h-4" />}
                一键生成
                <ChevronDown className="w-4 h-4" />
              </button>
            </PopoverTrigger>
            <PopoverContent align="end" className="w-56 p-2">
              <button
                type="button"
                onClick={() => void handleStartProjectBatchGenerate("images")}
                className="flex w-full items-center gap-2 rounded-md px-3 py-2 text-left text-sm hover:bg-accent transition-colors"
              >
                <ImagePlus className="w-4 h-4" />
                一键生成图片
              </button>
              <button
                type="button"
                onClick={() => void handleStartProjectBatchGenerate("videos")}
                className="mt-1 flex w-full items-center gap-2 rounded-md px-3 py-2 text-left text-sm hover:bg-accent transition-colors"
              >
                <Clapperboard className="w-4 h-4" />
                一键生成视频
              </button>
              <button
                type="button"
                onClick={() => void handleStartProjectBatchGenerate("transitions")}
                disabled={!batchTransitionState.allReady}
                className="mt-1 flex w-full items-center gap-2 rounded-md px-3 py-2 text-left text-sm hover:bg-accent transition-colors disabled:opacity-50"
              >
                <GitBranch className="w-4 h-4" />
                一键生成转场
              </button>
              <button
                type="button"
                onClick={() => void handleStartProjectBatchGenerate("images_and_videos")}
                className="mt-1 flex w-full items-center gap-2 rounded-md px-3 py-2 text-left text-sm text-primary hover:bg-primary/5 transition-colors"
              >
                <Sparkles className="w-4 h-4" />
                一键生成图片和视频
              </button>
              <button
                type="button"
                onClick={() => void handleStartProjectBatchGenerate("images_videos_and_transitions")}
                className="mt-1 flex w-full items-center gap-2 rounded-md px-3 py-2 text-left text-sm text-primary hover:bg-primary/5 transition-colors"
              >
                <Sparkles className="w-4 h-4" />
                一键自动图片、视频、转场
              </button>
              {!batchTransitionState.allReady ? (
                <p className="mt-2 px-3 text-[11px] leading-5 text-muted-foreground">
                  只有当所有未生成转场都满足“上一行有视频、下一行有已生成图片、转场提示词已填写”时，才能一键生成转场。
                </p>
              ) : null}
            </PopoverContent>
          </Popover>
          <Popover open={projectResetMenuOpen} onOpenChange={setProjectResetMenuOpen}>
            <PopoverTrigger asChild>
              <button
                type="button"
                disabled={resettingProject || resettingProjectStatus}
                className="flex items-center gap-2 rounded-md border border-destructive/40 px-4 py-2 text-sm text-destructive hover:bg-destructive/10 transition-colors disabled:opacity-50"
              >
                {(resettingProject || resettingProjectStatus) ? <Loader2 className="w-4 h-4 animate-spin" /> : <RefreshCw className="w-4 h-4" />}
                重置本项目
                <ChevronDown className="w-4 h-4" />
              </button>
            </PopoverTrigger>
            <PopoverContent align="end" className="w-56 p-2">
              <button
                type="button"
                onClick={() => {
                  setProjectResetMenuOpen(false);
                  setResetDialog({ kind: "project_status" });
                }}
                className="flex w-full items-center gap-2 rounded-md px-3 py-2 text-left text-sm hover:bg-accent transition-colors"
              >
                <RefreshCw className="w-4 h-4" />
                重置项目状态
              </button>
              <button
                type="button"
                onClick={() => {
                  setProjectResetMenuOpen(false);
                  setResetDialog({ kind: "project_full" });
                }}
                className="mt-1 flex w-full items-center gap-2 rounded-md px-3 py-2 text-left text-sm text-destructive hover:bg-destructive/10 transition-colors"
              >
                <RefreshCw className="w-4 h-4" />
                重置本项目所有
              </button>
            </PopoverContent>
          </Popover>
          <button
            type="button"
            onClick={() => setVideoSizeDialogOpen(true)}
            disabled={savingBatchVideoSize || scenes.length === 0}
            className="flex items-center gap-2 rounded-md border border-border px-4 py-2 text-sm hover:bg-accent transition-colors disabled:opacity-50"
          >
            <Video className="w-4 h-4" />
            批量替换视频尺寸
          </button>
          <button
            type="button"
            onClick={() => void saveProjectSettings()}
            disabled={savingProject}
            className="flex items-center gap-2 rounded-md border border-border px-4 py-2 text-sm hover:bg-accent transition-colors disabled:opacity-50"
          >
            <Save className="w-4 h-4" />
            保存项目设置
          </button>
          <button
            type="button"
            onClick={openPlanScenesDialog}
            disabled={savingProject || planningRunning}
            className="flex items-center gap-2 rounded-md bg-primary px-4 py-2 text-sm text-primary-foreground hover:bg-primary/90 transition-colors disabled:opacity-50"
          >
            <Sparkles className="w-4 h-4" />
            自动生成场景规划
          </button>
        </div>
      </div>

      <div className="grid grid-cols-1 xl:grid-cols-[320px_minmax(0,1fr)] gap-6">
        <div className="space-y-4">
          <div className="rounded-2xl border border-border bg-card p-4 shadow-sm">
            <div className="flex items-center gap-2 text-sm font-medium">
              <UserRound className="w-4 h-4" />
              主讲解人参考图
            </div>
            <div className="mt-3 rounded-2xl overflow-hidden border border-border/60 bg-muted/30 aspect-[4/5] flex items-center justify-center">
              {selectedReference?.image_path ? (
                <img
                  src={withAssetVersion(selectedReference.image_path, selectedReference.updated_at || project?.updated_at)}
                  alt="主参考图"
                  className="w-full h-full object-contain"
                />
              ) : (
                <Megaphone className="w-12 h-12 text-muted-foreground" />
              )}
            </div>
            <p className="mt-3 text-xs leading-6 text-muted-foreground">
              这里的主参考图只是帮助后续人物合成保持统一人脸与形象。综合讲解里，LLM 不会去猜这张图具体长什么样。
            </p>
            <input
              ref={appendReferenceInputRef}
              type="file"
              accept="image/*"
              multiple
              className="hidden"
              onChange={(e) => void handleAppendPresenterReferences(e.target.files)}
            />
            <button
              type="button"
              onClick={() => appendReferenceInputRef.current?.click()}
              disabled={savingProject}
              className="mt-4 flex w-full items-center justify-center gap-2 rounded-xl border border-dashed border-border px-3 py-3 text-sm hover:bg-accent transition-colors disabled:opacity-50"
            >
              <ImagePlus className="w-4 h-4" />
              追加讲解人参考图
            </button>
            <div className="mt-4 grid grid-cols-3 gap-2">
              {references.map((reference) => (
                <button
                  key={reference.id}
                  type="button"
                  onClick={() => void handleSelectReference(reference.id)}
                  disabled={selectingReferenceID === reference.id}
                  className={`rounded-xl overflow-hidden border transition-all ${
                    reference.id === selectedReference?.id ? "border-primary ring-2 ring-primary/20" : "border-border/60 hover:border-primary/40"
                  }`}
                >
                  <div className="aspect-[4/5] bg-muted/20">
                    <img
                      src={withAssetVersion(reference.image_path, reference.updated_at)}
                      alt={`参考图 ${reference.sort_order}`}
                      className="w-full h-full object-cover"
                    />
                  </div>
                  <div className="px-1 py-1 text-[10px] text-muted-foreground">
                    {reference.id === selectedReference?.id ? "当前主图" : `候选 ${reference.sort_order}`}
                  </div>
                </button>
              ))}
            </div>
          </div>

          <div className="rounded-2xl border border-border bg-card p-4 shadow-sm space-y-4">
            <div className="flex items-center gap-2 text-sm font-medium">
              <FolderInput className="w-4 h-4" />
              项目设置
            </div>

            <div className="space-y-2">
              <label className="text-sm font-medium">项目名称</label>
              <Input value={projectName} onChange={(e) => setProjectName(e.target.value)} />
            </div>

            <div className="space-y-2">
              <label className="text-sm font-medium">项目文件名</label>
              <Input value={projectCode} onChange={(e) => setProjectCode(e.target.value)} />
            </div>

            <div className="space-y-2">
              <label className="text-sm font-medium">备注（可选，仅用于识别项目）</label>
              <Textarea value={projectDescription} onChange={(e) => setProjectDescription(e.target.value)} className="min-h-[90px]" />
              <p className="text-xs leading-5 text-muted-foreground">备注只用于你自己区分项目，不会参与综合讲解的 LLM 场景规划。</p>
            </div>

            <div className="space-y-2">
              <label className="text-sm font-medium">讲解人性别</label>
              <select
                value={presenterGender}
                onChange={(e) => {
                  const nextGender = (e.target.value === "male" ? "male" : "female") as "male" | "female";
                  setPresenterGender(nextGender);
                  setPresenterPersona(nextGender === "male" ? "male_natural" : "female_natural");
                }}
                className="w-full rounded-md border border-input bg-background px-3 py-2 text-sm outline-none focus:border-ring focus:ring-2 focus:ring-ring/30"
              >
                <option value="female">女性</option>
                <option value="male">男性</option>
              </select>
              <p className="text-xs leading-5 text-muted-foreground">这个选项会传给 LLM，确保需要人物出镜和说话的场景始终维持正确性别。</p>
            </div>

            <div className="space-y-2">
              <label className="text-sm font-medium">讲解人人设</label>
              <select
                value={presenterPersona}
                onChange={(e) => setPresenterPersona(e.target.value as typeof presenterPersona)}
                className="w-full rounded-md border border-input bg-background px-3 py-2 text-sm outline-none focus:border-ring focus:ring-2 focus:ring-ring/30"
              >
                {presenterPersonaOptions[presenterGender].map((item) => (
                  <option key={item.value} value={item.value}>
                    {item.label}
                  </option>
                ))}
              </select>
              <p className="text-xs leading-5 text-muted-foreground">
                {presenterPersonaOptions[presenterGender].find((item) => item.value === presenterPersona)?.description} 这个设置也会一起传给
                LLM，让视频提示词里的说话语气、动作、表情和行为方式更符合角色设定。
              </p>
            </div>

            <div className="space-y-2">
              <div className="flex items-center gap-2 text-sm font-medium">
                <Tags className="w-4 h-4" />
                讲解标签
              </div>
              <div className="space-y-2 rounded-xl border border-border/60 bg-muted/20 p-3">
                {tags.map((tag) => (
                  <label key={tag.id} className="flex items-start gap-3 rounded-lg border border-border/50 bg-background px-3 py-3">
                    <Checkbox
                      checked={selectedTagIDs.includes(tag.id)}
                      onCheckedChange={() =>
                        setSelectedTagIDs((prev) => (prev.includes(tag.id) ? prev.filter((id) => id !== tag.id) : [...prev, tag.id]))
                      }
                    />
                    <div className="space-y-1">
                      <div className="text-sm font-medium">{tag.name}</div>
                      <div className="text-xs leading-5 text-muted-foreground">{tag.description}</div>
                    </div>
                  </label>
                ))}
              </div>
              {selectedTagObjects.length > 0 && (
                <div className="flex flex-wrap gap-2">
                  {selectedTagObjects.map((tag) => (
                    <span key={tag.id} className="rounded-full bg-muted px-2 py-1 text-xs text-muted-foreground">
                      {tag.name}
                    </span>
                  ))}
                </div>
              )}
            </div>
          </div>
        </div>

        <div className="space-y-6 min-w-0">
          <div className="rounded-2xl border border-border bg-card p-4 shadow-sm space-y-3">
            <div className="flex items-center justify-between gap-3">
              <div>
                <h2 className="text-lg font-semibold">项目总文案</h2>
                <p className="text-sm text-muted-foreground">
                  尽量少写，用一段自然描述就够了。系统会根据这里的内容和标签，自动拆成一行一行的讲解场景。
                </p>
              </div>
              {planningRunning && (
                <button
                  type="button"
                  onClick={() => setPlanningDialogOpen(true)}
                  className="rounded-md border border-border px-3 py-2 text-sm hover:bg-accent transition-colors"
                >
                  查看实时 LLM 流
                </button>
              )}
            </div>
            <Textarea
              value={planningContent}
              onChange={(e) => setPlanningContent(e.target.value)}
              className="min-h-[180px]"
              placeholder="例如：我现在有个店铺想出租，临街、商圈繁华、均价很便宜，而且不要物业费，老板娘怀孕了所以打算转出。我希望整体口吻亲和可爱，但看起来仍然专业可信。"
            />
          </div>

          <div className="space-y-4">
            <div className="flex items-center justify-between gap-4">
              <div>
                <h2 className="text-xl font-semibold">讲解场景</h2>
                <p className="text-sm text-muted-foreground">
                  LLM 会先返回每一行该拍什么、该上传什么图、该怎么讲；你再逐行上传图片和微调提示词。
                </p>
              </div>
              <div className="text-sm text-muted-foreground">当前 {scenes.length} 行</div>
            </div>

            {scenes.length === 0 ? (
              <div className="rounded-2xl border border-dashed border-border bg-card/50 p-10 text-center text-muted-foreground">
                还没有场景。先填写项目总文案，再点“自动生成场景规划”。
              </div>
            ) : (
              <div className="space-y-4">
                {scenes.map((scene) => {
                  const draft = sceneDrafts[scene.id] || emptyDraftFromScene(scene);
                  const isSavingScene = savingSceneID === scene.id;
                  const isResettingScene = resettingSceneID === scene.id;
                  const isClearingSceneRef = clearingSceneReferenceID === scene.id;
                  const transition = transitionByFromSceneID.get(scene.id) || null;
                  const nextScene = transition ? sceneById.get(transition.to_scene_id) || null : null;
                  const transitionDraft = transition ? transitionDrafts[transition.id] || emptyDraftFromTransition(transition) : null;
                  const isSavingTransition = transition ? savingTransitionID === transition.id : false;
                  const isExtractingTransition = transition ? extractingTransitionID === transition.id : false;
                  const isSubmittingTransition = transition ? submittingTransitionID === transition.id : false;
                  const isResettingTransition = transition ? resettingTransitionID === transition.id : false;
                  const nextScenePreview = nextScene ? withAssetVersion(nextScene.generated_image, nextScene.updated_at) : "";
                  const currentTransitionPreset = transitionDraft ? getTransitionPresetByPrompt(transitionPresetOptions, transitionDraft.transition_prompt) : null;
                  return (
                    <div key={`scene-block-${scene.id}`} className="space-y-4">
                    <div
                      ref={(node) => {
                        sceneCardRefs.current[scene.id] = node;
                      }}
                      className="rounded-2xl border border-border bg-card p-4 shadow-sm space-y-4"
                    >
                      <div className="flex items-start justify-between gap-4">
                            <div className="space-y-1 min-w-0">
                              <div className="flex flex-wrap items-center gap-2">
                            <span className="rounded-full bg-primary/10 px-2 py-1 text-xs font-medium text-primary">第 {scene.sort_order} 行</span>
                            <span className="rounded-full bg-muted px-2 py-1 text-xs text-muted-foreground">{getSceneTypeLabel(draft.scene_type)}</span>
                            <span className="rounded-full bg-muted px-2 py-1 text-xs text-muted-foreground">{getEnvironmentTypeLabel(draft.environment_type)}</span>
                            <span className="rounded-full bg-muted px-2 py-1 text-xs text-muted-foreground">{getImagePresetLabel(draft.image_preset)}</span>
                            <span className="rounded-full bg-muted px-2 py-1 text-xs text-muted-foreground">
                              {needsPresenterForDraft(draft) ? "会合成人物" : "纯素材直出"}
                            </span>
                            <span className="rounded-full bg-muted px-2 py-1 text-xs text-muted-foreground">
                              图片 {scene.image_status || "draft"} / 视频 {scene.video_status || "draft"}
                            </span>
                          </div>
                          <Input
                            value={draft.title}
                            onChange={(e) => updateSceneDraft(scene.id, (current) => ({ ...current, title: e.target.value }))}
                            className="text-base font-semibold"
                          />
                        </div>
                        <div className="flex shrink-0 flex-wrap items-center justify-end gap-2">
                          <Popover open={imageGenerateMenuSceneID === scene.id} onOpenChange={(open) => setImageGenerateMenuSceneID(open ? scene.id : null)}>
                            <PopoverTrigger asChild>
                              <button
                                type="button"
                                disabled={isSavingScene || isResettingScene || submittingImageSceneID === scene.id || scene.image_status === "generating" || scene.video_status === "generating"}
                                className="flex items-center gap-2 rounded-md border border-border px-4 py-2 text-sm hover:bg-accent transition-colors disabled:opacity-50"
                              >
                                {submittingImageSceneID === scene.id || scene.image_status === "generating" ? <Loader2 className="w-4 h-4 animate-spin" /> : <ImagePlus className="w-4 h-4" />}
                                生成图片
                                <ChevronDown className="w-4 h-4" />
                              </button>
                            </PopoverTrigger>
                            <PopoverContent align="end" className="w-48 p-2">
                              <button
                                type="button"
                                onClick={() => void handleGenerateImageClick(scene, false)}
                                className="flex w-full items-center gap-2 rounded-md px-3 py-2 text-left text-sm hover:bg-accent transition-colors"
                              >
                                <ImagePlus className="w-4 h-4" />
                                正常生成
                              </button>
                              <button
                                type="button"
                                onClick={() => void handleGenerateImageClick(scene, true)}
                                className="mt-1 flex w-full items-center gap-2 rounded-md px-3 py-2 text-left text-sm text-amber-800 hover:bg-amber-50 transition-colors"
                              >
                                <Sparkles className="w-4 h-4" />
                                抽卡生成
                              </button>
                            </PopoverContent>
                          </Popover>
                          <Popover open={videoGenerateMenuSceneID === scene.id} onOpenChange={(open) => setVideoGenerateMenuSceneID(open ? scene.id : null)}>
                            <PopoverTrigger asChild>
                              <button
                                type="button"
                                disabled={isSavingScene || isResettingScene || submittingVideoSceneID === scene.id || scene.image_status === "generating" || scene.video_status === "generating"}
                                className="flex items-center gap-2 rounded-md border border-border px-4 py-2 text-sm hover:bg-accent transition-colors disabled:opacity-50"
                              >
                                {submittingVideoSceneID === scene.id || scene.video_status === "generating" ? <Loader2 className="w-4 h-4 animate-spin" /> : <Clapperboard className="w-4 h-4" />}
                                生成视频
                                <ChevronDown className="w-4 h-4" />
                              </button>
                            </PopoverTrigger>
                            <PopoverContent align="end" className="w-48 p-2">
                              <button
                                type="button"
                                onClick={() => void handleGenerateVideoClick(scene, false)}
                                className="flex w-full items-center gap-2 rounded-md px-3 py-2 text-left text-sm hover:bg-accent transition-colors"
                              >
                                <Clapperboard className="w-4 h-4" />
                                正常生成
                              </button>
                              <button
                                type="button"
                                onClick={() => void handleGenerateVideoClick(scene, true)}
                                className="mt-1 flex w-full items-center gap-2 rounded-md px-3 py-2 text-left text-sm text-sky-800 hover:bg-sky-50 transition-colors"
                              >
                                <Sparkles className="w-4 h-4" />
                                抽卡生成
                              </button>
                            </PopoverContent>
                          </Popover>
                          <button
                            type="button"
                            onClick={() => void handleResetSceneState(scene)}
                            disabled={isResettingScene}
                            className="flex items-center gap-2 rounded-md border border-destructive/40 px-4 py-2 text-sm text-destructive hover:bg-destructive/10 transition-colors disabled:opacity-50"
                          >
                            {isResettingScene ? <Loader2 className="w-4 h-4 animate-spin" /> : <RefreshCw className="w-4 h-4" />}
                            重置这一行
                          </button>
                          <button
                            type="button"
                            onClick={() => void saveScene(scene.id)}
                            disabled={isSavingScene || isResettingScene}
                            className="flex items-center gap-2 rounded-md bg-primary px-4 py-2 text-sm text-primary-foreground hover:bg-primary/90 transition-colors disabled:opacity-50"
                          >
                            <Save className="w-4 h-4" />
                            保存这一行
                          </button>
                        </div>
                      </div>

                      <div className="grid grid-cols-1 xl:grid-cols-[320px_minmax(0,1fr)] gap-4">
                        <div className="space-y-4">
                          <div className="rounded-2xl border border-border/60 bg-muted/20 p-3">
                            <div className="flex items-center justify-between gap-2">
                              <div className="text-sm font-medium">场景参考图</div>
                              {scene.reference_image ? (
                                <button
                                  type="button"
                                  onClick={() => void handleClearSceneReference(scene.id)}
                                  disabled={isSavingScene || isResettingScene || isClearingSceneRef}
                                  className="text-xs text-destructive hover:underline disabled:opacity-50"
                                >
                                  清空
                                </button>
                              ) : null}
                            </div>
                            <div className="mt-3 rounded-xl border border-border/60 bg-background overflow-hidden aspect-video flex items-center justify-center">
                              {scene.reference_image ? (
                                <button
                                  type="button"
                                  className="w-full h-full"
                                  onClick={() => setPreviewImageUrl(withAssetVersion(scene.reference_image, scene.updated_at))}
                                >
                                  <img
                                    src={withAssetVersion(scene.reference_image, scene.updated_at)}
                                    alt={scene.title}
                                    className="w-full h-full object-contain"
                                  />
                                </button>
                              ) : (
                                <div className="flex flex-col items-center gap-2 text-muted-foreground">
                                  <UploadCloud className="w-8 h-8" />
                                  <span className="text-xs">还没上传这行所需图片</span>
                                </div>
                              )}
                            </div>
                            <input
                              ref={(node) => {
                                sceneReferenceInputRefs.current[scene.id] = node;
                              }}
                              type="file"
                              accept="image/*"
                              className="hidden"
                              onChange={(e) => void handleUploadSceneReference(scene.id, e.target.files?.[0] || null)}
                            />
                            <button
                              type="button"
                              onClick={() => sceneReferenceInputRefs.current[scene.id]?.click()}
                              disabled={isSavingScene || isResettingScene}
                              className="mt-3 flex w-full items-center justify-center gap-2 rounded-xl border border-dashed border-border px-3 py-3 text-sm hover:bg-accent transition-colors disabled:opacity-50"
                            >
                              <ImagePlus className="w-4 h-4" />
                              上传/替换这一行图片
                            </button>
                          </div>

                          <div className="rounded-2xl border border-border/60 bg-muted/20 p-3 space-y-3">
                            <div className="grid grid-cols-1 gap-3">
                              <div className="space-y-2">
                                <label className="text-sm font-medium">场景类型</label>
                                <select
                                  value={draft.scene_type}
                                  onChange={(e) =>
                                    updateSceneDraft(scene.id, (current) => ({
                                      ...current,
                                      scene_type: e.target.value as GeneralGuideScene["scene_type"],
                                      need_presenter:
                                        e.target.value === "material_scene" || current.image_preset === "material_only"
                                          ? false
                                          : current.need_presenter,
                                    }))
                                  }
                                  className="w-full rounded-md border border-input bg-background px-3 py-2 text-sm"
                                >
                                  <option value="presenter_scene">人物讲解场景</option>
                                  <option value="material_scene">纯素材场景</option>
                                  <option value="closing_scene">收尾场景</option>
                                </select>
                              </div>
                              <div className="space-y-2">
                                <label className="text-sm font-medium">环境类型</label>
                                <select
                                  value={draft.environment_type}
                                  onChange={(e) =>
                                    updateSceneDraft(scene.id, (current) => ({
                                      ...current,
                                      environment_type: e.target.value as GeneralGuideScene["environment_type"],
                                    }))
                                  }
                                  className="w-full rounded-md border border-input bg-background px-3 py-2 text-sm"
                                >
                                  <option value="indoor">室内</option>
                                  <option value="outdoor">室外</option>
                                </select>
                              </div>
                              <div className="space-y-2">
                                <label className="text-sm font-medium">图片合成预设</label>
                                <select
                                  value={draft.image_preset}
                                  onChange={(e) =>
                                    updateSceneDraft(scene.id, (current) => ({
                                      ...current,
                                      image_preset: e.target.value as GeneralGuideScene["image_preset"],
                                    }))
                                  }
                                  className="w-full rounded-md border border-input bg-background px-3 py-2 text-sm"
                                >
                                  <option value="presenter_front_halfbody">室外</option>
                                  <option value="presenter_room_foreground">室内</option>
                                  <option value="presenter_seated_table">桌边坐姿讲解</option>
                                  <option value="material_only">纯素材</option>
                                </select>
                              </div>
                              <div className="rounded-lg border border-border/60 bg-background px-3 py-3">
                                <div className="text-sm font-medium">{needsPresenterForDraft(draft) ? "这一行会进行场景与人物合成" : "这一行走纯素材直出"}</div>
                                <div className="mt-1 text-xs leading-5 text-muted-foreground">
                                  {needsPresenterForDraft(draft)
                                    ? "生成图片时会使用新的 FireRed 工作流，把讲解人参考图与这一行场景图进行合成。"
                                    : "这一行不会合成人物，直接把上传的场景图作为首帧素材，后续让 LTX2.3 直接驱动。"}
                                </div>
                              </div>
                            </div>
                          </div>
                        </div>

                        <div className="space-y-4 min-w-0">
                          <div className="grid grid-cols-1 xl:grid-cols-2 gap-4">
                            <div className="rounded-2xl border border-border/60 bg-muted/20 p-3">
                              <div className="flex items-center justify-between gap-2">
                                <div className="text-sm font-medium">合成图片</div>
                                {scene.image_generated_workflow ? (
                                  <span className="text-[11px] text-muted-foreground">{scene.image_generated_workflow}</span>
                                ) : null}
                              </div>
                              <div className="mt-3 rounded-xl border border-border/60 bg-background overflow-hidden h-36 flex items-center justify-center">
                                {scene.generated_image ? (
                                  <button
                                    type="button"
                                    className="w-full h-full"
                                    onClick={() => setPreviewImageUrl(withAssetVersion(scene.generated_image, scene.updated_at))}
                                  >
                                    <img
                                      src={withAssetVersion(scene.generated_image, scene.updated_at)}
                                      alt={`${scene.title} 合成图片`}
                                      className="w-full h-full object-contain"
                                    />
                                  </button>
                                ) : (
                                  <div className="flex flex-col items-center gap-2 text-muted-foreground">
                                    <ImagePlus className="w-8 h-8" />
                                    <span className="text-xs">还没有生成图片</span>
                                  </div>
                                )}
                              </div>
                              {scene.image_last_error ? <p className="mt-2 text-xs leading-5 text-destructive">{scene.image_last_error}</p> : null}
                            </div>

                            <div className="rounded-2xl border border-border/60 bg-muted/20 p-3">
                              <div className="flex items-center justify-between gap-2">
                                <div className="text-sm font-medium">生成视频</div>
                                {scene.video_generated_workflow ? (
                                  <span className="text-[11px] text-muted-foreground">{scene.video_generated_workflow}</span>
                                ) : null}
                              </div>
                              <div className="mt-3 rounded-xl border border-border/60 bg-background overflow-hidden h-36 flex items-center justify-center">
                                {scene.generated_video ? (
                                  <button
                                    type="button"
                                    className="w-full h-full"
                                    onClick={() => setPreviewVideoUrl(withAssetVersion(scene.generated_video, scene.updated_at))}
                                  >
                                    <video
                                      src={withAssetVersion(scene.generated_video, scene.updated_at)}
                                      className="w-full h-full object-contain"
                                      preload="metadata"
                                      muted
                                    />
                                  </button>
                                ) : (
                                  <div className="flex flex-col items-center gap-2 text-muted-foreground">
                                    <Video className="w-8 h-8" />
                                    <span className="text-xs">还没有生成视频</span>
                                  </div>
                                )}
                              </div>
                              {scene.video_last_error ? <p className="mt-2 text-xs leading-5 text-destructive">{scene.video_last_error}</p> : null}
                            </div>
                          </div>

                          <div className="space-y-2">
                            <div className="flex items-center gap-2 text-sm font-medium text-foreground">
                              <UploadCloud className="w-4 h-4 text-amber-600" />
                              上传要求
                            </div>
                            <div className="rounded-2xl border-2 border-amber-300 bg-gradient-to-br from-amber-50 to-orange-50 p-4 shadow-sm">
                              <div className="mb-3 flex flex-wrap items-center gap-2">
                                <span className="rounded-full bg-amber-600 px-3 py-1 text-xs font-semibold text-white">
                                  {getUploadGuideHeadline(draft)}
                                </span>
                                {getUploadGuideBadges(draft).map((badge) => (
                                  <span key={badge} className="rounded-full bg-white/90 px-2.5 py-1 text-[11px] font-medium text-amber-800 ring-1 ring-amber-200">
                                    {badge}
                                  </span>
                                ))}
                              </div>
                              <p className="text-sm leading-7 text-foreground whitespace-pre-wrap">
                                {draft.upload_requirement?.trim() || "当前还没有上传要求。"}
                              </p>
                              <p className="mt-3 text-xs leading-5 text-amber-900/80">
                                先按上面的要求准备并上传这一行对应的图片素材，再去做人物合成或视频生成会更稳。
                              </p>
                            </div>
                          </div>

                          <div className="space-y-2">
                            <div className="flex items-center gap-2 text-sm font-medium text-foreground">
                              <Megaphone className="w-4 h-4 text-primary" />
                              介绍摘要
                            </div>
                            <div className="rounded-2xl border border-border/60 bg-muted/20 p-4">
                              <p className="text-sm leading-7 text-foreground whitespace-pre-wrap">
                                {draft.intro_text?.trim() || "当前还没有介绍摘要。"}
                              </p>
                            </div>
                          </div>

                          <div className="space-y-2">
                            <label className="text-sm font-medium">视频提示词</label>
                            <Textarea
                              value={draft.video_positive_prompt}
                              onChange={(e) => updateSceneDraft(scene.id, (current) => ({ ...current, video_positive_prompt: e.target.value }))}
                              className="min-h-[180px] font-mono text-xs"
                            />
                          </div>

                          <div className="grid grid-cols-1 md:grid-cols-3 gap-4">
                            <div className="space-y-2">
                              <label className="text-sm font-medium">时长（秒）</label>
                              <Input
                                value={draft.video_duration_seconds}
                                onChange={(e) =>
                                  updateSceneDraft(scene.id, (current) => ({
                                    ...current,
                                    video_duration_seconds: e.target.value.replace(/[^\d]/g, ""),
                                  }))
                                }
                              />
                            </div>
                            <div className="space-y-2">
                              <label className="text-sm font-medium">视频宽度</label>
                              <Input
                                value={draft.video_width}
                                onChange={(e) =>
                                  updateSceneDraft(scene.id, (current) => ({
                                    ...current,
                                    video_width: e.target.value.replace(/[^\d]/g, ""),
                                  }))
                                }
                              />
                            </div>
                            <div className="space-y-2">
                              <label className="text-sm font-medium">视频高度</label>
                              <Input
                                value={draft.video_height}
                                onChange={(e) =>
                                  updateSceneDraft(scene.id, (current) => ({
                                    ...current,
                                    video_height: e.target.value.replace(/[^\d]/g, ""),
                                  }))
                                }
                              />
                            </div>
                          </div>
                        </div>
                      </div>
                    </div>
                    {transition && transitionDraft && nextScene ? (
                      <div className="rounded-xl border border-sky-300/70 bg-gradient-to-br from-sky-50 via-cyan-50 to-blue-50 ring-1 ring-sky-200/80 p-2.5 shadow-sm space-y-2.5">
                        <div className="flex items-start justify-between gap-4">
                          <div className="space-y-1 min-w-0">
                            <div className="flex flex-wrap items-center gap-2">
                              <span className="rounded-full bg-sky-600/10 px-2 py-1 text-xs font-medium text-sky-700">转场 {transition.from_sort_order} → {transition.to_sort_order}</span>
                              <span className="rounded-full bg-white/85 px-2 py-1 text-xs text-slate-600">{scene.title || `第 ${scene.sort_order} 行`}</span>
                              <GitBranch className="w-4 h-4 text-muted-foreground" />
                              <span className="rounded-full bg-white/85 px-2 py-1 text-xs text-slate-600">{nextScene.title || `第 ${nextScene.sort_order} 行`}</span>
                              <span className="rounded-full bg-white/85 px-2 py-1 text-xs text-slate-600">视频 {transition.video_status || "draft"}</span>
                              </div>
                              <p className="text-[11px] leading-5 text-slate-600">
                              {isFFmpegTransitionEngine
                                ? "FFmpeg 转场固定使用上一行最后一帧和下一行已生成图片，不支持手动抽帧。"
                                : "用上一行视频尾帧和下一行已生成图片生成转场。先抽尾帧确认稳定，再生成会更稳。"}
                              </p>
                            </div>
                          <div className="flex shrink-0 flex-wrap items-center justify-end gap-2">
                            <button
                              type="button"
                              onClick={() => void saveTransition(transition.id)}
                              disabled={isSavingTransition || isResettingTransition || transition.video_status === "generating"}
                              className="flex items-center gap-2 rounded-md border border-border px-4 py-2 text-sm hover:bg-accent transition-colors disabled:opacity-50"
                            >
                              {isSavingTransition ? <Loader2 className="w-4 h-4 animate-spin" /> : <Save className="w-4 h-4" />}
                              保存转场
                            </button>
                            <button
                              type="button"
                              onClick={() => void handleResetTransitionState(transition)}
                              disabled={isResettingTransition}
                              className="flex items-center gap-2 rounded-md border border-destructive/40 px-4 py-2 text-sm text-destructive hover:bg-destructive/10 transition-colors disabled:opacity-50"
                            >
                              {isResettingTransition ? <Loader2 className="w-4 h-4 animate-spin" /> : <RefreshCw className="w-4 h-4" />}
                              重置转场
                            </button>
                            <button
                              type="button"
                              onClick={() => void handleGenerateTransitionVideo(transition)}
                              disabled={isSavingTransition || isResettingTransition || isSubmittingTransition || transition.video_status === "generating" || !scene.generated_video || !nextScenePreview}
                              className="flex items-center gap-2 rounded-md bg-primary px-4 py-2 text-sm text-primary-foreground hover:bg-primary/90 transition-colors disabled:opacity-50"
                            >
                              {isSubmittingTransition || transition.video_status === "generating" ? <Loader2 className="w-4 h-4 animate-spin" /> : <Clapperboard className="w-4 h-4" />}
                              生成转场
                            </button>
                          </div>
                        </div>

                        <div className="grid grid-cols-1 xl:grid-cols-[170px_170px_minmax(0,1fr)] gap-2.5">
                          <div className="rounded-xl border border-sky-200/80 bg-white/75 p-2.5">
                            <div className="flex items-center justify-between gap-2">
                              <div className="text-sm font-medium">上一行尾帧</div>
                            </div>
                            <div className="mt-2 flex items-center gap-2">
                                <span className="text-[11px] text-muted-foreground whitespace-nowrap">倒数第</span>
                                <Input
                                  value={isFFmpegTransitionEngine ? "1" : transitionDraft.frames_from_end}
                                  onChange={(e) =>
                                    updateTransitionDraft(transition.id, (current) => ({
                                      ...current,
                                      frames_from_end: e.target.value.replace(/[^\d]/g, ""),
                                    }))
                                  }
                                  disabled={isFFmpegTransitionEngine || transition.video_status === "generating" || isResettingTransition}
                                  className="h-8 w-16 text-center"
                                />
                                <span className="text-[11px] text-muted-foreground whitespace-nowrap">帧</span>
                            </div>
                            {isFFmpegTransitionEngine ? (
                              <p className="mt-2 text-[11px] leading-5 text-slate-600">
                                FFmpeg 转场固定抽取上一行最后一帧。
                              </p>
                            ) : (
                              <div className="mt-2">
                                <button
                                  type="button"
                                  onClick={() => void handleExtractTailFrame(transition, scene)}
                                  disabled={isSavingTransition || isResettingTransition || isExtractingTransition}
                                  className="inline-flex items-center gap-2 rounded-md border border-border px-3 py-1.5 text-xs hover:bg-accent transition-colors disabled:opacity-50"
                                >
                                  {isExtractingTransition ? <Loader2 className="w-3.5 h-3.5 animate-spin" /> : <Scissors className="w-3.5 h-3.5" />}
                                  抽尾帧
                                </button>
                              </div>
                            )}
                            <div className="mt-2 rounded-lg border border-sky-100 bg-background overflow-hidden h-20 flex items-center justify-center">
                              {transition.tail_frame_image ? (
                                <button
                                  type="button"
                                  className="w-full h-full"
                                  onClick={() => setPreviewImageUrl(withAssetVersion(transition.tail_frame_image, transition.updated_at))}
                                >
                                  <img
                                    src={withAssetVersion(transition.tail_frame_image, transition.updated_at)}
                                    alt="尾帧预览"
                                    className="w-full h-full object-contain"
                                  />
                                </button>
                              ) : (
                                <div className="flex flex-col items-center gap-2 text-muted-foreground">
                                  <Scissors className="w-7 h-7" />
                                  <span className="text-xs">先抽尾帧</span>
                                </div>
                              )}
                            </div>
                            {!scene.generated_video ? (
                              <p className="mt-2 text-[11px] leading-5 text-muted-foreground">需要先生成上一行视频，才能抽取稳定尾帧。</p>
                            ) : null}
                          </div>

                          <div className="rounded-xl border border-sky-200/80 bg-white/75 p-2.5">
                            <div className="text-sm font-medium">下一行首帧</div>
                            <div className="mt-2 rounded-lg border border-sky-100 bg-background overflow-hidden h-20 flex items-center justify-center">
                              {nextScenePreview ? (
                                <button
                                  type="button"
                                  className="w-full h-full"
                                  onClick={() => setPreviewImageUrl(nextScenePreview)}
                                >
                                  <img
                                    src={nextScenePreview}
                                    alt="下一行首帧"
                                    className="w-full h-full object-contain"
                                  />
                                </button>
                              ) : (
                                <div className="flex flex-col items-center gap-2 text-muted-foreground">
                                  <ImagePlus className="w-7 h-7" />
                                  <span className="text-xs">缺少下一行首帧</span>
                                </div>
                              )}
                            </div>
                            {!nextScenePreview ? (
                              <p className="mt-2 text-[11px] leading-5 text-muted-foreground">请先生成下一行图片，转场不会直接使用参考图。</p>
                            ) : null}
                          </div>

                          <div className="space-y-2.5 min-w-0">
                            <div className="grid grid-cols-1 xl:grid-cols-[minmax(0,1fr)_128px] gap-2.5">
                              <div className="space-y-2">
                                <label className="text-sm font-medium">转场预设</label>
                                <select
                                  value={currentTransitionPreset?.key || (isFFmpegTransitionEngine ? transitionPresetOptions[0]?.key || "" : "__custom__")}
                                  onChange={(e) => {
                                    const selected = transitionPresetOptions.find((item) => item.key === e.target.value);
                                    if (!selected) return;
                                    updateTransitionDraft(transition.id, (current) => ({
                                      ...current,
                                      transition_prompt: selected.prompt,
                                      duration_seconds: `${selected.recommended_duration_seconds || current.duration_seconds || 2}`,
                                    }));
                                  }}
                                  disabled={transition.video_status === "generating" || isResettingTransition}
                                  className="w-full rounded-md border border-input bg-background px-3 py-2 text-sm"
                                >
                                  {transitionPresetOptions.map((item) => (
                                    <option key={item.key} value={item.key}>
                                      {item.label}
                                    </option>
                                  ))}
                                  {!isFFmpegTransitionEngine ? <option value="__custom__">自定义英文提示词</option> : null}
                                </select>
                                <p className="text-xs leading-5 text-slate-600">
                                  {currentTransitionPreset?.description || (isFFmpegTransitionEngine ? "FFmpeg 转场只需要选择中文类型，不需要英文提示词。" : "当前提示词已被手动修改，可以继续编辑英文转场提示词。")}
                                </p>
                              </div>

                              <div className="space-y-2">
                                <label className="text-sm font-medium">转场时长（秒）</label>
                                <Input
                                  value={transitionDraft.duration_seconds}
                                  onChange={(e) =>
                                    updateTransitionDraft(transition.id, (current) => ({
                                      ...current,
                                      duration_seconds: e.target.value.replace(/[^\d]/g, ""),
                                    }))
                                  }
                                  disabled={transition.video_status === "generating" || isResettingTransition}
                                />
                              </div>
                            </div>

                            <div className="grid grid-cols-1 xl:grid-cols-[minmax(0,1fr)_220px] gap-2.5">
                              <div className="space-y-2">
                                <label className="text-sm font-medium">{isFFmpegTransitionEngine ? "转场说明" : "转场提示词"}</label>
                                {isFFmpegTransitionEngine ? (
                                  <div className="rounded-xl border border-sky-200/80 bg-white/80 p-3 text-xs leading-6 text-slate-700">
                                    当前使用 FFmpeg 稳定转场，不需要英文提示词。系统会固定使用上一行最后一帧和下一行已生成图片，按你选择的中文转场类型生成过渡片段。
                                  </div>
                                ) : (
                                  <>
                                    <Textarea
                                      value={transitionDraft.transition_prompt}
                                      onChange={(e) =>
                                        updateTransitionDraft(transition.id, (current) => ({
                                          ...current,
                                          transition_prompt: e.target.value,
                                        }))
                                      }
                                      disabled={transition.video_status === "generating" || isResettingTransition}
                                      className="min-h-[92px] font-mono text-xs"
                                    />
                                    <p className="text-xs leading-5 text-muted-foreground">
                                      {transitionPresetEngine === "wan2_2" ? (
                                        <>这里保持英文单段落，重点写“从当前画面平滑过渡到下一个画面”的过程。你也可以先选中文预设，再微调英文内容。</>
                                      ) : (
                                        <>这里保持英文单段落，最后以 <span className="font-mono">, zhuanchang</span> 结尾。你也可以先选中文预设，再微调英文内容。</>
                                      )}
                                    </p>
                                  </>
                                )}
                              </div>

                              <div className="space-y-2">
                                <div className="rounded-xl border border-sky-200/80 bg-white/75 p-2.5">
                                  <div className="flex items-center justify-between gap-2">
                                    <div className="text-sm font-medium">转场视频</div>
                                    {transition.video_generated_workflow ? (
                                      <span className="text-[11px] text-muted-foreground">{transition.video_generated_workflow}</span>
                                    ) : null}
                                  </div>
                                  <div className="mt-2 rounded-lg border border-sky-100 bg-background overflow-hidden h-20 flex items-center justify-center">
                                    {transition.generated_video ? (
                                      <button
                                        type="button"
                                        className="w-full h-full"
                                        onClick={() => setPreviewVideoUrl(withAssetVersion(transition.generated_video, transition.updated_at))}
                                      >
                                        <video
                                          src={withAssetVersion(transition.generated_video, transition.updated_at)}
                                          className="w-full h-full object-contain"
                                          preload="metadata"
                                          muted
                                        />
                                      </button>
                                    ) : (
                                      <div className="flex flex-col items-center gap-2 text-muted-foreground">
                                        <Video className="w-7 h-7" />
                                        <span className="text-xs">还没有生成转场</span>
                                      </div>
                                    )}
                                  </div>
                                  {transition.video_last_error ? (
                                    <p className="mt-2 text-xs leading-5 text-destructive">{transition.video_last_error}</p>
                                  ) : null}
                                </div>
                              </div>

                              <div className="space-y-4">
                                <div className="rounded-xl border border-sky-200/80 bg-white/75 p-3">
                                  <div className="text-sm font-medium">当前参数</div>
                                  <div className="mt-3 space-y-3 text-sm">
                                    <div className="flex items-center justify-between gap-2">
                                      <span className="text-muted-foreground">转场时长</span>
                                      <span>{transitionDraft.duration_seconds || "2"} 秒</span>
                                    </div>
                                    <div className="flex items-center justify-between gap-2">
                                      <span className="text-muted-foreground">当前预设</span>
                                      <span>{currentTransitionPreset?.label || "自定义"}</span>
                                    </div>
                                  </div>
                                </div>
                              </div>
                            </div>
                          </div>
                        </div>
                      </div>
                    ) : null}
                    </div>
                  );
                })}
              </div>
            )}
          </div>
        </div>
      </div>

      <Dialog open={planningDialogOpen} onOpenChange={setPlanningDialogOpen}>
        <DialogContent className="max-w-4xl">
          <DialogHeader>
            <DialogTitle>实时 LLM 流</DialogTitle>
            <DialogDescription>
              正在根据项目总文案和标签规划综合讲解场景。完成后会自动写回下面的逐行场景列表。
            </DialogDescription>
          </DialogHeader>

          <div className="grid grid-cols-1 md:grid-cols-[220px_minmax(0,1fr)] gap-4">
            <div className="rounded-xl border border-border bg-muted/20 p-4 space-y-3 text-sm">
              <div className="space-y-1">
                <div className="text-muted-foreground">任务状态</div>
                <div className="font-medium">{planningTask?.status || (planningRunning ? "running" : "idle")}</div>
              </div>
              <div className="space-y-1">
                <div className="text-muted-foreground">进度</div>
                <div className="font-medium">{planningTask?.progress ?? 0}%</div>
              </div>
              {planningTask?.error ? (
                <div className="space-y-1">
                  <div className="text-muted-foreground">错误</div>
                  <div className="text-destructive leading-6">{planningTask.error}</div>
                </div>
              ) : null}
              {planningTask?.status === "completed" ? (
                <div className="flex items-center gap-2 text-emerald-600">
                  <CheckCircle2 className="w-4 h-4" />
                  已写回场景
                </div>
              ) : null}
            </div>

            <div
              ref={planningStreamRef}
              className="max-h-[420px] overflow-y-auto rounded-xl border border-border bg-muted/20 p-4 font-mono text-xs leading-6 whitespace-pre-wrap"
            >
              {planningTaskStream?.content?.trim() || "等待模型开始返回内容..."}
            </div>
          </div>

          <DialogFooter>
            <button
              type="button"
              onClick={() => setPlanningDialogOpen(false)}
              className="rounded-md border border-border px-4 py-2 hover:bg-accent transition-colors"
            >
              关闭
            </button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <Dialog open={!!resetDialog} onOpenChange={(open) => !open && setResetDialog(null)}>
        <DialogContent className="max-w-lg">
          <DialogHeader>
            <DialogTitle>
              {resetDialog?.kind === "transition"
                ? "确认重置转场"
                : resetDialog?.kind === "project_full"
                  ? "确认重置本项目所有"
                  : resetDialog?.kind === "project_status"
                    ? "确认重置项目状态"
                    : "确认重置这一行"}
            </DialogTitle>
            <DialogDescription>
              {resetDialog?.kind === "transition"
                ? `确定重置转场 ${resetDialog.target.from_sort_order} → ${resetDialog.target.to_sort_order} 吗？这会清空尾帧图、转场视频和转场状态。`
                : resetDialog?.kind === "project_full"
                  ? "确定重置本项目所有吗？这会删除当前项目下所有场景行、所有转场行，以及它们对应的场景参考图、合成图、视频、尾帧和转场视频，并清空规划状态；重置后可以重新点击自动生成场景规划。"
                  : resetDialog?.kind === "project_status"
                    ? "确定只重置项目状态吗？这会保留当前场景行、参考图、文案和转场提示词，但会清空已生成图片、视频、尾帧和所有任务状态，让你可以重新点击一键生成。"
                  : `确定重置“${resetDialog?.kind === "scene" ? (resetDialog.target.title || `第 ${resetDialog.target.sort_order} 行`) : ""}”吗？这会清空这一行已生成的图片、视频和状态，但会保留参考图与文案。`}
            </DialogDescription>
          </DialogHeader>
          <DialogFooter className="gap-2 sm:justify-end">
            <button
              type="button"
              onClick={() => setResetDialog(null)}
              className="rounded-md border border-border px-4 py-2 hover:bg-accent transition-colors"
            >
              取消
            </button>
            <button
              type="button"
              onClick={() => {
                if (!resetDialog) return;
                if (resetDialog.kind === "project_full") {
                  void handleResetProjectState();
                  return;
                }
                if (resetDialog.kind === "project_status") {
                  void handleResetProjectProcessingState();
                  return;
                }
                if (resetDialog.kind === "transition") {
                  void confirmResetTransitionState(resetDialog.target);
                  return;
                }
                void confirmResetSceneState(resetDialog.target);
              }}
              disabled={
                (resetDialog?.kind === "project_full" && resettingProject) ||
                (resetDialog?.kind === "project_status" && resettingProjectStatus) ||
                (resetDialog?.kind === "transition" && resettingTransitionID === resetDialog.target.id) ||
                (resetDialog?.kind === "scene" && resettingSceneID === resetDialog.target.id)
              }
              className="rounded-md bg-destructive px-4 py-2 text-destructive-foreground hover:bg-destructive/90 transition-colors disabled:opacity-50"
            >
              {((resetDialog?.kind === "project_full" && resettingProject) ||
                (resetDialog?.kind === "project_status" && resettingProjectStatus) ||
                (resetDialog?.kind === "transition" && resettingTransitionID === resetDialog.target.id) ||
                (resetDialog?.kind === "scene" && resettingSceneID === resetDialog.target.id)) ? (
                <span className="inline-flex items-center gap-2">
                  <Loader2 className="w-4 h-4 animate-spin" />
                  正在重置
                </span>
              ) : (
                "确认重置"
              )}
            </button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <Dialog open={!!previewImageUrl} onOpenChange={(open) => !open && setPreviewImageUrl("")}>
        <DialogContent className="max-w-5xl">
          <DialogHeader>
            <DialogTitle>图片预览</DialogTitle>
          </DialogHeader>
          {previewImageUrl ? (
            <div className="rounded-xl border border-border/60 bg-muted/20 p-3">
              <img src={previewImageUrl} alt="场景参考图预览" className="mx-auto max-h-[72vh] w-auto object-contain" />
            </div>
          ) : null}
        </DialogContent>
      </Dialog>

      <Dialog open={!!previewVideoUrl} onOpenChange={(open) => !open && setPreviewVideoUrl("")}>
        <DialogContent className="max-w-5xl">
          <DialogHeader>
            <DialogTitle>视频预览</DialogTitle>
          </DialogHeader>
          {previewVideoUrl ? (
            <div className="rounded-xl border border-border/60 bg-muted/20 p-3">
              <video src={previewVideoUrl} className="mx-auto max-h-[72vh] w-full object-contain" controls autoPlay preload="metadata" />
            </div>
          ) : null}
        </DialogContent>
      </Dialog>

      <Dialog open={!!imageGenerateScene} onOpenChange={(open) => !open && setImageGenerateScene(null)}>
        <DialogContent className="max-w-xl">
          <DialogHeader>
            <DialogTitle>{imageGenerateScene?.randomSeed ? "选择图片抽卡方式" : "选择图片生成方式"}</DialogTitle>
            <DialogDescription>
              {imageGenerateScene?.randomSeed
                ? "这次会使用随机 seed 重新抽卡。FireRed 图片工作流支持 Lightning LoRA 加速，你可以按当前这一行对融合感和生成速度的要求来选。"
                : "FireRed 图片工作流支持 Lightning LoRA 加速。你可以按当前这一行对融合感和生成速度的要求来选。"}
            </DialogDescription>
          </DialogHeader>

          <div className="space-y-3">
            <div className="rounded-xl border border-border/60 bg-muted/20 p-4">
              <div className="text-sm font-semibold">使用 LORA</div>
              <p className="mt-1 text-sm leading-6 text-muted-foreground">
                加速生成，但是人物和场景融合感会有 AI 感觉。
              </p>
            </div>
            <div className="rounded-xl border border-border/60 bg-muted/20 p-4">
              <div className="text-sm font-semibold">不使用 LORA</div>
              <p className="mt-1 text-sm leading-6 text-muted-foreground">
                生成图片慢，但是场景融合效果更好，AI 感觉较少。
              </p>
            </div>
          </div>

          <DialogFooter className="gap-2 sm:justify-end">
            <button
              type="button"
              onClick={() => setImageGenerateScene(null)}
              className="rounded-md border border-border px-4 py-2 hover:bg-accent transition-colors"
            >
              取消
            </button>
            <button
              type="button"
              onClick={() => imageGenerateScene && void triggerSceneImageGeneration(imageGenerateScene.scene, false, imageGenerateScene.randomSeed)}
              disabled={submittingImageSceneID === imageGenerateScene?.scene.id}
              className="rounded-md border border-border px-4 py-2 hover:bg-accent transition-colors disabled:opacity-50"
            >
              不使用 LORA
            </button>
            <button
              type="button"
              onClick={() => imageGenerateScene && void triggerSceneImageGeneration(imageGenerateScene.scene, true, imageGenerateScene.randomSeed)}
              disabled={submittingImageSceneID === imageGenerateScene?.scene.id}
              className="rounded-md bg-primary px-4 py-2 text-primary-foreground hover:bg-primary/90 transition-colors disabled:opacity-50"
            >
              使用 LORA
            </button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <Dialog open={!!projectImageBatchDialog} onOpenChange={(open) => !open && setProjectImageBatchDialog(null)}>
        <DialogContent className="max-w-xl">
          <DialogHeader>
            <DialogTitle>
              {projectImageBatchDialog?.action === "images"
                ? "选择批量图片生成方式"
                : projectImageBatchDialog?.action === "images_and_videos"
                  ? "选择批量图片和视频生成方式"
                  : "选择批量图片、视频和转场生成方式"}
            </DialogTitle>
            <DialogDescription>
              {projectImageBatchDialog?.action === "images"
                ? "FireRed 图片工作流支持 Lightning LoRA 加速。你可以按整体融合感和生成速度来选。"
                : projectImageBatchDialog?.action === "images_and_videos"
                  ? "这次会先批量生成所有图片，全部完成后再批量生成视频。FireRed 图片工作流支持 Lightning LoRA 加速，你可以按整体融合感和生成速度来选。"
                  : "这次会先批量生成所有图片，再批量生成视频，最后自动抽尾帧并批量生成转场。FireRed 图片工作流支持 Lightning LoRA 加速，你可以按整体融合感和生成速度来选。"}
            </DialogDescription>
          </DialogHeader>

          <div className="space-y-3">
            <div className="rounded-xl border border-border/60 bg-muted/20 p-4">
              <div className="text-sm font-semibold">使用 LORA</div>
              <p className="mt-1 text-sm leading-6 text-muted-foreground">
                加速生成，但是人物和场景融合感会有 AI 感觉。
              </p>
            </div>
            <div className="rounded-xl border border-border/60 bg-muted/20 p-4">
              <div className="text-sm font-semibold">不使用 LORA</div>
              <p className="mt-1 text-sm leading-6 text-muted-foreground">
                生成图片慢，但是场景融合效果更好，AI 感觉较少。
              </p>
            </div>
          </div>

          <DialogFooter className="gap-2 sm:justify-end">
            <button
              type="button"
              onClick={() => setProjectImageBatchDialog(null)}
              className="rounded-md border border-border px-4 py-2 hover:bg-accent transition-colors"
            >
              取消
            </button>
            <button
              type="button"
              onClick={() => {
                if (!projectImageBatchDialog || projectBatchRunning) return;
                void submitProjectBatchGenerate(projectImageBatchDialog.action, false);
                setProjectImageBatchDialog(null);
              }}
              disabled={projectBatchRunning}
              className="rounded-md border border-border px-4 py-2 hover:bg-accent transition-colors disabled:opacity-50"
            >
              不使用 LORA
            </button>
            <button
              type="button"
              onClick={() => {
                if (!projectImageBatchDialog || projectBatchRunning) return;
                void submitProjectBatchGenerate(projectImageBatchDialog.action, true);
                setProjectImageBatchDialog(null);
              }}
              disabled={projectBatchRunning}
              className="rounded-md bg-primary px-4 py-2 text-primary-foreground hover:bg-primary/90 transition-colors disabled:opacity-50"
            >
              使用 LORA
            </button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <Dialog
        open={!!missingSceneImageDialog}
        onOpenChange={(open) => {
          if (open) return;
          if (currentMissingScene) {
            toast.error("还有缺少场景图的行，请先补齐后再继续一键生成");
            scrollToSceneCard(currentMissingScene.id);
          }
          setMissingSceneImageDialog(null);
        }}
      >
        <DialogContent className="max-w-2xl">
          <DialogHeader>
            <DialogTitle>先补齐缺少的场景图</DialogTitle>
            <DialogDescription>
              这一轮一键生成需要先把缺少的场景图补齐。系统会按场景逐个告诉你该传什么图；全部补齐后，会继续刚才的一键生成流程。
            </DialogDescription>
          </DialogHeader>

          {currentMissingScene ? (
            <div className="space-y-4">
              <div className="flex items-center justify-between gap-3 rounded-xl border border-amber-300 bg-amber-50 px-4 py-3">
                <div className="space-y-1">
                  <div className="text-sm font-semibold text-amber-900">
                    {currentMissingScene.title || `第 ${currentMissingScene.sort_order} 行`}
                  </div>
                  <div className="text-xs text-amber-800">当前还缺 {missingSceneDialogScenes.length} 行场景图</div>
                </div>
                <span className="rounded-full bg-white/90 px-3 py-1 text-xs text-amber-700">
                  先补这一行
                </span>
              </div>

              <div className="rounded-2xl border-2 border-amber-300 bg-gradient-to-br from-amber-50 to-orange-50 p-4 shadow-sm">
                <div className="flex items-center gap-2 text-xs font-medium text-amber-700">
                  <UploadCloud className="w-4 h-4" />
                  上传要求
                </div>
                <div className="mt-2 rounded-xl bg-white/85 px-4 py-3 text-sm font-semibold text-amber-900">
                  {getUploadGuideHeadline(sceneDrafts[currentMissingScene.id] || emptyDraftFromScene(currentMissingScene))}
                </div>
                <p className="mt-3 text-sm leading-7 text-slate-800">
                  {(sceneDrafts[currentMissingScene.id]?.upload_requirement || currentMissingScene.upload_requirement || "").trim()}
                </p>
              </div>

              <div className="rounded-2xl border border-border/60 bg-muted/20 p-4">
                <div className="flex items-center justify-between gap-3">
                  <div className="text-sm font-medium">上传这一行所需图片</div>
                  <input
                    ref={batchMissingUploadInputRef}
                    type="file"
                    accept="image/*"
                    className="hidden"
                    onChange={(e) => {
                      const file = e.target.files?.[0] || null;
                      if (!currentMissingScene || !file) return;
                      void handleUploadSceneReference(currentMissingScene.id, file);
                      if (batchMissingUploadInputRef.current) {
                        batchMissingUploadInputRef.current.value = "";
                      }
                    }}
                  />
                  <button
                    type="button"
                    onClick={() => batchMissingUploadInputRef.current?.click()}
                    disabled={savingSceneID === currentMissingScene.id}
                    className="inline-flex items-center gap-2 rounded-md bg-primary px-4 py-2 text-sm text-primary-foreground hover:bg-primary/90 transition-colors disabled:opacity-50"
                  >
                    {savingSceneID === currentMissingScene.id ? <Loader2 className="w-4 h-4 animate-spin" /> : <ImagePlus className="w-4 h-4" />}
                    上传这张图
                  </button>
                </div>
                <p className="mt-3 text-xs leading-6 text-muted-foreground">
                  上传成功后会自动切到下一条缺图场景；如果你想回列表里手动处理，也可以先关闭弹窗。
                </p>
              </div>

              <div className="rounded-xl border border-border/60 bg-muted/20 p-3">
                <div className="text-xs font-medium text-muted-foreground">仍待补齐的场景</div>
                <div className="mt-2 flex flex-wrap gap-2">
                  {missingSceneDialogScenes.map((scene) => (
                    <button
                      key={scene.id}
                      type="button"
                      onClick={() => scrollToSceneCard(scene.id)}
                      className={`rounded-full px-3 py-1 text-xs transition-colors ${
                        scene.id === currentMissingScene.id ? "bg-amber-500 text-white" : "bg-white text-slate-600 hover:bg-accent"
                      }`}
                    >
                      {scene.title || `第 ${scene.sort_order} 行`}
                    </button>
                  ))}
                </div>
              </div>
            </div>
          ) : (
            <div className="rounded-xl border border-emerald-200 bg-emerald-50 px-4 py-3 text-sm text-emerald-700">
              缺少的场景图已经补齐，正在继续刚才的一键生成流程。
            </div>
          )}

          <DialogFooter className="gap-2 sm:justify-end">
            <button
              type="button"
              onClick={() => {
                if (currentMissingScene) {
                  toast.error("还有缺少场景图的行，请先补齐后再继续一键生成");
                  scrollToSceneCard(currentMissingScene.id);
                }
                setMissingSceneImageDialog(null);
              }}
              className="rounded-md border border-border px-4 py-2 hover:bg-accent transition-colors"
            >
              回到列表处理
            </button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <Dialog open={planVideoSizeDialogOpen} onOpenChange={setPlanVideoSizeDialogOpen}>
        <DialogContent className="max-w-lg">
          <DialogHeader>
            <DialogTitle>选择规划后的视频尺寸</DialogTitle>
            <DialogDescription>
              自动生成场景规划前必须先确定视频尺寸。LLM 返回后，所有新场景都会按这个尺寸直接入库；这套预设与“批量替换视频尺寸”保持一致。
            </DialogDescription>
          </DialogHeader>

          <div className="space-y-4">
            <div>
              <label className="text-sm font-medium">视频尺寸预设</label>
              <select
                value={selectedPlanVideoSizePreset}
                onChange={(e) => applyPlanVideoSizePreset(e.target.value)}
                className="mt-2 w-full rounded-md border border-border bg-background px-3 py-2 text-sm"
              >
                {videoSizePresets.map((preset) => (
                  <option key={preset.id} value={preset.id}>
                    {preset.label}
                  </option>
                ))}
                <option value="custom">自定义</option>
              </select>
            </div>

            <div className="grid grid-cols-2 gap-4">
              <div>
                <label className="text-sm font-medium">视频宽度</label>
                <Input
                  value={planVideoWidth}
                  onChange={(e) => {
                    setSelectedPlanVideoSizePreset("custom");
                    setPlanVideoWidth(e.target.value.replace(/[^\d]/g, ""));
                  }}
                  placeholder="720"
                  inputMode="numeric"
                  className="mt-2"
                />
              </div>
              <div>
                <label className="text-sm font-medium">视频高度</label>
                <Input
                  value={planVideoHeight}
                  onChange={(e) => {
                    setSelectedPlanVideoSizePreset("custom");
                    setPlanVideoHeight(e.target.value.replace(/[^\d]/g, ""));
                  }}
                  placeholder="1280"
                  inputMode="numeric"
                  className="mt-2"
                />
              </div>
            </div>
          </div>

          <DialogFooter className="gap-2 sm:justify-end">
            <button
              type="button"
              onClick={() => setPlanVideoSizeDialogOpen(false)}
              className="rounded-md border border-border px-4 py-2 hover:bg-accent transition-colors"
            >
              取消
            </button>
            <button
              type="button"
              onClick={() => void handlePlanScenes()}
              disabled={savingProject || planning}
              className="rounded-md bg-primary px-4 py-2 text-primary-foreground hover:bg-primary/90 transition-colors disabled:opacity-50"
            >
              {(savingProject || planning) ? (
                <span className="inline-flex items-center gap-2">
                  <Loader2 className="w-4 h-4 animate-spin" />
                  正在提交
                </span>
              ) : (
                "确定并开始规划"
              )}
            </button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <Dialog open={videoSizeDialogOpen} onOpenChange={setVideoSizeDialogOpen}>
        <DialogContent className="max-w-lg">
          <DialogHeader>
            <DialogTitle>批量替换视频尺寸</DialogTitle>
            <DialogDescription>
              选择一个预设，或自定义输入宽高。确认后会直接把当前项目所有场景行的视频宽高一起替换并保存。
            </DialogDescription>
          </DialogHeader>

          <div className="space-y-4">
            <div className="space-y-2">
              <label className="text-sm font-medium">尺寸预设</label>
              <select
                value={selectedVideoSizePreset}
                onChange={(e) => applyVideoSizePreset(e.target.value)}
                className="w-full rounded-md border border-input bg-background px-3 py-2 text-sm"
              >
                {videoSizePresets.map((preset) => (
                  <option key={preset.id} value={preset.id}>
                    {preset.label}
                  </option>
                ))}
                <option value="custom">自定义</option>
              </select>
            </div>

            <div className="grid grid-cols-2 gap-4">
              <div className="space-y-2">
                <label className="text-sm font-medium">视频宽度</label>
                <Input
                  value={batchVideoWidth}
                  onChange={(e) => {
                    setSelectedVideoSizePreset("custom");
                    setBatchVideoWidth(e.target.value.replace(/[^\d]/g, ""));
                  }}
                />
              </div>
              <div className="space-y-2">
                <label className="text-sm font-medium">视频高度</label>
                <Input
                  value={batchVideoHeight}
                  onChange={(e) => {
                    setSelectedVideoSizePreset("custom");
                    setBatchVideoHeight(e.target.value.replace(/[^\d]/g, ""));
                  }}
                />
              </div>
            </div>
          </div>

          <DialogFooter className="gap-2 sm:justify-end">
            <button
              type="button"
              onClick={() => setVideoSizeDialogOpen(false)}
              className="rounded-md border border-border px-4 py-2 hover:bg-accent transition-colors"
            >
              取消
            </button>
            <button
              type="button"
              onClick={() => void handleBatchReplaceVideoSize()}
              disabled={savingBatchVideoSize}
              className="rounded-md bg-primary px-4 py-2 text-primary-foreground hover:bg-primary/90 transition-colors disabled:opacity-50"
            >
              {savingBatchVideoSize ? "保存中..." : "替换并保存"}
            </button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}
