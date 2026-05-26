import { useEffect, useMemo, useState } from "react";
import { useNavigate, useParams } from "react-router-dom";
import axios from "axios";
import { ArrowLeft, Download, Images, RotateCcw, Trash2, X } from "lucide-react";
import { toast } from "sonner";

import type { MultiVisualImage, MultiVisualProject } from "@/types";
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

const normalizeMultiVisualType = (value?: string) => {
  if (value === "prop" || value === "scene") return value;
  return "character";
};

const getMultiVisualTypeLabel = (value?: string) => {
  switch (normalizeMultiVisualType(value)) {
    case "prop":
      return "道具";
    case "scene":
      return "场景";
    default:
      return "人物";
  }
};

const getMultiVisualTotalCount = (value?: string) => {
  switch (normalizeMultiVisualType(value)) {
    case "prop":
      return 17;
    case "scene":
      return 20;
    default:
      return 25;
  }
};

const withAssetVersion = (url?: string, version?: string) => {
  const trimmed = (url || "").trim();
  if (!trimmed) return "";
  const suffix = version ? encodeURIComponent(version) : `${Date.now()}`;
  return `${trimmed}${trimmed.includes("?") ? "&" : "?"}v=${suffix}`;
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

export default function MultiVisualProjectDetail() {
  const { id } = useParams<{ id: string }>();
  const navigate = useNavigate();
  const [project, setProject] = useState<MultiVisualProject | null>(null);
  const [images, setImages] = useState<MultiVisualImage[]>([]);
  const [loading, setLoading] = useState(false);
  const [actionLoading, setActionLoading] = useState(false);
  const [selectedIds, setSelectedIds] = useState<number[]>([]);
  const [previewIndex, setPreviewIndex] = useState<number | null>(null);
  const [pendingAction, setPendingAction] = useState<null | "regenerate" | "reset" | "batchDelete">(null);
  const totalCount = getMultiVisualTotalCount(project?.visual_type);

  const fetchProject = async () => {
    if (!id) return;
    const res = await axios.get(`/api/multi-visual-projects/${id}`);
    setProject(res.data);
  };

  const fetchImages = async () => {
    if (!id) return;
    const res = await axios.get(`/api/multi-visual-projects/${id}/images`);
    setImages(res.data);
  };

  const refreshAll = async () => {
    if (!id) return;
    setLoading(true);
    try {
      await Promise.all([fetchProject(), fetchImages()]);
    } catch (err) {
      console.error(err);
      toast.error("获取多视觉图详情失败");
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    refreshAll();
  }, [id]);

  useEffect(() => {
    if (!project?.current_task_id || project.status !== "generating") return;
    const timer = window.setInterval(async () => {
      try {
        const res = await axios.get(`/api/tasks/${project.current_task_id}`);
        const task = res.data;
        if (task.status === "completed" || task.status === "failed") {
          await refreshAll();
          if (task.status === "completed") {
            toast.success(`${totalCount} 张多视觉图已生成完成`);
          } else {
            toast.error(task.error || "多视觉图生成失败");
          }
          window.clearInterval(timer);
        } else {
          await fetchImages();
        }
      } catch (err) {
        console.error(err);
      }
    }, 3000);
    return () => window.clearInterval(timer);
  }, [project?.current_task_id, project?.status, totalCount]);

  const selectableImages = useMemo(
    () => images.filter((image) => image.generated_image?.trim()),
    [images],
  );

  const previewImages = selectableImages;
  const currentPreview =
    previewIndex !== null && previewIndex >= 0 && previewIndex < previewImages.length
      ? previewImages[previewIndex]
      : null;

  useEffect(() => {
    if (previewIndex === null) return;
    const onKeyDown = (event: KeyboardEvent) => {
      if (!previewImages.length) return;
      if (event.key === "ArrowRight") {
        event.preventDefault();
        setPreviewIndex((prev) => {
          if (prev === null) return 0;
          return (prev + 1) % previewImages.length;
        });
      } else if (event.key === "ArrowLeft") {
        event.preventDefault();
        setPreviewIndex((prev) => {
          if (prev === null) return 0;
          return (prev - 1 + previewImages.length) % previewImages.length;
        });
      } else if (event.key === "Escape") {
        setPreviewIndex(null);
      }
    };
    window.addEventListener("keydown", onKeyDown);
    return () => window.removeEventListener("keydown", onKeyDown);
  }, [previewIndex, previewImages.length]);

  const runRegenerate = async () => {
    if (!id || !project) return;
    setActionLoading(true);
    try {
      const res = await axios.post(`/api/multi-visual-projects/${id}/regenerate`);
      toast.success(`已提交 ${totalCount} 张多视觉图生成任务`);
      setProject((prev) =>
        prev
          ? {
              ...prev,
              status: "generating",
              current_task_id: res.data.task_id,
              last_error: "",
            }
          : prev,
      );
      setSelectedIds([]);
      await fetchImages();
    } catch (err: any) {
      console.error(err);
      toast.error(err.response?.data?.error || "提交生成失败");
    } finally {
      setActionLoading(false);
    }
  };

  const runResetState = async () => {
    if (!id) return;
    setActionLoading(true);
    try {
      await axios.post(`/api/multi-visual-projects/${id}/reset-state`);
      toast.success("状态已重置，现在可以重新生成");
      setSelectedIds([]);
      await refreshAll();
    } catch (err: any) {
      console.error(err);
      toast.error(err.response?.data?.error || "重置状态失败");
    } finally {
      setActionLoading(false);
    }
  };

  const runBatchDelete = async () => {
    if (!id || selectedIds.length === 0) {
      toast.error("请先选择要删除的图片");
      return;
    }
    setActionLoading(true);
    try {
      await axios.post(`/api/multi-visual-projects/${id}/images/delete-batch`, {
        image_ids: selectedIds,
      });
      toast.success("已删除选中图片");
      setSelectedIds([]);
      await fetchImages();
    } catch (err: any) {
      console.error(err);
      toast.error(err.response?.data?.error || "删除失败");
    } finally {
      setActionLoading(false);
    }
  };

  const confirmPendingAction = async () => {
    const action = pendingAction;
    setPendingAction(null);
    if (action === "regenerate") {
      await runRegenerate();
    } else if (action === "reset") {
      await runResetState();
    } else if (action === "batchDelete") {
      await runBatchDelete();
    }
  };

  const handleExport = async () => {
    if (!id) return;
    setActionLoading(true);
    try {
      const res = await axios.post(
        `/api/multi-visual-projects/${id}/export`,
        {
          image_ids: selectedIds,
        },
        {
          responseType: "blob",
        },
      );
      const filename = extractDownloadFilename(
        res.headers["content-disposition"],
        `${project?.code || "multi_visual"}_dataset.zip`,
      );
      triggerBlobDownload(res.data, filename);
      toast.success("训练集 ZIP 已导出");
    } catch (err: any) {
      console.error(err);
      toast.error(err.response?.data?.error || "导出失败");
    } finally {
      setActionLoading(false);
    }
  };

  const toggleSelection = (imageId: number) => {
    setSelectedIds((prev) =>
      prev.includes(imageId) ? prev.filter((idItem) => idItem !== imageId) : [...prev, imageId],
    );
  };

  const toggleSelectAll = () => {
    if (selectedIds.length === selectableImages.length) {
      setSelectedIds([]);
      return;
    }
    setSelectedIds(selectableImages.map((image) => image.id));
  };

  const generatedCount = selectableImages.length;

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between gap-4">
        <div className="flex items-center gap-3">
          <button
            onClick={() => navigate("/multi-visuals")}
            className="p-2 rounded-md hover:bg-accent transition-colors"
          >
            <ArrowLeft className="w-5 h-5" />
          </button>
          <div>
              <h1 className="text-3xl font-bold">{project?.name || "多视觉图项目"}</h1>
              <p className="mt-1 text-sm text-muted-foreground">
              {project?.description || ""} {project?.code ? `· 文件夹：${project.code}` : ""}{project?.visual_type ? ` · 类型：${getMultiVisualTypeLabel(project.visual_type)}` : ""}
              </p>
          </div>
        </div>
        <div className="flex gap-2">
          <button
            onClick={refreshAll}
            disabled={loading}
            className="flex items-center gap-2 bg-secondary text-secondary-foreground px-4 py-2 rounded-md hover:bg-secondary/80 transition-colors disabled:opacity-50"
          >
            <RotateCcw className={`w-4 h-4 ${loading ? "animate-spin" : ""}`} />
            刷新
          </button>
          <button
            onClick={() => setPendingAction("reset")}
            disabled={actionLoading}
            className="flex items-center gap-2 bg-secondary text-secondary-foreground px-4 py-2 rounded-md hover:bg-secondary/80 transition-colors disabled:opacity-50"
          >
            <RotateCcw className="w-4 h-4" />
            重置所有状态
          </button>
          <button
            onClick={() => setPendingAction("regenerate")}
            disabled={actionLoading || project?.status === "generating"}
            className="flex items-center gap-2 bg-primary text-primary-foreground px-4 py-2 rounded-md hover:bg-primary/90 transition-colors disabled:opacity-50"
          >
            <Images className="w-4 h-4" />
            一键重新生成 {totalCount} 张
          </button>
        </div>
      </div>

      <div className="grid grid-cols-1 lg:grid-cols-[320px,1fr] gap-6">
        <div className="bg-card border border-border rounded-lg p-4 space-y-4 h-fit">
          <div>
            <div className="text-sm text-muted-foreground mb-2">参考图</div>
            <div className="aspect-[4/5] max-h-[320px] rounded-lg overflow-hidden bg-muted/50 flex items-center justify-center">
              {project?.reference_image ? (
                <img
                  src={withAssetVersion(project.reference_image, project.updated_at)}
                  alt={project.name}
                  className="w-full h-full object-contain"
                />
              ) : (
                <Images className="w-8 h-8 text-muted-foreground" />
              )}
            </div>
          </div>
          <div className="space-y-2 text-sm">
            <div className="flex justify-between gap-3">
              <span className="text-muted-foreground">状态</span>
              <span className="font-medium">{project?.status || "draft"}</span>
            </div>
            <div className="flex justify-between gap-3">
              <span className="text-muted-foreground">已生成</span>
              <span className="font-medium">{generatedCount} / {totalCount}</span>
            </div>
            {project?.last_error ? (
              <div className="rounded-md bg-destructive/10 text-destructive px-3 py-2 text-xs whitespace-pre-wrap">
                {project.last_error}
              </div>
            ) : null}
          </div>
        </div>

        <div className="space-y-4">
          <div className="flex flex-wrap items-center justify-between gap-3">
            <div className="flex items-center gap-3">
              <label className="flex items-center gap-2 text-sm">
                <input
                  type="checkbox"
                  checked={selectableImages.length > 0 && selectedIds.length === selectableImages.length}
                  onChange={toggleSelectAll}
                  disabled={project?.status === "generating" || selectableImages.length === 0}
                />
                全选已生成图片
              </label>
              <span className="text-sm text-muted-foreground">已选 {selectedIds.length} 张</span>
            </div>
            <div className="flex gap-2">
              <button
                onClick={() => setPendingAction("batchDelete")}
                disabled={actionLoading || project?.status === "generating" || selectedIds.length === 0}
                className="flex items-center gap-2 bg-destructive text-destructive-foreground px-4 py-2 rounded-md hover:bg-destructive/90 transition-colors disabled:opacity-50"
              >
                <Trash2 className="w-4 h-4" />
                批量删除
              </button>
              <button
                onClick={handleExport}
                disabled={actionLoading || generatedCount === 0}
                className="flex items-center gap-2 bg-secondary text-secondary-foreground px-4 py-2 rounded-md hover:bg-secondary/80 transition-colors disabled:opacity-50"
              >
                <Download className="w-4 h-4" />
                批量导出 ZIP
              </button>
            </div>
          </div>

          <div className="grid grid-cols-3 md:grid-cols-4 xl:grid-cols-6 2xl:grid-cols-8 gap-3">
            {images.map((image) => (
              <div key={image.id} className="bg-card border border-border rounded-lg overflow-hidden">
                <button
                  type="button"
                  onClick={() => {
                    const index = previewImages.findIndex((item) => item.id === image.id);
                    if (index >= 0) setPreviewIndex(index);
                  }}
                  disabled={!image.generated_image}
                  className="w-full aspect-square bg-muted/40 overflow-hidden relative disabled:cursor-default"
                >
                  {image.generated_image ? (
                    <img
                      src={withAssetVersion(image.generated_image, image.updated_at)}
                      alt={image.label}
                      className="w-full h-full object-contain"
                    />
                  ) : (
                    <div className="w-full h-full flex flex-col items-center justify-center text-muted-foreground gap-2">
                      <Images className="w-8 h-8" />
                      <span className="text-xs">{image.status}</span>
                    </div>
                  )}
                  {image.generated_image ? (
                    <button
                      type="button"
                      onClick={(event) => {
                        event.preventDefault();
                        event.stopPropagation();
                        toggleSelection(image.id);
                      }}
                      className="absolute top-2 left-2 min-w-[68px] h-8 px-2.5 bg-black/70 text-white rounded-md text-xs font-medium flex items-center justify-center gap-2 hover:bg-black/80 transition-colors"
                    >
                      <input
                        type="checkbox"
                        checked={selectedIds.includes(image.id)}
                        readOnly
                        className="w-4 h-4 pointer-events-none"
                      />
                      选择
                    </button>
                  ) : null}
                </button>
                <div className="p-2 space-y-2">
                  <div>
                    <div className="text-[10px] text-muted-foreground mb-1">标签</div>
                    <p className="text-xs break-words leading-snug line-clamp-2">{image.label}</p>
                  </div>
                  <div className="flex items-center justify-between gap-2 text-[10px] text-muted-foreground">
                    <span>{image.shot_size_label}</span>
                    <span>{image.view_label}</span>
                  </div>
                </div>
              </div>
            ))}
          </div>
        </div>
      </div>

      {currentPreview && (
        <div className="fixed inset-0 z-50 bg-black/80 backdrop-blur-sm p-4 flex items-center justify-center">
          <div className="bg-card border border-border rounded-xl w-full max-w-6xl max-h-[92vh] overflow-hidden flex flex-col">
            <div className="flex items-center justify-between px-5 py-4 border-b border-border">
              <div>
                <h2 className="text-lg font-semibold">{currentPreview.label}</h2>
                <p className="text-sm text-muted-foreground">
                  {previewIndex! + 1} / {previewImages.length}
                </p>
              </div>
              <button
                onClick={() => setPreviewIndex(null)}
                className="p-2 rounded-md hover:bg-accent transition-colors"
              >
                <X className="w-5 h-5" />
              </button>
            </div>
            <div className="flex-1 overflow-hidden grid grid-cols-1 lg:grid-cols-[1fr,320px]">
              <div className="bg-black flex items-center justify-center p-6 overflow-hidden">
                <img
                  src={withAssetVersion(currentPreview.generated_image, currentPreview.updated_at)}
                  alt={currentPreview.label}
                  className="max-w-full max-h-[70vh] object-contain"
                />
              </div>
              <div className="p-5 space-y-4 border-l border-border">
                <div>
                  <div className="text-sm text-muted-foreground mb-1">标签</div>
                  <div className="text-sm leading-relaxed break-words">{currentPreview.label}</div>
                </div>
                <div className="grid grid-cols-2 gap-3 text-sm">
                  <div className="bg-muted/50 rounded-md p-3">
                    <div className="text-xs text-muted-foreground mb-1">景别</div>
                    <div>{currentPreview.shot_size_label}</div>
                  </div>
                  <div className="bg-muted/50 rounded-md p-3">
                    <div className="text-xs text-muted-foreground mb-1">角度</div>
                    <div>{currentPreview.view_label}</div>
                  </div>
                  <div className="bg-muted/50 rounded-md p-3">
                    <div className="text-xs text-muted-foreground mb-1">水平角</div>
                    <div>{currentPreview.horizontal_angle}°</div>
                  </div>
                  <div className="bg-muted/50 rounded-md p-3">
                    <div className="text-xs text-muted-foreground mb-1">垂直角</div>
                    <div>{currentPreview.vertical_angle}°</div>
                  </div>
                </div>
                <div className="flex gap-2 pt-2">
                  <button
                    onClick={() =>
                      setPreviewIndex((prev) =>
                        prev === null ? 0 : (prev - 1 + previewImages.length) % previewImages.length,
                      )
                    }
                    className="flex-1 px-4 py-2 rounded-md bg-secondary text-secondary-foreground hover:bg-secondary/80 transition-colors"
                  >
                    ← 上一张
                  </button>
                  <button
                    onClick={() =>
                      setPreviewIndex((prev) =>
                        prev === null ? 0 : (prev + 1) % previewImages.length,
                      )
                    }
                    className="flex-1 px-4 py-2 rounded-md bg-secondary text-secondary-foreground hover:bg-secondary/80 transition-colors"
                  >
                    下一张 →
                  </button>
                </div>
              </div>
            </div>
          </div>
        </div>
      )}

      <AlertDialog open={pendingAction !== null} onOpenChange={(open) => !open && setPendingAction(null)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>
              {pendingAction === "regenerate" ? "重新生成多视觉图" : pendingAction === "reset" ? "重置所有状态" : "批量删除图片"}
            </AlertDialogTitle>
            <AlertDialogDescription>
              {pendingAction === "regenerate"
                ? `确定重新生成 ${totalCount} 张多视觉图吗？这会清空当前项目下已生成的图片与导出资源。`
                : pendingAction === "reset"
                  ? "确定重置当前项目的全部状态吗？这会清空任务锁定状态，并物理删除当前项目下已生成的图片与导出资源。"
                  : `确定删除选中的 ${selectedIds.length} 张图片吗？`}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>取消</AlertDialogCancel>
            <AlertDialogAction onClick={confirmPendingAction}>
              确认
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}
