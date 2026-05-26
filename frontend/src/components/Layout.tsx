import { Outlet } from "react-router-dom";
import { Sidebar } from "./Sidebar";

export default function Layout() {
  return (
    <div className="min-h-screen bg-background text-foreground">
      <Sidebar />
      <div className="md:pl-64 flex flex-col flex-1 min-h-screen pt-16 md:pt-0 transition-all duration-300">
        <main className="flex-1 py-6 px-4 sm:px-6 md:px-8">
            <Outlet />
        </main>
      </div>
    </div>
  );
}
