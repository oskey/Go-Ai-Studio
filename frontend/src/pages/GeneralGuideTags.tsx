import { useEffect, useState } from "react";
import axios from "axios";
import { Plus, Trash2, Edit2, RotateCcw, X, Save, Tags, FileText, ArrowDownUp } from "lucide-react";
import type { GeneralGuideTag } from "@/types";
import { toast } from "sonner";

import { Input } from "@/components/ui/input";
import { Textarea } from "@/components/ui/textarea";

export default function GeneralGuideTagsPage() {
  const [tags, setTags] = useState<GeneralGuideTag[]>([]);
  const [loading, setLoading] = useState(false);
  const [isEditing, setIsEditing] = useState(false);
  const [currentTag, setCurrentTag] = useState<Partial<GeneralGuideTag>>({});

  useEffect(() => {
    fetchTags();
  }, []);

  const fetchTags = () => {
    setLoading(true);
    axios
      .get("/api/general-guide-tags")
      .then((res) => {
        setTags(Array.isArray(res.data) ? res.data : []);
      })
      .catch((err) => {
        console.error(err);
        toast.error("获取讲解标签失败");
      })
      .finally(() => setLoading(false));
  };

  const handleSave = () => {
    if (!currentTag.name || !currentTag.description || !currentTag.rules) {
      toast.error("请填写标签名称、说明和规则正文");
      return;
    }

    const payload = {
      ...currentTag,
      sort_order: Number(currentTag.sort_order || 0),
    };

    const req = currentTag.id
      ? axios.put(`/api/general-guide-tags/${currentTag.id}`, payload)
      : axios.post("/api/general-guide-tags", payload);

    req
      .then(() => {
        setIsEditing(false);
        setCurrentTag({});
        fetchTags();
        toast.success(currentTag.id ? "讲解标签已更新" : "讲解标签已创建");
      })
      .catch((err) => {
        console.error(err);
        toast.error(err.response?.data?.error || "保存失败");
      });
  };

  const handleDelete = (id: number) => {
    toast("确定要删除这个讲解标签吗？", {
      action: {
        label: "删除",
        onClick: () => {
          axios
            .delete(`/api/general-guide-tags/${id}`)
            .then(() => {
              fetchTags();
              toast.success("讲解标签已删除");
            })
            .catch((err) => {
              console.error(err);
              toast.error("删除失败");
            });
        },
      },
      cancel: {
        label: "取消",
        onClick: () => {},
      },
    });
  };

  return (
    <div className="space-y-6">
      <div className="flex justify-between items-center">
        <div>
          <h1 className="text-3xl font-bold">讲解标签</h1>
          <p className="mt-1 text-sm text-muted-foreground">
            在这里维护综合讲解的标签规则。勾选后，对应规则会追加到综合讲解场景规划的 LLM 输入里。
          </p>
        </div>
        <div className="flex gap-2">
          <button
            onClick={fetchTags}
            disabled={loading}
            className="flex items-center gap-2 bg-secondary text-secondary-foreground px-4 py-2 rounded-md hover:bg-secondary/80 transition-colors disabled:opacity-50"
          >
            <RotateCcw className={`w-4 h-4 ${loading ? "animate-spin" : ""}`} /> 刷新
          </button>
          <button
            onClick={() => {
              setCurrentTag({ sort_order: tags.length * 10 + 10 });
              setIsEditing(true);
            }}
            className="flex items-center gap-2 bg-primary text-primary-foreground px-4 py-2 rounded-md hover:bg-primary/90 transition-colors"
          >
            <Plus className="w-4 h-4" /> 新增讲解标签
          </button>
        </div>
      </div>

      <div className="max-h-[70vh] overflow-y-auto pr-2">
        <div className="grid grid-cols-1 md:grid-cols-2 xl:grid-cols-3 gap-6">
          {tags.map((tag) => (
            <div key={tag.id} className="bg-card border border-border rounded-lg shadow-sm hover:shadow-md transition-shadow flex flex-col">
              <div className="p-6 flex-1 space-y-4">
                <div className="flex justify-between items-start gap-3">
                  <div className="min-w-0">
                    <h3 className="text-lg font-semibold flex items-center gap-2 truncate" title={tag.name}>
                      <Tags className="w-4 h-4 text-primary" />
                      {tag.name}
                    </h3>
                    <p className="mt-1 text-xs text-muted-foreground">排序：{tag.sort_order}</p>
                  </div>
                  <div className="flex gap-1 shrink-0">
                    <button
                      onClick={() => {
                        setCurrentTag(tag);
                        setIsEditing(true);
                      }}
                      className="p-1 hover:text-blue-400 transition-colors"
                    >
                      <Edit2 className="w-4 h-4" />
                    </button>
                    <button onClick={() => handleDelete(tag.id)} className="p-1 hover:text-destructive transition-colors">
                      <Trash2 className="w-4 h-4" />
                    </button>
                  </div>
                </div>

                <div>
                  <div className="flex items-center gap-1 mb-1 text-xs font-medium uppercase tracking-wider opacity-70">
                    <FileText className="w-3 h-3" /> 说明
                  </div>
                  <p className="text-sm text-muted-foreground line-clamp-3" title={tag.description}>
                    {tag.description}
                  </p>
                </div>

                <div>
                  <div className="flex items-center gap-1 mb-1 text-xs font-medium uppercase tracking-wider opacity-70">
                    <FileText className="w-3 h-3" /> 规则正文
                  </div>
                  <p className="text-sm text-muted-foreground whitespace-pre-wrap line-clamp-6" title={tag.rules}>
                    {tag.rules}
                  </p>
                </div>
              </div>
            </div>
          ))}
        </div>
      </div>

      {isEditing && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50 backdrop-blur-sm p-4">
          <div className="bg-card border border-border rounded-lg w-full max-w-3xl shadow-lg relative animate-in fade-in zoom-in duration-200 max-h-[90vh] flex flex-col">
            <button onClick={() => setIsEditing(false)} className="absolute top-4 right-4 text-muted-foreground hover:text-foreground z-10">
              <X className="w-5 h-5" />
            </button>

            <div className="px-6 pt-6 pb-4 shrink-0 border-b border-border/60">
              <h2 className="text-xl font-bold flex items-center gap-2">
                {currentTag.id ? <Edit2 className="w-5 h-5" /> : <Plus className="w-5 h-5" />}
                {currentTag.id ? "编辑讲解标签" : "新增讲解标签"}
              </h2>
            </div>

            <div className="flex-1 overflow-y-auto px-6 py-4 pr-4">
              <div className="space-y-4">
                <div>
                  <label className="block text-sm font-medium mb-1">标签名称</label>
                  <Input value={currentTag.name || ""} onChange={(e) => setCurrentTag({ ...currentTag, name: e.target.value })} />
                </div>

                <div>
                  <label className="block text-sm font-medium mb-1">标签说明</label>
                  <Textarea
                    value={currentTag.description || ""}
                    onChange={(e) => setCurrentTag({ ...currentTag, description: e.target.value })}
                    className="h-24"
                    autoResize={false}
                  />
                </div>

                <div>
                  <label className="block text-sm font-medium mb-1">规则正文</label>
                  <Textarea
                    value={currentTag.rules || ""}
                    onChange={(e) => setCurrentTag({ ...currentTag, rules: e.target.value })}
                    className="h-[420px] font-mono text-sm"
                    autoResize={false}
                  />
                </div>

                <div>
                  <label className="block text-sm font-medium mb-1">排序</label>
                  <div className="relative">
                    <ArrowDownUp className="absolute left-3 top-1/2 -translate-y-1/2 w-4 h-4 text-muted-foreground" />
                    <Input
                      type="number"
                      value={currentTag.sort_order ?? 0}
                      onChange={(e) => setCurrentTag({ ...currentTag, sort_order: Number(e.target.value || 0) })}
                      className="pl-10"
                    />
                  </div>
                </div>
              </div>
            </div>

            <div className="px-6 py-4 shrink-0 border-t border-border/60 bg-card rounded-b-lg flex justify-end gap-3">
              <button onClick={() => setIsEditing(false)} className="px-4 py-2 rounded-md hover:bg-accent transition-colors">
                取消
              </button>
              <button onClick={handleSave} className="flex items-center gap-2 bg-primary text-primary-foreground px-4 py-2 rounded-md hover:bg-primary/90 transition-colors">
                <Save className="w-4 h-4" /> 保存标签
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}
