"use client";

import { useState, useEffect } from "react";

interface Repo {
  id: number;
  full_name: string;
  is_active: boolean;
}

interface RepoSettings {
  is_active: boolean;
  security: boolean;
  bug: boolean;
  lint: boolean;
  performance: boolean;
  architecture: boolean;
}

const LAYER_ICONS: Record<string, string> = {
  security: "🔒",
  bug: "🐛",
  lint: "✨",
  performance: "⚡",
  architecture: "🏗️",
};

export default function Dashboard() {
  const [user, setUser] = useState<{ github_id: string; api_key: string } | null>(null);
  const [authLoading, setAuthLoading] = useState(true);
  const [repos, setRepos] = useState<Repo[]>([]);
  const [loadingRepos, setLoadingRepos] = useState(false);

  // UI State
  const [globalKey, setGlobalKey] = useState("");
  const [savingKey, setSavingKey] = useState(false);
  const [keyMessage, setKeyMessage] = useState<{ type: "success" | "error"; text: string } | null>(null);

  // Expanded Repo State
  const [expandedRepoId, setExpandedRepoId] = useState<number | null>(null);
  const [repoSettings, setRepoSettings] = useState<RepoSettings | null>(null);
  const [loadingSettings, setLoadingSettings] = useState(false);
  const [savingSettings, setSavingSettings] = useState(false);

  // Fetch User & Repos
  useEffect(() => {
    fetch("http://localhost:8080/api/me", { credentials: "include" })
      .then(async (res) => {
        if (res.ok) {
          const data = await res.json();
          setUser(data);
          setGlobalKey(data.api_key || "");
          fetchRepos();
        } else {
          setUser(null);
        }
      })
      .catch(() => setUser(null))
      .finally(() => setAuthLoading(false));
  }, []);

  const fetchRepos = async () => {
    setLoadingRepos(true);
    try {
      const res = await fetch("http://localhost:8080/api/repos", { credentials: "include" });
      if (res.ok) {
        const data = await res.json();
        setRepos(data || []);
      }
    } catch (e) {
      console.error("Failed to fetch repos", e);
    }
    setLoadingRepos(false);
  };

  const login = () => {
    window.location.href = "http://localhost:8080/auth/login";
  };

  const saveGlobalKey = async (keyToSave: string) => {
    setSavingKey(true);
    setKeyMessage(null);
    try {
      await fetch("http://localhost:8080/api/users/apikey", {
        method: "PUT",
        headers: { "Content-Type": "application/json" },
        credentials: "include",
        body: JSON.stringify({ api_key: keyToSave }),
      });
      setGlobalKey(keyToSave);
      setKeyMessage({ type: "success", text: keyToSave ? "API Key saved!" : "API Key cleared (using default)" });
    } catch (e) {
      setKeyMessage({ type: "error", text: "Failed to save" });
    }
    setSavingKey(false);
    setTimeout(() => setKeyMessage(null), 3000);
  };

  const clearApiKey = () => {
    if (confirm("Clear your API key? The system will use the default key if available.")) {
      saveGlobalKey("");
    }
  };

  const toggleRepoActive = async (repo: Repo) => {
    const newStatus = !repo.is_active;
    setRepos(repos.map(r => r.id === repo.id ? { ...r, is_active: newStatus } : r));
    try {
      await fetchSettingsAndUpdate(repo.id, { is_active: newStatus });
    } catch (e) {
      console.error(e);
      setRepos(repos.map(r => r.id === repo.id ? { ...r, is_active: !newStatus } : r));
    }
  };

  const fetchSettingsAndUpdate = async (repoID: number, patch: Partial<RepoSettings>) => {
    const res = await fetch(`http://localhost:8080/api/settings/${repoID}`, { credentials: "include" });
    let current: RepoSettings = {
      is_active: true, security: true, bug: true, lint: true, performance: true, architecture: true
    };
    if (res.ok) {
      const data = await res.json();
      current = {
        is_active: data.is_active,
        security: data.security_enabled,
        bug: data.bug_enabled,
        lint: data.lint_enabled,
        performance: data.performance_enabled,
        architecture: data.architecture_enabled
      };
    }
    const updated = { ...current, ...patch };
    await fetch("http://localhost:8080/api/settings", {
      method: "PUT",
      headers: { "Content-Type": "application/json" },
      credentials: "include",
      body: JSON.stringify({
        repo_id: repoID.toString(),
        is_active: updated.is_active,
        layers: {
          security_enabled: updated.security,
          bug_enabled: updated.bug,
          lint_enabled: updated.lint,
          performance_enabled: updated.performance,
          architecture_enabled: updated.architecture
        }
      })
    });
  };

  const openRepoSettings = async (repoID: number) => {
    if (expandedRepoId === repoID) {
      setExpandedRepoId(null);
      return;
    }
    setExpandedRepoId(repoID);
    setLoadingSettings(true);
    try {
      const res = await fetch(`http://localhost:8080/api/settings/${repoID}`, { credentials: "include" });
      if (res.ok) {
        const data = await res.json();
        setRepoSettings({
          is_active: data.is_active,
          security: data.security_enabled,
          bug: data.bug_enabled,
          lint: data.lint_enabled,
          performance: data.performance_enabled,
          architecture: data.architecture_enabled
        });
      }
    } catch (e) { console.error(e); }
    setLoadingSettings(false);
  };

  const saveRepoLayers = async (repoID: number) => {
    if (!repoSettings) return;
    setSavingSettings(true);
    await fetchSettingsAndUpdate(repoID, repoSettings);
    setSavingSettings(false);
    setExpandedRepoId(null);
  };

  if (authLoading) {
    return (
      <div className="min-h-screen bg-gradient-to-br from-slate-900 via-slate-800 to-slate-900 flex items-center justify-center">
        <div className="animate-pulse text-blue-400 text-xl font-medium">Loading...</div>
      </div>
    );
  }

  if (!user) {
    return (
      <main className="min-h-screen bg-gradient-to-br from-slate-900 via-slate-800 to-slate-900 text-white flex flex-col items-center justify-center p-8">
        <div className="max-w-md w-full bg-slate-800/80 backdrop-blur-sm p-10 rounded-2xl shadow-2xl border border-slate-700/50 text-center">
          <div className="w-16 h-16 mx-auto mb-6 bg-gradient-to-br from-blue-500 to-purple-600 rounded-2xl flex items-center justify-center shadow-lg">
            <span className="text-3xl">🤖</span>
          </div>
          <h1 className="text-3xl font-bold mb-3 bg-gradient-to-r from-blue-400 to-purple-400 bg-clip-text text-transparent">AI Code Review</h1>
          <p className="text-slate-400 mb-8 text-sm leading-relaxed">
            Automated code reviews powered by AI.<br />
            <span className="text-slate-500">(Requires @appointy.com email)</span>
          </p>
          <button onClick={login} className="w-full py-3.5 bg-white text-slate-900 rounded-xl font-bold text-base hover:bg-slate-100 transition-all duration-200 flex items-center justify-center gap-3 shadow-lg hover:shadow-xl hover:-translate-y-0.5">
            <svg viewBox="0 0 24 24" className="w-5 h-5" fill="currentColor"><path d="M12 0c-6.626 0-12 5.373-12 12 0 5.302 3.438 9.8 8.207 11.387.599.111.793-.261.793-.577v-2.234c-3.338.726-4.033-1.416-4.033-1.416-.546-1.387-1.333-1.756-1.333-1.756-1.089-.745.083-.729.083-.729 1.205.084 1.839 1.237 1.839 1.237 1.07 1.834 2.807 1.304 3.492.997.107-.775.418-1.305.762-1.604-2.665-.305-5.467-1.334-5.467-5.931 0-1.311.469-2.381 1.236-3.221-.124-.303-.535-1.524.117-3.176 0 0 1.008-.322 3.301 1.23.957-.266 1.983-.399 3.003-.404 1.02.005 2.047.138 3.006.404 2.291-1.552 3.297-1.23 3.297-1.23.653 1.653.242 2.874.118 3.176.77.84 1.235 1.911 1.235 3.221 0 4.609-2.807 5.624-5.479 5.921.43.372.823 1.102.823 2.222v3.293c0 .319.192.694.801.576 4.765-1.589 8.199-6.086 8.199-11.386 0-6.627-5.373-12-12-12z" /></svg>
            Sign in with GitHub
          </button>
        </div>
      </main>
    );
  }

  const activeCount = repos.filter(r => r.is_active).length;

  return (
    <main className="min-h-screen bg-gradient-to-br from-slate-900 via-slate-800 to-slate-900 text-white">
      {/* Navigation Bar */}
      <nav className="border-b border-slate-700/50 bg-slate-900/50 backdrop-blur-sm sticky top-0 z-10">
        <div className="max-w-5xl mx-auto px-6 py-4 flex justify-between items-center">
          <div className="flex items-center gap-3">
            <div className="w-9 h-9 bg-gradient-to-br from-blue-500 to-purple-600 rounded-lg flex items-center justify-center">
              <span className="text-lg">🤖</span>
            </div>
            <span className="font-bold text-lg">AI Code Review</span>
          </div>
          <div className="flex items-center gap-4">
            <span className="text-sm text-slate-400">ID: {user.github_id}</span>
            <a
              href="https://github.com/apps/code-review-new/installations/new"
              target="_blank"
              className="px-4 py-2 bg-gradient-to-r from-red-600 to-orange-600 hover:from-red-500 hover:to-orange-500 rounded-lg font-semibold text-sm transition-all duration-200 flex items-center gap-2 shadow-lg hover:shadow-red-500/25"
            >
              <svg className="w-4 h-4" fill="currentColor" viewBox="0 0 24 24"><path d="M12 0c-6.626 0-12 5.373-12 12 0 5.302 3.438 9.8 8.207 11.387.599.111.793-.261.793-.577v-2.234c-3.338.726-4.033-1.416-4.033-1.416-.546-1.387-1.333-1.756-1.333-1.756-1.089-.745.083-.729.083-.729 1.205.084 1.839 1.237 1.839 1.237 1.07 1.834 2.807 1.304 3.492.997.107-.775.418-1.305.762-1.604-2.665-.305-5.467-1.334-5.467-5.931 0-1.311.469-2.381 1.236-3.221-.124-.303-.535-1.524.117-3.176 0 0 1.008-.322 3.301 1.23.957-.266 1.983-.399 3.003-.404 1.02.005 2.047.138 3.006.404 2.291-1.552 3.297-1.23 3.297-1.23.653 1.653.242 2.874.118 3.176.77.84 1.235 1.911 1.235 3.221 0 4.609-2.807 5.624-5.479 5.921.43.372.823 1.102.823 2.222v3.293c0 .319.192.694.801.576 4.765-1.589 8.199-6.086 8.199-11.386 0-6.627-5.373-12-12-12z" /></svg>
              GitHub App (Legacy)
            </a>
            <a
              href="https://github.com/apps/YOUR_NEW_APP_NAME/installations/new"
              target="_blank"
              className="px-4 py-2 bg-gradient-to-r from-blue-600 to-indigo-600 hover:from-blue-500 hover:to-indigo-500 rounded-lg font-semibold text-sm transition-all duration-200 flex items-center gap-2 shadow-lg hover:shadow-blue-500/25"
            >
              <svg className="w-4 h-4" fill="currentColor" viewBox="0 0 24 24"><path d="M12 0c-6.626 0-12 5.373-12 12 0 5.302 3.438 9.8 8.207 11.387.599.111.793-.261.793-.577v-2.234c-3.338.726-4.033-1.416-4.033-1.416-.546-1.387-1.333-1.756-1.333-1.756-1.089-.745.083-.729.083-.729 1.205.084 1.839 1.237 1.839 1.237 1.07 1.834 2.807 1.304 3.492.997.107-.775.418-1.305.762-1.604-2.665-.305-5.467-1.334-5.467-5.931 0-1.311.469-2.381 1.236-3.221-.124-.303-.535-1.524.117-3.176 0 0 1.008-.322 3.301 1.23.957-.266 1.983-.399 3.003-.404 1.02.005 2.047.138 3.006.404 2.291-1.552 3.297-1.23 3.297-1.23.653 1.653.242 2.874.118 3.176.77.84 1.235 1.911 1.235 3.221 0 4.609-2.807 5.624-5.479 5.921.43.372.823 1.102.823 2.222v3.293c0 .319.192.694.801.576 4.765-1.589 8.199-6.086 8.199-11.386 0-6.627-5.373-12-12-12z" /></svg>
              GitHub App (Actions)
            </a>
          </div>
        </div>
      </nav>

      <div className="max-w-5xl mx-auto px-6 py-8 space-y-8">
        {/* API Key Section */}
        <section className="bg-slate-800/50 backdrop-blur-sm rounded-2xl border border-slate-700/50 overflow-hidden">
          <div className="px-6 py-4 border-b border-slate-700/50 bg-slate-800/30">
            <h2 className="font-semibold text-lg flex items-center gap-2">
              <span>🔑</span> API Configuration
            </h2>
          </div>
          <div className="p-6">
            <div className="flex flex-col sm:flex-row gap-4">
              <div className="flex-1">
                <label className="block text-sm text-slate-400 mb-2">Gemini API Key</label>
                <input
                  type="password"
                  value={globalKey}
                  onChange={(e) => setGlobalKey(e.target.value)}
                  className="w-full bg-slate-900/50 border border-slate-600 p-3 rounded-xl focus:ring-2 focus:ring-blue-500 focus:border-transparent outline-none transition-all placeholder:text-slate-500"
                  placeholder={globalKey ? "••••••••••••" : "Enter your API key (optional)"}
                />
                <p className="text-xs text-slate-500 mt-2">
                  {globalKey ? "Custom API key configured" : "Using default system key"}
                </p>
              </div>
              <div className="flex gap-2 sm:items-end">
                <button
                  onClick={() => saveGlobalKey(globalKey)}
                  disabled={savingKey}
                  className="px-5 py-3 bg-blue-600 hover:bg-blue-500 rounded-xl font-semibold transition-all duration-200 disabled:opacity-50 h-[46px]"
                >
                  {savingKey ? "Saving..." : "Save"}
                </button>
                {globalKey && (
                  <button
                    onClick={clearApiKey}
                    disabled={savingKey}
                    className="px-5 py-3 bg-slate-700 hover:bg-red-600/80 rounded-xl font-semibold transition-all duration-200 disabled:opacity-50 h-[46px] text-slate-300 hover:text-white"
                  >
                    Clear
                  </button>
                )}
              </div>
            </div>
            {keyMessage && (
              <div className={`mt-4 px-4 py-2 rounded-lg text-sm ${keyMessage.type === "success" ? "bg-green-500/20 text-green-300" : "bg-red-500/20 text-red-300"}`}>
                {keyMessage.text}
              </div>
            )}
          </div>
        </section>

        {/* Stats Bar */}
        <div className="grid grid-cols-3 gap-4">
          <div className="bg-slate-800/50 backdrop-blur-sm rounded-xl border border-slate-700/50 p-4 text-center">
            <p className="text-2xl font-bold text-white">{repos.length}</p>
            <p className="text-xs text-slate-400 mt-1">Total Repos</p>
          </div>
          <div className="bg-slate-800/50 backdrop-blur-sm rounded-xl border border-slate-700/50 p-4 text-center">
            <p className="text-2xl font-bold text-green-400">{activeCount}</p>
            <p className="text-xs text-slate-400 mt-1">Active</p>
          </div>
          <div className="bg-slate-800/50 backdrop-blur-sm rounded-xl border border-slate-700/50 p-4 text-center">
            <p className="text-2xl font-bold text-slate-500">{repos.length - activeCount}</p>
            <p className="text-xs text-slate-400 mt-1">Paused</p>
          </div>
        </div>

        {/* Repositories Section */}
        <section className="bg-slate-800/50 backdrop-blur-sm rounded-2xl border border-slate-700/50 overflow-hidden">
          <div className="px-6 py-4 border-b border-slate-700/50 bg-slate-800/30">
            <h2 className="font-semibold text-lg flex items-center gap-2">
              <span>📦</span> Repositories
            </h2>
          </div>

          {loadingRepos ? (
            <div className="p-12 text-center text-slate-500">
              <div className="animate-spin w-8 h-8 border-2 border-blue-500 border-t-transparent rounded-full mx-auto mb-4"></div>
              Loading repositories...
            </div>
          ) : repos.length === 0 ? (
            <div className="p-12 text-center">
              <div className="w-16 h-16 mx-auto mb-4 bg-slate-700/50 rounded-2xl flex items-center justify-center">
                <span className="text-3xl opacity-50">📭</span>
              </div>
              <p className="text-slate-400 mb-2">No repositories found</p>
              <p className="text-sm text-slate-500">Install the GitHub App to get started</p>
            </div>
          ) : (
            <div className="divide-y divide-slate-700/50">
              {repos.map(repo => (
                <div key={repo.id} className="transition-colors hover:bg-slate-700/20">
                  <div className="px-6 py-4 flex items-center justify-between">
                    <div className="flex items-center gap-4">
                      {/* Toggle */}
                      <button
                        onClick={() => toggleRepoActive(repo)}
                        className={`relative w-12 h-7 rounded-full transition-all duration-300 ${repo.is_active ? "bg-green-500" : "bg-slate-600"}`}
                      >
                        <div className={`absolute top-1 w-5 h-5 bg-white rounded-full shadow-md transition-all duration-300 ${repo.is_active ? "left-6" : "left-1"}`}></div>
                      </button>
                      {/* Repo Info */}
                      <div>
                        <span className="font-medium text-base">{repo.full_name}</span>
                        <span className={`ml-3 text-xs px-2 py-0.5 rounded-full ${repo.is_active ? "bg-green-500/20 text-green-400" : "bg-slate-600/50 text-slate-400"}`}>
                          {repo.is_active ? "Active" : "Paused"}
                        </span>
                      </div>
                    </div>
                    <button
                      onClick={() => openRepoSettings(repo.id)}
                      className="text-sm text-slate-400 hover:text-white transition-colors flex items-center gap-1"
                    >
                      <span>{expandedRepoId === repo.id ? "▲" : "▼"}</span>
                      <span className="hidden sm:inline">{expandedRepoId === repo.id ? "Hide" : "Layers"}</span>
                    </button>
                  </div>

                  {/* Expanded Settings */}
                  {expandedRepoId === repo.id && (
                    <div className="px-6 pb-6 pt-2 bg-slate-900/30">
                      {loadingSettings || !repoSettings ? (
                        <p className="text-sm text-slate-500">Loading...</p>
                      ) : (
                        <div className="space-y-4">
                          <div className="grid grid-cols-2 sm:grid-cols-5 gap-3">
                            {Object.entries(repoSettings).map(([key, val]) => {
                              if (key === "is_active") return null;
                              return (
                                <label
                                  key={key}
                                  className={`flex items-center gap-2 p-3 rounded-xl border cursor-pointer transition-all ${val
                                    ? "bg-blue-500/20 border-blue-500/50 text-blue-300"
                                    : "bg-slate-800/50 border-slate-600 text-slate-400 hover:border-slate-500"
                                    }`}
                                >
                                  <input
                                    type="checkbox"
                                    checked={val}
                                    onChange={() => setRepoSettings({ ...repoSettings, [key]: !val })}
                                    className="sr-only"
                                  />
                                  <span className="text-base">{LAYER_ICONS[key] || "•"}</span>
                                  <span className="capitalize text-sm font-medium">{key}</span>
                                </label>
                              );
                            })}
                          </div>
                          <div className="flex justify-end">
                            <button
                              onClick={() => saveRepoLayers(repo.id)}
                              disabled={savingSettings}
                              className="px-5 py-2 bg-blue-600 hover:bg-blue-500 rounded-lg text-sm font-semibold transition-all disabled:opacity-50"
                            >
                              {savingSettings ? "Saving..." : "Save Layers"}
                            </button>
                          </div>
                        </div>
                      )}
                    </div>
                  )}
                </div>
              ))}
            </div>
          )}
        </section>
      </div>
    </main>
  );
}
