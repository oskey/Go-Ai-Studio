import { useEffect, useRef, useState } from "react";
import { useNavigate, useParams } from "react-router-dom";
import axios from "axios";
import { ArrowLeft, ChevronDown, Download, Pencil, Play, Plus, RefreshCw, Save, Trash2, UploadCloud, Wand2 } from "lucide-react";
import { toast } from "sonner";

import type { AudioCloneCharacter, AudioCloneLine, AudioCloneProject } from "@/types";
import { Input } from "@/components/ui/input";
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";
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

const withAssetVersion = (url?: string, version?: string) => {
  const trimmed = (url || "").trim();
  if (!trimmed) return "";
  if (!version) return trimmed;
  return `${trimmed}${trimmed.includes("?") ? "&" : "?"}v=${encodeURIComponent(version)}`;
};

export default function AudioCloneProjectDetail() {
  const { id } = useParams();
  const navigate = useNavigate();
  const [project, setProject] = useState<AudioCloneProject | null>(null);
  const [characters, setCharacters] = useState<AudioCloneCharacter[]>([]);
  const [lines, setLines] = useState<AudioCloneLine[]>([]);
  const [scriptText, setScriptText] = useState("");
  const [loading, setLoading] = useState(false);
  const [generating, setGenerating] = useState(false);
  const [generateMenuOpen, setGenerateMenuOpen] = useState(false);
  const [lineGenerateMenuOpenId, setLineGenerateMenuOpenId] = useState<number | null>(null);
  const [lineSeedDrafts, setLineSeedDrafts] = useState<Record<number, number>>({});
  const [savingLineId, setSavingLineId] = useState<number | null>(null);

  const [characterDialogOpen, setCharacterDialogOpen] = useState(false);
  const [editingCharacter, setEditingCharacter] = useState<AudioCloneCharacter | null>(null);
  const [characterName, setCharacterName] = useState("");
  const [referenceText, setReferenceText] = useState("");
  const [referenceAudio, setReferenceAudio] = useState<File | null>(null);
  const [referenceAudioPreview, setReferenceAudioPreview] = useState("");
  const [savingCharacter, setSavingCharacter] = useState(false);
  const [deleteCharacterTarget, setDeleteCharacterTarget] = useState<AudioCloneCharacter | null>(null);
  const fileInputRef = useRef<HTMLInputElement | null>(null);

  const [missingDialogOpen, setMissingDialogOpen] = useState(false);
  const [missingItems, setMissingItems] = useState<{ name: string; missing_reason: string }[]>([]);

  const syncLineDrafts = (nextLines: AudioCloneLine[]) => {
    const seeds: Record<number, number> = {};
    nextLines.forEach((line) => {
      seeds[line.id] = Number(line.seed || 0);
    });
    setLineSeedDrafts(seeds);
  };

  const fetchAll = async () => {
    if (!id) return;
    setLoading(true);
    try {
      const [projectRes, charactersRes, linesRes] = await Promise.all([
        axios.get(`/api/audio-clone-projects/${id}`),
        axios.get(`/api/audio-clone-projects/${id}/characters`),
        axios.get(`/api/audio-clone-projects/${id}/lines`),
      ]);
      setProject(projectRes.data);
      setScriptText(projectRes.data?.script_text || "");
      setCharacters(Array.isArray(charactersRes.data) ? charactersRes.data : []);
      const nextLines = Array.isArray(linesRes.data) ? linesRes.data : [];
      setLines(nextLines);
      syncLineDrafts(nextLines);
    } catch (err) {
      console.error(err);
      toast.error("读取 LongChat 项目失败");
    } finally {
      setLoading(false);
    }
  };

  const fetchLines = async () => {
    if (!id) return;
    try {
      const res = await axios.get(`/api/audio-clone-projects/${id}/lines`);
      const nextLines = Array.isArray(res.data) ? res.data : [];
      setLines(nextLines);
      syncLineDrafts(nextLines);
    } catch (err) {
      console.error(err);
    }
  };

  const fetchCharacters = async () => {
    if (!id) return;
    try {
      const res = await axios.get(`/api/audio-clone-projects/${id}/characters`);
      setCharacters(Array.isArray(res.data) ? res.data : []);
    } catch (err) {
      console.error(err);
    }
  };

  useEffect(() => {
    fetchAll();
  }, [id]);

  useEffect(() => {
    if (!lines.some((line) => line.status === "generating")) return;
    const timer = window.setInterval(fetchLines, 2500);
    return () => window.clearInterval(timer);
  }, [lines, id]);

  useEffect(() => {
    if (!characters.some((character) => character.reference_text_status === "generating")) return;
    const timer = window.setInterval(fetchCharacters, 2500);
    return () => window.clearInterval(timer);
  }, [characters, id]);

  useEffect(() => {
    if (!referenceAudio) {
      setReferenceAudioPreview("");
      return;
    }
    const url = URL.createObjectURL(referenceAudio);
    setReferenceAudioPreview(url);
    return () => URL.revokeObjectURL(url);
  }, [referenceAudio]);

  const openCreateCharacter = () => {
    setEditingCharacter(null);
    setCharacterName("");
    setReferenceText("");
    setReferenceAudio(null);
    if (fileInputRef.current) fileInputRef.current.value = "";
    setCharacterDialogOpen(true);
  };

  const openEditCharacter = (character: AudioCloneCharacter) => {
    setEditingCharacter(character);
    setCharacterName(character.name || "");
    setReferenceText(character.reference_text || "");
    setReferenceAudio(null);
    if (fileInputRef.current) fileInputRef.current.value = "";
    setCharacterDialogOpen(true);
  };

  const saveCharacter = async () => {
    if (!id) return;
    if (!characterName.trim()) {
      toast.error("请填写角色名");
      return;
    }
    setSavingCharacter(true);
    try {
      const formData = new FormData();
      formData.append("name", characterName.trim());
      formData.append("reference_text", referenceText.trim());
      if (referenceAudio) {
        formData.append("reference_audio", referenceAudio);
      }
      if (editingCharacter) {
        await axios.put(`/api/audio-clone-characters/${editingCharacter.id}`, formData, {
          headers: { "Content-Type": "multipart/form-data" },
        });
        toast.success("角色声音资产已更新");
      } else {
        await axios.post(`/api/audio-clone-projects/${id}/characters`, formData, {
          headers: { "Content-Type": "multipart/form-data" },
        });
        toast.success("角色声音资产已创建");
      }
      setCharacterDialogOpen(false);
      await fetchAll();
    } catch (err: any) {
      console.error(err);
      toast.error(err.response?.data?.error || "保存角色失败");
    } finally {
      setSavingCharacter(false);
    }
  };

  const deleteCharacter = async () => {
    if (!deleteCharacterTarget) return;
    try {
      await axios.delete(`/api/audio-clone-characters/${deleteCharacterTarget.id}`);
      toast.success("角色声音资产已删除");
      setDeleteCharacterTarget(null);
      await fetchAll();
    } catch (err: any) {
      console.error(err);
      toast.error(err.response?.data?.error || "删除失败");
    }
  };

  const recognizeCharacterReference = async (character: AudioCloneCharacter) => {
    try {
      await axios.post(`/api/audio-clone-characters/${character.id}/recognize-reference`);
      toast.success("参考音频识别任务已提交");
      await fetchCharacters();
    } catch (err: any) {
      console.error(err);
      toast.error(err.response?.data?.error || "提交识别任务失败");
    }
  };

  const saveScriptLines = async () => {
    if (!id) return;
    try {
      const res = await axios.post(`/api/audio-clone-projects/${id}/save-lines`, {
        script_text: scriptText,
      });
      const nextLines = Array.isArray(res.data?.lines) ? res.data.lines : [];
      setLines(nextLines);
      syncLineDrafts(nextLines);
      setProject((prev) => (prev ? { ...prev, script_text: scriptText } : prev));
      toast.success("脚本已解析保存");
    } catch (err: any) {
      console.error(err);
      toast.error(err.response?.data?.error || "保存脚本失败");
    }
  };

  const saveLineParamsDraft = async (line: AudioCloneLine, options?: { silent?: boolean }) => {
    setSavingLineId(line.id);
    try {
      await axios.put(`/api/audio-clone-lines/${line.id}`, {
        text: line.text,
        seed: Number(lineSeedDrafts[line.id] || line.seed || 0),
      });
      if (!options?.silent) {
        toast.success("行参数已保存");
      }
    } catch (err: any) {
      console.error(err);
      toast.error(err.response?.data?.error || "保存行参数失败");
      throw err;
    } finally {
      setSavingLineId(null);
    }
  };

  const saveAllLineParamsDrafts = async () => {
    for (const line of lines) {
      await saveLineParamsDraft(line, { silent: true });
    }
  };

  const generateAll = async (randomSeed = false) => {
    if (!id) return;
    setGenerateMenuOpen(false);
    setGenerating(true);
    try {
      await saveAllLineParamsDrafts();
      const res = await axios.post(`/api/audio-clone-projects/${id}/generate-lines`, {
        script_text: scriptText,
        random_seed: randomSeed,
      });
      toast.success(res.data?.message || "生成任务已提交");
      await fetchAll();
    } catch (err: any) {
      console.error(err);
      const missing = err.response?.data?.missing_characters;
      if (Array.isArray(missing) && missing.length > 0) {
        setMissingItems(missing);
        setMissingDialogOpen(true);
      }
      toast.error(err.response?.data?.error || "提交生成失败");
    } finally {
      setGenerating(false);
    }
  };

  const regenerateLine = async (line: AudioCloneLine, randomSeed = false) => {
    setLineGenerateMenuOpenId(null);
    try {
      await saveLineParamsDraft(line, { silent: true });
      await axios.post(`/api/audio-clone-lines/${line.id}/generate`, {
        random_seed: randomSeed,
      });
      toast.success(randomSeed ? "单行随机 seed 任务已提交" : "单行当前 seed 任务已提交");
      await fetchLines();
    } catch (err: any) {
      console.error(err);
      const missing = err.response?.data?.missing_characters;
      if (Array.isArray(missing) && missing.length > 0) {
        setMissingItems(missing);
        setMissingDialogOpen(true);
      }
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
          <button onClick={() => navigate("/audio-clone-projects")} className="mb-2 flex items-center gap-1 text-sm text-muted-foreground hover:text-foreground">
            <ArrowLeft className="h-4 w-4" />
            返回 LongChat
          </button>
          <h1 className="truncate text-3xl font-bold">{project?.name || "LongChat 项目"}</h1>
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
              <button
                type="button"
                onClick={() => void generateAll(false)}
                className="flex w-full flex-col rounded-md px-3 py-2 text-left text-sm hover:bg-accent"
              >
                <span className="font-medium">使用当前行 seed</span>
                <span className="text-xs text-muted-foreground">按下面每行保存的 Seed 生成。</span>
              </button>
              <button
                type="button"
                onClick={() => void generateAll(true)}
                className="mt-1 flex w-full flex-col rounded-md px-3 py-2 text-left text-sm hover:bg-accent"
              >
                <span className="font-medium">随机 seed 抽卡</span>
                <span className="text-xs text-muted-foreground">本次批量只抽一个统一 Seed，并同步给所有行。</span>
              </button>
            </PopoverContent>
          </Popover>
        </div>
      </div>

      <section className="rounded-xl border bg-card p-4 shadow-sm">
        <div className="mb-3 flex items-center justify-between gap-3">
          <div>
            <h2 className="text-lg font-semibold">角色声音资产</h2>
            <p className="text-xs text-muted-foreground">角色名必须和脚本里的 {"{角色名}"} 完全一致；上传参考音频后可用 ASR 自动识别参考文本。</p>
          </div>
          <button onClick={openCreateCharacter} className="rounded-md border px-3 py-2 text-sm hover:bg-muted">
            <Plus className="mr-1 inline h-4 w-4" />
            新增角色
          </button>
        </div>
        <div className="grid gap-3 md:grid-cols-2">
          {characters.map((character) => (
            <div key={character.id} className="rounded-lg border bg-background/60 p-3">
              <div className="flex items-center justify-between gap-2">
                <div>
                  <h3 className="font-semibold">{character.name}</h3>
                  <p className="text-xs text-muted-foreground">{character.reference_audio ? "已上传参考音频" : "缺少参考音频"}</p>
                </div>
                <div className="flex gap-2">
                  <button onClick={() => openEditCharacter(character)} className="rounded-md border px-2 py-1 text-xs hover:bg-muted">
                    <Pencil className="mr-1 inline h-3 w-3" />
                    编辑
                  </button>
                  <button
                    onClick={() => recognizeCharacterReference(character)}
                    disabled={!character.reference_audio || character.reference_text_status === "generating"}
                    className="rounded-md border px-2 py-1 text-xs hover:bg-muted disabled:opacity-60"
                  >
                    <RefreshCw className="mr-1 inline h-3 w-3" />
                    {character.reference_text_status === "generating" ? "识别中" : "重新识别"}
                  </button>
                  <button onClick={() => setDeleteCharacterTarget(character)} className="rounded-md border px-2 py-1 text-xs text-destructive hover:bg-destructive/10">
                    <Trash2 className="mr-1 inline h-3 w-3" />
                    删除
                  </button>
                </div>
              </div>
              {character.reference_audio ? (
                <audio controls className="mt-3 w-full" src={withAssetVersion(character.reference_audio, character.updated_at)} />
              ) : null}
              <div className="mt-3 rounded-md bg-muted/40 p-2 text-xs leading-relaxed text-muted-foreground">
                <div className="mb-1 flex flex-wrap items-center gap-2">
                  <span className="font-medium text-foreground">参考音频内容</span>
                  <span className="rounded-full bg-background px-2 py-0.5 text-[11px] text-muted-foreground">{character.reference_text_status || "draft"}</span>
                </div>
                {character.reference_text ||
                  (character.reference_text_status === "generating"
                    ? "正在识别参考音频内容，完成后会自动回填到这里。"
                    : "还没有参考音频内容。请先自动识别，或编辑角色后手动填写。")}
                {character.reference_text_error ? <div className="mt-2 text-destructive">{character.reference_text_error}</div> : null}
              </div>
            </div>
          ))}
          {characters.length === 0 ? <div className="rounded-lg border border-dashed p-6 text-center text-muted-foreground">还没有角色声音资产。</div> : null}
        </div>
      </section>

      <section className="rounded-xl border bg-card p-4 shadow-sm">
        <div className="mb-3 flex items-center justify-between gap-3">
          <div>
            <h2 className="text-lg font-semibold">脚本</h2>
            <p className="text-xs text-muted-foreground">格式示例：{"{张继先}"}要说的话。每行一条。</p>
          </div>
          <button onClick={saveScriptLines} className="rounded-md border px-3 py-2 text-sm hover:bg-muted">
            <Save className="mr-1 inline h-4 w-4" />
            保存并解析
          </button>
        </div>
        <Textarea
          rows={14}
          value={scriptText}
          onChange={(e) => setScriptText(e.target.value)}
          placeholder="{张继先}李大人，你已被邪物蛊惑，定海神珠乃镇压之物，落入你手天下必遭浩劫，我愿以身赎罪，绝不交出！&#10;{李四}xxxxxxxxxxxx"
          className="min-h-[360px] resize-y overflow-y-scroll whitespace-pre-wrap font-mono leading-relaxed"
        />
      </section>

      <section className="rounded-xl border bg-card p-4 shadow-sm">
        <h2 className="mb-3 text-lg font-semibold">生成结果</h2>
        <div className="space-y-3">
          {lines.map((line) => (
            <div key={line.id} className="rounded-lg border bg-background/60 p-3">
              <div className="flex flex-wrap items-start justify-between gap-3">
                <div className="min-w-0 flex-1">
                  <div className="flex items-center gap-2">
                    <span className="rounded-full bg-primary/10 px-2 py-0.5 text-xs font-semibold text-primary">第 {line.sort_order} 行</span>
                    <span className="font-semibold">{line.character_name}</span>
                    <span className="rounded-full bg-muted px-2 py-0.5 text-xs text-muted-foreground">{line.status || "draft"}</span>
                  </div>
                  <p className="mt-2 text-sm leading-relaxed">{line.text}</p>
                  {line.last_error ? <p className="mt-2 text-xs text-destructive">{line.last_error}</p> : null}
                </div>
                <Popover open={lineGenerateMenuOpenId === line.id} onOpenChange={(open) => setLineGenerateMenuOpenId(open ? line.id : null)}>
                  <PopoverTrigger asChild>
                    <button
                      disabled={line.status === "generating"}
                      className="rounded-md border px-3 py-2 text-sm hover:bg-muted disabled:opacity-60"
                    >
                      <Play className="mr-1 inline h-4 w-4" />
                      {line.generated_audio ? "重新生成" : "生成"}
                      <ChevronDown className="ml-1 inline h-4 w-4" />
                    </button>
                  </PopoverTrigger>
                  <PopoverContent align="end" className="w-52 p-2">
                    <button
                      type="button"
                      onClick={() => regenerateLine(line, false)}
                      className="flex w-full flex-col rounded-md px-3 py-2 text-left text-sm hover:bg-accent"
                    >
                      <span className="font-medium">使用当前行 seed</span>
                      <span className="text-xs text-muted-foreground">按这一行保存的 Seed 生成。</span>
                    </button>
                    <button
                      type="button"
                      onClick={() => regenerateLine(line, true)}
                      className="mt-1 flex w-full flex-col rounded-md px-3 py-2 text-left text-sm hover:bg-accent"
                    >
                      <span className="font-medium">随机 seed 抽卡</span>
                      <span className="text-xs text-muted-foreground">重新随机生成这一行。</span>
                    </button>
                  </PopoverContent>
                </Popover>
              </div>
              <div className="mt-3 rounded-md border bg-muted/20 p-3">
                <div className="flex flex-wrap items-end gap-3">
                  <div className="w-56">
                    <label className="text-xs font-medium text-muted-foreground">Seed</label>
                    <Input
                      type="number"
                      value={lineSeedDrafts[line.id] ?? line.seed ?? 0}
                      onChange={(event) =>
                        setLineSeedDrafts((prev) => ({
                          ...prev,
                          [line.id]: Number(event.target.value || 0),
                        }))
                      }
                    />
                  </div>
                  <button
                    type="button"
                    onClick={() => void saveLineParamsDraft(line)}
                    disabled={savingLineId === line.id}
                    className="rounded-md border px-3 py-2 text-sm hover:bg-muted disabled:opacity-60"
                  >
                    <Save className="mr-1 inline h-4 w-4" />
                    {savingLineId === line.id ? "保存中..." : "保存参数"}
                  </button>
                  <p className="text-xs text-muted-foreground">LongChat 工作流没有 Temperature / 提示词输入；这里仅支持控制本行 Seed。</p>
                </div>
              </div>
              {line.generated_audio ? (
                <div className="mt-3 flex flex-wrap items-center gap-3 rounded-md bg-muted/30 p-3">
                  <audio controls className="min-w-[260px] flex-1" src={withAssetVersion(line.generated_audio, line.updated_at)} />
                  <a href={line.generated_audio} download className="rounded-md border px-3 py-2 text-sm hover:bg-muted">
                    <Download className="mr-1 inline h-4 w-4" />
                    下载
                  </a>
                </div>
              ) : null}
            </div>
          ))}
          {lines.length === 0 ? <div className="rounded-lg border border-dashed p-8 text-center text-muted-foreground">还没有解析脚本行。</div> : null}
        </div>
      </section>

      <Dialog open={characterDialogOpen} onOpenChange={setCharacterDialogOpen}>
        <DialogContent className="max-w-2xl">
          <DialogHeader>
            <DialogTitle>{editingCharacter ? "编辑角色声音资产" : "新增角色声音资产"}</DialogTitle>
            <DialogDescription>参考音频内容必须对应上传音频里原本说的话；如果留空并上传音频，保存后会自动调用 ASR 识别。</DialogDescription>
          </DialogHeader>
          <div className="space-y-4">
            <div>
              <label className="text-sm font-medium">角色名</label>
              <Input value={characterName} onChange={(e) => setCharacterName(e.target.value)} placeholder="张继先" />
            </div>
            <div>
              <label className="text-sm font-medium">参考音频</label>
              <div
                role="button"
                tabIndex={0}
                onClick={() => fileInputRef.current?.click()}
                onKeyDown={(event) => {
                  if (event.key === "Enter" || event.key === " ") {
                    event.preventDefault();
                    fileInputRef.current?.click();
                  }
                }}
                className="mt-2 rounded-xl border border-dashed bg-muted/30 p-4 transition hover:border-primary/60 hover:bg-primary/5"
              >
                <input
                  ref={fileInputRef}
                  type="file"
                  accept="audio/*,.mp3,.wav,.flac,.m4a"
                  onChange={(e) => setReferenceAudio(e.target.files?.[0] || null)}
                  className="hidden"
                />
                <div className="flex flex-wrap items-center justify-between gap-3">
                  <div className="flex min-w-0 items-center gap-3">
                    <div className="flex h-11 w-11 shrink-0 items-center justify-center rounded-full bg-primary/10 text-primary">
                      <UploadCloud className="h-5 w-5" />
                    </div>
                    <div className="min-w-0">
                      <p className="truncate text-sm font-medium">
                        {referenceAudio?.name || (editingCharacter?.reference_audio ? "已上传参考音频，可点击替换" : "上传参考音频")}
                      </p>
                      <p className="text-xs text-muted-foreground">支持 mp3 / wav / flac / m4a。参考音频内容留空时，保存后会自动识别。</p>
                    </div>
                  </div>
                  <button type="button" className="rounded-md border bg-background px-3 py-2 text-sm hover:bg-muted">
                    {referenceAudio || editingCharacter?.reference_audio ? "更换音频" : "选择音频"}
                  </button>
                </div>
              </div>
              {referenceAudio ? (
                <div className="mt-3 rounded-lg border bg-background/70 p-3">
                  <div className="mb-2 flex items-center justify-between gap-2">
                    <span className="text-xs font-medium text-muted-foreground">本次选择预览</span>
                    <button
                      type="button"
                      onClick={() => {
                        setReferenceAudio(null);
                        if (fileInputRef.current) fileInputRef.current.value = "";
                      }}
                      className="text-xs text-muted-foreground hover:text-destructive"
                    >
                      清除本次选择
                    </button>
                  </div>
                  <audio controls className="w-full" src={referenceAudioPreview} />
                </div>
              ) : editingCharacter?.reference_audio ? (
                <div className="mt-3 rounded-lg border bg-background/70 p-3">
                  <div className="mb-2 text-xs font-medium text-muted-foreground">当前参考音频</div>
                  <audio controls className="w-full" src={withAssetVersion(editingCharacter.reference_audio, editingCharacter.updated_at)} />
                </div>
              ) : null}
            </div>
            <div>
              <label className="text-sm font-medium">参考音频内容</label>
              <Textarea
                rows={5}
                value={referenceText}
                onChange={(e) => setReferenceText(e.target.value)}
                placeholder="可先留空，保存后自动识别；如果识别不准，再手动改成参考音频实际说的话。"
              />
            </div>
          </div>
          <DialogFooter>
            <button onClick={() => setCharacterDialogOpen(false)} className="rounded-md border px-4 py-2 text-sm">
              取消
            </button>
            <button onClick={saveCharacter} disabled={savingCharacter} className="rounded-md bg-primary px-4 py-2 text-sm text-primary-foreground disabled:opacity-60">
              <UploadCloud className="mr-1 inline h-4 w-4" />
              {savingCharacter ? "保存中..." : "保存角色"}
            </button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <Dialog open={missingDialogOpen} onOpenChange={setMissingDialogOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>缺少角色声音资产</DialogTitle>
            <DialogDescription>生成前需要先补齐这些角色，否则无法调用音频复制工作流。</DialogDescription>
          </DialogHeader>
          <div className="space-y-2">
            {missingItems.map((item) => (
              <div key={item.name} className="rounded-md border bg-muted/30 p-3 text-sm">
                <span className="font-semibold">{item.name}</span>
                <span className="ml-2 text-muted-foreground">{item.missing_reason}</span>
              </div>
            ))}
          </div>
          <DialogFooter>
            <button onClick={() => setMissingDialogOpen(false)} className="rounded-md border px-4 py-2 text-sm">
              我知道了
            </button>
            <button
              onClick={() => {
                setMissingDialogOpen(false);
                openCreateCharacter();
              }}
              className="rounded-md bg-primary px-4 py-2 text-sm text-primary-foreground"
            >
              新增角色
            </button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <AlertDialog open={!!deleteCharacterTarget} onOpenChange={(open) => !open && setDeleteCharacterTarget(null)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>删除角色声音资产？</AlertDialogTitle>
            <AlertDialogDescription>会删除这个角色的参考音频文件，但不会自动删除已经生成的音频行。</AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>取消</AlertDialogCancel>
            <AlertDialogAction onClick={deleteCharacter} className="bg-destructive text-destructive-foreground hover:bg-destructive/90">
              删除
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}
