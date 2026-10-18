import { c as ae, o as de, a as ee, p as O, t as S, e as $, j as e, G as le, B as L, r as N, K as ie, C as ce, Z as K, q as se, s as U, X as te, v as xe, i as re, P as H } from "./index-yz9zKHrD.js";

// Lucide SVG Icons
const IconActivity = ae("activity", [["path", { d: "M22 12h-4l-3 9L9 3l-3 9H2", key: "act1" }]]);
const IconCheckCircle = ae("check-circle-2", [["circle", { cx: "12", cy: "12", r: "10", key: "c1" }], ["path", { d: "m9 12 2 2 4-4", key: "c2" }]]);
const IconAlertCircle = ae("alert-circle", [["circle", { cx: "12", cy: "12", r: "10", key: "a1" }], ["line", { x1: "12", x2: "12", y1: "8", y2: "12", key: "a2" }], ["line", { x1: "12", x2: "12.01", y1: "16", y2: "16", key: "a3" }]]);
const IconRefreshCw = ae("refresh-cw", [["path", { d: "M3 12a9 9 0 0 1 9-9 9.75 9.75 0 0 1 6.74 2.74L21 8", key: "r1" }], ["path", { d: "M21 3v5h-5", key: "r2" }], ["path", { d: "M21 12a9 9 0 0 1-9 9 9.75 9.75 0 0 1-6.74-2.74L3 16", key: "r3" }], ["path", { d: "M8 16H3v5", key: "r4" }]]);
const IconZap = ae("zap", [["polygon", { points: "13 2 3 14 12 14 11 22 21 10 12 10 13 2", key: "z1" }]]);
const IconSearch = ae("search", [["circle", { cx: "11", cy: "11", r: "8", key: "s1" }], ["line", { x1: "21", x2: "16.65", y1: "21", y2: "16.65", key: "s2" }]]);
const IconHelpCircle = ae("help-circle", [["circle", { cx: "12", cy: "12", r: "10", key: "h1" }], ["path", { d: "M9.09 9a3 3 0 0 1 5.83 1c0 2-3 3-3 3", key: "h2" }], ["line", { x1: "12", x2: "12.01", y1: "17", y2: "17", key: "h3" }]]);
const IconClock = ae("clock", [["circle", { cx: "12", cy: "12", r: "10", key: "clk1" }], ["polyline", { points: "12 6 12 12 16 14", key: "clk2" }]]);

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
  if (latency <= 0) return e.jsx("span", { className: "text-muted-foreground/60 font-mono text-xs", children: "--" });
  let colorClass = "bg-emerald-500/10 text-emerald-600 dark:text-emerald-400 border-emerald-500/20";
  if (latency > 800) {
    colorClass = "bg-rose-500/10 text-rose-600 dark:text-rose-400 border-rose-500/20";
  } else if (latency > 350) {
    colorClass = "bg-amber-500/10 text-amber-600 dark:text-amber-400 border-amber-500/20";
  }
  return e.jsxs("span", {
    className: `inline-flex items-center gap-1 rounded-md border px-2 py-0.5 text-xs font-mono font-bold ${colorClass}`,
    children: [e.jsx(IconZap, { className: "size-3" }), `${latency} ms`]
  });
}

function StatusBadge({ status, statusCode }) {
  if (status === "healthy") {
    return e.jsxs("span", {
      className: "inline-flex items-center gap-1.5 rounded-full border border-emerald-500/30 bg-emerald-500/10 px-2.5 py-0.5 text-[11px] font-bold text-emerald-600 dark:text-emerald-400",
      children: [e.jsx(IconCheckCircle, { className: "size-3.5" }), "HEALTHY"]
    });
  }
  if (status === "offline") {
    return e.jsxs("span", {
      className: "inline-flex items-center gap-1.5 rounded-full border border-rose-500/30 bg-rose-500/10 px-2.5 py-0.5 text-[11px] font-bold text-rose-600 dark:text-rose-400",
      children: [e.jsx(IconAlertCircle, { className: "size-3.5" }), statusCode ? `OFFLINE (${statusCode})` : "OFFLINE"]
    });
  }
  if (status === "probing") {
    return e.jsxs("span", {
      className: "inline-flex items-center gap-1.5 rounded-full border border-sky-500/30 bg-sky-500/10 px-2.5 py-0.5 text-[11px] font-bold text-sky-600 dark:text-sky-400",
      children: [e.jsx(IconRefreshCw, { className: "size-3.5 animate-spin" }), "PROBING"]
    });
  }
  return e.jsxs("span", {
    className: "inline-flex items-center gap-1.5 rounded-full border border-border/80 bg-secondary/50 px-2.5 py-0.5 text-[11px] font-medium text-muted-foreground",
    children: [e.jsx(IconHelpCircle, { className: "size-3.5" }), "UNTESTED"]
  });
}

function HealthMatrixPage() {
  const queryClient = de();
  const [search, setSearch] = N.useState("");
  const [statusFilter, setStatusFilter] = N.useState("all");
  const [probingModels, setProbingModels] = N.useState(new Set());
  const [isProbingAll, setIsProbingAll] = N.useState(false);

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

  const handleProbeAll = async () => {
    setIsProbingAll(true);
    S.success("Starting concurrent health probe on all models...");
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

  const filteredModels = N.useMemo(() => {
    const q = search.trim().toLowerCase();
    return models.filter(m => {
      const matchSearch = !q || m.model_id.toLowerCase().includes(q) || (m.provider_id && m.provider_id.toLowerCase().includes(q));
      if (!matchSearch) return false;
      if (statusFilter === "healthy") return m.status === "healthy";
      if (statusFilter === "offline") return m.status === "offline";
      if (statusFilter === "untested") return m.status !== "healthy" && m.status !== "offline";
      return true;
    });
  }, [models, search, statusFilter]);

  return e.jsxs("div", {
    className: "mx-auto w-full max-w-6xl space-y-6 font-sans pb-12",
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
                  e.jsxs("button", {
                    type: "button",
                    onClick: handleProbeAll,
                    disabled: isProbingAll,
                    className: "inline-flex h-9 items-center justify-center gap-1.5 rounded-lg bg-primary px-4 text-xs font-semibold text-primary-foreground shadow-xs hover:bg-primary/90 transition-all cursor-pointer disabled:opacity-50",
                    children: [
                      e.jsx(isProbingAll ? IconRefreshCw : IconZap, { className: `size-3.5 ${isProbingAll ? "animate-spin" : ""}` }),
                      e.jsx("span", { children: isProbingAll ? "Testing All Models..." : "Test All Models" })
                    ]
                  }),
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
                  e.jsx("span", { className: "text-[11px] font-semibold text-muted-foreground uppercase tracking-wider", children: "Monitored Models" }),
                  e.jsxs("div", {
                    className: "text-xl sm:text-2xl font-bold font-mono text-foreground",
                    children: [stats.total || models.length, " Models"]
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
            className: "inline-flex items-center rounded-lg border border-border/80 bg-card p-1 text-xs font-medium self-start sm:self-auto",
            children: [
              { id: "all", label: `All (${models.length})` },
              { id: "healthy", label: `Healthy (${stats.healthy})` },
              { id: "offline", label: `Offline (${stats.offline})` },
              { id: "untested", label: `Untested (${stats.untested})` }
            ].map(tab => e.jsx("button", {
              key: tab.id,
              type: "button",
              onClick: () => setStatusFilter(tab.id),
              className: `px-3 py-1.5 rounded-md font-semibold transition-all cursor-pointer ${statusFilter === tab.id ? "bg-foreground text-background shadow-xs font-bold" : "text-muted-foreground hover:text-foreground"}`
            }, tab.id))
          })
        ]
      }),

      // DESKTOP VIEW: Data Table (Hidden on mobile)
      e.jsx("div", {
        className: "hidden md:block overflow-hidden rounded-xl border border-border/80 bg-card shadow-2xs",
        children: e.jsxs("table", {
          className: "w-full text-left border-collapse text-xs",
          children: [
            e.jsx("thead", {
              className: "border-b border-border/80 bg-secondary/40 text-muted-foreground uppercase text-[10px] font-bold tracking-wider",
              children: e.jsxs("tr", {
                children: [
                  e.jsx("th", { className: "py-3 px-4", children: "Model & Provider" }),
                  e.jsx("th", { className: "py-3 px-4", children: "Status" }),
                  e.jsx("th", { className: "py-3 px-4", children: "Roundtrip Latency" }),
                  e.jsx("th", { className: "py-3 px-4", children: "Last Verified" }),
                  e.jsx("th", { className: "py-3 px-4 text-right", children: "Action" })
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
                  const effectiveStatus = isProbing ? "probing" : m.status;
                  return e.jsxs("tr", {
                    key: m.model_id,
                    className: "hover:bg-secondary/30 transition-colors group",
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
                        children: e.jsx(StatusBadge, { status: effectiveStatus, statusCode: m.status_code })
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
                        children: e.jsxs("button", {
                          type: "button",
                          onClick: () => handleProbeSingle(m.model_id),
                          disabled: isProbing || isProbingAll,
                          className: "inline-flex h-8 items-center justify-center gap-1 rounded-md border border-border/80 bg-secondary/50 hover:bg-secondary px-2.5 text-xs font-semibold text-foreground transition-all cursor-pointer disabled:opacity-40",
                          children: [
                            e.jsx(IconZap, { className: `size-3 text-primary ${isProbing ? "animate-spin" : ""}` }),
                            e.jsx("span", { children: isProbing ? "Testing..." : "Test" })
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

      // MOBILE VIEW: Stacked Responsive Cards (Hidden on desktop, >=44px touch targets)
      e.jsx("div", {
        className: "md:hidden space-y-3",
        children: filteredModels.length === 0 ?
          e.jsx("div", {
            className: "rounded-xl border border-border/80 bg-card p-8 text-center text-xs text-muted-foreground font-mono",
            children: "No models found."
          }) :
          filteredModels.map(m => {
            const isProbing = probingModels.has(m.model_id);
            const effectiveStatus = isProbing ? "probing" : m.status;
            return e.jsxs("div", {
              key: m.model_id,
              className: "rounded-xl border border-border/80 bg-card p-4 space-y-3 shadow-2xs",
              children: [
                e.jsxs("div", {
                  className: "flex items-start justify-between gap-2 border-b border-border/60 pb-3",
                  children: [
                    e.jsxs("div", {
                      className: "flex items-center gap-2 min-w-0 font-mono",
                      children: [
                        e.jsx(H, { providerId: m.provider_id || "custom", className: "size-4.5 shrink-0" }),
                        e.jsxs("div", {
                          className: "min-w-0",
                          children: [
                            e.jsx("div", { className: "text-xs font-bold text-foreground truncate", children: m.model_id }),
                            e.jsx("div", { className: "text-[10px] text-muted-foreground uppercase", children: m.provider_id })
                          ]
                        })
                      ]
                    }),
                    e.jsx(StatusBadge, { status: effectiveStatus, statusCode: m.status_code })
                  ]
                }),
                e.jsxs("div", {
                  className: "flex items-center justify-between text-xs",
                  children: [
                    e.jsxs("div", {
                      className: "flex items-center gap-1.5",
                      children: [
                        e.jsx("span", { className: "text-muted-foreground text-[11px]", children: "Latency:" }),
                        e.jsx(LatencyBadge, { latency: m.latency_ms })
                      ]
                    }),
                    e.jsxs("div", {
                      className: "text-[11px] text-muted-foreground",
                      children: [
                        e.jsx(IconClock, { className: "inline size-3 mr-1 -mt-0.5" }),
                        formatRelativeTime(m.last_checked_at)
                      ]
                    })
                  ]
                }),
                m.last_error ? e.jsxs("div", {
                  className: "rounded-lg border border-rose-500/20 bg-rose-500/10 p-2 text-[11px] text-rose-500",
                  children: ["Error: ", m.last_error]
                }) : null,
                e.jsxs("button", {
                  type: "button",
                  onClick: () => handleProbeSingle(m.model_id),
                  disabled: isProbing || isProbingAll,
                  className: "w-full h-11 flex items-center justify-center gap-1.5 rounded-lg border border-border/80 bg-secondary/60 hover:bg-secondary text-xs font-semibold text-foreground transition-all cursor-pointer active:scale-[0.98] disabled:opacity-40",
                  children: [
                    e.jsx(IconZap, { className: `size-3.5 text-primary ${isProbing ? "animate-spin" : ""}` }),
                    e.jsx("span", { children: isProbing ? "Testing Model..." : "Test Model" })
                  ]
                })
              ]
            });
          })
      })
    ]
  });
}

export { HealthMatrixPage as component };
