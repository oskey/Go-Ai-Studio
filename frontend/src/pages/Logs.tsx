import { useEffect, useState, useRef } from "react";
import axios from "axios";
import { Trash2, RotateCcw, Info, AlertTriangle, XCircle, FileText, Loader2, CheckCircle2 } from "lucide-react";
import type { SystemLog, LLMStreamState, SystemLogListResponse } from "@/types";
import { toast } from "sonner";
import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogDescription } from "@/components/ui/dialog";

interface Task {
    id: string;
    type: string;
    status: "pending" | "running" | "completed" | "failed";
    progress: number;
    result: string;
    error: string;
    created_at: string;
    updated_at: string;
}

const LogLevelIcon = ({ level }: { level: string }) => {
    switch (level) {
        case "INFO":
            return <Info className="w-4 h-4 text-blue-500" />;
        case "WARN":
            return <AlertTriangle className="w-4 h-4 text-yellow-500" />;
        case "ERROR":
            return <XCircle className="w-4 h-4 text-red-500" />;
        default:
            return <Info className="w-4 h-4 text-muted-foreground" />;
    }
};

export default function Logs() {
    const [logs, setLogs] = useState<SystemLog[]>([]);
    const [loading, setLoading] = useState(false);
    const [selectedLog, setSelectedLog] = useState<SystemLog | null>(null);
    const [activeTask, setActiveTask] = useState<Task | null>(null);
    const [showTaskDialog, setShowTaskDialog] = useState(false);
    const [liveStream, setLiveStream] = useState<LLMStreamState | null>(null);
    const [showStreamDialog, setShowStreamDialog] = useState(false);
    const [logPage, setLogPage] = useState(1);
    const [logPageSize, setLogPageSize] = useState(100);
    const [logTotal, setLogTotal] = useState(0);
    const [logTotalPages, setLogTotalPages] = useState(1);
    
    // Refs to prevent request stacking
    const isFetchingLogs = useRef(false);
    const isFetchingTask = useRef(false);
    const isFetchingStream = useRef(false);
    const logPageRef = useRef(logPage);
    const logPageSizeRef = useRef(logPageSize);

    const fetchLogs = () => {
        if (isFetchingLogs.current) return;
        isFetchingLogs.current = true;
        setLoading(true);
        axios.get<SystemLogListResponse | SystemLog[]>("/api/logs", {
            params: {
                page: logPageRef.current,
                limit: logPageSizeRef.current,
            },
        })
            .then(res => {
                const payload = Array.isArray(res.data)
                    ? {
                        items: res.data,
                        total: res.data.length,
                        page: 1,
                        limit: logPageSizeRef.current,
                        total_pages: 1,
                    }
                    : res.data;
                setLogs(payload.items || []);
                setLogTotal(payload.total || 0);
                setLogTotalPages(Math.max(payload.total_pages || 1, 1));
                if ((payload.page || 1) !== logPageRef.current) {
                    setLogPage(payload.page || 1);
                }
            })
            .catch(console.error)
            .finally(() => {
                isFetchingLogs.current = false;
                setLoading(false);
            });
    };

    const fetchActiveTask = () => {
        if (isFetchingTask.current) return;
        isFetchingTask.current = true;
        axios.get("/api/tasks?limit=1")
            .then(res => {
                if (res.data && res.data.length > 0) {
                    const task = res.data[0];
                    // Keep showing the task if dialog is open, even if completed
                    if (showTaskDialogRef.current) {
                        setActiveTask(task);
                    } else if (task.status === "running") {
                        setActiveTask(task);
                    } else {
                        setActiveTask(null);
                    }
                }
            })
            .catch(console.error)
            .finally(() => { isFetchingTask.current = false; });
    };

    const fetchCurrentStream = () => {
        if (isFetchingStream.current) return;
        isFetchingStream.current = true;
        axios.get("/api/llm/stream/current")
            .then(res => setLiveStream(res.data?.stream || null))
            .catch(console.error)
            .finally(() => { isFetchingStream.current = false; });
    };

    // Refs for state access inside intervals
    const showTaskDialogRef = useRef(showTaskDialog);
    const activeTaskRef = useRef(activeTask);
    const showStreamDialogRef = useRef(showStreamDialog);
    const liveStreamRef = useRef(liveStream);

    useEffect(() => {
        showTaskDialogRef.current = showTaskDialog;
    }, [showTaskDialog]);

    useEffect(() => {
        activeTaskRef.current = activeTask;
    }, [activeTask]);

    useEffect(() => {
        showStreamDialogRef.current = showStreamDialog;
        if (showStreamDialog) {
            fetchCurrentStream();
        }
    }, [showStreamDialog]);

    useEffect(() => {
        liveStreamRef.current = liveStream;
    }, [liveStream]);

    useEffect(() => {
        logPageRef.current = logPage;
    }, [logPage]);

    useEffect(() => {
        logPageSizeRef.current = logPageSize;
    }, [logPageSize]);

    useEffect(() => {
        fetchLogs();
    }, [logPage, logPageSize]);

    useEffect(() => {
        let logTimerId: ReturnType<typeof setTimeout>;
        let taskTimerId: ReturnType<typeof setTimeout>;
        let streamTimerId: ReturnType<typeof setTimeout>;
        let isMounted = true;

        // Initial fetch
        fetchActiveTask();
        fetchCurrentStream();
        
        // Log Polling Loop
        const logLoop = async () => {
            if (!isMounted) return;
            await fetchLogs(); // Assuming fetchLogs is safe (it uses ref check)
            if (isMounted) logTimerId = setTimeout(logLoop, 5000);
        };
        logTimerId = setTimeout(logLoop, 5000);

        // Task Polling Loop
        const taskLoop = async () => {
            if (!isMounted) return;
            
            // Check if we need to poll frequently
            await fetchActiveTask();
            
            const delay = (showTaskDialogRef.current || (activeTaskRef.current && activeTaskRef.current.status === 'running')) ? 2000 : 5000;
            
            if (isMounted) taskTimerId = setTimeout(taskLoop, delay);
        };
        taskTimerId = setTimeout(taskLoop, 2000);

        const streamLoop = async () => {
            if (!isMounted) return;
            fetchCurrentStream();
            const streamDelay = (showStreamDialogRef.current || liveStreamRef.current?.status === "running") ? 1000 : 5000;
            if (isMounted) streamTimerId = setTimeout(streamLoop, streamDelay);
        };
        streamTimerId = setTimeout(streamLoop, 5000);

        return () => {
            isMounted = false;
            clearTimeout(logTimerId);
            clearTimeout(taskTimerId);
            clearTimeout(streamTimerId);
            // Reset refs on unmount
            isFetchingLogs.current = false;
            isFetchingTask.current = false;
            isFetchingStream.current = false;
        };
    }, []); // Empty dependency array

    const handleClear = () => {
        toast("确定要清空所有日志吗？此操作不可撤销。", {
            action: {
                label: "清空",
                onClick: () => {
                    axios.delete("/api/logs")
                        .then(() => {
                            fetchLogs();
                            toast.success("日志已清空");
                        })
                        .catch(err => {
                            console.error(err);
                            toast.error("清空失败");
                        });
                }
            },
            cancel: {
                label: "取消",
                onClick: () => {}, // Added onClick to satisfy Action type
            }
        });
    };

    const formatDate = (dateStr: string) => {
        return new Date(dateStr).toLocaleString("zh-CN");
    };

    // Use effect to scroll to bottom when log content changes
    const logContentRef = useRef<HTMLDivElement>(null);
    useEffect(() => {
        if (showTaskDialog && activeTask && logContentRef.current) {
            logContentRef.current.scrollTop = logContentRef.current.scrollHeight;
        }
    }, [activeTask?.result, showTaskDialog]);

    return (
        <div className="space-y-6">
            <div className="flex justify-between items-center">
                <h1 className="text-3xl font-bold">系统日志</h1>
                <div className="flex gap-2">
                    <button
                        onClick={fetchLogs}
                        disabled={loading}
                        className="flex items-center gap-2 bg-secondary text-secondary-foreground px-4 py-2 rounded-md hover:bg-secondary/80 transition-colors disabled:opacity-50"
                    >
                        <RotateCcw className={`w-4 h-4 ${loading ? "animate-spin" : ""}`} /> 刷新
                    </button>
                    <button
                        onClick={handleClear}
                        className="flex items-center gap-2 bg-destructive text-destructive-foreground px-4 py-2 rounded-md hover:bg-destructive/90 transition-colors"
                    >
                        <Trash2 className="w-4 h-4" /> 清空日志
                    </button>
                    {liveStream && (
                        <button
                            onClick={() => setShowStreamDialog(true)}
                            className="flex items-center gap-2 bg-emerald-600 text-white px-4 py-2 rounded-md hover:bg-emerald-700 transition-colors"
                        >
                            <FileText className="w-4 h-4" /> 查看LLM流
                        </button>
                    )}
                    {activeTask && (
                        <button
                            onClick={() => setShowTaskDialog(true)}
                            className="flex items-center gap-2 bg-blue-500 text-white px-4 py-2 rounded-md hover:bg-blue-600 transition-colors animate-pulse"
                        >
                            <Loader2 className="w-4 h-4 animate-spin" /> 查看生成中...
                        </button>
                    )}
                </div>
            </div>

            <div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between text-sm text-muted-foreground">
                <div>
                    共 {logTotal} 条日志，第 {Math.max(logPage, 1)} / {Math.max(logTotalPages, 1)} 页
                </div>
                <div className="flex flex-wrap items-center gap-2">
                    <label className="text-sm">每页</label>
                    <select
                        value={logPageSize}
                        onChange={(e) => {
                            const nextSize = Number(e.target.value) || 100;
                            setLogPage(1);
                            setLogPageSize(nextSize);
                        }}
                        className="h-9 rounded-md border border-input bg-background px-3 text-sm"
                    >
                        <option value={50}>50</option>
                        <option value={100}>100</option>
                        <option value={200}>200</option>
                    </select>
                    <button
                        onClick={() => setLogPage((page) => Math.max(page - 1, 1))}
                        disabled={loading || logPage <= 1}
                        className="px-3 py-2 rounded-md border border-border bg-background hover:bg-accent disabled:opacity-50"
                    >
                        上一页
                    </button>
                    <button
                        onClick={() => setLogPage((page) => Math.min(page + 1, Math.max(logTotalPages, 1)))}
                        disabled={loading || logPage >= logTotalPages}
                        className="px-3 py-2 rounded-md border border-border bg-background hover:bg-accent disabled:opacity-50"
                    >
                        下一页
                    </button>
                </div>
            </div>

            <div className="bg-card border border-border rounded-lg shadow-sm overflow-hidden">
                <div className="overflow-x-auto">
                    <table className="w-full text-sm text-left">
                        <thead className="text-xs text-muted-foreground uppercase bg-accent/50">
                            <tr>
                                <th className="px-6 py-3">级别</th>
                                <th className="px-6 py-3">时间</th>
                                <th className="px-6 py-3">消息</th>
                                <th className="px-6 py-3">详情</th>
                            </tr>
                        </thead>
                        <tbody>
                            {logs.length === 0 ? (
                                <tr>
                                    <td colSpan={4} className="px-6 py-8 text-center text-muted-foreground">
                                        暂无日志记录
                                    </td>
                                </tr>
                            ) : (
                                logs.map((log) => (
                                    <tr key={log.id} className="border-b border-border hover:bg-accent/50 transition-colors">
                                        <td className="px-6 py-4 font-medium">
                                            <div className="flex items-center gap-2">
                                                <LogLevelIcon level={log.level} />
                                                <span>{log.level}</span>
                                            </div>
                                        </td>
                                        <td className="px-6 py-4 whitespace-nowrap text-muted-foreground">
                                            {formatDate(log.created_at)}
                                        </td>
                                        <td className="px-6 py-4 font-medium">
                                            {log.message}
                                        </td>
                                        <td className="px-6 py-4 text-muted-foreground">
                                            <div className="flex items-center gap-2">
                                                <div className="max-w-[300px] truncate" title={log.details}>
                                                    {log.details}
                                                </div>
                                                {log.details && log.details.length > 50 && (
                                                    <button 
                                                        onClick={() => setSelectedLog(log)}
                                                        className="text-blue-500 hover:text-blue-600 p-1 rounded hover:bg-blue-500/10"
                                                        title="查看详情"
                                                    >
                                                        <FileText className="w-4 h-4" />
                                                    </button>
                                                )}
                                            </div>
                                        </td>
                                    </tr>
                                ))
                            )}
                        </tbody>
                    </table>
                </div>
            </div>

            {/* Log Details Dialog */}
            <Dialog open={!!selectedLog} onOpenChange={(open) => !open && setSelectedLog(null)}>
                <DialogContent className="max-w-3xl max-h-[80vh] overflow-hidden flex flex-col">
                    <DialogHeader>
                        <DialogTitle className="flex items-center gap-2">
                            <LogLevelIcon level={selectedLog?.level || "INFO"} />
                            {selectedLog?.message || "日志详情"}
                        </DialogTitle>
                        <DialogDescription>
                            {selectedLog?.created_at && formatDate(selectedLog.created_at)}
                        </DialogDescription>
                    </DialogHeader>
                    <div className="flex-1 overflow-y-auto bg-muted/50 p-4 rounded-md mt-2">
                        <pre className="text-xs font-mono whitespace-pre-wrap break-all text-foreground">
                            {selectedLog?.details}
                        </pre>
                    </div>
                </DialogContent>
            </Dialog>

            <Dialog open={showStreamDialog} onOpenChange={setShowStreamDialog}>
                <DialogContent className="max-w-4xl max-h-[80vh] overflow-hidden flex flex-col">
                    <DialogHeader>
                        <DialogTitle>LLM 实时输出</DialogTitle>
                        <DialogDescription>
                            {liveStream ? `${liveStream.provider_name} / ${liveStream.label} / ${liveStream.status} / ${liveStream.char_count} 字 / ${formatDate(liveStream.updated_at)}` : "当前没有可查看的实时流"}
                        </DialogDescription>
                    </DialogHeader>
                    <div className="flex-1 overflow-y-auto bg-muted/50 p-4 rounded-md mt-2">
                        <pre className="text-xs font-mono whitespace-pre-wrap break-all text-foreground">
                            {liveStream?.content || "当前没有 LLM 实时输出。"}
                        </pre>
                    </div>
                </DialogContent>
            </Dialog>

            {/* Real-time Task Log Dialog (Mirrored from Dashboard) */}
            <Dialog open={showTaskDialog} onOpenChange={setShowTaskDialog}>
                <DialogContent className="max-w-3xl max-h-[80vh] overflow-hidden flex flex-col">
                    <DialogHeader>
                        <DialogTitle className="flex items-center gap-2">
                            {activeTask?.status === "running" ? (
                                <>
                                    <Loader2 className="w-5 h-5 text-blue-500 animate-spin" />
                                    任务执行中... ({activeTask?.progress}%)
                                </>
                            ) : activeTask?.status === "completed" ? (
                                <>
                                    <CheckCircle2 className="w-5 h-5 text-green-500" />
                                    任务已完成
                                </>
                            ) : (
                                <>
                                    <XCircle className="w-5 h-5 text-red-500" />
                                    任务失败
                                </>
                            )}
                        </DialogTitle>
                        <DialogDescription>
                            {activeTask?.status === "running" ? "正在实时接收 LLM 生成内容" : "生成任务已结束，完整日志已保存"}
                        </DialogDescription>
                    </DialogHeader>
                    <div 
                        ref={logContentRef}
                        className="flex-1 overflow-y-auto bg-muted/50 p-4 rounded-md mt-2 font-mono text-xs whitespace-pre-wrap break-all"
                    >
                        {activeTask?.result || (activeTask?.status === "completed" ? "任务已成功完成，但未返回详细日志。" : activeTask?.error || "等待数据返回...")}
                    </div>
                    {activeTask?.status !== "running" && (
                        <div className="flex justify-end pt-2">
                            <button 
                                onClick={() => setShowTaskDialog(false)}
                                className="px-4 py-2 bg-primary text-primary-foreground rounded-md hover:bg-primary/90 transition-colors"
                            >
                                关闭
                            </button>
                        </div>
                    )}
                </DialogContent>
            </Dialog>
        </div>
    )
}
