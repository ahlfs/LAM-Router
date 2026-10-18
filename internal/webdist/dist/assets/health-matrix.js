import { c as ae, o as de, a as ee, p as O, t as S, e as $, j as e, G as le, B as L, r as N, K as ie, C as ce, Z as K, q as se, s as U, X as te, v as xe, i as re, P as H } from "./index-yz9zKHrD.js";

// Lucide SVG Icons (Strictly zero raw emojis)
const IconActivity = ae("activity", [["path", { d: "M22 12h-4l-3 9L9 3l-3 9H2", key: "act1" }]]);
const IconCheckCircle = ae("check-circle-2", [["circle", { cx: "12", cy: "12", r: "10", key: "c1" }], ["path", { d: "m9 12 2 2 4-4", key: "c2" }]]);
const IconAlertCircle = ae("alert-circle", [["circle", { cx: "12", cy: "12", r: "10", key: "a1" }], ["line", { x1: "12", x2: "12", y1: "8", y2: "12", key: "a2" }], ["line", { x1: "12", x2: "12.01", y1: "16", y2: "16", key: "a3" }]]);
const IconRefreshCw = ae("refresh-cw", [["path", { d: "M3 12a9 9 0 0 1 9-9 9.75 9.75 0 0 1 6.74 2.74L21 8", key: "r1" }], ["path", { d: "M21 3v5h-5", key: "r2" }], ["path", { d: "M21 12a9 9 0 0 1-9 9 9.75 9.75 0 0 1-6.74-2.74L3 16", key: "r3" }], ["path", { d: "M8 16H3v5", key: "r4" }]]);
const IconZap = ae("zap", [["polygon", { points: "13 2 3 14 12 14 11 22 21 10 12 10 13 2", key: "z1" }]]);
const IconSearch = ae("search", [["circle", { cx: "11", cy: "11", r: "8", key: "s1" }], ["line", { x1: "21", x2: "16.65", y1: "21", y2: "16.65", key: "s2" }]]);
const IconHelpCircle = ae("help-circle", [["circle", { cx: "12", cy: "12", r: "10", key: "h1" }], ["path", { d: "M9.09 9a3 3 0 0 1 5.83 1c0 2-3 3-3 3", key: "h2" }], ["line", { x1: "12", x2: "12.01", y1: "17", y2: "17", key: "h3" }]]);
const IconClock = ae("clock", [["circle", { cx: "12", cy: "12", r: "10", key: "clk1" }], ["polyline", { points: "12 6 12 12 16 14", key: "clk2" }]]);
const IconSlash = ae("ban", [["circle", { cx: "12", cy: "12", r: "10", key: "b1" }], ["path", { d: "m4.93 4.93 14.14 14.14", key: "b2" }]]);
const IconPower = ae("power", [["path", { d: "M18.36 6.64a9 9 0 1 1-12.73 0", key: "pw1" }], ["line", { x1: "12", x2: "12", y1: "2", y2: "12", key: "pw2" }]]);
const IconClose = ae("x", [["line", { x1: "18", x2: "6", y1: "6", y2: "18", key: "x1" }], ["line", { x1: "6", x2: "18", y1: "6", y2: "18", key: "x2" }]]);

function formatRelativeTime(timestamp) {
  if (!timestamp) return "Never";
  const seconds = Math.floor((Date.now() / 1000) - timestamp);
  if (seconds < 5) return "Just now";
  if (seconds < 60) return `${seconds}s ago`;
  const minutes = Math.floor(seconds / 60);
  if (minutes < 60) return `${minutes}m ago`;
  const hours = Math.floor(minutes / 60);
  if (hours < 24) return `${hours}h ago`;
  return `${Math.floor(hours / 24)}d ago`;
}

function LatencyBadge({ latency }) {
  if (latency <= 0) return e.jsx("span", { className: "text-muted-foreground/50 font-mono text-xs", children: "--" });
  let colorClass = "bg-emerald-500/10 text-emerald-600 dark:text-emerald-400 border-emerald-500/20";
  if (latency > 800) {
    colorClass = "bg-rose-500/10 text-rose-600 dark:text-rose-400 border-rose-500/20";
  } else if (latency > 350) {
    colorClass = "bg-amber-500/10 text-amber-600 dark:text-amber-400 border-amber-500/20";
  }
  return e.jsxs("span", {
    className: `inline-flex items-center gap-1 rounded-md border px-2 py-0.5 text-xs font-mono font-bold ${colorClass}`,
    children: [e.jsx(IconZap, { className: "size-3 shrink-0" }), `${latency} ms`]
  });
}

function StatusBadge({ status, statusCode, isEnabled }) {
  return e.jsxs("div", {
    className: "flex items-center gap-1.5 flex-wrap",
    children: [
      status === "healthy" ? e.jsxs("span", {
        className: "inline-flex items-center gap-1.5 rounded-full border border-emerald-500/30 bg-emerald-500/10 px-2.5 py-0.5 text-[11px] font-bold text-emerald-600 dark:text-emerald-400",
        children: [e.jsx(IconCheckCircle, { className: "size-3.5 shrink-0" }), "HEALTHY"]
      }) : status === "offline" ? e.jsxs("span", {
        className: "inline-flex items-center gap-1.5 rounded-full border border-rose-500/30 bg-rose-500/10 px-2.5 py-0.5 text-[11px] font-bold text-rose-600 dark:text-rose-400",
        children: [e.jsx(IconAlertCircle, { className: "size-3.5 shrink-0" }), statusCode ? `OFFLINE (${statusCode})` : "OFFLINE"]
      }) : status === "probing" ? e.jsxs("span", {
        className: "inline-flex items-center gap-1.5 rounded-full border border-sky-500/30 bg-sky-500/10 px-2.5 py-0.5 text-[11px] font-bold text-sky-600 dark:text-sky-400",
        children: [e.jsx(IconRefreshCw, { className: "size-3.5 shrink-0 animate-spin" }), "PROBING"]
      }) : e.jsxs("span", {
        className: "inline-flex items-center gap-1.5 rounded-full border border-border/80 bg-secondary/50 px-2.5 py-0.5 text-[11px] font-medium text-muted-foreground",
        children: [e.jsx(IconHelpCircle, { className: "size-3.5 shrink-0" }), "UNTESTED"]
      }),
      isEnabled === false ? e.jsxs("span", {
        className: "inline-flex items-center gap-1 rounded-full border border-border/80 bg-secondary/90 px-2 py-0.5 text-[10px] font-bold text-muted-foreground",
        children: [e.jsx(IconSlash, { className: "size-2.5" }), "DISABLED"]
      }) : null
    ]
  });
}

function HealthMatrixPage() {
  const queryClient = de();
  const [search, setSearch] = N.useState("");
  const [statusFilter, setStatusFilter] = N.useState("all");
  const [probingModels, setProbingModels] = N.useState(new Set());
  const [togglingModels, setTogglingModels] = N.useState(new Set());
  const [isProbingAll, setIsProbingAll] = N.useState(false);
  const [confirmDisableOpen, setConfirmDisableOpen] = N.useState(false);
  const [isDisabling, setIsDisabling] = N.useState(false);

  const { data, isLoading } = ee({
    queryKey: ["model_health"],
    queryFn: async () => {
      try {
        return await $.get("/v1/health/models");
      } catch (err) {
        return { models: [], stats: { total: 0, healthy: 0, offline: 0, untested: 0, avg_latency_ms: 0 } };
      }
    },
    refetchInterval: 5000,
  });

  const models = (data && data.models) ? data.models : [];
  const stats = (data && data.stats) ? data.stats : { total: 0, healthy: 0, offline: 0, untested: 0, avg_latency_ms: 0 };
  const offlineModels = N.useMemo(() => models.filter(m => m.status === "offline" && m.is_enabled !== false), [models]);
  const disabledCount = N.useMemo(() => models.filter(m => m.is_enabled === false).length, [models]);
  const activeCount = N.useMemo(() => models.filter(m => m.is_enabled !== false).length, [models]);

  const handleProbeSingle = async (modelId) => {
    setProbingModels(prev => new Set(prev).add(modelId));
    try {
      const res = await $.post("/v1/health/models/probe", { model_id: modelId });
      queryClient.invalidateQueries({ queryKey: ["model_health"] });
      if (res && res.status === "healthy") {
        S.success(`${modelId}: Healthy (${res.latency_ms} ms)`);
      } else {
        S.error(`${modelId}: Offline (${res ? res.last_error || "Check failed" : "Error"})`);
      }
    } catch (err) {
      S.error(`Failed to probe ${modelId}: ${err.message || "Network error"}`);
    } finally {
      setProbingModels(prev => {
        const next = new Set(prev);
        next.delete(modelId);
        return next;
      });
    }
  };

  const handleToggleSingle = async (modelId, targetEnabled) => {
    setTogglingModels(prev => new Set(prev).add(modelId));
    try {
      await $.post("/v1/health/models/toggle", { model_id: modelId, enabled: targetEnabled });
      queryClient.invalidateQueries({ queryKey: ["model_health"] });
      queryClient.invalidateQueries({ queryKey: ["models"] });
      if (targetEnabled) {
        S.success(`${modelId} berhasil diaktifkan kembali!`);
      } else {
        S.success(`${modelId} dinonaktifkan dari katalog.`);
      }
    } catch (err) {
      S.error(`Gagal toggle model: ${err.message || "Error"}`);
    } finally {
      setTogglingModels(prev => {
        const next = new Set(prev);
        next.delete(modelId);
        return next;
      });
    }
  };

  const handleProbeAll = async () => {
    setIsProbingAll(true);
    S.success("Starting concurrent health probe on all active models...");
    try {
      await $.post("/v1/health/models/probe", { all: true });
      queryClient.invalidateQueries({ queryKey: ["model_health"] });
      S.success("All models probe completed!");
    } catch (err) {
      S.error(`Batch probe error: ${err.message || "Failed"}`);
    } finally {
      setIsProbingAll(false);
    }
  };

  const handleReset = async () => {
    try {
      await $.post("/v1/health/models/reset", {});
      queryClient.invalidateQueries({ queryKey: ["model_health"] });
      S.success("Model health records reset to Untested.");
    } catch (err) {
      S.error(`Reset failed: ${err.message || "Error"}`);
    }
  };

  const handleConfirmDisable = async () => {
    if (offlineModels.length === 0) return;
    setIsDisabling(true);
    try {
      const targetIds = offlineModels.map(m => m.model_id);
      const res = await $.post("/v1/health/models/disable-offline", { model_ids: targetIds });
      queryClient.invalidateQueries({ queryKey: ["model_health"] });
      queryClient.invalidateQueries({ queryKey: ["models"] });
      S.success(`${res.disabled_count ?? targetIds.length} model offline berhasil dinonaktifkan dari katalog!`);
      setConfirmDisableOpen(false);
    } catch (err) {
      S.error(`Gagal menonaktifkan model: ${err.message || "Error"}`);
    } finally {
      setIsDisabling(false);
    }
  };

  const filteredModels = N.useMemo(() => {
    const q = search.trim().toLowerCase();
    return models.filter(m => {
      const matchSearch = !q || m.model_id.toLowerCase().includes(q) || (m.provider_id && m.provider_id.toLowerCase().includes(q));
      if (!matchSearch) return false;
      if (statusFilter === "active") return m.is_enabled !== false;
      if (statusFilter === "disabled") return m.is_enabled === false;
      if (statusFilter === "healthy") return m.status === "healthy";
      if (statusFilter === "offline") return m.status === "offline";
      if (statusFilter === "untested") return m.status !== "healthy" && m.status !== "offline";
      return true;
    });
  }, [models, search, statusFilter]);

  return e.jsxs("div", {
    className: "mx-auto w-full max-w-6xl space-y-5 font-sans pb-12",
    children: [
      // Header Section
      e.jsxs("header", {
        className: "rounded-2xl border border-border/80 bg-card p-5 sm:p-6 shadow-2xs",
        children: [
          e.jsxs("div", {
            className: "flex flex-col sm:flex-row sm:items-center justify-between gap-4 border-b border-border/60 pb-5",
            children: [
              e.jsxs("div", {
                className: "space-y-1 text-left min-w-0",
                children: [
                  e.jsxs("div", {
                    className: "flex items-center gap-2.5",
                    children: [
                      e.jsx("div", {
                        className: "flex size-8 items-center justify-center rounded-xl border border-border/80 bg-primary/10 text-primary shadow-2xs",
                        children: e.jsx(IconActivity, { className: "size-4" })
                      }),
                      e.jsx("h1", {
                        className: "text-lg sm:text-xl font-bold tracking-tight text-foreground",
                        children: "Model Health & Latency Matrix"
                      })
                    ]
                  }),
                  e.jsx("p", {
                    className: "text-xs text-muted-foreground leading-relaxed",
                    children: "Live upstream inference verification, latency diagnostics, and quota availability matrix."
                  })
                ]
              }),
              e.jsxs("div", {
                className: "flex items-center gap-2.5 shrink-0 flex-wrap",
                children: [
                  // Test All Models Button
                  e.jsxs("button", {
                    type: "button",
                    onClick: handleProbeAll,
                    disabled: isProbingAll,
                    className: "inline-flex h-9 items-center justify-center gap-1.5 rounded-lg bg-primary px-3.5 text-xs font-semibold text-primary-foreground shadow-xs hover:bg-primary/90 transition-all cursor-pointer disabled:opacity-50",
                    children: [
                      e.jsx(isProbingAll ? IconRefreshCw : IconZap, { className: `size-3.5 ${isProbingAll ? "animate-spin" : ""}` }),
                      e.jsx("span", { children: isProbingAll ? "Testing All..." : "Test All Models" })
                    ]
                  }),
                  // Disable Offline Models Button
                  e.jsxs("button", {
                    type: "button",
                    onClick: () => setConfirmDisableOpen(true),
                    disabled: offlineModels.length === 0 || isDisabling,
                    className: "inline-flex h-9 items-center justify-center gap-1.5 rounded-lg border border-rose-500/30 bg-rose-500/10 hover:bg-rose-500/20 px-3 text-xs font-semibold text-rose-600 dark:text-rose-400 transition-all cursor-pointer disabled:opacity-40 disabled:cursor-not-allowed shadow-2xs",
                    title: "Nonaktifkan semua model offline agar tidak muncul di /v1/models",
                    children: [
                      e.jsx(IconSlash, { className: "size-3.5 shrink-0" }),
                      e.jsxs("span", { children: ["Nonaktifkan Offline (", offlineModels.length, ")"] })
                    ]
                  }),
                  // Reset Button
                  e.jsxs("button", {
                    type: "button",
                    onClick: handleReset,
                    className: "inline-flex h-9 items-center justify-center gap-1.5 rounded-lg border border-border/80 bg-secondary/40 hover:bg-secondary px-3 text-xs font-semibold text-muted-foreground hover:text-foreground transition-all cursor-pointer",
                    children: [
                      e.jsx(IconRefreshCw, { className: "size-3.5" }),
                      e.jsx("span", { children: "Reset" })
                    ]
                  })
                ]
              })
            ]
          }),

          // KPI Cards
          e.jsxs("div", {
            className: "grid grid-cols-2 lg:grid-cols-4 gap-3 pt-4",
            children: [
              e.jsxs("div", {
                className: "rounded-xl border border-border/70 bg-secondary/20 p-3.5 space-y-1",
                children: [
                  e.jsx("span", { className: "text-[11px] font-semibold text-muted-foreground uppercase tracking-wider", children: "Active Models" }),
                  e.jsxs("div", {
                    className: "text-xl sm:text-2xl font-bold font-mono text-foreground",
                    children: [activeCount, " / ", models.length]
                  })
                ]
              }),
              e.jsxs("div", {
                className: "rounded-xl border border-emerald-500/20 bg-emerald-500/5 p-3.5 space-y-1",
                children: [
                  e.jsx("span", { className: "text-[11px] font-semibold text-emerald-600 dark:text-emerald-400 uppercase tracking-wider", children: "Healthy & Ready" }),
                  e.jsxs("div", {
                    className: "text-xl sm:text-2xl font-bold font-mono text-emerald-600 dark:text-emerald-400",
                    children: [stats.healthy, " Active"]
                  })
                ]
              }),
              e.jsxs("div", {
                className: "rounded-xl border border-rose-500/20 bg-rose-500/5 p-3.5 space-y-1",
                children: [
                  e.jsx("span", { className: "text-[11px] font-semibold text-rose-600 dark:text-rose-400 uppercase tracking-wider", children: "Offline / Degraded" }),
                  e.jsxs("div", {
                    className: "text-xl sm:text-2xl font-bold font-mono text-rose-600 dark:text-rose-400",
                    children: [stats.offline, " Error"]
                  })
                ]
              }),
              e.jsxs("div", {
                className: "rounded-xl border border-indigo-500/20 bg-indigo-500/5 p-3.5 space-y-1",
                children: [
                  e.jsx("span", { className: "text-[11px] font-semibold text-indigo-600 dark:text-indigo-400 uppercase tracking-wider", children: "Average Latency" }),
                  e.jsxs("div", {
                    className: "text-xl sm:text-2xl font-bold font-mono text-indigo-600 dark:text-indigo-400",
                    children: [stats.avg_latency_ms || "--", " ms"]
                  })
                ]
              })
            ]
          })
        ]
      }),

      // Filter & Search Toolbar
      e.jsxs("div", {
        className: "flex flex-col sm:flex-row items-stretch sm:items-center justify-between gap-3",
        children: [
          e.jsxs("div", {
            className: "relative flex-1 max-w-md",
            children: [
              e.jsx(IconSearch, { className: "absolute left-3 top-1/2 -translate-y-1/2 size-4 text-muted-foreground" }),
              e.jsx("input", {
                type: "text",
                value: search,
                onChange: ev => setSearch(ev.target.value),
                placeholder: "Search model ID or provider...",
                className: "h-9 w-full rounded-lg border border-border/80 bg-card pl-9 pr-3 text-xs text-foreground placeholder:text-muted-foreground/60 focus:outline-none focus:ring-1 focus:ring-primary font-mono transition-all"
              })
            ]
          }),
          e.jsx("div", {
            className: "inline-flex items-center rounded-lg border border-border/80 bg-card p-1 text-xs font-medium self-start sm:self-auto flex-wrap gap-1",
            children: [
              { id: "all", label: `All (${models.length})` },
              { id: "active", label: `Active (${activeCount})` },
              { id: "disabled", label: `Disabled (${disabledCount})` },
              { id: "healthy", label: `Healthy (${stats.healthy})` },
              { id: "offline", label: `Offline (${stats.offline})` }
            ].map(tab => e.jsx("button", {
              key: tab.id,
              type: "button",
              onClick: () => setStatusFilter(tab.id),
              className: `px-2.5 py-1.5 rounded-md font-semibold transition-all cursor-pointer text-xs ${statusFilter === tab.id ? "bg-foreground text-background shadow-xs font-bold" : "text-muted-foreground hover:text-foreground"}`
            }, tab.id))
          })
        ]
      }),

      // UNIFIED GRID ROW TABLE
      e.jsx("div", {
        className: "overflow-x-auto rounded-xl border border-border/80 bg-card shadow-2xs",
        children: e.jsxs("table", {
          className: "w-full text-left border-collapse text-xs min-w-[640px]",
          children: [
            e.jsx("thead", {
              className: "border-b border-border/80 bg-secondary/40 text-muted-foreground uppercase text-[10px] font-bold tracking-wider",
              children: e.jsxs("tr", {
                children: [
                  e.jsx("th", { className: "py-3 px-4", children: "Model & Provider" }),
                  e.jsx("th", { className: "py-3 px-4", children: "Status" }),
                  e.jsx("th", { className: "py-3 px-4", children: "Roundtrip Latency" }),
                  e.jsx("th", { className: "py-3 px-4", children: "Last Verified" }),
                  e.jsx("th", { className: "py-3 px-4 text-right", children: "Actions" })
                ]
              })
            }),
            e.jsx("tbody", {
              className: "divide-y divide-border/60",
              children: filteredModels.length === 0 ?
                e.jsx("tr", {
                  children: e.jsx("td", {
                    colSpan: 5,
                    className: "py-10 text-center text-muted-foreground text-xs font-mono",
                    children: "No models found matching criteria."
                  })
                }) :
                filteredModels.map(m => {
                  const isProbing = probingModels.has(m.model_id);
                  const isToggling = togglingModels.has(m.model_id);
                  const effectiveStatus = isProbing ? "probing" : m.status;
                  const isEnabled = m.is_enabled !== false;
                  return e.jsxs("tr", {
                    key: m.model_id,
                    className: `hover:bg-secondary/30 transition-colors group ${!isEnabled ? "opacity-75 bg-secondary/10" : ""}`,
                    children: [
                      e.jsxs("td", {
                        className: "py-3 px-4 font-mono",
                        children: [
                          e.jsxs("div", {
                            className: "flex items-center gap-2",
                            children: [
                              e.jsx(H, { providerId: m.provider_id || "custom", className: "size-4 shrink-0" }),
                              e.jsx("span", { className: "font-bold text-foreground", children: m.model_id })
                            ]
                          }),
                          m.last_error ? e.jsxs("div", {
                            className: "mt-1 text-[10.5px] text-rose-500 max-w-md truncate",
                            title: m.last_error,
                            children: ["Error: ", m.last_error]
                          }) : null
                        ]
                      }),
                      e.jsx("td", {
                        className: "py-3 px-4",
                        children: e.jsx(StatusBadge, { status: effectiveStatus, statusCode: m.status_code, isEnabled: isEnabled })
                      }),
                      e.jsx("td", {
                        className: "py-3 px-4",
                        children: e.jsx(LatencyBadge, { latency: m.latency_ms })
                      }),
                      e.jsxs("td", {
                        className: "py-3 px-4 text-muted-foreground text-[11px]",
                        children: [
                          e.jsx(IconClock, { className: "inline size-3 mr-1 -mt-0.5" }),
                          formatRelativeTime(m.last_checked_at)
                        ]
                      }),
                      e.jsx("td", {
                        className: "py-3 px-4 text-right",
                        children: e.jsxs("div", {
                          className: "flex items-center justify-end gap-1.5",
                          children: [
                            // Test Button
                            e.jsxs("button", {
                              type: "button",
                              onClick: () => handleProbeSingle(m.model_id),
                              disabled: isProbing || isProbingAll,
                              className: "inline-flex h-8 items-center justify-center gap-1 rounded-md border border-border/80 bg-secondary/50 hover:bg-secondary px-2.5 text-xs font-semibold text-foreground transition-all cursor-pointer disabled:opacity-40",
                              title: "Test latency and health",
                              children: [
                                e.jsx(IconZap, { className: `size-3 text-primary ${isProbing ? "animate-spin" : ""}` }),
                                e.jsx("span", { children: isProbing ? "Testing..." : "Test" })
                              ]
                            }),
                            // Individual Enable/Disable Button
                            isEnabled ? e.jsxs("button", {
                              type: "button",
                              onClick: () => handleToggleSingle(m.model_id, false),
                              disabled: isToggling,
                              className: "inline-flex h-8 items-center justify-center gap-1 rounded-md border border-border/80 bg-secondary/30 hover:bg-rose-500/10 hover:text-rose-500 hover:border-rose-500/30 px-2.5 text-xs font-semibold text-muted-foreground transition-all cursor-pointer disabled:opacity-40",
                              title: "Nonaktifkan model ini dari /v1/models",
                              children: [
                                e.jsx(isToggling ? IconRefreshCw : IconSlash, { className: `size-3 ${isToggling ? "animate-spin text-primary" : ""}` }),
                                e.jsx("span", { children: "Disable" })
                              ]
                            }) : e.jsxs("button", {
                              type: "button",
                              onClick: () => handleToggleSingle(m.model_id, true),
                              disabled: isToggling,
                              className: "inline-flex h-8 items-center justify-center gap-1 rounded-md border border-emerald-500/30 bg-emerald-500/10 hover:bg-emerald-500/20 px-2.5 text-xs font-semibold text-emerald-600 dark:text-emerald-400 transition-all cursor-pointer disabled:opacity-40",
                              title: "Aktifkan kembali model ini ke /v1/models",
                              children: [
                                e.jsx(isToggling ? IconRefreshCw : IconPower, { className: `size-3 text-emerald-500 ${isToggling ? "animate-spin" : ""}` }),
                                e.jsx("span", { children: "Enable" })
                              ]
                            })
                          ]
                        })
                      })
                    ]
                  });
                })
            })
          ]
        })
      }),

      // CONFIRMATION DIALOG MODAL
      confirmDisableOpen ? e.jsx("div", {
        className: "fixed inset-0 z-50 flex items-center justify-center bg-black/75 backdrop-blur-xs p-4 animate-in fade-in-50",
        onClick: () => !isDisabling && setConfirmDisableOpen(false),
        children: e.jsxs("div", {
          className: "relative flex flex-col w-full max-w-lg rounded-2xl border border-border bg-card text-foreground shadow-2xl overflow-hidden font-sans",
          onClick: ev => ev.stopPropagation(),
          children: [
            // Modal Header
            e.jsxs("div", {
              className: "flex items-center justify-between border-b border-border/80 px-5 py-4 bg-secondary/30",
              children: [
                e.jsxs("div", {
                  className: "flex items-center gap-2.5",
                  children: [
                    e.jsx("div", {
                      className: "flex size-8 items-center justify-center rounded-lg border border-rose-500/30 bg-rose-500/10 text-rose-500",
                      children: e.jsx(IconSlash, { className: "size-4" })
                    }),
                    e.jsx("h2", {
                      className: "text-sm font-bold text-foreground tracking-tight",
                      children: "Nonaktifkan Model Offline"
                    })
                  ]
                }),
                e.jsx("button", {
                  type: "button",
                  disabled: isDisabling,
                  onClick: () => setConfirmDisableOpen(false),
                  className: "rounded-md p-1 text-muted-foreground hover:text-foreground hover:bg-secondary transition-colors cursor-pointer",
                  children: e.jsx(IconClose, { className: "size-4" })
                })
              ]
            }),

            // Modal Body
            e.jsxs("div", {
              className: "p-5 space-y-4 text-left",
              children: [
                e.jsxs("div", {
                  className: "rounded-xl border border-amber-500/30 bg-amber-500/10 p-3.5 text-xs text-amber-600 dark:text-amber-400 leading-relaxed",
                  children: [
                    e.jsx("span", { className: "font-bold block mb-1 text-foreground", children: `Ada ${offlineModels.length} model offline yang akan dinonaktifkan:` }),
                    "Model yang dinonaktifkan tidak akan muncul lagi di ",
                    e.jsx("code", { className: "font-mono font-bold text-primary", children: "/v1/models" }),
                    " dan tidak akan menerima request chat. Kamu bisa mengaktifkannya kembali satu per satu dari tabel ini atau dari halaman Provider."
                  ]
                }),

                // List of Models to Disable
                e.jsx("div", {
                  className: "max-h-60 overflow-y-auto rounded-xl border border-border/80 bg-secondary/15 p-2 space-y-1 divide-y divide-border/40 font-mono text-xs",
                  children: offlineModels.map(m => e.jsxs("div", {
                    key: m.model_id,
                    className: "pt-1.5 pb-1 flex items-center justify-between gap-2",
                    children: [
                      e.jsxs("div", {
                        className: "flex items-center gap-2 min-w-0 pr-2",
                        children: [
                          e.jsx(H, { providerId: m.provider_id || "custom", className: "size-4 shrink-0" }),
                          e.jsxs("div", {
                            className: "min-w-0",
                            children: [
                              e.jsx("span", { className: "font-bold text-foreground truncate block", children: m.model_id }),
                              m.last_error ? e.jsxs("span", { className: "text-[10px] text-rose-500 truncate block", title: m.last_error, children: ["Error: ", m.last_error] }) : null
                            ]
                          })
                        ]
                      }),
                      e.jsx(StatusBadge, { status: "offline", statusCode: m.status_code, isEnabled: true })
                    ]
                  }))
                })
              ]
            }),

            // Modal Footer Actions
            e.jsxs("div", {
              className: "flex items-center justify-end gap-2.5 px-5 py-3.5 border-t border-border/80 bg-secondary/30",
              children: [
                e.jsx("button", {
                  type: "button",
                  disabled: isDisabling,
                  onClick: () => setConfirmDisableOpen(false),
                  className: "px-4 py-2 rounded-lg text-xs font-semibold text-muted-foreground hover:text-foreground hover:bg-secondary transition-colors cursor-pointer",
                  children: "Batal"
                }),
                e.jsxs("button", {
                  type: "button",
                  disabled: isDisabling,
                  onClick: handleConfirmDisable,
                  className: "inline-flex items-center gap-1.5 px-4 py-2 rounded-lg text-xs font-semibold bg-rose-600 hover:bg-rose-700 text-white shadow-xs transition-all cursor-pointer disabled:opacity-50",
                  children: [
                    e.jsx(isDisabling ? IconRefreshCw : IconSlash, { className: `size-3.5 ${isDisabling ? "animate-spin" : ""}` }),
                    e.jsxs("span", { children: [isDisabling ? "Menonaktifkan..." : `Nonaktifkan ${offlineModels.length} Model`] })
                  ]
                })
              ]
            })
          ]
        })
      }) : null
    ]
  });
}

export { HealthMatrixPage as component };
