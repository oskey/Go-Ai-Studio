import { useEffect, useMemo, useRef, useState } from "react";
import { useNavigate } from "react-router-dom";
import axios from "axios";
import { FolderOpen, ImagePlus, Pencil, Plus, Store, Trash2, UploadCloud } from "lucide-react";
import { toast } from "sonner";

import type { StoreVisitProject } from "@/types";
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
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";

const withAssetVersion = (url?: string, version?: string) => {
  const trimmed = (url || "").trim();
  if (!trimmed) return "";
  const suffix = version ? encodeURIComponent(version) : `${Date.now()}`;
  return `${trimmed}${trimmed.includes("?") ? "&" : "?"}v=${suffix}`;
};

export default function StoreVisits() {
  const navigate = useNavigate();
  const [projects, setProjects] = useState<StoreVisitProject[]>([]);
  const [loading, setLoading] = useState(false);
  const [createOpen, setCreateOpen] = useState(false);
  const [editingProject, setEditingProject] = useState<StoreVisitProject | null>(null);
  const [deleteTarget, setDeleteTarget] = useState<StoreVisitProject | null>(null);
  const [name, setName] = useState("");
  const [code, setCode] = useState("");
  const [description, setDescription] = useState("");
  const [referenceImages, setReferenceImages] = useState<File[]>([]);
  const [saving, setSaving] = useState(false);
  const fileInputRef = useRef<HTMLInputElement | null>(null);

  const previewItems = useMemo(
    () =>
      referenceImages.map((file) => ({
        name: file.name,
        url: URL.createObjectURL(file),
      })),
    [referenceImages],
  );

  useEffect(() => {
    return () => {
      previewItems.forEach((item) => URL.revokeObjectURL(item.url));
    };
  }, [previewItems]);

  const resetForm = () => {
    setName("");
    setCode("");
    setDescription("");
    setReferenceImages([]);
    if (fileInputRef.current) {
      fileInputRef.current.value = "";
    }
  };

  const openCreateDialog = () => {
    resetForm();
    setEditingProject(null);
    setCreateOpen(true);
  };

  const openEditDialog = (project: StoreVisitProject) => {
    setName(project.name || "");
    setCode(project.code || "");
    setDescription(project.description || "");
    setReferenceImages([]);
    if (fileInputRef.current) {
      fileInputRef.current.value = "";
    }
    setEditingProject(project);
    setCreateOpen(true);
  };

  const fetchProjects = async () => {
    setLoading(true);
    try {
      const res = await axios.get("/api/store-visits");
      setProjects(res.data);
    } catch (err) {
      console.error(err);
      toast.error("获取博主探店项目失败");
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    fetchProjects();
  }, []);

  const handleCreateOrUpdate = async () => {
    if (!name.trim() || !code.trim() || !description.trim()) {
      toast.error("请填写项目名称、项目文件名和备注");
      return;
    }
    if (!editingProject && referenceImages.length === 0) {
      toast.error("请至少上传一张博主参考图");
      return;
    }

    setSaving(true);
    try {
      const formData = new FormData();
      formData.append("name", name.trim());
      formData.append("code", code.trim());
      formData.append("description", description.trim());
      referenceImages.forEach((file) => {
        formData.append("blogger_reference_images", file);
      });
      const method = editingProject ? "put" : "post";
      const url = editingProject ? `/api/store-visits/${editingProject.id}` : "/api/store-visits";
      const res = await axios[method](url, formData, {
        headers: { "Content-Type": "multipart/form-data" },
      });
      toast.success(editingProject ? "博主探店项目已更新" : "博主探店项目已创建");
      setCreateOpen(false);
      resetForm();
      setEditingProject(null);
      await fetchProjects();
      if (!editingProject) {
        navigate(`/store-visits/${res.data.id}`);
      }
    } catch (err: any) {
      console.error(err);
      toast.error(err.response?.data?.error || (editingProject ? "更新失败" : "创建失败"));
    } finally {
      setSaving(false);
    }
  };

  const handleDelete = async () => {
    if (!deleteTarget) return;
    try {
      await axios.delete(`/api/store-visits/${deleteTarget.id}`);
      toast.success("项目已删除");
      setDeleteTarget(null);
      await fetchProjects();
    } catch (err: any) {
      console.error(err);
      toast.error(err.response?.data?.error || "删除失败");
    }
  };

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-3xl font-bold">博主探店</h1>
          <p className="mt-1 text-sm text-muted-foreground">
            管理博主参考图、门头参考图，以及固定探店图像与视频生成流程。
          </p>
        </div>
        <button
          onClick={openCreateDialog}
          className="flex items-center gap-2 bg-primary text-primary-foreground px-4 py-2 rounded-md hover:bg-primary/90 transition-colors"
        >
          <Plus className="w-4 h-4" />
          新建项目
        </button>
      </div>

      <div className="space-y-3">
        {projects.map((project) => (
          <div
            key={project.id}
            className="bg-card border border-border rounded-xl shadow-sm hover:shadow-md transition-shadow p-3"
          >
            <div className="grid grid-cols-[110px_1fr_auto] gap-4 items-center">
              <button
                onClick={() => navigate(`/store-visits/${project.id}`)}
                className="aspect-[4/5] rounded-xl overflow-hidden bg-muted/40 flex items-center justify-center border border-border/60"
              >
                {project.blogger_reference_image ? (
                  <img
                    src={withAssetVersion(project.blogger_reference_image, project.updated_at)}
                    alt={project.name}
                    className="w-full h-full object-contain"
                  />
                ) : (
                  <Store className="w-8 h-8 text-muted-foreground" />
                )}
              </button>

              <button onClick={() => navigate(`/store-visits/${project.id}`)} className="text-left min-w-0">
                <div className="space-y-1">
                  <h3 className="text-base font-semibold truncate">{project.name}</h3>
                  <p className="text-xs text-muted-foreground">文件名：{project.code}</p>
                  <p className="text-sm text-muted-foreground line-clamp-2">{project.description}</p>
                </div>
              </button>

              <div className="flex flex-col gap-2">
                <button
                  onClick={() => navigate(`/store-visits/${project.id}`)}
                  className="flex items-center justify-center gap-2 bg-primary text-primary-foreground px-3 py-2 rounded-md text-sm hover:bg-primary/90 transition-colors whitespace-nowrap"
                >
                  <FolderOpen className="w-4 h-4" />
                  进入管理
                </button>
                <button
                  onClick={() => openEditDialog(project)}
                  className="flex items-center justify-center gap-2 border border-border px-3 py-2 rounded-md text-sm hover:bg-accent transition-colors whitespace-nowrap"
                >
                  <Pencil className="w-4 h-4" />
                  编辑
                </button>
                <button
                  onClick={() => setDeleteTarget(project)}
                  className="flex items-center justify-center gap-2 bg-destructive text-destructive-foreground px-3 py-2 rounded-md text-sm hover:bg-destructive/90 transition-colors whitespace-nowrap"
                >
                  <Trash2 className="w-4 h-4" />
                  删除
                </button>
              </div>
            </div>
          </div>
        ))}
      </div>

      {!loading && projects.length === 0 && (
        <div className="rounded-xl border border-dashed border-border bg-card/60 p-10 text-center text-muted-foreground">
          还没有博主探店项目，先创建一个试试。
        </div>
      )}

      <Dialog
        open={createOpen}
        onOpenChange={(open) => {
          if (saving) return;
          setCreateOpen(open);
          if (!open) {
            setEditingProject(null);
            resetForm();
          }
        }}
      >
        <DialogContent className="max-w-2xl">
          <DialogHeader>
            <DialogTitle>{editingProject ? "编辑博主探店项目" : "新建博主探店项目"}</DialogTitle>
            <DialogDescription>
              {editingProject
                ? "可以修改项目基础信息，并继续追加新的博主参考图。新追加的图片会出现在详情页左侧供你切换使用。"
                : "先建立博主参考图和项目基础信息，系统会自动创建默认的“门头”条目。"}
            </DialogDescription>
          </DialogHeader>

          <div className="space-y-4">
            <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
              <div>
                <label className="text-sm font-medium">项目名称</label>
                <Input value={name} onChange={(e) => setName(e.target.value)} placeholder="例如：火锅店探店" />
              </div>
              <div>
                <label className="text-sm font-medium">项目文件名</label>
                <Input value={code} onChange={(e) => setCode(e.target.value)} placeholder="例如：huoguo_tandian" />
              </div>
            </div>
            <div>
              <label className="text-sm font-medium">备注</label>
              <Textarea
                value={description}
                onChange={(e) => setDescription(e.target.value)}
                placeholder="例如：主播形象偏职场、门店偏暖光、整体要真实探店感。"
                className="min-h-[90px]"
              />
            </div>
            <div className="space-y-2">
              <label className="text-sm font-medium">{editingProject ? "追加博主参考图" : "博主参考图"}</label>
              <input
                ref={fileInputRef}
                type="file"
                accept="image/*"
                className="hidden"
                multiple
                onChange={(e) => setReferenceImages(Array.from(e.target.files || []))}
              />
              <button
                type="button"
                onClick={() => fileInputRef.current?.click()}
                className="w-full rounded-2xl border border-dashed border-border bg-muted/30 p-4 text-left hover:bg-muted/50 transition-colors"
              >
                <div className="grid grid-cols-1 md:grid-cols-[260px_1fr] gap-4 items-center">
                  <div className="rounded-xl bg-background border border-border/60 p-3">
                    {previewItems.length > 0 ? (
                      <div className="grid grid-cols-3 gap-2">
                        {previewItems.slice(0, 6).map((item, index) => (
                          <div key={`${item.name}-${index}`} className="space-y-1">
                            <div className="aspect-[4/5] rounded-lg overflow-hidden border border-border/60 bg-muted/20">
                              <img src={item.url} alt={item.name} className="w-full h-full object-cover" />
                            </div>
                            <div className="text-[10px] text-muted-foreground truncate">{index === 0 ? "默认使用" : `候选 ${index + 1}`}</div>
                          </div>
                        ))}
                      </div>
                    ) : editingProject?.blogger_reference_image ? (
                      <div className="grid grid-cols-3 gap-2">
                        <div className="space-y-1">
                          <div className="aspect-[4/5] rounded-lg overflow-hidden border border-border/60 bg-muted/20">
                            <img
                              src={withAssetVersion(editingProject.blogger_reference_image, editingProject.updated_at)}
                              alt={editingProject.name}
                              className="w-full h-full object-cover"
                            />
                          </div>
                          <div className="text-[10px] text-muted-foreground truncate">当前主参考图</div>
                        </div>
                      </div>
                    ) : (
                      <div className="aspect-[4/5] rounded-xl bg-background border border-border/60 overflow-hidden flex items-center justify-center">
                        <div className="flex flex-col items-center justify-center gap-2 text-muted-foreground">
                          <ImagePlus className="w-10 h-10" />
                          <span className="text-xs">点击上传博主参考图</span>
                        </div>
                      </div>
                    )}
                  </div>
                  <div className="space-y-2">
                    <div className="flex items-center gap-2 text-sm font-medium">
                      <UploadCloud className="w-4 h-4" />
                      {editingProject ? "追加新的博主参考图" : "上传多张博主参考图"}
                    </div>
                    <p className="text-sm text-muted-foreground leading-6">
                      {editingProject
                        ? "这里上传的是新增图片，不会覆盖原来的博主参考图。保存后你可以在详情页左侧切换当前使用的人物参考照片。"
                        : "可以一次上传多张博主参考图。系统默认使用第一张，进入项目后你还能在左侧点击切换当前使用的人物参考照片。"}
                    </p>
                    {editingProject?.blogger_reference_image && referenceImages.length === 0 && (
                      <div className="inline-flex items-center gap-2 rounded-full bg-muted px-3 py-1 text-xs text-muted-foreground">
                        当前主参考图已保留，可继续追加新图
                      </div>
                    )}
                    {previewItems.length > 0 && (
                      <div className="inline-flex items-center gap-2 rounded-full bg-primary/10 px-3 py-1 text-xs text-primary">
                        已选择 {previewItems.length} 张，{editingProject ? "保存后追加到现有列表尾部" : "默认使用第 1 张"}
                      </div>
                    )}
                  </div>
                </div>
              </button>
            </div>
          </div>

          <DialogFooter>
            <button
              type="button"
              onClick={() => setCreateOpen(false)}
              className="px-4 py-2 rounded-md border border-border hover:bg-accent transition-colors"
              disabled={saving}
            >
              取消
            </button>
            <button
              type="button"
              onClick={handleCreateOrUpdate}
              disabled={saving}
              className="px-4 py-2 rounded-md bg-primary text-primary-foreground hover:bg-primary/90 transition-colors disabled:opacity-50"
            >
              {saving ? (editingProject ? "保存中..." : "创建中...") : editingProject ? "保存修改" : "创建项目"}
            </button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <AlertDialog open={!!deleteTarget} onOpenChange={(open) => !open && setDeleteTarget(null)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>删除博主探店项目</AlertDialogTitle>
            <AlertDialogDescription>
              删除后会连同项目内部的参考图、门头图片和视频一起物理删除，不能恢复。
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>取消</AlertDialogCancel>
            <AlertDialogAction onClick={handleDelete} className="bg-destructive text-destructive-foreground hover:bg-destructive/90">
              确认删除
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}
