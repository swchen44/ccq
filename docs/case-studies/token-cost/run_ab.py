#!/usr/bin/env python3
"""
run_ab.py — real Claude Code A/B harness for the ccq ROI case study.

For each task in tasks.json, run a headless Claude Code agent TWICE under
identical model + prompt, differing only in whether `ccq` is available:

  * baseline   — ccq is NOT on PATH (command-not-found); the agent can only
                 grep / read files. This is "an agent without ccq".
  * ccq        — ccq is on PATH and the project is pre-indexed; the prompt
                 tells the agent to prefer it.

We read the REAL token usage + cost from `claude -p --output-format json`
(no proxy/estimate), score the answer against ground truth, and aggregate.

Usage:
  python3 run_ab.py --tasks T1,T2 --runs 1        # calibrate (cheap)
  python3 run_ab.py --runs 3                       # full run, N=3
  python3 run_ab.py --report                       # rebuild table from raw/

Honest by construction: same model, same prompt, baseline genuinely cannot
reach ccq (PATH-excluded). Non-determinism is handled by N runs (mean+range).
Raw per-run JSON is kept under raw/ for audit.
"""
import argparse, json, os, shutil, statistics, subprocess, sys, tempfile, time
from pathlib import Path

HERE = Path(__file__).resolve().parent
RAW = HERE / "raw"
HOME = Path.home()
CCQ = HOME / "git" / "ccq" / "ccq"
BENCH = HOME / "git" / "cbm-vs-codegraph-bench" / "repos"
REPOS = {"redis": BENCH / "redis", "wpa": BENCH / "wpa_supplicant"}
MODEL = os.environ.get("AB_MODEL", "sonnet")
MAX_TURNS = os.environ.get("AB_MAX_TURNS", "40")

CCQ_HINT = (
    "\n\nA command-line tool `ccq` is available and this project is ALREADY indexed. "
    "Prefer it over grep: `ccq callers <sym>`, `ccq callees <sym>`, `ccq explore <sym>`, "
    "`ccq impact <sym> -d N`. Run `ccq --help` for the full list. Pass `-p " "<repo>` if needed."
)


def claude_path_dir(with_ccq: bool) -> str:
    """Return a PATH string. For ccq runs, prepend a tmp bin with a `ccq` symlink.
    For baseline, return the inherited PATH (ccq is not on it — verified)."""
    base = os.environ["PATH"]
    if not with_ccq:
        return base
    binv = Path(tempfile.mkdtemp(prefix="ccqbin-"))
    link = binv / "ccq"
    if not link.exists():
        link.symlink_to(CCQ)
    return f"{binv}:{base}"


def run_one(task, with_ccq, run_idx):
    repo = REPOS[task["repo"]]
    prompt = task["prompt"] + (CCQ_HINT if with_ccq else "")
    env = dict(os.environ)
    env["PATH"] = claude_path_dir(with_ccq)
    cmd = [
        "claude", "-p", prompt,
        "--output-format", "json",
        "--model", MODEL,
        "--max-turns", MAX_TURNS,
        "--dangerously-skip-permissions",
        "--allowedTools", "Bash", "Read", "Grep", "Glob", "LS",
    ]
    t0 = time.time()
    proc = subprocess.run(
        cmd, cwd=str(repo), env=env, stdin=subprocess.DEVNULL,
        capture_output=True, text=True, timeout=1200,
    )
    wall = time.time() - t0
    cond = "ccq" if with_ccq else "baseline"
    raw_path = RAW / f"{task['id']}_{cond}_{run_idx}.json"
    try:
        d = json.loads(proc.stdout)
    except Exception:
        raw_path.write_text(proc.stdout + "\n--STDERR--\n" + proc.stderr)
        return {"ok": False, "wall": wall, "cond": cond}
    d["_wall_s"] = round(wall, 1)
    raw_path.write_text(json.dumps(d, indent=2))
    u = d.get("usage", {}) or {}
    result = d.get("result", "") or ""
    gt = task["gt"]
    hits = sum(1 for g in gt if g in result)
    return {
        "ok": not d.get("is_error"),
        "cond": cond,
        "in": u.get("input_tokens", 0),
        "out": u.get("output_tokens", 0),
        "cache_r": u.get("cache_read_input_tokens", 0),
        "cache_c": u.get("cache_creation_input_tokens", 0),
        "total_tok": (u.get("input_tokens", 0) + u.get("output_tokens", 0)
                      + u.get("cache_read_input_tokens", 0) + u.get("cache_creation_input_tokens", 0)),
        "cost": d.get("total_cost_usd", 0.0),
        "turns": d.get("num_turns", 0),
        "wall": round(wall, 1),
        "recall": hits / len(gt) if gt else 0.0,
        "hits": hits, "gt_n": len(gt),
    }


def prewarm(task_ids, tasks):
    repos = {tasks[t]["repo"] for t in task_ids}
    for r in repos:
        print(f"  prewarming ccq index for {r} …", flush=True)
        env = dict(os.environ); env["PATH"] = f"{CCQ.parent}:{env['PATH']}"
        subprocess.run([str(CCQ), "wait-index", "-p", str(REPOS[r])],
                       env=env, stdin=subprocess.DEVNULL, capture_output=True, timeout=600)


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--tasks", default="", help="comma list e.g. T1,T2 (default all)")
    ap.add_argument("--runs", type=int, default=3)
    ap.add_argument("--report", action="store_true", help="only rebuild the table from raw/")
    args = ap.parse_args()

    tasks = {t["id"]: t for t in json.loads((HERE / "tasks.json").read_text())}
    ids = [s.strip() for s in args.tasks.split(",") if s.strip()] or list(tasks)
    RAW.mkdir(exist_ok=True)

    if not args.report:
        prewarm(ids, tasks)
        for tid in ids:
            for with_ccq in (False, True):
                for i in range(1, args.runs + 1):
                    cond = "ccq" if with_ccq else "baseline"
                    print(f"[{tid}/{cond}/run{i}] running…", flush=True)
                    r = run_one(tasks[tid], with_ccq, i)
                    if r.get("ok"):
                        print(f"    tok={r['total_tok']:>7} cost=${r['cost']:.3f} "
                              f"turns={r['turns']:>2} {r['wall']:>5}s recall={r['recall']:.0%}")
                    else:
                        print(f"    FAILED (see raw/{tid}_{cond}_{i}.json)")

    aggregate_and_print(ids, tasks, args.runs)


def load_runs(tid, cond):
    out = []
    for p in sorted(RAW.glob(f"{tid}_{cond}_*.json")):
        try:
            d = json.loads(p.read_text())
        except Exception:
            continue
        u = d.get("usage", {}) or {}
        gt = []  # recall recomputed in aggregate via tasks
        out.append(d)
    return out


def aggregate_and_print(ids, tasks, runs):
    def mean(xs): return statistics.mean(xs) if xs else 0
    rows = []
    for tid in ids:
        t = tasks[tid]
        for cond in ("baseline", "ccq"):
            toks, costs, turns, walls, recalls = [], [], [], [], []
            for p in sorted(RAW.glob(f"{tid}_{cond}_*.json")):
                try:
                    d = json.loads(p.read_text())
                except Exception:
                    continue
                u = d.get("usage", {}) or {}
                tt = sum(u.get(k, 0) for k in ("input_tokens", "output_tokens",
                         "cache_read_input_tokens", "cache_creation_input_tokens"))
                toks.append(tt); costs.append(d.get("total_cost_usd", 0))
                turns.append(d.get("num_turns", 0)); walls.append(d.get("_wall_s", 0))
                res = d.get("result", "") or ""
                recalls.append(sum(1 for g in t["gt"] if g in res) / len(t["gt"]))
            if toks:
                rows.append((tid, t["title"], t["mode"], cond, mean(toks), mean(costs),
                             mean(turns), mean(walls), mean(recalls), len(toks)))
    print("\n## Results (mean over N runs)\n")
    print("| Task | mode | cond | tokens | $cost | turns | wall s | recall | N |")
    print("|------|------|------|-------:|------:|------:|-------:|-------:|--:|")
    for (tid, title, mode, cond, tok, cost, tn, wl, rc, n) in rows:
        print(f"| {tid} {title} | {mode} | {cond} | {tok:,.0f} | ${cost:.3f} | "
              f"{tn:.0f} | {wl:.0f} | {rc:.0%} | {n} |")
    # savings summary
    print("\n## Savings (baseline ÷ ccq)\n")
    print("| Task | token× | cost× | baseline recall | ccq recall |")
    print("|------|-------:|------:|:----------------|:-----------|")
    by = {}
    for r in rows:
        by.setdefault(r[0], {})[r[3]] = r
    for tid in ids:
        if tid in by and "baseline" in by[tid] and "ccq" in by[tid]:
            b, c = by[tid]["baseline"], by[tid]["ccq"]
            tx = b[4] / c[4] if c[4] else 0
            cx = b[5] / c[5] if c[5] else 0
            print(f"| {tid} | {tx:.1f}× | {cx:.1f}× | {b[8]:.0%} | {c[8]:.0%} |")


if __name__ == "__main__":
    main()
