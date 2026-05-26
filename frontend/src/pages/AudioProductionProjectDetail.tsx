import { useEffect, useState } from "react";
import { useNavigate, useParams } from "react-router-dom";
import axios from "axios";
import { ArrowLeft, ChevronDown, Download, Play, RefreshCw, Save, Wand2 } from "lucide-react";
import { toast } from "sonner";

import type { AudioProductionLine, AudioProductionPresetOption, AudioProductionProject } from "@/types";
import { Input } from "@/components/ui/input";
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";
import { Textarea } from "@/components/ui/textarea";

type AudioProductionMode = "custom_voice" | "voice_prompt";

const modeMeta = {
  custom_voice: {
    title: "按人设生成（Qwen3-TTS）",
    backPath: "/audio-production/custom-voice",
    settingsTitle: "Qwen3 按人设生成参数",
  },
  voice_prompt: {
    title: "按提示生成（Qwen3-TTS）",
    backPath: "/audio-production/voice-prompt",
    settingsTitle: "Qwen3 按提示生成参数",
  },
} satisfies Record<AudioProductionMode, { title: string; backPath: string; settingsTitle: string }>;

export default function AudioProductionProjectDetail({ mode }: { mode: AudioProductionMode }) {
  const { id } = useParams();
  const navigate = useNavigate();
  const meta = modeMeta[mode];
  const [project, setProject] = useState<AudioProductionProject | null>(null);
  const [lines, setLines] = useState<AudioProductionLine[]>([]);
  const [speakerPresets, setSpeakerPresets] = useState<AudioProductionPresetOption[]>([]);
  const [instructPresets, setInstructPresets] = useState<AudioProductionPresetOption[]>([]);
  const [voicePromptPresets, setVoicePromptPresets] = useState<AudioProductionPresetOption[]>([]);
  const [text, setText] = useState("");
  const [speaker, setSpeaker] = useState("");
  const [instruct, setInstruct] = useState("");
  const [voiceInstruction, setVoiceInstruction] = useState("");
  const [temperature, setTemperature] = useState("0.7");
  const [loading, setLoading] = useState(false);
  const [savingSettings, setSavingSettings] = useState(false);
  const [generating, setGenerating] = useState(false);
  const [generateMenuOpen, setGenerateMenuOpen] = useState(false);
  const [lineGenerateMenuOpenId, setLineGenerateMenuOpenId] = useState<number | null>(null);

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

  const fetchLines = async () => {
    if (!id) return;
    try {
      const res = await axios.get(`/api/audio-production-projects/${id}/lines`);
      setLines(Array.isArray(res.data) ? res.data : []);
    } catch (err) {
      console.error(err);
    }
  };

  const fetchAll = async () => {
    if (!id) return;
    setLoading(true);
    try {
      const [projectRes, linesRes] = await Promise.all([
        axios.get(`/api/audio-production-projects/${id}`),
        axios.get(`/api/audio-production-projects/${id}/lines`),
      ]);
      setProject(projectRes.data);
      setText(projectRes.data?.text || "");
      setSpeaker(projectRes.data?.speaker || "");
      setInstruct(projectRes.data?.instruct || "");
      setVoiceInstruction(projectRes.data?.voice_instruction || "");
      setTemperature(String(projectRes.data?.temperature || 0.7));
      setLines(Array.isArray(linesRes.data) ? linesRes.data : []);
    } catch (err) {
      console.error(err);
      toast.error("读取音频生产项目失败");
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    void fetchPresets();
    void fetchAll();
  }, [id, mode]);

  useEffect(() => {
    if (!lines.some((line) => line.status === "generating")) return;
    const timer = window.setInterval(fetchLines, 2500);
    return () => window.clearInterval(timer);
  }, [lines, id]);

  const saveProjectSettings = async () => {
    if (!project) return;
    setSavingSettings(true);
    try {
      const payload = {
        mode,
        name: project.name,
        code: project.code,
        description: project.description || "",
        text,
        speaker: speaker.trim(),
        instruct: instruct.trim(),
        voice_instruction: voiceInstruction.trim(),
        temperature: Number(temperature) || 0.7,
      };
      await axios.put(`/api/audio-production-projects/${project.id}`, payload);
      setProject((prev) =>
        prev
          ? {
              ...prev,
              text: payload.text,
              speaker: payload.speaker,
              instruct: payload.instruct,
              voice_instruction: payload.voice_instruction,
              temperature: payload.temperature,
            }
          : prev,
      );
      toast.success("生成参数已保存");
    } catch (err: any) {
      console.error(err);
      toast.error(err.response?.data?.error || "保存参数失败");
    } finally {
      setSavingSettings(false);
    }
  };

  const saveLines = async () => {
    if (!id) return;
    try {
      const res = await axios.post(`/api/audio-production-projects/${id}/save-lines`, { text });
      setLines(Array.isArray(res.data?.lines) ? res.data.lines : []);
      setProject((prev) => (prev ? { ...prev, text } : prev));
      toast.success("文本已解析保存");
    } catch (err: any) {
      console.error(err);
      toast.error(err.response?.data?.error || "保存文本失败");
    }
  };

  const generateAll = async (randomSeed = false) => {
    if (!id) return;
    setGenerateMenuOpen(false);
    setGenerating(true);
    try {
      const res = await axios.post(`/api/audio-production-projects/${id}/generate-lines`, {
        text,
        random_seed: randomSeed,
      });
      toast.success(res.data?.message || "生成任务已提交");
      await fetchAll();
    } catch (err: any) {
      console.error(err);
      toast.error(err.response?.data?.error || "提交生成失败");
    } finally {
      setGenerating(false);
    }
  };

  const regenerateLine = async (line: AudioProductionLine, randomSeed = false) => {
    setLineGenerateMenuOpenId(null);
    try {
      await axios.post(`/api/audio-production-lines/${line.id}/generate`, { random_seed: randomSeed });
      toast.success(randomSeed ? "单行随机 seed 任务已提交" : "单行系统 seed 任务已提交");
      await fetchLines();
    } catch (err: any) {
      console.error(err);
      toast.error(err.response?.data?.error || "提交生成失败");
    }
  };

  if (!project && loading) {
    return <div className="p-6 text-muted-foreground">加载中...</div>;
  }

  return (
    <div className="space-y-6">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div className="min-w-0">
          <button onClick={() => navigate(meta.backPath)} className="mb-2 flex items-center gap-1 text-sm text-muted-foreground hover:text-foreground">
            <ArrowLeft className="h-4 w-4" />
            返回 {meta.title}
          </button>
          <h1 className="truncate text-3xl font-bold">{project?.name || meta.title}</h1>
          <p className="mt-1 text-sm text-muted-foreground">文件名：{project?.code}</p>
        </div>
        <div className="flex flex-wrap gap-2">
          <button onClick={fetchAll} className="rounded-md border px-3 py-2 text-sm hover:bg-muted">
            <RefreshCw className="mr-1 inline h-4 w-4" />
            刷新
          </button>
          <Popover open={generateMenuOpen} onOpenChange={setGenerateMenuOpen}>
            <PopoverTrigger asChild>
              <button disabled={generating} className="rounded-md bg-primary px-4 py-2 text-sm text-primary-foreground disabled:opacity-60">
                <Wand2 className="mr-1 inline h-4 w-4" />
                {generating ? "提交中..." : "生成全部"}
                <ChevronDown className="ml-1 inline h-4 w-4" />
              </button>
            </PopoverTrigger>
            <PopoverContent align="end" className="w-56 p-2">
              <button type="button" onClick={() => void generateAll(false)} className="flex w-full flex-col rounded-md px-3 py-2 text-left text-sm hover:bg-accent">
                <span className="font-medium">使用系统 seed</span>
                <span className="text-xs text-muted-foreground">读取系统设置里的全局 Seed。</span>
              </button>
              <button type="button" onClick={() => void generateAll(true)} className="mt-1 flex w-full flex-col rounded-md px-3 py-2 text-left text-sm hover:bg-accent">
                <span className="font-medium">随机 seed 抽卡</span>
                <span className="text-xs text-muted-foreground">每一行都使用新的随机 Seed。</span>
              </button>
            </PopoverContent>
          </Popover>
        </div>
      </div>

      <section className="rounded-xl border bg-card p-4 shadow-sm">
        <div className="mb-3 flex items-center justify-between gap-3">
          <div>
            <h2 className="text-lg font-semibold">{meta.settingsTitle}</h2>
            <p className="text-xs text-muted-foreground">提交到 ComfyUI 前会把文本、提示词整理成单行；页面里的原文不被改写。</p>
          </div>
          <button onClick={saveProjectSettings} disabled={savingSettings} className="rounded-md border px-3 py-2 text-sm hover:bg-muted disabled:opacity-60">
            <Save className="mr-1 inline h-4 w-4" />
            {savingSettings ? "保存中..." : "保存参数"}
          </button>
        </div>
        <div className="grid gap-3 md:grid-cols-[1fr_180px]">
          <div className="space-y-3">
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
                <Textarea rows={4} value={voiceInstruction} onChange={(e) => setVoiceInstruction(e.target.value)} placeholder="例如：体现温柔治愈的女性声音，音色柔和，语速稍慢。" />
              </div>
            )}
          </div>
          <div>
            <label className="text-sm font-medium">Temperature</label>
            <Input type="number" step="0.05" min="0.1" max="2" value={temperature} onChange={(e) => setTemperature(e.target.value)} />
            <p className="mt-2 text-xs text-muted-foreground">默认 0.7。数值越高，表达随机性越强。</p>
          </div>
        </div>
      </section>

      <section className="rounded-xl border bg-card p-4 shadow-sm">
        <div className="mb-3 flex items-center justify-between gap-3">
          <div>
            <h2 className="text-lg font-semibold">生成文本</h2>
            <p className="text-xs text-muted-foreground">一行生成一条音频。工作流节点要求单行时，后端会在提交前自动压成单行。</p>
          </div>
          <button onClick={saveLines} className="rounded-md border px-3 py-2 text-sm hover:bg-muted">
            保存为生成行
          </button>
        </div>
        <Textarea rows={8} value={text} onChange={(e) => setText(e.target.value)} placeholder="哥哥，你回来啦，人家等了你好久好久了，要抱抱！" />
      </section>

      <section className="rounded-xl border bg-card p-4 shadow-sm">
        <div className="mb-3 flex items-center justify-between gap-3">
          <div>
            <h2 className="text-lg font-semibold">生成结果</h2>
            <p className="text-xs text-muted-foreground">每行可以单独播放、下载或重新生成。</p>
          </div>
          <button onClick={fetchLines} className="rounded-md border px-3 py-2 text-sm hover:bg-muted">刷新结果</button>
        </div>
        <div className="space-y-3">
          {lines.map((line) => (
            <div key={line.id} className="rounded-lg border bg-background p-3">
              <div className="flex flex-wrap items-start justify-between gap-3">
                <div className="min-w-0 flex-1">
                  <p className="text-sm font-semibold">第 {line.sort_order} 行</p>
                  <p className="mt-1 whitespace-pre-wrap text-sm">{line.text}</p>
                  {line.last_error ? <p className="mt-2 text-xs text-destructive">{line.last_error}</p> : null}
                  {line.generated_workflow ? <p className="mt-1 text-xs text-muted-foreground">工作流：{line.generated_workflow}</p> : null}
                </div>
                <div className="flex flex-wrap items-center gap-2">
                  <span className="rounded-full bg-muted px-2 py-1 text-xs text-muted-foreground">{line.status || "draft"}</span>
                  <Popover open={lineGenerateMenuOpenId === line.id} onOpenChange={(open) => setLineGenerateMenuOpenId(open ? line.id : null)}>
                    <PopoverTrigger asChild>
                      <button disabled={line.status === "generating"} className="rounded-md border px-3 py-2 text-sm hover:bg-muted disabled:opacity-60">
                        {line.status === "generating" ? "生成中..." : "生成"}
                        <ChevronDown className="ml-1 inline h-4 w-4" />
                      </button>
                    </PopoverTrigger>
                    <PopoverContent align="end" className="w-52 p-2">
                      <button type="button" onClick={() => void regenerateLine(line, false)} className="w-full rounded-md px-3 py-2 text-left text-sm hover:bg-accent">
                        使用系统 seed
                      </button>
                      <button type="button" onClick={() => void regenerateLine(line, true)} className="mt-1 w-full rounded-md px-3 py-2 text-left text-sm hover:bg-accent">
                        随机 seed 抽卡
                      </button>
                    </PopoverContent>
                  </Popover>
                </div>
              </div>
              {line.generated_audio ? (
                <div className="mt-3 flex flex-wrap items-center gap-3">
                  <audio controls src={line.generated_audio} className="h-10 min-w-[260px] flex-1">
                    <track kind="captions" />
                  </audio>
                  <a href={line.generated_audio} download className="rounded-md border px-3 py-2 text-sm hover:bg-muted">
                    <Download className="mr-1 inline h-4 w-4" />
                    下载
                  </a>
                  <a href={line.generated_audio} target="_blank" rel="noreferrer" className="rounded-md border px-3 py-2 text-sm hover:bg-muted">
                    <Play className="mr-1 inline h-4 w-4" />
                    打开
                  </a>
                </div>
              ) : null}
            </div>
          ))}
          {lines.length === 0 ? <div className="rounded-lg border border-dashed p-8 text-center text-muted-foreground">还没有生成行，先保存文本或直接生成全部。</div> : null}
        </div>
      </section>
    </div>
  );
}
