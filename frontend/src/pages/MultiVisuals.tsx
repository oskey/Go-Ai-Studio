import { useEffect, useMemo, useRef, useState } from "react";
import { useNavigate } from "react-router-dom";
import axios from "axios";
import { FolderOpen, Images, Pencil, Plus, RotateCcw, Trash2, X, Save, UploadCloud, ImagePlus } from "lucide-react";
import { toast } from "sonner";

import type { MultiVisualProject } from "@/types";
import { Input } from "@/components/ui/input";
import { Textarea } from "@/components/ui/textarea";
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

const MULTI_VISUAL_TYPE_OPTIONS = [
  { value: "character", label: "人物", count: 25 },
  { value: "prop", label: "道具", count: 17 },
  { value: "scene", label: "场景", count: 20 },
] as const;

const normalizeMultiVisualType = (value?: string) => {
  if (value === "prop" || value === "scene") return value;
  return "character";
};

const getMultiVisualTypeLabel = (value?: string) =>
  MULTI_VISUAL_TYPE_OPTIONS.find((option) => option.value === normalizeMultiVisualType(value))?.label || "人物";

const getMultiVisualTotalCount = (value?: string) =>
  MULTI_VISUAL_TYPE_OPTIONS.find((option) => option.value === normalizeMultiVisualType(value))?.count || 25;

const withAssetVersion = (url?: string, version?: string) => {
  const trimmed = (url || "").trim();
  if (!trimmed) return "";
  const suffix = version ? encodeURIComponent(version) : `${Date.now()}`;
  return `${trimmed}${trimmed.includes("?") ? "&" : "?"}v=${suffix}`;
};

export default function MultiVisuals() {
  const navigate = useNavigate();
  const [projects, setProjects] = useState<MultiVisualProject[]>([]);
  const [loading, setLoading] = useState(false);
  const [isCreating, setIsCreating] = useState(false);
  const [editingProject, setEditingProject] = useState<MultiVisualProject | null>(null);
  const [pendingDeleteProject, setPendingDeleteProject] = useState<MultiVisualProject | null>(null);
  const [name, setName] = useState("");
  const [code, setCode] = useState("");
  const [visualType, setVisualType] = useState<"character" | "prop" | "scene">("character");
  const [description, setDescription] = useState("");
  const [referenceImage, setReferenceImage] = useState<File | null>(null);
  const [saving, setSaving] = useState(false);
  const fileInputRef = useRef<HTMLInputElement | null>(null);

  const selectedReferencePreview = useMemo(() => {
    if (!referenceImage) return "";
    return URL.createObjectURL(referenceImage);
  }, [referenceImage]);

  useEffect(() => {
    return () => {
      if (selectedReferencePreview) {
        URL.revokeObjectURL(selectedReferencePreview);
      }
    };
  }, [selectedReferencePreview]);

  const fetchProjects = () => {
    setLoading(true);
    axios
      .get("/api/multi-visual-projects")
      .then((res) => setProjects(res.data))
      .catch((err) => {
        console.error(err);
        toast.error("获取多视觉图项目失败");
      })
      .finally(() => setLoading(false));
  };

  useEffect(() => {
    fetchProjects();
  }, []);

  const resetCreateState = () => {
    setName("");
    setCode("");
    setVisualType("character");
    setDescription("");
    setReferenceImage(null);
    if (fileInputRef.current) {
      fileInputRef.current.value = "";
    }
    setIsCreating(false);
    setEditingProject(null);
  };

  const openCreateModal = () => {
    resetCreateState();
    setIsCreating(true);
  };

  const openEditModal = (project: MultiVisualProject) => {
    setEditingProject(project);
    setName(project.name);
    setCode(project.code);
    setVisualType(normalizeMultiVisualType(project.visual_type));
    setDescription(project.description);
    setReferenceImage(null);
    setIsCreating(true);
  };

  const handleSubmit = async () => {
    if (!name.trim() || !code.trim() || !description.trim()) {
      toast.error("请填写名称、文件夹和描述");
      return;
    }
    if (!editingProject && !referenceImage) {
      toast.error("请上传参考图");
      return;
    }
    setSaving(true);
    try {
      const formData = new FormData();
      formData.append("name", name.trim());
      formData.append("code", code.trim());
      formData.append("visual_type", visualType);
      formData.append("description", description.trim());
      if (referenceImage) {
        formData.append("reference_image", referenceImage);
      }
      if (editingProject) {
        const res = await axios.put(`/api/multi-visual-projects/${editingProject.id}`, formData, {
          headers: { "Content-Type": "multipart/form-data" },
        });
        toast.success("多视觉图项目已更新");
        resetCreateState();
        fetchProjects();
        navigate(`/multi-visuals/${res.data.id}`);
      } else {
        const res = await axios.post("/api/multi-visual-projects", formData, {
          headers: { "Content-Type": "multipart/form-data" },
        });
        toast.success("多视觉图项目已创建");
        resetCreateState();
        fetchProjects();
        navigate(`/multi-visuals/${res.data.id}`);
      }
    } catch (err: any) {
      console.error(err);
      toast.error(err.response?.data?.error || (editingProject ? "更新失败" : "创建失败"));
    } finally {
      setSaving(false);
    }
  };

  const handleDelete = async () => {
    if (!pendingDeleteProject) return;
    try {
      await axios.delete(`/api/multi-visual-projects/${pendingDeleteProject.id}`);
      toast.success("项目已删除");
      setPendingDeleteProject(null);
      fetchProjects();
    } catch (err: any) {
      console.error(err);
      toast.error(err.response?.data?.error || "删除失败");
    }
  };

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-3xl font-bold">多视觉图</h1>
          <p className="mt-1 text-sm text-muted-foreground">
            上传一张参考图，按类型固定生成多视觉图片：人物 25 张、道具 17 张、场景 20 张，用于 LoRA 训练素材整理。
          </p>
        </div>
        <div className="flex gap-2">
          <button
            onClick={fetchProjects}
            disabled={loading}
            className="flex items-center gap-2 bg-secondary text-secondary-foreground px-4 py-2 rounded-md hover:bg-secondary/80 transition-colors disabled:opacity-50"
          >
            <RotateCcw className={`w-4 h-4 ${loading ? "animate-spin" : ""}`} />
            刷新
          </button>
          <button
            onClick={openCreateModal}
            className="flex items-center gap-2 bg-primary text-primary-foreground px-4 py-2 rounded-md hover:bg-primary/90 transition-colors"
          >
            <Plus className="w-4 h-4" />
            新增项目
          </button>
        </div>
      </div>

      <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4 2xl:grid-cols-5 gap-3">
        {projects.map((project) => (
          <div
            key={project.id}
            className="bg-card border border-border rounded-lg shadow-sm hover:shadow-md transition-shadow overflow-hidden"
          >
            <button
              onClick={() => navigate(`/multi-visuals/${project.id}`)}
              className="w-full text-left p-3 space-y-2"
            >
              <div className="flex items-start justify-between gap-3">
                <div className="min-w-0">
                  <h3 className="text-sm font-semibold truncate">{project.name}</h3>
                  <p className="text-[11px] text-muted-foreground mt-0.5 truncate">
                    文件夹：{project.code} · 类型：{getMultiVisualTypeLabel(project.visual_type)}
                  </p>
                </div>
                <span className="shrink-0 rounded-full px-2 py-0.5 text-[10px] bg-accent text-accent-foreground">
                  {project.status || "draft"}
                </span>
              </div>
              <div className="aspect-[16/10] rounded-md overflow-hidden bg-muted/50 flex items-center justify-center">
                {project.reference_image ? (
                  <img
                    src={withAssetVersion(project.reference_image, project.updated_at)}
                    alt={project.name}
                    className="w-full h-full object-contain"
                  />
                ) : (
                  <Images className="w-8 h-8 text-muted-foreground" />
                )}
              </div>
              <p className="text-[11px] text-muted-foreground line-clamp-2">{project.description}</p>
            </button>
            <div className="px-3 pb-3 flex items-center justify-between gap-2">
              <div className="flex gap-2">
                <button
                  onClick={() => navigate(`/multi-visuals/${project.id}`)}
                  className="flex items-center gap-1.5 text-[11px] bg-primary text-primary-foreground px-2.5 py-1.5 rounded-md hover:bg-primary/90 transition-colors"
                >
                  <FolderOpen className="w-3.5 h-3.5" />
                  进入管理
                </button>
                <button
                  onClick={() => openEditModal(project)}
                  className="flex items-center gap-1.5 text-[11px] bg-secondary text-secondary-foreground px-2.5 py-1.5 rounded-md hover:bg-secondary/80 transition-colors"
                >
                  <Pencil className="w-3.5 h-3.5" />
                  编辑
                </button>
              </div>
              <button
                onClick={() => setPendingDeleteProject(project)}
                className="flex items-center gap-1.5 text-[11px] bg-destructive text-destructive-foreground px-2.5 py-1.5 rounded-md hover:bg-destructive/90 transition-colors"
              >
                <Trash2 className="w-3.5 h-3.5" />
                删除
              </button>
            </div>
          </div>
        ))}
      </div>

      {isCreating && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50 backdrop-blur-sm p-4">
          <div className="bg-card border border-border rounded-lg p-6 w-full max-w-2xl shadow-lg relative">
            <button
              onClick={resetCreateState}
              className="absolute top-4 right-4 text-muted-foreground hover:text-foreground"
            >
              <X className="w-5 h-5" />
            </button>

            <h2 className="text-xl font-bold mb-6 flex items-center gap-2">
              {editingProject ? <Pencil className="w-5 h-5" /> : <Plus className="w-5 h-5" />}
              {editingProject ? "编辑多视觉图项目" : "新增多视觉图项目"}
            </h2>

            <div className="space-y-4">
              <div>
                <label className="block text-sm font-medium mb-1">名称</label>
                <Input value={name} onChange={(e) => setName(e.target.value)} placeholder="例如：黄旭多角度训练集" />
              </div>
              <div>
                <label className="block text-sm font-medium mb-1">文件夹</label>
                <Input value={code} onChange={(e) => setCode(e.target.value)} placeholder="例如：a_huangxu" />
                <p className="text-xs text-muted-foreground mt-1">只允许英文、数字、下划线或连字符，且不能与现有文件夹冲突。</p>
              </div>
              <div>
                <label className="block text-sm font-medium mb-1">类型</label>
                <select
                  value={visualType}
                  onChange={(e) => setVisualType(normalizeMultiVisualType(e.target.value) as "character" | "prop" | "scene")}
                  className="flex h-10 w-full rounded-md border border-input bg-background px-3 py-2 text-sm"
                >
                  {MULTI_VISUAL_TYPE_OPTIONS.map((option) => (
                    <option key={option.value} value={option.value}>
                      {option.label}（固定 {option.count} 张）
                    </option>
                  ))}
                </select>
                <p className="text-xs text-muted-foreground mt-1">
                  当前将固定生成 {getMultiVisualTotalCount(visualType)} 张{getMultiVisualTypeLabel(visualType)}多视觉图片。
                </p>
              </div>
              <div>
                <label className="block text-sm font-medium mb-1">描述</label>
                <Textarea
                  value={description}
                  onChange={(e) => setDescription(e.target.value)}
                  className="h-28"
                  placeholder="例如：a_huangxu"
                />
                <p className="text-xs text-muted-foreground mt-1">后续导出的训练标签会优先使用这里的内容作为前缀。</p>
              </div>
              <div>
                <label className="block text-sm font-medium mb-1">{editingProject ? "替换参考图（可选）" : "参考图上传"}</label>
                <input
                  ref={fileInputRef}
                  type="file"
                  accept="image/png,image/jpeg,image/webp"
                  onChange={(e) => setReferenceImage(e.target.files?.[0] || null)}
                  className="hidden"
                />
                <button
                  type="button"
                  onClick={() => fileInputRef.current?.click()}
                  className="w-full rounded-xl border border-dashed border-border bg-muted/30 hover:bg-muted/50 transition-colors p-4 text-left"
                >
                  <div className="flex items-start gap-4">
                    <div className="w-28 h-28 shrink-0 rounded-lg overflow-hidden bg-background border border-border flex items-center justify-center">
                      {selectedReferencePreview ? (
                        <img
                          src={selectedReferencePreview}
                          alt="selected reference"
                          className="w-full h-full object-contain"
                        />
                      ) : editingProject?.reference_image ? (
                        <img
                          src={withAssetVersion(editingProject.reference_image, editingProject.updated_at)}
                          alt={editingProject.name}
                          className="w-full h-full object-contain"
                        />
                      ) : (
                        <ImagePlus className="w-8 h-8 text-muted-foreground" />
                      )}
                    </div>
                    <div className="min-w-0 flex-1 space-y-2">
                      <div className="flex items-center gap-2 text-sm font-medium">
                        <UploadCloud className="w-4 h-4" />
                        {referenceImage ? "已选择新参考图" : editingProject ? "点击替换参考图" : "点击上传参考图"}
                      </div>
                      <p className="text-xs text-muted-foreground leading-5">
                        支持 PNG、JPG、WEBP。{editingProject ? "不重新选择则保留当前参考图。" : "建议上传主体清晰、背景简单的参考图。"}
                      </p>
                      <div className="text-xs text-muted-foreground break-all">
                        {referenceImage
                          ? referenceImage.name
                          : editingProject?.reference_image
                            ? "当前已存在参考图"
                            : "尚未选择文件"}
                      </div>
                    </div>
                  </div>
                </button>
              </div>
            </div>

            <div className="mt-8 flex justify-end gap-3">
              <button
                onClick={resetCreateState}
                className="px-4 py-2 rounded-md hover:bg-accent transition-colors"
              >
                取消
              </button>
              <button
                onClick={handleSubmit}
                disabled={saving}
                className="flex items-center gap-2 bg-primary text-primary-foreground px-4 py-2 rounded-md hover:bg-primary/90 transition-colors disabled:opacity-50"
              >
                <Save className="w-4 h-4" />
                {saving ? (editingProject ? "保存中..." : "创建中...") : editingProject ? "保存修改" : "保存项目"}
              </button>
            </div>
          </div>
        </div>
      )}

      <AlertDialog open={!!pendingDeleteProject} onOpenChange={(open) => !open && setPendingDeleteProject(null)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>删除多视觉图项目</AlertDialogTitle>
            <AlertDialogDescription>
              {pendingDeleteProject
                ? `确定删除多视觉图项目「${pendingDeleteProject.name}」吗？这会删除内部全部图片和资源。`
                : ""}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>取消</AlertDialogCancel>
            <AlertDialogAction
              onClick={handleDelete}
              className="bg-destructive text-destructive-foreground hover:bg-destructive/90"
            >
              确认删除
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}
