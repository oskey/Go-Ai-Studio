import { useEffect, useState } from "react";
import { useNavigate } from "react-router-dom";
import axios from "axios";
import { Mic2, Pencil, Plus, Trash2 } from "lucide-react";
import { toast } from "sonner";

import type { QwenTTSProject } from "@/types";
import { Input } from "@/components/ui/input";
import { Textarea } from "@/components/ui/textarea";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
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

export default function QwenTTSProjects() {
  const navigate = useNavigate();
  const [projects, setProjects] = useState<QwenTTSProject[]>([]);
  const [loading, setLoading] = useState(false);
  const [dialogOpen, setDialogOpen] = useState(false);
  const [editingProject, setEditingProject] = useState<QwenTTSProject | null>(null);
  const [deleteTarget, setDeleteTarget] = useState<QwenTTSProject | null>(null);
  const [name, setName] = useState("");
  const [code, setCode] = useState("");
  const [description, setDescription] = useState("");
  const [scriptText, setScriptText] = useState("");
  const [instruct, setInstruct] = useState("");
  const [temperature, setTemperature] = useState("0.9");
  const [saving, setSaving] = useState(false);

  const fetchProjects = async () => {
    setLoading(true);
    try {
      const res = await axios.get("/api/qwen-tts-projects");
      setProjects(Array.isArray(res.data) ? res.data : []);
    } catch (err) {
      console.error(err);
      toast.error("获取 Qwen3 TTS 项目失败");
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    fetchProjects();
  }, []);

  const resetForm = () => {
    setName("");
    setCode("");
    setDescription("");
    setScriptText("");
    setInstruct("");
    setTemperature("0.9");
  };

  const openCreate = () => {
    resetForm();
    setEditingProject(null);
    setDialogOpen(true);
  };

  const openEdit = (project: QwenTTSProject) => {
    setName(project.name || "");
    setCode(project.code || "");
    setDescription(project.description || "");
    setScriptText(project.script_text || "");
    setInstruct(project.instruct || "");
    setTemperature(String(project.temperature || 0.9));
    setEditingProject(project);
    setDialogOpen(true);
  };

  const saveProject = async () => {
    if (!name.trim() || !code.trim()) {
      toast.error("请填写项目名称和项目文件名");
      return;
    }
    setSaving(true);
    try {
      const payload = {
        name: name.trim(),
        code: code.trim(),
        description: description.trim(),
        script_text: scriptText.trim(),
        instruct: instruct.trim(),
        temperature: Number(temperature) || 0.9,
      };
      const res = editingProject
        ? await axios.put(`/api/qwen-tts-projects/${editingProject.id}`, payload)
        : await axios.post("/api/qwen-tts-projects", payload);
      toast.success(editingProject ? "项目已更新" : "项目已创建");
      setDialogOpen(false);
      resetForm();
      setEditingProject(null);
      await fetchProjects();
      if (!editingProject) {
        navigate(`/qwen-tts-projects/${res.data.id}`);
      }
    } catch (err: any) {
      console.error(err);
      toast.error(err.response?.data?.error || "保存失败");
    } finally {
      setSaving(false);
    }
  };

  const deleteProject = async () => {
    if (!deleteTarget) return;
    try {
      await axios.delete(`/api/qwen-tts-projects/${deleteTarget.id}`);
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
          <h1 className="text-3xl font-bold">Qwen3 TTS</h1>
          <p className="mt-1 text-sm text-muted-foreground">
            音频复制模板：上传角色参考音频，默认由 SenseVoice 自动识别参考文本，可用 instruct 和 temperature 调整生成风格。
          </p>
        </div>
        <button
          onClick={openCreate}
          className="flex items-center gap-2 rounded-md bg-primary px-4 py-2 text-primary-foreground hover:bg-primary/90"
        >
          <Plus className="h-4 w-4" />
          新建项目
        </button>
      </div>

      <div className="space-y-3">
        {projects.map((project) => (
          <div key={project.id} className="rounded-xl border bg-card p-4 shadow-sm">
            <div className="flex items-center gap-4">
              <button
                onClick={() => navigate(`/qwen-tts-projects/${project.id}`)}
                className="flex h-16 w-16 shrink-0 items-center justify-center rounded-xl border bg-muted/40"
              >
                <Mic2 className="h-8 w-8 text-muted-foreground" />
              </button>
              <button onClick={() => navigate(`/qwen-tts-projects/${project.id}`)} className="min-w-0 flex-1 text-left">
                <h3 className="truncate text-lg font-semibold">{project.name}</h3>
                <p className="text-xs text-muted-foreground">文件名：{project.code}</p>
                <p className="mt-1 text-xs text-muted-foreground">Temperature：{project.temperature || 0.9}</p>
                {project.description ? <p className="mt-1 line-clamp-2 text-sm text-muted-foreground">{project.description}</p> : null}
              </button>
              <div className="flex gap-2">
                <button onClick={() => openEdit(project)} className="rounded-md border px-3 py-2 text-sm hover:bg-muted">
                  <Pencil className="mr-1 inline h-4 w-4" />
                  编辑
                </button>
                <button onClick={() => setDeleteTarget(project)} className="rounded-md border px-3 py-2 text-sm text-destructive hover:bg-destructive/10">
                  <Trash2 className="mr-1 inline h-4 w-4" />
                  删除
                </button>
              </div>
            </div>
          </div>
        ))}
        {!loading && projects.length === 0 ? (
          <div className="rounded-xl border border-dashed p-12 text-center text-muted-foreground">还没有 Qwen3 TTS 项目。</div>
        ) : null}
      </div>

      <Dialog open={dialogOpen} onOpenChange={setDialogOpen}>
        <DialogContent className="max-w-2xl">
          <DialogHeader>
            <DialogTitle>{editingProject ? "编辑 Qwen3 TTS 项目" : "新建 Qwen3 TTS 项目"}</DialogTitle>
            <DialogDescription>参考音频内容默认由工作流自动识别；需要更稳时可在角色资产里手动覆盖。</DialogDescription>
          </DialogHeader>
          <div className="space-y-4">
            <div>
              <label className="text-sm font-medium">项目名称</label>
              <Input value={name} onChange={(e) => setName(e.target.value)} placeholder="例如：Qwen3 张继先配音" />
            </div>
            <div>
              <label className="text-sm font-medium">项目文件名</label>
              <Input value={code} onChange={(e) => setCode(e.target.value)} placeholder="例如：qwen_zhang_jixian_voice" disabled={!!editingProject} />
            </div>
            <div>
              <label className="text-sm font-medium">备注</label>
              <Input value={description} onChange={(e) => setDescription(e.target.value)} placeholder="只给自己看，不参与生成" />
            </div>
            <div>
              <label className="text-sm font-medium">提示词 instruct（可选）</label>
              <Textarea
                rows={3}
                value={instruct}
                onChange={(e) => setInstruct(e.target.value)}
                placeholder="例如：语气更紧张，情绪更有压迫感，保持角色声音特征。"
              />
              <p className="mt-1 text-xs text-muted-foreground">会注入 Qwen3TTSVoiceClone 的 instruct 输入；不填则保持空。</p>
            </div>
            <div>
              <label className="text-sm font-medium">Temperature</label>
              <Input type="number" step="0.05" min="0.1" max="2" value={temperature} onChange={(e) => setTemperature(e.target.value)} />
              <p className="mt-1 text-xs text-muted-foreground">默认 0.9。这个参数会明显影响声音随机性和表达方式。</p>
            </div>
            <div>
              <label className="text-sm font-medium">默认脚本（可选）</label>
              <Textarea rows={5} value={scriptText} onChange={(e) => setScriptText(e.target.value)} placeholder="{张继先}李大人，你已被邪物蛊惑……" />
            </div>
          </div>
          <DialogFooter>
            <button onClick={() => setDialogOpen(false)} className="rounded-md border px-4 py-2 text-sm">
              取消
            </button>
            <button onClick={saveProject} disabled={saving} className="rounded-md bg-primary px-4 py-2 text-sm text-primary-foreground disabled:opacity-60">
              {saving ? "保存中..." : "保存"}
            </button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <AlertDialog open={!!deleteTarget} onOpenChange={(open) => !open && setDeleteTarget(null)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>删除 Qwen3 TTS 项目？</AlertDialogTitle>
            <AlertDialogDescription>会删除项目、角色声音资产、生成音频和所有物理文件。</AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>取消</AlertDialogCancel>
            <AlertDialogAction onClick={deleteProject} className="bg-destructive text-destructive-foreground hover:bg-destructive/90">
              删除
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}
