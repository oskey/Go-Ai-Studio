import { useEffect, useMemo, useState } from "react";
import { NavLink, useLocation } from "react-router-dom";
import { LayoutDashboard, Folder, Palette, BrainCircuit, ScrollText, Settings, Menu, X, Tags, Images, Store, ChevronDown, Megaphone, Clapperboard, Mic2 } from "lucide-react";
import { cn } from "../lib/utils";
import { ThemeToggle } from "./ThemeToggle";

const navigation = [
  { name: "项目首页", href: "/", icon: LayoutDashboard },
  { name: "画风管理", href: "/styles", icon: Palette },
  { name: "多视觉图", href: "/multi-visuals", icon: Images },
  { name: "LLM 引擎", href: "/llm", icon: BrainCircuit },
  { name: "系统日志", href: "/logs", icon: ScrollText },
  { name: "系统设置", href: "/settings", icon: Settings },
];

const dramaNavigation = [
  { name: "剧情管理", href: "/projects", icon: Folder },
  { name: "剧情标签", href: "/auto-generate-tags", icon: Tags },
];

const templateNavigation = [
  { name: "讲解标签", href: "/general-guide-tags", icon: Tags },
  { name: "博主探店", href: "/store-visits", icon: Store },
  { name: "综合讲解", href: "/general-guides", icon: Megaphone },
];

const audioCloneNavigation = [
  { name: "LongChat", href: "/audio-clone-projects", icon: Mic2 },
  { name: "Qwen3 TTS", href: "/qwen-tts-projects", icon: Mic2 },
];

const audioProductionNavigation = [
  { name: "按人设生成（Qwen3-TTS）", href: "/audio-production/custom-voice", icon: Mic2 },
  { name: "按提示生成（Qwen3-TTS）", href: "/audio-production/voice-prompt", icon: Mic2 },
];

export function Sidebar() {
  const [isOpen, setIsOpen] = useState(false);
  const location = useLocation();

  const dramaGroupActive = useMemo(
    () => dramaNavigation.some((item) => location.pathname === item.href || location.pathname.startsWith(`${item.href}/`)),
    [location.pathname],
  );
  const templateGroupActive = useMemo(
    () => templateNavigation.some((item) => location.pathname === item.href || location.pathname.startsWith(`${item.href}/`)),
    [location.pathname],
  );
  const audioCloneGroupActive = useMemo(
    () =>
      [...audioCloneNavigation, ...audioProductionNavigation].some(
        (item) => location.pathname === item.href || location.pathname.startsWith(`${item.href}/`),
      ),
    [location.pathname],
  );
  const [dramaOpen, setDramaOpen] = useState(dramaGroupActive);
  const [templateOpen, setTemplateOpen] = useState(templateGroupActive);
  const [audioCloneOpen, setAudioCloneOpen] = useState(audioCloneGroupActive);

  useEffect(() => {
    if (dramaGroupActive) {
      setDramaOpen(true);
    }
  }, [dramaGroupActive]);

  useEffect(() => {
    if (templateGroupActive) {
      setTemplateOpen(true);
    }
  }, [templateGroupActive]);

  useEffect(() => {
    if (audioCloneGroupActive) {
      setAudioCloneOpen(true);
    }
  }, [audioCloneGroupActive]);

  return (
    <>
      {/* Mobile Top Bar */}
      <div className="md:hidden fixed top-0 left-0 right-0 h-16 z-50 flex items-center justify-between px-4 bg-card border-b border-border shadow-sm">
          <h1 className="text-xl font-bold bg-gradient-to-r from-blue-400 to-purple-500 bg-clip-text text-transparent">
            KT-AI-Studio
          </h1>
          <div className="flex items-center gap-2">
            <ThemeToggle />
            <button
              onClick={() => setIsOpen(!isOpen)}
              className="p-2 text-muted-foreground hover:text-foreground"
            >
              {isOpen ? <X className="w-6 h-6" /> : <Menu className="w-6 h-6" />}
            </button>
          </div>
      </div>

      {/* Sidebar Content (Desktop & Mobile) */}
      <div className={cn(
        "fixed inset-y-0 left-0 z-40 w-64 bg-card border-r border-border transform transition-transform duration-300 ease-in-out md:translate-x-0",
        isOpen ? "translate-x-0 mt-16 md:mt-0" : "-translate-x-full md:translate-x-0"
      )}>
        <div className="flex flex-col h-full">
          {/* Desktop Header */}
          <div className="hidden md:flex items-center justify-between h-16 flex-shrink-0 px-4 bg-background/50 backdrop-blur-sm border-b border-border">
            <h1 className="text-xl font-bold bg-gradient-to-r from-blue-400 to-purple-500 bg-clip-text text-transparent">
              KT-AI-Studio
            </h1>
            <ThemeToggle />
          </div>

          <div className="flex-1 flex flex-col overflow-y-auto py-4">
            <nav className="flex-1 px-2 space-y-1">
              {navigation.map((item) => (
                <NavLink
                  key={item.name}
                  to={item.href}
                  onClick={() => setIsOpen(false)} // Close on mobile click
                  className={({ isActive }) =>
                    cn(
                      isActive
                        ? "bg-accent text-accent-foreground"
                        : "text-muted-foreground hover:bg-accent/50 hover:text-foreground",
                      "group flex items-center px-2 py-2 text-sm font-medium rounded-md transition-colors"
                    )
                  }
                >
                  <item.icon
                    className={cn(
                        "mr-3 flex-shrink-0 h-5 w-5",
                    )}
                    aria-hidden="true"
                  />
                  {item.name}
                </NavLink>
              ))}

              <div className="pt-2">
                <button
                  type="button"
                  onClick={() => setDramaOpen((prev) => !prev)}
                  className={cn(
                    dramaGroupActive ? "bg-accent text-accent-foreground" : "text-muted-foreground hover:bg-accent/50 hover:text-foreground",
                    "group flex w-full items-center justify-between px-2 py-2 text-sm font-medium rounded-md transition-colors",
                  )}
                >
                  <span className="flex items-center">
                    <Clapperboard className="mr-3 h-5 w-5 flex-shrink-0" />
                    AI短剧
                  </span>
                  <ChevronDown className={cn("h-4 w-4 transition-transform", dramaOpen ? "rotate-180" : "rotate-0")} />
                </button>

                {dramaOpen ? (
                  <div className="mt-1 space-y-1 pl-3">
                    {dramaNavigation.map((item) => (
                      <NavLink
                        key={item.name}
                        to={item.href}
                        onClick={() => setIsOpen(false)}
                        className={({ isActive }) =>
                          cn(
                            isActive
                              ? "bg-accent text-accent-foreground"
                              : "text-muted-foreground hover:bg-accent/50 hover:text-foreground",
                            "group flex items-center px-2 py-2 text-sm font-medium rounded-md transition-colors",
                          )
                        }
                      >
                        <item.icon className="mr-3 h-5 w-5 flex-shrink-0" aria-hidden="true" />
                        {item.name}
                      </NavLink>
                    ))}
                  </div>
                ) : null}
              </div>

              <div className="pt-2">
                <button
                  type="button"
                  onClick={() => setTemplateOpen((prev) => !prev)}
                  className={cn(
                    templateGroupActive ? "bg-accent text-accent-foreground" : "text-muted-foreground hover:bg-accent/50 hover:text-foreground",
                    "group flex w-full items-center justify-between px-2 py-2 text-sm font-medium rounded-md transition-colors",
                  )}
                >
                  <span className="flex items-center">
                    <Tags className="mr-3 h-5 w-5 flex-shrink-0" />
                    讲解模板
                  </span>
                  <ChevronDown className={cn("h-4 w-4 transition-transform", templateOpen ? "rotate-180" : "rotate-0")} />
                </button>

                {templateOpen ? (
                  <div className="mt-1 space-y-1 pl-3">
                    {templateNavigation.map((item) => (
                      <NavLink
                        key={item.name}
                        to={item.href}
                        onClick={() => setIsOpen(false)}
                        className={({ isActive }) =>
                          cn(
                            isActive
                              ? "bg-accent text-accent-foreground"
                              : "text-muted-foreground hover:bg-accent/50 hover:text-foreground",
                            "group flex items-center px-2 py-2 text-sm font-medium rounded-md transition-colors",
                          )
                        }
                      >
                        <item.icon className="mr-3 h-5 w-5 flex-shrink-0" aria-hidden="true" />
                        {item.name}
                      </NavLink>
                    ))}
                  </div>
                ) : null}
              </div>

              <div className="pt-2">
                <button
                  type="button"
                  onClick={() => setAudioCloneOpen((prev) => !prev)}
                  className={cn(
                    audioCloneGroupActive ? "bg-accent text-accent-foreground" : "text-muted-foreground hover:bg-accent/50 hover:text-foreground",
                    "group flex w-full items-center justify-between px-2 py-2 text-sm font-medium rounded-md transition-colors",
                  )}
                >
                  <span className="flex items-center">
                    <Mic2 className="mr-3 h-5 w-5 flex-shrink-0" />
                    音频复制
                  </span>
                  <ChevronDown className={cn("h-4 w-4 transition-transform", audioCloneOpen ? "rotate-180" : "rotate-0")} />
                </button>

                {audioCloneOpen ? (
                  <div className="mt-1 space-y-1 pl-3">
                    <div className="px-2 pt-1 text-[11px] font-semibold uppercase tracking-wide text-muted-foreground/70">语音克隆</div>
                    {audioCloneNavigation.map((item) => (
                      <NavLink
                        key={item.name}
                        to={item.href}
                        onClick={() => setIsOpen(false)}
                        className={({ isActive }) =>
                          cn(
                            isActive
                              ? "bg-accent text-accent-foreground"
                              : "text-muted-foreground hover:bg-accent/50 hover:text-foreground",
                            "group flex items-center px-2 py-2 text-sm font-medium rounded-md transition-colors",
                          )
                        }
                      >
                        <item.icon className="mr-3 h-5 w-5 flex-shrink-0" aria-hidden="true" />
                        {item.name}
                      </NavLink>
                    ))}
                    <div className="px-2 pt-3 text-[11px] font-semibold uppercase tracking-wide text-muted-foreground/70">音频生产</div>
                    {audioProductionNavigation.map((item) => (
                      <NavLink
                        key={item.name}
                        to={item.href}
                        onClick={() => setIsOpen(false)}
                        className={({ isActive }) =>
                          cn(
                            isActive
                              ? "bg-accent text-accent-foreground"
                              : "text-muted-foreground hover:bg-accent/50 hover:text-foreground",
                            "group flex items-center px-2 py-2 text-sm font-medium rounded-md transition-colors",
                          )
                        }
                      >
                        <item.icon className="mr-3 h-5 w-5 flex-shrink-0" aria-hidden="true" />
                        {item.name}
                      </NavLink>
                    ))}
                  </div>
                ) : null}
              </div>
            </nav>
          </div>
        </div>
      </div>

      {/* Overlay for mobile */}
      {isOpen && (
        <div 
          className="fixed inset-0 bg-black/50 z-30 md:hidden"
          onClick={() => setIsOpen(false)}
        />
      )}
    </>
  );
}
