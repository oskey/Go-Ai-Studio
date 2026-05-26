import { useEffect, useState } from "react";
import { useNavigate } from "react-router-dom";
import axios from "axios";
import { Mic2, Pencil, Plus, Trash2 } from "lucide-react";
import { toast } from "sonner";

import type { AudioProductionPresetOption, AudioProductionProject } from "@/types";
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
import { Input } from "@/components/ui/input";
import { Textarea } from "@/components/ui/textarea";

type AudioProductionMode = "custom_voice" | "voice_prompt";

const modeMeta = {
  custom_voice: {
    title: "按人设生成（Qwen3-TTS）",
    description: "使用 Qwen3-TTs-Custom-Voice 工作流，从内置 speaker 人设生成音频。",
    basePath: "/audio-production/custom-voice",
  },
  voice_prompt: {
    title: "按提示生成（Qwen3-TTS）",
    description: "使用 Qwen3-TTS Voice-Prompt 工作流，通过声音提示词设计音色与表达。",
    basePath: "/audio-production/voice-prompt",
  },
} satisfies Record<AudioProductionMode, { title: string; description: string; basePath: string }>;

export default function AudioProductionProjects({ mode }: { mode: AudioProductionMode }) {
  const navigate = useNavigate();
  const meta = modeMeta[mode];
  const [projects, setProjects] = useState<AudioProductionProject[]>([]);
  const [speakerPresets, setSpeakerPresets] = useState<AudioProductionPresetOption[]>([]);
  const [instructPresets, setInstructPresets] = useState<AudioProductionPresetOption[]>([]);
  const [voicePromptPresets, setVoicePromptPresets] = useState<AudioProductionPresetOption[]>([]);
  const [loading, setLoading] = useState(false);
  const [dialogOpen, setDialogOpen] = useState(false);
  const [editingProject, setEditingProject] = useState<AudioProductionProject | null>(null);
  const [deleteTarget, setDeleteTarget] = useState<AudioProductionProject | null>(null);
  const [name, setName] = useState("");
  const [code, setCode] = useState("");
  const [description, setDescription] = useState("");
  const [text, setText] = useState("");
  const [speaker, setSpeaker] = useState("");
  const [instruct, setInstruct] = useState("");
  const [voiceInstruction, setVoiceInstruction] = useState("");
  const [temperature, setTemperature] = useState("0.7");
  const [saving, setSaving] = useState(false);

  const fetchProjects = async () => {
    setLoading(true);
    try {
      const res = await axios.get(`/api/audio-production-projects?mode=${mode}`);
      setProjects(Array.isArray(res.data) ? res.data : []);
    } catch (err) {
      console.error(err);
      toast.error("获取音频生产项目失败");
    } finally {
      setLoading(false);
    }
  };

  const fetchPresets = async () => {
    try {
      const res = await axios.get(`/api/audio-production-presets?mode=${mode}`);
      setSpeakerPresets(Array.isArray(res.data?.speakers) ? res.data.speakers : []);
      setInstructPresets(Array.isArray(res.data?.instructs) ? res.data.instructs : []);
      setVoicePromptPresets(Array.isArray(res.data?.voice_prompts) ? res.data.voice_prompts : []);
    } catch (err) {
      console.error(err);
    }
  };

  useEffect(() => {
    void fetchProjects();
    void fetchPresets();
  }, [mode]);

  const resetForm = () => {
    setName("");
    setCode("");
    setDescription("");
    setText("");
    setSpeaker(speakerPresets[0]?.value || "");
    setInstruct("");
    setVoiceInstruction("");
    setTemperature("0.7");
  };

  const openCreate = () => {
    resetForm();
    setEditingProject(null);
    setDialogOpen(true);
  };

  const openEdit = (project: AudioProductionProject) => {
    setEditingProject(project);
    setName(project.name || "");
    setCode(project.code || "");
    setDescription(project.description || "");
    setText(project.text || "");
    setSpeaker(project.speaker || speakerPresets[0]?.value || "");
    setInstruct(project.instruct || "");
    setVoiceInstruction(project.voice_instruction || "");
    setTemperature(String(project.temperature || 0.7));
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
        mode,
        name: name.trim(),
        code: code.trim(),
        description: description.trim(),
        text: text.trim(),
        speaker: speaker.trim(),
        instruct: instruct.trim(),
        voice_instruction: voiceInstruction.trim(),
        temperature: Number(temperature) || 0.7,
      };
      const res = editingProject
        ? await axios.put(`/api/audio-production-projects/${editingProject.id}`, payload)
        : await axios.post("/api/audio-production-projects", payload);
      toast.success(editingProject ? "项目已更新" : "项目已创建");
      setDialogOpen(false);
      resetForm();
      setEditingProject(null);
      await fetchProjects();
      if (!editingProject) {
        navigate(`${meta.basePath}/${res.data.id}`);
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
      await axios.delete(`/api/audio-production-projects/${deleteTarget.id}`);
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
          <h1 className="text-3xl font-bold">{meta.title}</h1>
          <p className="mt-1 text-sm text-muted-foreground">{meta.description}</p>
        </div>
        <button onClick={openCreate} className="flex items-center gap-2 rounded-md bg-primary px-4 py-2 text-primary-foreground hover:bg-primary/90">
          <Plus className="h-4 w-4" />
          新建项目
        </button>
      </div>

      <div className="space-y-3">
        {projects.map((project) => (
          <div key={project.id} className="rounded-xl border bg-card p-4 shadow-sm">
            <div className="flex items-center gap-4">
              <button onClick={() => navigate(`${meta.basePath}/${project.id}`)} className="flex h-16 w-16 shrink-0 items-center justify-center rounded-xl border bg-muted/40">
                <Mic2 className="h-8 w-8 text-muted-foreground" />
              </button>
              <button onClick={() => navigate(`${meta.basePath}/${project.id}`)} className="min-w-0 flex-1 text-left">
                <h3 className="truncate text-lg font-semibold">{project.name}</h3>
                <p className="text-xs text-muted-foreground">文件名：{project.code}</p>
                <p className="mt-1 text-xs text-muted-foreground">Temperature：{project.temperature || 0.7}</p>
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
        {!loading && projects.length === 0 ? <div className="rounded-xl border border-dashed p-12 text-center text-muted-foreground">还没有 {meta.title} 项目。</div> : null}
      </div>

      <Dialog open={dialogOpen} onOpenChange={setDialogOpen}>
        <DialogContent className="max-w-2xl">
          <DialogHeader>
            <DialogTitle>{editingProject ? `编辑 ${meta.title} 项目` : `新建 ${meta.title} 项目`}</DialogTitle>
            <DialogDescription>文本和提示词提交到 ComfyUI 前会被整理成单行，不会修改你页面里的原文。</DialogDescription>
          </DialogHeader>
          <div className="space-y-4">
            <div>
              <label className="text-sm font-medium">项目名称</label>
              <Input value={name} onChange={(e) => setName(e.target.value)} placeholder={mode === "custom_voice" ? "例如：Vivian 广告音频" : "例如：撒娇女声批量生成"} />
            </div>
            <div>
              <label className="text-sm font-medium">项目文件名</label>
              <Input value={code} onChange={(e) => setCode(e.target.value)} placeholder="例如：qwen_audio_production_01" disabled={!!editingProject} />
            </div>
            <div>
              <label className="text-sm font-medium">备注</label>
              <Input value={description} onChange={(e) => setDescription(e.target.value)} placeholder="只给自己看，不参与生成" />
            </div>
            {mode === "custom_voice" ? (
              <>
                <div>
                  <label className="text-sm font-medium">内置人设 speaker</label>
                  <select value={speaker} onChange={(e) => setSpeaker(e.target.value)} className="mt-1 w-full rounded-md border bg-background px-3 py-2 text-sm">
                    {speakerPresets.map((item) => (
                      <option key={item.value} value={item.value}>
                        {item.label}
                      </option>
                    ))}
                  </select>
                  <Input className="mt-2" value={speaker} onChange={(e) => setSpeaker(e.target.value)} placeholder="内部 speaker 值，可手动覆盖" />
                </div>
                <div>
                  <label className="text-sm font-medium">提示词 instruct</label>
                  <select value="" onChange={(e) => e.target.value && setInstruct(e.target.value)} className="mb-2 mt-1 w-full rounded-md border bg-background px-3 py-2 text-sm">
                    <option value="">选择一个预设填入...</option>
                    {instructPresets.map((item) => (
                      <option key={item.label} value={item.value}>
                        {item.label}
                      </option>
                    ))}
                  </select>
                  <Textarea rows={3} value={instruct} onChange={(e) => setInstruct(e.target.value)} placeholder="例如：开心、明亮、语速自然，带一点轻快的笑意。" />
                </div>
              </>
            ) : (
              <div>
                <label className="text-sm font-medium">声音提示词 voice_instruction</label>
                <select value="" onChange={(e) => e.target.value && setVoiceInstruction(e.target.value)} className="mb-2 mt-1 w-full rounded-md border bg-background px-3 py-2 text-sm">
                  <option value="">选择一个预设填入...</option>
                  {voicePromptPresets.map((item) => (
                    <option key={item.label} value={item.value}>
                      {item.label}
                    </option>
                  ))}
                </select>
                <Textarea rows={3} value={voiceInstruction} onChange={(e) => setVoiceInstruction(e.target.value)} placeholder="例如：体现温柔治愈的女性声音，音色柔和，语速稍慢。" />
              </div>
            )}
            <div>
              <label className="text-sm font-medium">Temperature</label>
              <Input type="number" step="0.05" min="0.1" max="2" value={temperature} onChange={(e) => setTemperature(e.target.value)} />
            </div>
            <div>
              <label className="text-sm font-medium">默认生成文本（可选，一行一条）</label>
              <Textarea rows={5} value={text} onChange={(e) => setText(e.target.value)} placeholder="哥哥，你回来啦，人家等了你好久好久了，要抱抱！" />
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
            <AlertDialogTitle>删除 {meta.title} 项目？</AlertDialogTitle>
            <AlertDialogDescription>会删除项目、生成音频和所有物理文件。</AlertDialogDescription>
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
