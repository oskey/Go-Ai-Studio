import { useEffect, useMemo, useState } from "react";
import axios from "axios";
import {
    BarChart3,
    CheckCircle,
    Edit2,
    Plus,
    RefreshCw,
    RotateCcw,
    Save,
    Trash2,
    X,
    Zap,
} from "lucide-react";
import type {
    LLMProvider,
    LLMProviderUsageStats,
    LLMUsagePoint,
    LLMUsageSummary,
    LLMUsageWindowStats,
} from "@/types";
import { toast } from "sonner";

import { Input } from "@/components/ui/input";
import { Combobox } from "@/components/ui/combobox";
import { Switch } from "@/components/ui/switch";

type ProviderOption = { id: string; name: string };

const PROVIDER_OPTIONS: ProviderOption[] = [
    { id: "OpenAI", name: "OpenAI" },
    { id: "Claude", name: "Claude" },
    { id: "Gemini", name: "Gemini" },
    { id: "豆包", name: "豆包" },
    { id: "Deepseek", name: "Deepseek" },
    { id: "Qwen", name: "Qwen" },
    { id: "Local", name: "Local" },
    { id: "Custom", name: "自定义" },
];
const CHART_MODES = [
    { key: "hour", label: "小时统计" },
    { key: "day", label: "天统计" },
    { key: "month", label: "月统计" },
    { key: "year", label: "年统计" },
] as const;

type ChartMode = (typeof CHART_MODES)[number]["key"];

function normalizeProviderValue(provider?: string) {
    const value = (provider || "").trim();
    if (!value) return "";
    if (value === "璞嗗寘") return "豆包";
    if (value === "自定义") return "Custom";
    return value;
}

function getProviderLabel(provider?: string) {
    const value = normalizeProviderValue(provider);
    const matched = PROVIDER_OPTIONS.find((item) => item.id === value);
    return matched?.name || value;
}

function formatTokenCount(value?: number) {
    return (value || 0).toLocaleString("zh-CN");
}

function UsageStatRow({ label, stats }: { label: string; stats?: LLMUsageWindowStats }) {
    const current = stats || {
        input_tokens: 0,
        output_tokens: 0,
        total_tokens: 0,
        request_count: 0,
    };

    return (
        <div className="rounded-md border border-border/60 bg-background/60 p-3">
            <div className="mb-2 text-xs font-medium text-muted-foreground">{label}</div>
            <div className="grid grid-cols-2 gap-2 text-sm">
                <div>
                    <div className="text-xs text-muted-foreground">输入</div>
                    <div className="font-semibold">{formatTokenCount(current.input_tokens)}</div>
                </div>
                <div>
                    <div className="text-xs text-muted-foreground">输出</div>
                    <div className="font-semibold">{formatTokenCount(current.output_tokens)}</div>
                </div>
                <div>
                    <div className="text-xs text-muted-foreground">总量</div>
                    <div className="font-semibold">{formatTokenCount(current.total_tokens)}</div>
                </div>
                <div>
                    <div className="text-xs text-muted-foreground">请求数</div>
                    <div className="font-semibold">{formatTokenCount(current.request_count)}</div>
                </div>
            </div>
        </div>
    );
}

function UsageLineChart({ data }: { data: LLMUsagePoint[] }) {
    const width = 860;
    const height = 260;
    const padding = 28;
    const maxValue = Math.max(
        1,
        ...data.flatMap((item) => [item.input_tokens || 0, item.output_tokens || 0])
    );

    const toPoint = (value: number, index: number) => {
        const x = padding + (index * (width - padding * 2)) / Math.max(data.length - 1, 1);
        const y = height - padding - (value / maxValue) * (height - padding * 2);
        return `${x},${y}`;
    };

    const inputPoints = data.map((item, index) => toPoint(item.input_tokens || 0, index)).join(" ");
    const outputPoints = data.map((item, index) => toPoint(item.output_tokens || 0, index)).join(" ");
    const gridValues = [0, 0.25, 0.5, 0.75, 1];

    return (
        <div className="rounded-xl border border-border bg-card p-4">
            <div className="mb-4 flex items-center gap-4 text-sm">
                <div className="flex items-center gap-2">
                    <span className="h-2.5 w-2.5 rounded-full bg-sky-500" />
                    <span>输入 Token</span>
                </div>
                <div className="flex items-center gap-2">
                    <span className="h-2.5 w-2.5 rounded-full bg-amber-500" />
                    <span>输出 Token</span>
                </div>
            </div>

            <div className="overflow-x-auto">
                <svg viewBox={`0 0 ${width} ${height}`} className="min-w-[760px]">
                    {gridValues.map((value) => {
                        const y = height - padding - value * (height - padding * 2);
                        return (
                            <g key={value}>
                                <line
                                    x1={padding}
                                    y1={y}
                                    x2={width - padding}
                                    y2={y}
                                    stroke="currentColor"
                                    strokeOpacity="0.12"
                                />
                                <text
                                    x={8}
                                    y={y + 4}
                                    className="fill-muted-foreground text-[10px]"
                                >
                                    {formatTokenCount(Math.round(maxValue * value))}
                                </text>
                            </g>
                        );
                    })}

                    <polyline
                        fill="none"
                        stroke="#0ea5e9"
                        strokeWidth="3"
                        strokeLinejoin="round"
                        strokeLinecap="round"
                        points={inputPoints}
                    />
                    <polyline
                        fill="none"
                        stroke="#f59e0b"
                        strokeWidth="3"
                        strokeLinejoin="round"
                        strokeLinecap="round"
                        points={outputPoints}
                    />

                    {data.map((item, index) => {
                        const [inputX, inputY] = toPoint(item.input_tokens || 0, index).split(",");
                        const [outputX, outputY] = toPoint(item.output_tokens || 0, index).split(",");
                        const x = padding + (index * (width - padding * 2)) / Math.max(data.length - 1, 1);
                        return (
                            <g key={`${item.label}-${index}`}>
                                <circle cx={inputX} cy={inputY} r="3.5" fill="#0ea5e9" />
                                <circle cx={outputX} cy={outputY} r="3.5" fill="#f59e0b" />
                                <text
                                    x={x}
                                    y={height - 8}
                                    textAnchor="middle"
                                    className="fill-muted-foreground text-[10px]"
                                >
                                    {item.label}
                                </text>
                            </g>
                        );
                    })}
                </svg>
            </div>
        </div>
    );
}

function ProviderUsagePanel({ stats }: { stats?: LLMProviderUsageStats }) {
    return (
        <div className="space-y-3">
            <UsageStatRow label="累计统计" stats={stats?.total} />
            <div className="grid grid-cols-2 gap-3">
                <UsageStatRow label="当前小时" stats={stats?.hour} />
                <UsageStatRow label="当前天" stats={stats?.day} />
                <UsageStatRow label="当前月" stats={stats?.month} />
                <UsageStatRow label="当前年" stats={stats?.year} />
            </div>
        </div>
    );
}

export default function LLMEngine() {
    const [providers, setProviders] = useState<LLMProvider[]>([]);
    const [usageSummary, setUsageSummary] = useState<LLMUsageSummary | null>(null);
    const [isEditing, setIsEditing] = useState(false);
    const [currentProvider, setCurrentProvider] = useState<Partial<LLMProvider>>({});
    const [requestMaxTokensInput, setRequestMaxTokensInput] = useState("");
    const [requestTemperatureInput, setRequestTemperatureInput] = useState("");
    const [loading, setLoading] = useState(false);
    const [chartMode, setChartMode] = useState<ChartMode>("day");
    const [refreshingStats, setRefreshingStats] = useState(false);
    const providerValue = normalizeProviderValue(currentProvider.provider);
    const isCustomProvider = providerValue === "Custom";

    useEffect(() => {
        void fetchAllData();
    }, []);

    const chartData = useMemo(() => {
        if (!usageSummary) return [];
        if (chartMode === "hour") return usageSummary.hour_series;
        if (chartMode === "day") return usageSummary.day_series;
        if (chartMode === "month") return usageSummary.month_series;
        return usageSummary.year_series;
    }, [chartMode, usageSummary]);

    const fetchProviders = async () => {
        const res = await axios.get("/api/llm");
        setProviders(
            res.data.map((provider: LLMProvider) => ({
                ...provider,
                provider: normalizeProviderValue(provider.provider),
            }))
        );
    };

    const fetchUsageSummary = async () => {
        const res = await axios.get("/api/llm/stats");
        setUsageSummary(res.data);
    };

    const fetchAllData = async () => {
        try {
            await Promise.all([fetchProviders(), fetchUsageSummary()]);
        } catch (err) {
            console.error(err);
            toast.error("获取 LLM 引擎或统计数据失败");
        }
    };

    const handleSave = () => {
        if (!currentProvider.name || !currentProvider.provider || !currentProvider.api_address) {
            toast.error("请填写必要信息(名称, 服务商, API 地址)");
            return;
        }

        const parsedRequestMaxTokens = /^\d+$/.test(requestMaxTokensInput.trim())
            ? Number(requestMaxTokensInput.trim())
            : 0;
        const parsedRequestTemperature = /^\d+(\.\d+)?$/.test(requestTemperatureInput.trim())
            ? Number(requestTemperatureInput.trim())
            : 0;

        const payload = {
            ...currentProvider,
            provider: normalizeProviderValue(currentProvider.provider),
            request_max_tokens: parsedRequestMaxTokens,
            request_temperature: parsedRequestTemperature,
        };

        const req = currentProvider.id
            ? axios.put(`/api/llm/${currentProvider.id}`, payload)
            : axios.post("/api/llm", payload);

        req.then(() => {
            setIsEditing(false);
            setCurrentProvider({});
            void fetchAllData();
            toast.success(currentProvider.id ? "更新成功" : "创建成功");
        }).catch((err) => {
            console.error(err);
            toast.error("保存失败");
        });
    };

    const handleDelete = (id: number) => {
        toast("确定要删除吗？", {
            action: {
                label: "删除",
                onClick: () => {
                    axios.delete(`/api/llm/${id}`)
                        .then(() => {
                            void fetchAllData();
                            toast.success("删除成功");
                        })
                        .catch((err) => {
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

    const handleTest = (provider: LLMProvider) => {
        setLoading(true);
        axios.post("/api/llm/test", provider)
            .then((res) => {
                toast.success(res.data.message);
            })
            .catch((err) => {
                const msg = err.response?.data?.message || "连接失败";
                toast.error(msg);
            })
            .finally(() => setLoading(false));
    };

    const handleActivate = (id: number) => {
        axios.put(`/api/llm/${id}/active`)
            .then(() => {
                void fetchAllData();
                toast.success("默认引擎已更新");
            })
            .catch((err) => {
                console.error(err);
                toast.error("切换失败");
            });
    };

    const handleRefreshStats = async () => {
        setRefreshingStats(true);
        try {
            const res = await axios.post("/api/llm/stats/refresh");
            setUsageSummary(res.data.summary);
            await fetchProviders();
            toast.success("统计已强制刷新");
        } catch (err) {
            console.error(err);
            toast.error("强制刷新统计失败");
        } finally {
            setRefreshingStats(false);
        }
    };

    const handleResetProviderStats = (provider: LLMProvider) => {
        toast(`确定重置 ${provider.name} 的统计记录吗？`, {
            action: {
                label: "重置",
                onClick: () => {
                    axios.post(`/api/llm/${provider.id}/stats/reset`)
                        .then(() => {
                            void fetchAllData();
                            toast.success("该引擎统计已重置");
                        })
                        .catch((err) => {
                            console.error(err);
                            toast.error("重置统计失败");
                        });
                },
            },
            cancel: {
                label: "取消",
                onClick: () => {},
            },
        });
    };

    const handleResetAllStats = () => {
        toast("确定重置全部 LLM 统计记录吗？", {
            action: {
                label: "全部重置",
                onClick: () => {
                    axios.post("/api/llm/stats/reset")
                        .then(() => {
                            void fetchAllData();
                            toast.success("全部统计已重置");
                        })
                        .catch((err) => {
                            console.error(err);
                            toast.error("重置全部统计失败");
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
        <div className="space-y-8">
            <div className="flex flex-wrap items-center justify-between gap-4">
                <div>
                    <h1 className="text-3xl font-bold">LLM 引擎配置</h1>
                    <p className="mt-1 text-sm text-muted-foreground">
                        管理模型接入，同时查看输入 / 输出 Token 用量趋势。
                    </p>
                </div>
                <div className="flex flex-wrap items-center gap-3">
                    <button
                        onClick={handleRefreshStats}
                        disabled={refreshingStats}
                        className="flex items-center gap-2 rounded-md border border-border bg-card px-4 py-2 text-sm hover:bg-accent disabled:opacity-60"
                    >
                        <RefreshCw className={`h-4 w-4 ${refreshingStats ? "animate-spin" : ""}`} />
                        强制刷新统计
                    </button>
                    <button
                        onClick={handleResetAllStats}
                        className="flex items-center gap-2 rounded-md border border-destructive/30 bg-card px-4 py-2 text-sm text-destructive hover:bg-destructive/10"
                    >
                        <RotateCcw className="h-4 w-4" />
                        重置全部统计
                    </button>
                    <button
                        onClick={() => {
                            setCurrentProvider({
                                provider: "OpenAI",
                                api_address: "https://api.openai.com/v1",
                                enable_advanced_request_params: false,
                                request_max_tokens: 0,
                                request_temperature: 0,
                            });
                            setRequestMaxTokensInput("");
                            setRequestTemperatureInput("");
                            setIsEditing(true);
                        }}
                        className="flex items-center gap-2 rounded-md bg-primary px-4 py-2 text-primary-foreground hover:bg-primary/90"
                    >
                        <Plus className="h-4 w-4" />
                        新增引擎
                    </button>
                </div>
            </div>

            {usageSummary && (
                <section className="space-y-5 rounded-2xl border border-border bg-card p-6 shadow-sm">
                    <div className="flex flex-wrap items-center justify-between gap-3">
                        <div className="flex items-center gap-2">
                            <BarChart3 className="h-5 w-5 text-primary" />
                            <h2 className="text-xl font-semibold">整体 Token 统计</h2>
                        </div>
                        <div className="text-xs text-muted-foreground">
                            上次落库刷新：
                            {usageSummary.last_flushed
                                ? new Date(usageSummary.last_flushed).toLocaleString("zh-CN")
                                : " 暂无"}
                        </div>
                    </div>

                    <div className="grid grid-cols-1 gap-4 md:grid-cols-5">
                        <UsageStatRow label="累计" stats={usageSummary.total} />
                        <UsageStatRow label="当前小时" stats={usageSummary.hour} />
                        <UsageStatRow label="当前天" stats={usageSummary.day} />
                        <UsageStatRow label="当前月" stats={usageSummary.month} />
                        <UsageStatRow label="当前年" stats={usageSummary.year} />
                    </div>

                    <div className="flex flex-wrap items-center gap-2">
                        {CHART_MODES.map((mode) => (
                            <button
                                key={mode.key}
                                onClick={() => setChartMode(mode.key)}
                                className={`rounded-full px-4 py-2 text-sm transition-colors ${
                                    chartMode === mode.key
                                        ? "bg-primary text-primary-foreground"
                                        : "bg-accent text-accent-foreground hover:bg-accent/80"
                                }`}
                            >
                                {mode.label}
                            </button>
                        ))}
                    </div>

                    <UsageLineChart data={chartData} />
                </section>
            )}

            <div className="grid grid-cols-1 gap-6 xl:grid-cols-2">
                {providers.map((p) => (
                    <div
                        key={p.id}
                        className={`flex flex-col justify-between rounded-2xl border bg-card p-6 shadow-sm transition-colors ${
                            p.is_active ? "border-primary ring-1 ring-primary" : "border-border"
                        }`}
                    >
                        <div className="space-y-5">
                            <div className="flex items-start justify-between gap-4">
                                <div>
                                    <div className="flex items-center gap-2">
                                        <h3 className="text-lg font-semibold">{p.name}</h3>
                                        {p.is_active && (
                                            <span className="inline-flex items-center gap-1 rounded-full bg-primary px-2 py-0.5 text-xs text-primary-foreground">
                                                <CheckCircle className="h-3 w-3" />
                                                默认
                                            </span>
                                        )}
                                    </div>
                                    <span className="mt-1 inline-block rounded-full bg-accent px-2 py-1 text-xs text-accent-foreground">
                                        {getProviderLabel(p.provider)}
                                    </span>
                                </div>
                                <div className="flex gap-2">
                                    {!p.is_active && (
                                        <button
                                            onClick={() => handleActivate(p.id)}
                                            className="p-1 transition-colors hover:text-green-500"
                                            title="设为默认"
                                        >
                                            <CheckCircle className="h-4 w-4" />
                                        </button>
                                    )}
                                    <button
                                        onClick={() => {
                                            setCurrentProvider(p);
                                            setRequestMaxTokensInput(
                                                p.request_max_tokens && p.request_max_tokens > 0
                                                    ? String(p.request_max_tokens)
                                                    : ""
                                            );
                                            setRequestTemperatureInput(
                                                p.request_temperature && p.request_temperature > 0
                                                    ? String(p.request_temperature)
                                                    : ""
                                            );
                                            setIsEditing(true);
                                        }}
                                        className="p-1 transition-colors hover:text-blue-400"
                                        title="编辑"
                                    >
                                        <Edit2 className="h-4 w-4" />
                                    </button>
                                    <button
                                        onClick={() => handleDelete(p.id)}
                                        className="p-1 transition-colors hover:text-destructive"
                                        title="删除"
                                    >
                                        <Trash2 className="h-4 w-4" />
                                    </button>
                                </div>
                            </div>

                            <div className="space-y-2 text-sm text-muted-foreground">
                                <p><span className="font-medium">模型:</span> {p.model_name || "默认"}</p>
                                <p className="break-all"><span className="font-medium">API:</span> {p.api_address}</p>
                            </div>

                            <ProviderUsagePanel stats={p.usage_stats} />
                        </div>

                        <div className="mt-6 flex flex-wrap items-center justify-between gap-3 border-t border-border pt-4">
                            <button
                                onClick={() => handleTest(p)}
                                disabled={loading}
                                className="flex items-center gap-2 text-sm transition-colors hover:text-primary disabled:opacity-50"
                            >
                                <Zap className="h-4 w-4" />
                                测试连接
                            </button>
                            <div className="flex items-center gap-3">
                                <button
                                    onClick={() => handleResetProviderStats(p)}
                                    className="flex items-center gap-2 text-sm text-destructive transition-colors hover:text-destructive/80"
                                >
                                    <RotateCcw className="h-4 w-4" />
                                    重置统计
                                </button>
                                {loading && <span className="text-xs animate-pulse">测试中...</span>}
                            </div>
                        </div>
                    </div>
                ))}
            </div>

            {isEditing && (
                <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50 backdrop-blur-sm">
                    <div className="relative w-full max-w-lg animate-in fade-in zoom-in rounded-lg border border-border bg-card p-6 shadow-lg duration-200">
                        <button
                            onClick={() => setIsEditing(false)}
                            className="absolute right-4 top-4 text-muted-foreground hover:text-foreground"
                        >
                            <X className="h-5 w-5" />
                        </button>

                        <h2 className="mb-6 text-xl font-bold">{currentProvider.id ? "编辑引擎" : "新增引擎"}</h2>

                        <div className="space-y-4">
                            <div className="grid grid-cols-2 gap-4">
                                <div>
                                    <label className="mb-1 block text-sm font-medium">名称</label>
                                    <Input
                                        value={currentProvider.name || ""}
                                        onChange={(e) => setCurrentProvider({ ...currentProvider, name: e.target.value })}
                                        placeholder="例如: 我的 GPT-4"
                                    />
                                </div>
                                <div>
                                    <label className="mb-1 block text-sm font-medium">服务商</label>
                                    <Combobox
                                        items={PROVIDER_OPTIONS}
                                        value={providerValue}
                                        onChange={(val) => setCurrentProvider({ ...currentProvider, provider: String(val) })}
                                        placeholder="选择服务商"
                                        searchPlaceholder="搜索服务商..."
                                        getItemValue={(item) => item.id}
                                        getItemLabel={(item) => item.name}
                                        renderItem={(item) => <span>{item.name}</span>}
                                    />
                                </div>
                            </div>

                            <div>
                                <label className="mb-1 block text-sm font-medium">API 地址</label>
                                <Input
                                    value={currentProvider.api_address || ""}
                                    onChange={(e) => setCurrentProvider({ ...currentProvider, api_address: e.target.value })}
                                    placeholder={isCustomProvider ? "https://open.bigmodel.cn/api/paas/v4/chat/completions" : "https://api.openai.com/v1"}
                                />
                                {isCustomProvider && (
                                    <p className="mt-1 text-xs text-muted-foreground">
                                        自定义服务商会直接请求你填写的完整 URI，不再自动拼接 `/chat/completions`。
                                    </p>
                                )}
                            </div>

                            <div>
                                <label className="mb-1 block text-sm font-medium">API Key</label>
                                <Input
                                    type="password"
                                    value={currentProvider.api_key || ""}
                                    onChange={(e) => setCurrentProvider({ ...currentProvider, api_key: e.target.value })}
                                    placeholder="sk-..."
                                />
                            </div>

                            <div>
                                <label className="mb-1 block text-sm font-medium">模型名称</label>
                                <Input
                                    value={currentProvider.model_name || ""}
                                    onChange={(e) => setCurrentProvider({ ...currentProvider, model_name: e.target.value })}
                                    placeholder="gpt-4o"
                                />
                            </div>

                            <div className="rounded-lg border border-border/60 bg-muted/20 p-4">
                                <div className="flex items-start justify-between gap-4">
                                    <div>
                                        <div className="text-sm font-medium">启用高级请求参数</div>
                                        <p className="mt-1 text-xs leading-5 text-muted-foreground">
                                            只有勾选后，下面这两个参数才会随请求一起发给 LLM。
                                            默认不启用，避免影响当前稳定链路。
                                        </p>
                                    </div>
                                    <Switch
                                        checked={!!currentProvider.enable_advanced_request_params}
                                        onCheckedChange={(checked) =>
                                            setCurrentProvider({
                                                ...currentProvider,
                                                enable_advanced_request_params: checked,
                                            })
                                        }
                                    />
                                </div>

                                <div className="mt-4 grid grid-cols-1 gap-4 md:grid-cols-2">
                                    <div>
                                        <label className="mb-1 block text-sm font-medium">最大输出 Token 上限</label>
                                        <Input
                                            type="text"
                                            inputMode="numeric"
                                            value={requestMaxTokensInput}
                                            onChange={(e) => setRequestMaxTokensInput(e.target.value.replace(/[^\d]/g, ""))}
                                            placeholder="例如 32000"
                                            disabled={!currentProvider.enable_advanced_request_params}
                                        />
                                        <p className="mt-1 text-xs leading-5 text-muted-foreground">
                                            用于给长 JSON 或长分镜结果预留更大的输出空间。填 0 表示不额外提交该参数。
                                        </p>
                                    </div>

                                    <div>
                                        <label className="mb-1 block text-sm font-medium">采样温度</label>
                                        <Input
                                            type="text"
                                            inputMode="decimal"
                                            value={requestTemperatureInput}
                                            onChange={(e) => {
                                                const next = e.target.value;
                                                if (/^\d*(\.\d*)?$/.test(next)) {
                                                    setRequestTemperatureInput(next);
                                                }
                                            }}
                                            placeholder="例如 0.1"
                                            disabled={!currentProvider.enable_advanced_request_params}
                                        />
                                        <p className="mt-1 text-xs leading-5 text-muted-foreground">
                                            数值越低，输出越稳定、越适合严格 JSON。建议从 0.1 开始测试。
                                        </p>
                                    </div>
                                </div>
                            </div>
                        </div>

                        <div className="mt-8 flex justify-end gap-3">
                            <button
                                onClick={() => setIsEditing(false)}
                                className="rounded-md px-4 py-2 transition-colors hover:bg-accent"
                            >
                                取消
                            </button>
                            <button
                                onClick={handleSave}
                                className="flex items-center gap-2 rounded-md bg-primary px-4 py-2 text-primary-foreground transition-colors hover:bg-primary/90"
                            >
                                <Save className="h-4 w-4" />
                                保存配置
                            </button>
                        </div>
                    </div>
                </div>
            )}
        </div>
    );
}
