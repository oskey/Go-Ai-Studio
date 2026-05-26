import { useEffect, useRef, useState } from "react";
import { useNavigate, useParams } from "react-router-dom";
import axios from "axios";
import { ArrowLeft, ChevronDown, Copy, Download, Pencil, Play, Plus, RefreshCw, Save, Trash2, UploadCloud, Wand2 } from "lucide-react";
import { toast } from "sonner";

import type { QwenTTSCharacter, QwenTTSLine, QwenTTSProject } from "@/types";
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
import { Switch } from "@/components/ui/switch";

const withAssetVersion = (url?: string, version?: string) => {
  const trimmed = (url || "").trim();
  if (!trimmed) return "";
  if (!version) return trimmed;
  return `${trimmed}${trimmed.includes("?") ? "&" : "?"}v=${encodeURIComponent(version)}`;
};

const extractDownloadFilename = (contentDisposition: string | undefined, fallback: string) => {
  if (!contentDisposition) return fallback;
  const utfMatch = contentDisposition.match(/filename\*=UTF-8''([^;]+)/i);
  if (utfMatch?.[1]) {
    try {
      return decodeURIComponent(utfMatch[1]);
    } catch {
      return utfMatch[1];
    }
  }
  const plainMatch = contentDisposition.match(/filename=\"?([^\"]+)\"?/i);
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

const qwenTTSInstructPresets = [
  { label: "开心活泼", value: "开心、明亮、语速自然，带一点轻快的笑意。" },
  { label: "温柔亲切", value: "温柔、亲切、放松，像在认真安慰对方。" },
  { label: "严肃坚定", value: "严肃、坚定、语气有分量，情绪克制。" },
  { label: "愤怒质问", value: "愤怒、压迫感强，语气带质问感。" },
  { label: "紧张急促", value: "紧张、急促，语速略快，情绪明显但不破音。" },
  { label: "古风台词", value: "古风台词感，语气郑重，节奏有停顿。" },
  { label: "广告热情", value: "热情、有感染力，适合促销和推荐。" },
];

interface ImportedScriptItem {
  character_name: string;
  text: string;
  instruct: string;
}

export default function QwenTTSProjectDetail() {
  const { id } = useParams();
  const navigate = useNavigate();
  const [project, setProject] = useState<QwenTTSProject | null>(null);
  const [characters, setCharacters] = useState<QwenTTSCharacter[]>([]);
  const [lines, setLines] = useState<QwenTTSLine[]>([]);
  const [scriptText, setScriptText] = useState("");
  const [instruct, setInstruct] = useState("");
  const [temperature, setTemperature] = useState("0.5");
  const [xVectorOnly, setXVectorOnly] = useState(false);
  const [loading, setLoading] = useState(false);
  const [savingSettings, setSavingSettings] = useState(false);
  const [generating, setGenerating] = useState(false);
  const [exportingArchive, setExportingArchive] = useState(false);
  const [savingScript, setSavingScript] = useState(false);
  const [parsingScript, setParsingScript] = useState(false);
  const [importDialogOpen, setImportDialogOpen] = useState(false);
  const [importSourceText, setImportSourceText] = useState("");
  const [importPromptText, setImportPromptText] = useState("");
  const [importResultText, setImportResultText] = useState("");
  const [buildingImportPrompt, setBuildingImportPrompt] = useState(false);
  const [importingResult, setImportingResult] = useState(false);
  const [importedItems, setImportedItems] = useState<ImportedScriptItem[]>([]);
  const [importedScriptText, setImportedScriptText] = useState("");
  const [generateMenuOpen, setGenerateMenuOpen] = useState(false);
  const [resetMenuOpen, setResetMenuOpen] = useState(false);
  const [resetAllLinesConfirmOpen, setResetAllLinesConfirmOpen] = useState(false);
  const [lineGenerateMenuOpenId, setLineGenerateMenuOpenId] = useState<number | null>(null);
  const [lineInstructDrafts, setLineInstructDrafts] = useState<Record<number, string>>({});
  const [lineTemperatureDrafts, setLineTemperatureDrafts] = useState<Record<number, string>>({});
  const [lineSeedDrafts, setLineSeedDrafts] = useState<Record<number, string>>({});
  const [savingLineId, setSavingLineId] = useState<number | null>(null);

  const [characterDialogOpen, setCharacterDialogOpen] = useState(false);
  const [editingCharacter, setEditingCharacter] = useState<QwenTTSCharacter | null>(null);
  const [characterName, setCharacterName] = useState("");
  const [referenceText, setReferenceText] = useState("");
  const [referenceAudio, setReferenceAudio] = useState<File | null>(null);
  const [referenceAudioPreview, setReferenceAudioPreview] = useState("");
  const [savingCharacter, setSavingCharacter] = useState(false);
  const [deleteCharacterTarget, setDeleteCharacterTarget] = useState<QwenTTSCharacter | null>(null);
  const fileInputRef = useRef<HTMLInputElement | null>(null);

  const [missingDialogOpen, setMissingDialogOpen] = useState(false);
  const [missingItems, setMissingItems] = useState<{ name: string; missing_reason: string }[]>([]);

  const syncLineInstructDrafts = (items: QwenTTSLine[]) => {
    const instructDrafts: Record<number, string> = {};
    const temperatureDrafts: Record<number, string> = {};
    const seedDrafts: Record<number, string> = {};
    items.forEach((line) => {
      instructDrafts[line.id] = line.instruct ?? "";
      temperatureDrafts[line.id] = String(line.temperature || project?.temperature || 0.5);
      seedDrafts[line.id] = String(line.seed || "");
    });
    setLineInstructDrafts(instructDrafts);
    setLineTemperatureDrafts(temperatureDrafts);
    setLineSeedDrafts(seedDrafts);
  };

  const fetchAll = async () => {
    if (!id) return;
    setLoading(true);
    try {
      const [projectRes, charactersRes, linesRes] = await Promise.all([
        axios.get(`/api/qwen-tts-projects/${id}`),
        axios.get(`/api/qwen-tts-projects/${id}/characters`),
        axios.get(`/api/qwen-tts-projects/${id}/lines`),
      ]);
      setProject(projectRes.data);
      setScriptText(projectRes.data?.script_text || "");
      setImportedItems([]);
      setImportedScriptText("");
      setInstruct(projectRes.data?.instruct || "");
      setTemperature(String(projectRes.data?.temperature || 0.5));
      setXVectorOnly(Boolean(projectRes.data?.x_vector_only));
      setCharacters(Array.isArray(charactersRes.data) ? charactersRes.data : []);
      const nextLines = Array.isArray(linesRes.data) ? linesRes.data : [];
      setLines(nextLines);
      syncLineInstructDrafts(nextLines);
    } catch (err) {
      console.error(err);
      toast.error("读取 Qwen3 TTS 项目失败");
    } finally {
      setLoading(false);
    }
  };

  const fetchLines = async () => {
    if (!id) return;
    try {
      const res = await axios.get(`/api/qwen-tts-projects/${id}/lines`);
      const nextLines = Array.isArray(res.data) ? res.data : [];
      setLines(nextLines);
      syncLineInstructDrafts(nextLines);
    } catch (err) {
      console.error(err);
    }
  };

  const fetchCharacters = async () => {
    if (!id) return;
    try {
      const res = await axios.get(`/api/qwen-tts-projects/${id}/characters`);
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

  const saveProjectSettings = async () => {
    if (!project) return;
    setSavingSettings(true);
    try {
      const payload = {
        name: project.name,
        code: project.code,
        description: project.description || "",
        script_text: scriptText,
        instruct: instruct.trim(),
        temperature: Number(temperature) || 0.5,
        x_vector_only: xVectorOnly,
      };
      const res = await axios.put(`/api/qwen-tts-projects/${project.id}`, payload);
      setProject((prev) =>
        prev
          ? {
              ...prev,
              instruct: payload.instruct,
              temperature: payload.temperature,
              x_vector_only: payload.x_vector_only,
              script_text: payload.script_text,
              updated_at: res.data?.updated_at || prev.updated_at,
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

  const applyTopParamsToLineDrafts = () => {
    if (lines.length === 0) {
      toast.info("下方还没有可赋值的台词行");
      return;
    }
    const temperatureValue = temperature.trim() || "0.5";
    const nextInstructs: Record<number, string> = {};
    const nextTemperatures: Record<number, string> = {};
    lines.forEach((line) => {
      nextInstructs[line.id] = instruct;
      nextTemperatures[line.id] = temperatureValue;
    });
    setLineInstructDrafts((prev) => ({ ...prev, ...nextInstructs }));
    setLineTemperatureDrafts((prev) => ({ ...prev, ...nextTemperatures }));
    toast.success("已把顶部提示词和 Temperature 填入下方所有行");
  };

  const openCreateCharacter = () => {
    setEditingCharacter(null);
    setCharacterName("");
    setReferenceText("");
    setReferenceAudio(null);
    if (fileInputRef.current) fileInputRef.current.value = "";
    setCharacterDialogOpen(true);
  };

  const openEditCharacter = (character: QwenTTSCharacter) => {
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
        await axios.put(`/api/qwen-tts-characters/${editingCharacter.id}`, formData, {
          headers: { "Content-Type": "multipart/form-data" },
        });
        toast.success("角色声音资产已更新");
      } else {
        await axios.post(`/api/qwen-tts-projects/${id}/characters`, formData, {
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
      await axios.delete(`/api/qwen-tts-characters/${deleteCharacterTarget.id}`);
      toast.success("角色声音资产已删除");
      setDeleteCharacterTarget(null);
      await fetchAll();
    } catch (err: any) {
      console.error(err);
      toast.error(err.response?.data?.error || "删除失败");
    }
  };

  const recognizeCharacterReference = async (character: QwenTTSCharacter) => {
    try {
      await axios.post(`/api/qwen-tts-characters/${character.id}/recognize-reference`);
      toast.success("参考音频识别任务已提交");
      await fetchCharacters();
    } catch (err: any) {
      console.error(err);
      toast.error(err.response?.data?.error || "提交识别任务失败");
    }
  };

  const saveScriptText = async () => {
    if (!id) return;
    setSavingScript(true);
    try {
      await axios.post(`/api/qwen-tts-projects/${id}/save-script`, {
        script_text: scriptText,
      });
      setProject((prev) => (prev ? { ...prev, script_text: scriptText } : prev));
      toast.success("脚本已保存");
    } catch (err: any) {
      console.error(err);
      toast.error(err.response?.data?.error || "保存脚本失败");
    } finally {
      setSavingScript(false);
    }
  };

  const parseScriptLines = async () => {
    if (!id) return;
    setParsingScript(true);
    try {
      const res = await axios.post(`/api/qwen-tts-projects/${id}/save-lines`, {
        script_text: scriptText,
        imported_items: importedItems,
      });
      const nextLines = Array.isArray(res.data?.lines) ? res.data.lines : [];
      const missing = Array.isArray(res.data?.missing_characters) ? res.data.missing_characters : [];
      setLines(nextLines);
      syncLineInstructDrafts(nextLines);
      setProject((prev) => (prev ? { ...prev, script_text: scriptText } : prev));
      if (missing.length > 0) {
        setMissingItems(missing);
        setMissingDialogOpen(true);
        toast.success("脚本已解析，并检测到待补齐的角色声音资产");
      } else {
        toast.success("脚本已解析");
      }
    } catch (err: any) {
      console.error(err);
      toast.error(err.response?.data?.error || "解析脚本失败");
    } finally {
      setParsingScript(false);
    }
  };

  const openImportDialog = () => {
    setImportDialogOpen(true);
  };

  const buildImportPrompt = async () => {
    if (!id) return;
    const sourceText = importSourceText.trim();
    if (!sourceText) {
      toast.error("请先输入小说原文");
      return;
    }
    setBuildingImportPrompt(true);
    try {
      const res = await axios.post(`/api/qwen-tts-projects/${id}/import-prompt`, {
        source_text: sourceText,
      });
      const nextContent = String(res.data?.content || "").trim();
      setImportPromptText(nextContent);
      toast.success("可复制内容已生成");
    } catch (err: any) {
      console.error(err);
      toast.error(err.response?.data?.error || "生成导入内容失败");
    } finally {
      setBuildingImportPrompt(false);
    }
  };

  const copyImportPrompt = async () => {
    const content = importPromptText.trim();
    if (!content) {
      toast.error("请先生成可复制内容");
      return;
    }
    try {
      await navigator.clipboard.writeText(content);
      toast.success("已复制到剪贴板");
    } catch (err) {
      console.error(err);
      toast.error("复制失败，请手动复制下方文本");
    }
  };

  const importThirdPartyResult = async () => {
    if (!id) return;
    const content = importResultText.trim();
    if (!content) {
      toast.error("请先粘贴第三方 AI 返回的 JSON");
      return;
    }
    setImportingResult(true);
    try {
      const res = await axios.post(`/api/qwen-tts-projects/${id}/import-result`, {
        content,
      });
      const nextScriptText = String(res.data?.script_text || "").trim();
      const nextItems = Array.isArray(res.data?.items) ? (res.data.items as ImportedScriptItem[]) : [];
      if (!nextScriptText || nextItems.length === 0) {
        toast.error("导入结果里没有可用内容");
        return;
      }
      setScriptText(nextScriptText);
      setImportedItems(nextItems);
      setImportedScriptText(nextScriptText);
      setImportDialogOpen(false);
      toast.success(`已导入 ${res.data?.count || nextItems.length} 条到脚本区，点击“解析”即可写入台词和 instruct`);
    } catch (err: any) {
      console.error(err);
      toast.error(err.response?.data?.error || "导入返回结果失败");
    } finally {
      setImportingResult(false);
    }
  };

  const handleScriptTextChange = (value: string) => {
    setScriptText(value);
    if (importedItems.length > 0 && value !== importedScriptText) {
      setImportedItems([]);
      setImportedScriptText("");
    }
  };

  const saveLineInstructDraft = async (line: QwenTTSLine) => {
    const instructDraft = lineInstructDrafts[line.id] ?? "";
    const temperatureDraft = Number(lineTemperatureDrafts[line.id]) || Number(temperature) || 0.5;
    const seedDraft = Number.parseInt(lineSeedDrafts[line.id] || "0", 10) || 0;
    if (
      (line.instruct ?? "") === instructDraft &&
      Number(line.temperature || 0) === temperatureDraft &&
      Number(line.seed || 0) === seedDraft
    ) {
      return;
    }
    await axios.put(`/api/qwen-tts-lines/${line.id}`, {
      text: line.text,
      instruct: instructDraft,
      temperature: temperatureDraft,
      seed: seedDraft,
    });
  };

  const generateAll = async (randomSeed = false) => {
    if (!id) return;
    setGenerateMenuOpen(false);
    setGenerating(true);
    try {
      for (const line of lines) {
        await saveLineInstructDraft(line);
      }
      const res = await axios.post(`/api/qwen-tts-projects/${id}/generate-lines`, {
        script_text: scriptText,
        random_seed: randomSeed,
        imported_items: importedItems,
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

  const regenerateLine = async (line: QwenTTSLine, randomSeed = false) => {
    setLineGenerateMenuOpenId(null);
    try {
      await saveLineInstructDraft(line);
      await axios.post(`/api/qwen-tts-lines/${line.id}/generate`, {
        random_seed: randomSeed,
      });
      toast.success(randomSeed ? "单行随机 seed 任务已提交" : "单行任务已提交");
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

  const resetProjectLineStates = async () => {
    if (!id) return;
    setResetMenuOpen(false);
    try {
      await axios.post(`/api/qwen-tts-projects/${id}/reset-line-states`);
      toast.success("全部行状态已重置，可以重新生成全部");
      await fetchLines();
    } catch (err: any) {
      console.error(err);
      toast.error(err.response?.data?.error || "重置全部状态失败");
    }
  };

  const resetProjectLines = async () => {
    if (!id) return;
    try {
      await axios.post(`/api/qwen-tts-projects/${id}/reset-lines`);
      setLines([]);
      syncLineInstructDrafts([]);
      toast.success("下方所有行已清空，脚本内容已保留");
      await fetchLines();
    } catch (err: any) {
      console.error(err);
      toast.error(err.response?.data?.error || "全部重置失败");
    } finally {
      setResetAllLinesConfirmOpen(false);
    }
  };

  const resetLineState = async (line: QwenTTSLine) => {
    try {
      await axios.post(`/api/qwen-tts-lines/${line.id}/reset-state`);
      toast.success("本行状态已重置，可以重新生成");
      await fetchLines();
    } catch (err: any) {
      console.error(err);
      toast.error(err.response?.data?.error || "重置本行状态失败");
    }
  };

  const interruptGeneration = async () => {
    if (!id) return;
    try {
      await axios.post(`/api/qwen-tts-projects/${id}/interrupt-generation`);
      toast.success("已停止当前 Qwen3 TTS 生成");
      await fetchLines();
    } catch (err: any) {
      console.error(err);
      toast.error(err.response?.data?.error || "停止生成失败");
    }
  };

  const exportArchive = async () => {
    if (!project) return;
    setExportingArchive(true);
    try {
      const res = await axios.post(`/api/qwen-tts-projects/${project.id}/export`, {}, {
        responseType: "blob",
      });
      const filename = extractDownloadFilename(
        res.headers["content-disposition"],
        `${project.code || "qwen_tts"}_export.zip`,
      );
      triggerBlobDownload(res.data, filename);
      toast.success("导出压缩包已开始下载");
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
      toast.error(message);
    } finally {
      setExportingArchive(false);
    }
  };

  const saveLineInstruct = async (line: QwenTTSLine) => {
    setSavingLineId(line.id);
    try {
      await saveLineInstructDraft(line);
      toast.success("本行提示词已保存");
      await fetchLines();
    } catch (err: any) {
      console.error(err);
      toast.error(err.response?.data?.error || "保存本行提示词失败");
    } finally {
      setSavingLineId(null);
    }
  };

  if (!project && loading) {
    return <div className="p-6 text-muted-foreground">加载中...</div>;
  }

  return (
    <div className="space-y-6">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div className="min-w-0">
          <button onClick={() => navigate("/qwen-tts-projects")} className="mb-2 flex items-center gap-1 text-sm text-muted-foreground hover:text-foreground">
            <ArrowLeft className="h-4 w-4" />
            返回 Qwen3 TTS
          </button>
          <h1 className="truncate text-3xl font-bold">{project?.name || "Qwen3 TTS 项目"}</h1>
          <p className="mt-1 text-sm text-muted-foreground">文件名：{project?.code}</p>
        </div>
        <div className="flex flex-wrap gap-2">
          <button onClick={fetchAll} className="rounded-md border px-3 py-2 text-sm hover:bg-muted">
            <RefreshCw className="mr-1 inline h-4 w-4" />
            刷新
          </button>
          <button onClick={exportArchive} disabled={exportingArchive} className="rounded-md border px-3 py-2 text-sm hover:bg-muted disabled:opacity-60">
            <Download className="mr-1 inline h-4 w-4" />
            {exportingArchive ? "导出中..." : "导出"}
          </button>
          <button
            onClick={interruptGeneration}
            disabled={!lines.some((line) => line.status === "generating")}
            className="rounded-md border px-3 py-2 text-sm hover:bg-muted disabled:opacity-60"
          >
            停止本次生成
          </button>
          <Popover open={resetMenuOpen} onOpenChange={setResetMenuOpen}>
            <PopoverTrigger asChild>
              <button className="rounded-md border px-3 py-2 text-sm hover:bg-muted">
                <RefreshCw className="mr-1 inline h-4 w-4" />
                重置
                <ChevronDown className="ml-1 inline h-4 w-4" />
              </button>
            </PopoverTrigger>
            <PopoverContent align="end" className="w-56 p-2">
              <button type="button" onClick={() => void resetProjectLineStates()} className="flex w-full flex-col rounded-md px-3 py-2 text-left text-sm hover:bg-accent">
                <span className="font-medium">重置状态</span>
                <span className="text-xs text-muted-foreground">保留所有行和音频，只清掉生成中/错误状态。</span>
              </button>
              <button
                type="button"
                onClick={() => {
                  setResetMenuOpen(false);
                  setResetAllLinesConfirmOpen(true);
                }}
                className="mt-1 flex w-full flex-col rounded-md px-3 py-2 text-left text-sm text-destructive hover:bg-destructive/10"
              >
                <span className="font-medium">全部重置</span>
                <span className="text-xs text-muted-foreground">清空下方所有行和生成音频，但保留脚本。</span>
              </button>
            </PopoverContent>
          </Popover>
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
                <span className="font-medium">使用当前行 seed</span>
                <span className="text-xs text-muted-foreground">使用每一行当前保存的 Seed。</span>
              </button>
              <button type="button" onClick={() => void generateAll(true)} className="mt-1 flex w-full flex-col rounded-md px-3 py-2 text-left text-sm hover:bg-accent">
                <span className="font-medium">随机 seed 抽卡</span>
                <span className="text-xs text-muted-foreground">本次批量只抽一个 Seed，并同步给所有行。</span>
              </button>
            </PopoverContent>
          </Popover>
        </div>
      </div>

      <section className="rounded-xl border bg-card p-4 shadow-sm">
        <div className="mb-3 flex flex-wrap items-center justify-between gap-3">
          <div>
            <h2 className="text-lg font-semibold">Qwen3 生成参数</h2>
            <p className="text-xs text-muted-foreground">instruct 会影响表达方向；temperature 会明显影响随机性和声音表现。</p>
          </div>
          <div className="flex flex-wrap gap-2">
            <button type="button" onClick={applyTopParamsToLineDrafts} className="rounded-md border px-3 py-2 text-sm hover:bg-muted">
              批量填入下方行
            </button>
            <button onClick={saveProjectSettings} disabled={savingSettings} className="rounded-md border px-3 py-2 text-sm hover:bg-muted disabled:opacity-60">
              <Save className="mr-1 inline h-4 w-4" />
              {savingSettings ? "保存中..." : "保存参数"}
            </button>
          </div>
        </div>
        <div className="grid gap-3 md:grid-cols-[1fr_180px]">
          <div>
            <div className="mb-2 flex flex-wrap items-center justify-between gap-2">
              <label className="text-sm font-medium">提示词 instruct（可选）</label>
              <select
                value=""
                onChange={(e) => {
                  const value = e.target.value;
                  if (!value) return;
                  setInstruct(value);
                }}
                className="min-w-48 rounded-md border bg-background px-3 py-2 text-sm"
              >
                <option value="">选择预设填入...</option>
                {qwenTTSInstructPresets.map((item) => (
                  <option key={item.label} value={item.value}>
                    {item.label}
                  </option>
                ))}
              </select>
            </div>
            <Textarea
              rows={3}
              value={instruct}
              onChange={(e) => setInstruct(e.target.value)}
              placeholder="例如：语气更激动、更有压迫感，保持角色声音特征。"
            />
          </div>
          <div>
            <label className="text-sm font-medium">Temperature</label>
            <Input type="number" step="0.05" min="0.1" max="2" value={temperature} onChange={(e) => setTemperature(e.target.value)} />
            <p className="mt-1 text-xs text-muted-foreground">默认 0.5</p>
            <div className="mt-3 rounded-md border bg-muted/20 p-3">
              <div className="flex items-center justify-between gap-3">
                <div className="min-w-0">
                  <div className="text-sm font-medium">仅用声纹（x_vector_only）</div>
                  <p className="text-xs text-muted-foreground">
                    开启后主要依赖 speaker embedding，参考文本影响会弱很多；更省事，但官方说明克隆质量可能下降。
                  </p>
                </div>
                <Switch checked={xVectorOnly} onCheckedChange={setXVectorOnly} />
              </div>
            </div>
          </div>
        </div>
      </section>

      <section className="rounded-xl border bg-card p-4 shadow-sm">
        <div className="mb-3 flex items-center justify-between gap-3">
          <div>
            <h2 className="text-lg font-semibold">角色声音资产</h2>
            <p className="text-xs text-muted-foreground">上传参考音频后会用专用 ASR 工作流自动识别参考文本；你确认或修改后，正式生成会直接复用这段文本。</p>
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
              {character.reference_audio ? <audio controls className="mt-3 w-full" src={withAssetVersion(character.reference_audio, character.updated_at)} /> : null}
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
            <p className="text-xs text-muted-foreground">格式示例：{"{张继先}"}要说的话。保存只保存上方脚本；解析会更新下方台词行并提示缺少的角色声音资产。</p>
          </div>
          <div className="flex flex-wrap gap-2">
            <button onClick={openImportDialog} className="rounded-md border px-3 py-2 text-sm hover:bg-muted">
              <Copy className="mr-1 inline h-4 w-4" />
              导入
            </button>
            <button onClick={saveScriptText} disabled={savingScript} className="rounded-md border px-3 py-2 text-sm hover:bg-muted disabled:opacity-60">
              <Save className="mr-1 inline h-4 w-4" />
              {savingScript ? "保存中..." : "保存"}
            </button>
            <button onClick={parseScriptLines} disabled={parsingScript} className="rounded-md border px-3 py-2 text-sm hover:bg-muted disabled:opacity-60">
              <Wand2 className="mr-1 inline h-4 w-4" />
              {parsingScript ? "解析中..." : "解析"}
            </button>
          </div>
        </div>
        <Textarea
          rows={14}
          value={scriptText}
          onChange={(e) => handleScriptTextChange(e.target.value)}
          placeholder="{张继先}李大人，你已被邪物蛊惑……&#10;{李四}xxxxxxxxxxxx"
          className="min-h-[360px] resize-y overflow-y-scroll whitespace-pre-wrap font-mono leading-relaxed"
        />
        {importedItems.length > 0 ? (
          <p className="mt-2 text-xs text-muted-foreground">
            已载入第三方 AI 返回的 {importedItems.length} 条 instruct。点击“解析”时会一并写入下方台词行；如果你手动修改脚本，这批 instruct 会自动失效。
          </p>
        ) : null}
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
                  <div className="mt-3 rounded-md border bg-muted/20 p-3">
                    <div className="mb-2 flex flex-wrap items-center justify-between gap-2">
                      <span className="text-xs font-semibold text-muted-foreground">本行提示词 instruct</span>
                      <button
                        type="button"
                        onClick={() => saveLineInstruct(line)}
                        disabled={savingLineId === line.id}
                        className="rounded-md border bg-background px-2 py-1 text-xs hover:bg-muted disabled:opacity-60"
                      >
                        <Save className="mr-1 inline h-3 w-3" />
                        {savingLineId === line.id ? "保存中..." : "保存提示词"}
                      </button>
                    </div>
                    <Textarea
                      rows={2}
                      value={lineInstructDrafts[line.id] ?? ""}
                      onChange={(e) => setLineInstructDrafts((prev) => ({ ...prev, [line.id]: e.target.value }))}
                      placeholder="留空则这一行传空 instruct；需要默认值时可从顶部默认提示词或预设复制。"
                    />
                    <div className="mt-2 grid gap-2 md:grid-cols-2">
                      <div>
                        <label className="text-[11px] font-medium text-muted-foreground">本行 Temperature</label>
                        <Input
                          type="number"
                          step="0.05"
                          min="0.1"
                          max="2"
                          value={lineTemperatureDrafts[line.id] ?? String(line.temperature || temperature || 0.5)}
                          onChange={(e) => setLineTemperatureDrafts((prev) => ({ ...prev, [line.id]: e.target.value }))}
                        />
                      </div>
                      <div>
                        <label className="text-[11px] font-medium text-muted-foreground">本行 Seed</label>
                        <Input
                          type="number"
                          min="1"
                          max="2147483647"
                          value={lineSeedDrafts[line.id] ?? String(line.seed || "")}
                          onChange={(e) => setLineSeedDrafts((prev) => ({ ...prev, [line.id]: e.target.value }))}
                          placeholder="默认使用系统 seed"
                        />
                      </div>
                    </div>
                    <p className="mt-1 text-[11px] text-muted-foreground">可以按这一句单独控制语气、情绪、随机性和温度；清空提示词会给这一行传空 instruct。</p>
                  </div>
                  {line.last_error ? <p className="mt-2 text-xs text-destructive">{line.last_error}</p> : null}
                </div>
                <Popover open={lineGenerateMenuOpenId === line.id} onOpenChange={(open) => setLineGenerateMenuOpenId(open ? line.id : null)}>
                  <PopoverTrigger asChild>
                    <button disabled={line.status === "generating"} className="rounded-md border px-3 py-2 text-sm hover:bg-muted disabled:opacity-60">
                      <Play className="mr-1 inline h-4 w-4" />
                      {line.generated_audio ? "重新生成" : "生成"}
                      <ChevronDown className="ml-1 inline h-4 w-4" />
                    </button>
                  </PopoverTrigger>
                  <PopoverContent align="end" className="w-52 p-2">
                    <button type="button" onClick={() => regenerateLine(line, false)} className="flex w-full flex-col rounded-md px-3 py-2 text-left text-sm hover:bg-accent">
                      <span className="font-medium">使用当前行 seed</span>
                      <span className="text-xs text-muted-foreground">使用这一行当前保存的 Seed。</span>
                    </button>
                    <button type="button" onClick={() => regenerateLine(line, true)} className="mt-1 flex w-full flex-col rounded-md px-3 py-2 text-left text-sm hover:bg-accent">
                      <span className="font-medium">随机 seed 抽卡</span>
                      <span className="text-xs text-muted-foreground">重新随机生成这一行。</span>
                    </button>
                  </PopoverContent>
                </Popover>
                <button
                  type="button"
                  onClick={() => void resetLineState(line)}
                  className="rounded-md border px-3 py-2 text-sm hover:bg-muted"
                >
                  <RefreshCw className="mr-1 inline h-4 w-4" />
                  重置状态
                </button>
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
            <DialogDescription>参考音频内容会在上传后自动识别并回填；如果识别不准，你可以在这里手动覆盖，保存后不会再自动调用 ASR。</DialogDescription>
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
                      <p className="text-xs text-muted-foreground">支持 mp3 / wav / flac / m4a。留空参考文本时，会自动调用专用 ASR 工作流识别。</p>
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
              <label className="text-sm font-medium">参考音频内容覆盖（可选）</label>
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

      <Dialog open={importDialogOpen} onOpenChange={setImportDialogOpen}>
        <DialogContent className="flex max-h-[85vh] max-w-4xl flex-col overflow-hidden">
          <DialogHeader>
            <DialogTitle>导入</DialogTitle>
            <DialogDescription>先生成提示词发给第三方 AI，再把它返回的 JSON 粘贴到下面。系统会自动转成脚本区格式，并把 instruct 保留下来用于后续解析。</DialogDescription>
          </DialogHeader>
          <div className="flex-1 space-y-4 overflow-y-auto pr-1">
            <div>
              <div className="mb-2 text-sm font-medium">小说原文</div>
              <Textarea
                rows={12}
                value={importSourceText}
                onChange={(e) => {
                  setImportSourceText(e.target.value);
                  setImportPromptText("");
                }}
                placeholder="请粘贴小说原文、剧本原文或对白原文。"
                className="min-h-[220px] w-full resize-y font-mono text-sm leading-relaxed"
              />
            </div>
            <div>
              <div className="mb-2 flex items-center justify-between gap-2">
                <span className="text-sm font-medium">可复制内容</span>
                <button
                  type="button"
                  onClick={() => void copyImportPrompt()}
                  disabled={!importPromptText.trim()}
                  className="rounded-md border px-3 py-2 text-sm hover:bg-muted disabled:opacity-60"
                >
                  <Copy className="mr-1 inline h-4 w-4" />
                  复制
                </button>
              </div>
              <Textarea
                rows={16}
                value={importPromptText}
                readOnly
                placeholder="点击下方“生成可复制内容”后，这里会显示完整提示词文本。"
                className="min-h-[320px] w-full resize-y font-mono text-xs leading-relaxed"
              />
            </div>
            <div>
              <div className="mb-2 text-sm font-medium">第三方 AI 返回 JSON</div>
              <Textarea
                rows={14}
                value={importResultText}
                onChange={(e) => setImportResultText(e.target.value)}
                placeholder={'请把第三方 AI 返回的 JSON 粘贴到这里，例如：\n{\n  "total": 98,\n  "items": [\n    {\n      "character_name": "旁白",\n      "text": "邛海沉城：青龙泣血报孝恩",\n      "instruct": "语气沉稳庄重，字字清晰，如评书开场，节奏稍缓，带史诗感。"\n    }\n  ]\n}'}
                className="min-h-[280px] w-full resize-y font-mono text-xs leading-relaxed"
              />
            </div>
          </div>
          <DialogFooter className="mt-4 shrink-0 border-t pt-4">
            <button
              type="button"
              onClick={() => setImportDialogOpen(false)}
              className="rounded-md border px-4 py-2 text-sm"
            >
              关闭
            </button>
            <button
              type="button"
              onClick={() => void buildImportPrompt()}
              disabled={buildingImportPrompt}
              className="rounded-md border px-4 py-2 text-sm hover:bg-muted disabled:opacity-60"
            >
              {buildingImportPrompt ? "生成中..." : "生成可复制内容"}
            </button>
            <button
              type="button"
              onClick={() => void importThirdPartyResult()}
              disabled={importingResult}
              className="rounded-md bg-primary px-4 py-2 text-sm text-primary-foreground disabled:opacity-60"
            >
              {importingResult ? "导入中..." : "导入返回结果"}
            </button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <Dialog open={missingDialogOpen} onOpenChange={setMissingDialogOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>缺少角色声音资产</DialogTitle>
            <DialogDescription>脚本里这些角色还没补齐声音资产。现在只是提醒；正式生成前需要先补齐。</DialogDescription>
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

      <AlertDialog open={resetAllLinesConfirmOpen} onOpenChange={setResetAllLinesConfirmOpen}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>全部重置 Qwen3 TTS 行？</AlertDialogTitle>
            <AlertDialogDescription>
              会清空下方已经解析出来的所有台词行和生成音频，但不会清空上方脚本内容。确认后需要重新保存/解析脚本才能生成新的行。
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>取消</AlertDialogCancel>
            <AlertDialogAction onClick={() => void resetProjectLines()} className="bg-destructive text-destructive-foreground hover:bg-destructive/90">
              全部重置
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>

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
