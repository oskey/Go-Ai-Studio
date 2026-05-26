import { useEffect, useMemo, useRef, useState } from "react";
import { useNavigate } from "react-router-dom";
import axios from "axios";
import { FolderOpen, ImagePlus, Megaphone, Pencil, Plus, Tags, Trash2, UploadCloud } from "lucide-react";
import { toast } from "sonner";

import type { GeneralGuideProject, GeneralGuideTag } from "@/types";
import { Checkbox } from "@/components/ui/checkbox";
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
  if (!version) return trimmed;
  return `${trimmed}${trimmed.includes("?") ? "&" : "?"}v=${encodeURIComponent(version)}`;
};

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

export default function GeneralGuideProjects() {
  const navigate = useNavigate();
  const [projects, setProjects] = useState<GeneralGuideProject[]>([]);
  const [tags, setTags] = useState<GeneralGuideTag[]>([]);
  const [loading, setLoading] = useState(false);
  const [createOpen, setCreateOpen] = useState(false);
  const [editingProject, setEditingProject] = useState<GeneralGuideProject | null>(null);
  const [deleteTarget, setDeleteTarget] = useState<GeneralGuideProject | null>(null);
  const [name, setName] = useState("");
  const [code, setCode] = useState("");
  const [description, setDescription] = useState("");
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
  const [autoGenerateContent, setAutoGenerateContent] = useState("");
  const [selectedTagIDs, setSelectedTagIDs] = useState<number[]>([]);
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
    setPresenterGender("female");
    setPresenterPersona("female_natural");
    setAutoGenerateContent("");
    setSelectedTagIDs([]);
    setReferenceImages([]);
    if (fileInputRef.current) {
      fileInputRef.current.value = "";
    }
  };

  const fetchProjects = async () => {
    setLoading(true);
    try {
      const res = await axios.get("/api/general-guides");
      setProjects(Array.isArray(res.data) ? res.data : []);
    } catch (err) {
      console.error(err);
      toast.error("获取综合讲解项目失败");
    } finally {
      setLoading(false);
    }
  };

  const fetchTags = async () => {
    try {
      const res = await axios.get("/api/general-guide-tags");
      setTags(Array.isArray(res.data) ? res.data : []);
    } catch (err) {
      console.error(err);
      toast.error("获取标签失败");
    }
  };

  useEffect(() => {
    fetchProjects();
    fetchTags();
  }, []);

  const openCreateDialog = () => {
    resetForm();
    setEditingProject(null);
    setCreateOpen(true);
  };

  const openEditDialog = (project: GeneralGuideProject) => {
    setName(project.name || "");
    setCode(project.code || "");
    setDescription(project.description || "");
    setPresenterGender(project.presenter_gender === "male" ? "male" : "female");
    setPresenterPersona(
      project.presenter_persona && project.presenter_gender === "male"
        ? ((["male_natural", "male_steady", "male_confident", "male_warm"] as const).includes(project.presenter_persona as any)
            ? (project.presenter_persona as any)
            : "male_natural")
        : project.presenter_persona && project.presenter_gender !== "male"
          ? ((["female_natural", "female_playful", "female_sexy", "female_gentle"] as const).includes(project.presenter_persona as any)
              ? (project.presenter_persona as any)
              : "female_natural")
          : project.presenter_gender === "male"
            ? "male_natural"
            : "female_natural",
    );
    setAutoGenerateContent(project.auto_generate_content || "");
    setSelectedTagIDs(project.tag_ids || []);
    setReferenceImages([]);
    if (fileInputRef.current) {
      fileInputRef.current.value = "";
    }
    setEditingProject(project);
    setCreateOpen(true);
  };

  const toggleTag = (tagID: number) => {
    setSelectedTagIDs((prev) => (prev.includes(tagID) ? prev.filter((id) => id !== tagID) : [...prev, tagID]));
  };

  const handleCreateOrUpdate = async () => {
    if (!name.trim() || !code.trim()) {
      toast.error("请填写项目名称和项目文件名");
      return;
    }
    if (!editingProject && referenceImages.length === 0) {
      toast.error("请至少上传一张讲解人参考图");
      return;
    }

    setSaving(true);
    try {
      const formData = new FormData();
      formData.append("name", name.trim());
      formData.append("code", code.trim());
      formData.append("description", description.trim());
      formData.append("presenter_gender", presenterGender);
      formData.append("presenter_persona", presenterPersona);
      formData.append("auto_generate_content", autoGenerateContent.trim());
      formData.append("tag_ids_json", JSON.stringify(selectedTagIDs));
      referenceImages.forEach((file) => {
        formData.append("presenter_reference_images", file);
      });

      const method = editingProject ? "put" : "post";
      const url = editingProject ? `/api/general-guides/${editingProject.id}` : "/api/general-guides";
      const res = await axios[method](url, formData, {
        headers: { "Content-Type": "multipart/form-data" },
      });
      toast.success(editingProject ? "综合讲解项目已更新" : "综合讲解项目已创建");
      setCreateOpen(false);
      resetForm();
      setEditingProject(null);
      await fetchProjects();
      if (!editingProject) {
        navigate(`/general-guides/${res.data.id}`);
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
      await axios.delete(`/api/general-guides/${deleteTarget.id}`);
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
          <h1 className="text-3xl font-bold">综合讲解</h1>
          <p className="mt-1 text-sm text-muted-foreground">
            用更少的文字自动规划讲解场景，适用于店铺、房产、商品、服务等通用讲解内容。
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
                onClick={() => navigate(`/general-guides/${project.id}`)}
                className="aspect-[4/5] rounded-xl overflow-hidden bg-muted/40 flex items-center justify-center border border-border/60"
              >
                {project.presenter_reference_image ? (
                  <img
                    src={withAssetVersion(project.presenter_reference_image, project.updated_at)}
                    alt={project.name}
                    className="w-full h-full object-contain"
                  />
                ) : (
                  <Megaphone className="w-8 h-8 text-muted-foreground" />
                )}
              </button>

              <button onClick={() => navigate(`/general-guides/${project.id}`)} className="text-left min-w-0">
                <div className="space-y-1">
                  <h3 className="text-base font-semibold truncate">{project.name}</h3>
                  <p className="text-xs text-muted-foreground">文件名：{project.code}</p>
                  {project.description ? <p className="text-sm text-muted-foreground line-clamp-2">{project.description}</p> : null}
                  {project.tag_ids && project.tag_ids.length > 0 && (
                    <div className="flex flex-wrap gap-2 pt-1">
                      {project.tag_ids.slice(0, 4).map((tagID) => {
                        const tag = tags.find((item) => item.id === tagID);
                        if (!tag) return null;
                        return (
                          <span key={tagID} className="rounded-full bg-muted px-2 py-0.5 text-[11px] text-muted-foreground">
                            {tag.name}
                          </span>
                        );
                      })}
                    </div>
                  )}
                </div>
              </button>

              <div className="flex flex-col gap-2">
                <button
                  onClick={() => navigate(`/general-guides/${project.id}`)}
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
          还没有综合讲解项目，先创建一个试试。
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
        <DialogContent className="max-w-3xl">
          <DialogHeader>
            <DialogTitle>{editingProject ? "编辑综合讲解项目" : "新建综合讲解项目"}</DialogTitle>
            <DialogDescription>
              {editingProject
                ? "可以修改基础信息、标签和项目总文案，并继续追加新的讲解人参考图。备注只用于你自己识别项目，不参与 LLM 生成。"
                : "先建立讲解人参考图和项目基础信息，后面再用项目总文案自动规划讲解场景。备注只用于你自己识别项目，不参与 LLM 生成。"}
            </DialogDescription>
          </DialogHeader>

          <div className="space-y-4">
            <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
              <div>
                <label className="text-sm font-medium">项目名称</label>
                <Input value={name} onChange={(e) => setName(e.target.value)} placeholder="例如：临街店铺转让讲解" />
              </div>
              <div>
                <label className="text-sm font-medium">项目文件名</label>
                <Input value={code} onChange={(e) => setCode(e.target.value)} placeholder="例如：shop_transfer_guide" />
              </div>
            </div>

            <div>
              <label className="text-sm font-medium">备注（可选，仅用于识别项目）</label>
              <Textarea
                value={description}
                onChange={(e) => setDescription(e.target.value)}
                placeholder="例如：整体希望更亲和、更像真人讲解，适合短视频口播。"
                className="min-h-[80px]"
              />
            </div>

            <div>
              <label className="text-sm font-medium">讲解人性别</label>
              <select
                value={presenterGender}
                onChange={(e) => {
                  const nextGender = (e.target.value === "male" ? "male" : "female") as "male" | "female";
                  setPresenterGender(nextGender);
                  setPresenterPersona(nextGender === "male" ? "male_natural" : "female_natural");
                }}
                className="mt-1 w-full rounded-md border border-input bg-background px-3 py-2 text-sm outline-none focus:border-ring focus:ring-2 focus:ring-ring/30"
              >
                <option value="female">女性</option>
                <option value="male">男性</option>
              </select>
              <p className="mt-1 text-xs text-muted-foreground">
                这个选项会传给 LLM，帮助它在需要人物出镜和说话的场景里保持性别一致，减少 LTX2.3 乱猜带来的口型和气质偏差。
              </p>
            </div>

            <div>
              <label className="text-sm font-medium">讲解人人设</label>
              <select
                value={presenterPersona}
                onChange={(e) => setPresenterPersona(e.target.value as typeof presenterPersona)}
                className="mt-1 w-full rounded-md border border-input bg-background px-3 py-2 text-sm outline-none focus:border-ring focus:ring-2 focus:ring-ring/30"
              >
                {presenterPersonaOptions[presenterGender].map((item) => (
                  <option key={item.value} value={item.value}>
                    {item.label}
                  </option>
                ))}
              </select>
              <p className="mt-1 text-xs text-muted-foreground">
                {presenterPersonaOptions[presenterGender].find((item) => item.value === presenterPersona)?.description} 这个设置会一起传给 LLM，
                让视频提示词里的说话语气、表情、动作和行为方式更符合这个角色设定。
              </p>
            </div>

            <div>
              <label className="text-sm font-medium">默认总文案（可选）</label>
              <Textarea
                value={autoGenerateContent}
                onChange={(e) => setAutoGenerateContent(e.target.value)}
                placeholder="例如：我现在有个店铺想出租，临街、商圈繁华、均价便宜，不要物业费，想用亲和可爱的方式做整套介绍。"
                className="min-h-[110px]"
              />
            </div>

            <div className="space-y-2">
              <div className="flex items-center gap-2 text-sm font-medium">
                <Tags className="w-4 h-4" />
                讲解标签（可选）
              </div>
              <div className="rounded-xl border border-border bg-muted/20 p-3">
                <div className="grid grid-cols-1 md:grid-cols-2 gap-3">
                  {tags.map((tag) => (
                    <label key={tag.id} className="flex items-start gap-3 rounded-lg border border-border/60 bg-background px-3 py-3">
                      <Checkbox checked={selectedTagIDs.includes(tag.id)} onCheckedChange={() => toggleTag(tag.id)} />
                      <div className="space-y-1">
                        <div className="text-sm font-medium">{tag.name}</div>
                        <div className="text-xs text-muted-foreground leading-5">{tag.description}</div>
                      </div>
                    </label>
                  ))}
                </div>
              </div>
            </div>

            <div className="space-y-2">
              <label className="text-sm font-medium">{editingProject ? "追加讲解人参考图" : "讲解人参考图"}</label>
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
                    ) : editingProject?.presenter_reference_image ? (
                      <div className="grid grid-cols-3 gap-2">
                        <div className="space-y-1">
                          <div className="aspect-[4/5] rounded-lg overflow-hidden border border-border/60 bg-muted/20">
                            <img
                              src={withAssetVersion(editingProject.presenter_reference_image, editingProject.updated_at)}
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
                          <span className="text-xs">点击上传讲解人参考图</span>
                        </div>
                      </div>
                    )}
                  </div>
                  <div className="space-y-2">
                    <div className="flex items-center gap-2 text-sm font-medium">
                      <UploadCloud className="w-4 h-4" />
                      {editingProject ? "追加新的讲解人参考图" : "上传多张讲解人参考图"}
                    </div>
                    <p className="text-sm text-muted-foreground leading-6">
                      这里上传的是讲解人的人物参考图。后续如果某个场景需要人物出镜，系统会优先按照这些参考图的人脸和形象来做合成。
                    </p>
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
            <AlertDialogTitle>删除综合讲解项目</AlertDialogTitle>
            <AlertDialogDescription>
              删除后会连同项目内的讲解人参考图、场景参考图和已生成媒体一起删除，不能恢复。
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
