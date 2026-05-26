import { useEffect, useState } from "react";
import axios from "axios";
import { Plus, Trash2, Edit2, RotateCcw, X, Save, Palette, FileText } from "lucide-react";
import type { ArtStyle } from "@/types";
import { toast } from "sonner";

import { Input } from "@/components/ui/input";
import { Textarea } from "@/components/ui/textarea";

export default function Styles() {
    const [styles, setStyles] = useState<ArtStyle[]>([]);
    const [loading, setLoading] = useState(false);
    const [isEditing, setIsEditing] = useState(false);
    const [currentStyle, setCurrentStyle] = useState<Partial<ArtStyle>>({});

    useEffect(() => {
        fetchStyles();
    }, []);

    const fetchStyles = () => {
        setLoading(true);
        axios.get("/api/styles")
            .then(res => {
                setStyles(res.data);
            })
            .catch(err => {
                console.error(err);
                toast.error("获取画风失败");
            })
            .finally(() => setLoading(false));
    };

    const handleSave = () => {
        if (!currentStyle.name || !currentStyle.description) {
            toast.error("请填写所有必填字段");
            return;
        }

        const payload = {
            ...currentStyle,
        };

        const req = currentStyle.id
            ? axios.put(`/api/styles/${currentStyle.id}`, payload)
            : axios.post("/api/styles", payload);

        req.then(() => {
            setIsEditing(false);
            setCurrentStyle({});
            fetchStyles();
            toast.success(currentStyle.id ? "画风已更新" : "画风已创建");
        }).catch(err => {
            console.error(err);
            toast.error("保存失败");
        });
    };

    const handleDelete = (id: number) => {
        toast("确定要删除这个画风吗？", {
            action: {
                label: "删除",
                onClick: () => {
                    axios.delete(`/api/styles/${id}`)
                        .then(() => {
                            fetchStyles();
                            toast.success("画风已删除");
                        })
                        .catch(err => {
                            console.error(err);
                            toast.error("删除失败");
                        });
                }
            },
            cancel: {
                label: "取消",
                onClick: () => {},
            }
        });
    };

    return (
        <div className="space-y-6">
            <div className="flex justify-between items-center">
                <h1 className="text-3xl font-bold">画风管理</h1>
                <div className="flex gap-2">
                    <button
                        onClick={fetchStyles}
                        disabled={loading}
                        className="flex items-center gap-2 bg-secondary text-secondary-foreground px-4 py-2 rounded-md hover:bg-secondary/80 transition-colors disabled:opacity-50"
                    >
                        <RotateCcw className={`w-4 h-4 ${loading ? "animate-spin" : ""}`} /> 刷新
                    </button>
                    <button
                        onClick={() => {
                            setCurrentStyle({});
                            setIsEditing(true);
                        }}
                        className="flex items-center gap-2 bg-primary text-primary-foreground px-4 py-2 rounded-md hover:bg-primary/90 transition-colors"
                    >
                        <Plus className="w-4 h-4" /> 新增画风
                    </button>
                </div>
            </div>

            <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4 gap-6">
                {styles.map(style => (
                    <div key={style.id} className="bg-card border border-border rounded-lg shadow-sm hover:shadow-md transition-shadow flex flex-col">
                        <div className="p-6 flex-1">
                            <div className="flex justify-between items-start mb-4">
                                <h3 className="text-lg font-semibold flex items-center gap-2 truncate" title={style.name}>
                                    <Palette className="w-4 h-4 text-primary" />
                                    {style.name}
                                </h3>
                                <div className="flex gap-1 shrink-0">
                                    <button
                                        onClick={() => {
                                            setCurrentStyle(style);
                                            setIsEditing(true);
                                        }}
                                        className="p-1 hover:text-blue-400 transition-colors"
                                    >
                                        <Edit2 className="w-4 h-4" />
                                    </button>
                                    <button
                                        onClick={() => handleDelete(style.id)}
                                        className="p-1 hover:text-destructive transition-colors"
                                    >
                                        <Trash2 className="w-4 h-4" />
                                    </button>
                                </div>
                            </div>
                            
                            <div className="space-y-4 text-sm text-muted-foreground">
                                <div>
                                    <div className="flex items-center gap-1 mb-1 text-xs font-medium uppercase tracking-wider opacity-70">
                                        <FileText className="w-3 h-3" /> 描述
                                    </div>
                                    <p className="line-clamp-3" title={style.description}>
                                        {style.description}
                                    </p>
                                </div>
                            </div>
                        </div>
                    </div>
                ))}
            </div>

            {/* Modal */}
            {isEditing && (
                <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50 backdrop-blur-sm p-4">
                    <div className="bg-card border border-border rounded-lg p-6 w-full max-w-2xl shadow-lg relative animate-in fade-in zoom-in duration-200 max-h-[90vh] overflow-y-auto">
                        <button
                            onClick={() => setIsEditing(false)}
                            className="absolute top-4 right-4 text-muted-foreground hover:text-foreground"
                        >
                            <X className="w-5 h-5" />
                        </button>
                        
                        <h2 className="text-xl font-bold mb-6 flex items-center gap-2">
                            {currentStyle.id ? <Edit2 className="w-5 h-5" /> : <Plus className="w-5 h-5" />}
                            {currentStyle.id ? "编辑画风" : "新增画风"}
                        </h2>
                        
                        <div className="space-y-4">
                            <div>
                                <label className="block text-sm font-medium mb-1">画风名称</label>
                                <Input
                                    value={currentStyle.name || ""}
                                    onChange={e => setCurrentStyle({ ...currentStyle, name: e.target.value })}
                                    placeholder="例如: 国风仙侠 · 写实"
                                />
                            </div>

                            <div>
                                <label className="block text-sm font-medium mb-1">画风描述</label>
                                <Textarea
                                    value={currentStyle.description || ""}
                                    onChange={e => setCurrentStyle({ ...currentStyle, description: e.target.value })}
                                    className="h-32"
                                    placeholder="描述该画风的视觉特征、色彩、构图等..."
                                />
                                <p className="text-xs text-muted-foreground mt-1">这段文字会在角色、场景、视频最终提交给 ComfyUI 时拼接到正向提示词顶部。</p>
                            </div>
                        </div>

                        <div className="mt-8 flex justify-end gap-3">
                            <button
                                onClick={() => setIsEditing(false)}
                                className="px-4 py-2 rounded-md hover:bg-accent transition-colors"
                            >
                                取消
                            </button>
                            <button
                                onClick={handleSave}
                                className="flex items-center gap-2 bg-primary text-primary-foreground px-4 py-2 rounded-md hover:bg-primary/90 transition-colors"
                            >
                                <Save className="w-4 h-4" /> 保存画风
                            </button>
                        </div>
                    </div>
                </div>
            )}
        </div>
    )
}
