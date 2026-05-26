import { BrowserRouter, Routes, Route } from "react-router-dom";
import Layout from "./components/Layout";
import Dashboard from "./pages/Dashboard";
import Projects from "./pages/Projects";
import ProjectDetail from "./pages/ProjectDetail";
import Styles from "./pages/Styles";
import AutoGenerateTagsPage from "./pages/AutoGenerateTags";
import MultiVisuals from "./pages/MultiVisuals";
import MultiVisualProjectDetail from "./pages/MultiVisualProjectDetail";
import StoreVisits from "./pages/StoreVisits";
import StoreVisitProjectDetail from "./pages/StoreVisitProjectDetail";
import GeneralGuideProjects from "./pages/GeneralGuideProjects";
import GeneralGuideProjectDetail from "./pages/GeneralGuideProjectDetail";
import GeneralGuideTagsPage from "./pages/GeneralGuideTags";
import AudioCloneProjects from "./pages/AudioCloneProjects";
import AudioCloneProjectDetail from "./pages/AudioCloneProjectDetail";
import QwenTTSProjects from "./pages/QwenTTSProjects";
import QwenTTSProjectDetail from "./pages/QwenTTSProjectDetail";
import AudioProductionProjects from "./pages/AudioProductionProjects";
import AudioProductionProjectDetail from "./pages/AudioProductionProjectDetail";
import LLMEngine from "./pages/LLMEngine";
import Logs from "./pages/Logs";
import Settings from "./pages/Settings";
import { ThemeProvider } from "./components/ThemeProvider";
import { Toaster } from "sonner";

function App() {
  return (
    <ThemeProvider defaultTheme="light" storageKey="vite-ui-theme">
      <BrowserRouter>
        <Routes>
          <Route path="/" element={<Layout />}>
            <Route index element={<Dashboard />} />
            <Route path="projects" element={<Projects />} />
            <Route path="projects/:id" element={<ProjectDetail />} />
            <Route path="styles" element={<Styles />} />
            <Route path="auto-generate-tags" element={<AutoGenerateTagsPage />} />
            <Route path="multi-visuals" element={<MultiVisuals />} />
            <Route path="multi-visuals/:id" element={<MultiVisualProjectDetail />} />
            <Route path="store-visits" element={<StoreVisits />} />
            <Route path="store-visits/:id" element={<StoreVisitProjectDetail />} />
            <Route path="general-guide-tags" element={<GeneralGuideTagsPage />} />
            <Route path="general-guides" element={<GeneralGuideProjects />} />
            <Route path="general-guides/:id" element={<GeneralGuideProjectDetail />} />
            <Route path="audio-clone-projects" element={<AudioCloneProjects />} />
            <Route path="audio-clone-projects/:id" element={<AudioCloneProjectDetail />} />
            <Route path="qwen-tts-projects" element={<QwenTTSProjects />} />
            <Route path="qwen-tts-projects/:id" element={<QwenTTSProjectDetail />} />
            <Route path="audio-production/custom-voice" element={<AudioProductionProjects mode="custom_voice" />} />
            <Route path="audio-production/custom-voice/:id" element={<AudioProductionProjectDetail mode="custom_voice" />} />
            <Route path="audio-production/voice-prompt" element={<AudioProductionProjects mode="voice_prompt" />} />
            <Route path="audio-production/voice-prompt/:id" element={<AudioProductionProjectDetail mode="voice_prompt" />} />
            <Route path="llm" element={<LLMEngine />} />
            <Route path="logs" element={<Logs />} />
            <Route path="settings" element={<Settings />} />
          </Route>
        </Routes>
      </BrowserRouter>
      <Toaster 
        position="top-center" 
        richColors 
        closeButton 
        toastOptions={{
          className: "text-lg p-6 font-medium shadow-2xl border-2 min-w-[350px] flex justify-center items-center gap-3",
          duration: 3000,
          style: {
             marginTop: '40vh',
             transform: 'scale(1.3)',
          }
        }}
      />
    </ThemeProvider>
  );
}

export default App;
