# Inertia Engine: Parallel Agent Approach

**Unix philosophy: Do one thing well, compose with pipes.**

## Architecture

Instead of a custom Go orchestrator calling `openclaw chat`, use **parallel agent spawning** (OpenClaw's built-in capability).

### Phase 1: Context Generation (Main Agent)
Same as before - main agent runs `prepare-context` skill, outputs `logs/inertia-context-YYYY-MM-DD.json`.

### Phase 2: Parallel Task Processing (Bash + Subagents)

```bash
# Fetch all tasks
td task list --json --full

# For each leaf node task
for task_id in $LEAF_TASKS; do
  # Spawn isolated agent instance
  openclaw sessions spawn \
    --agent inertial \
    --mode run \
    --task "Process task $task_id with context" &
done
wait
```

**Each agent:**
1. Reads context JSON (shared)
2. Reads its specific task JSON
3. Applies deterministic decision rules
4. Executes `td` command
5. Outputs result
6. Exits

**Orchestrator** (bash script):
- Fetches tasks
- Filters leaf nodes
- Spawns N agents in parallel (bounded concurrency)
- Waits for completion
- Aggregates results

## Why This Is Better Than Go Orchestrator

### Unix Philosophy
✓ **Simple tools** - each agent does one task decision  
✓ **Composable** - bash script + built-in OpenClaw spawning  
✓ **Transparent** - can see each agent's logs in `/tmp/`  
✗ Custom orchestrator - monolithic, opaque, hard to debug

### Maintainability
✓ **Native OpenClaw** - uses `sessions_spawn` (documented, supported)  
✓ **No compilation** - bash script, edit and run  
✓ **Debuggable** - inspect individual agent outputs  
✗ Go binary - requires rebuild, debugging via logs only

### Parallelism
✓ **Built-in** - OpenClaw handles agent isolation, routing, cleanup  
✓ **Bounded** - script uses `wait -n` for concurrency limit  
✓ **Fault-tolerant** - one agent failure doesn't crash others  
✗ Go goroutines - need custom error handling, aggregation

### Decision Logic
✓ **Rule-based** - deterministic if/then (fast, transparent)  
✓ **Optional LLM** - can add later if needed for complex cases  
✗ LLM per task - slow, expensive, opaque, requires `openclaw chat`

## Usage

```bash
# Run with defaults
./run-parallel.sh

# Specify context file and concurrency
./run-parallel.sh logs/inertia-context-2026-02-22.json 20

# Dry run (no td commands executed)
./run-parallel.sh logs/inertia-context-2026-02-22.json 10 true
```

## Output

```
=== Inertia Engine (Parallel Agent Mode) ===
Context: logs/inertia-context-2026-02-22.json
Max parallel: 10
Total tasks: 69
Leaf node tasks: 65

Spawning agents (max 10 in parallel)...
[1/65] Spawning agent for: Journal entry
[2/65] Spawning agent for: Set up home office
...

Waiting for all agents to complete...

=== Results ===
✓ Task ABC: DECISION: reprioritize priority=2 reason="10yr commitment"
✓ Task DEF: DECISION: ice-box reason="No historical context, 45 days old"
✗ Task GHI: Failed (see /tmp/inertia_result_GHI.txt)

=== Summary ===
Total leaf tasks: 65
Successful: 63
Failed: 2
```

Each agent's output saved to `/tmp/inertia_result_*.txt` for debugging.

## Decision Rules (Deterministic)

**Ice-box:** age > 30 days + no historical context (span_years = 0)  
**Decompose:** age > 14 days + vague wording → atomic habit  
**Reprioritize:** span_years ≥ 7 + low priority → bump to p2  
**Recontextualize:** vague patterns ("work on", "deal with") → specific action  
**Skip:** none of above

Rules can be tuned in `TASK_DECISION.md` without touching orchestrator.

## Comparison: Go vs Parallel Agents

| Aspect | Go Orchestrator | Parallel Agents |
|--------|----------------|-----------------|
| Complexity | Custom binary, goroutines | Bash + OpenClaw built-in |
| Parallelism | Manual semaphore | Native agent spawning |
| Debugging | Compiled logs | Per-agent output files |
| LLM calls | Requires `openclaw chat` | Optional, agent decides |
| Decision logic | Hardcoded in Go | Documented in skill |
| Iteration speed | Rebuild required | Edit skill, re-run |
| Fault tolerance | Manual handling | Isolated agents |
| Unix-y | No | Yes |

## Integration

Add to `run-orchestrator.md` skill or create new `run-parallel` skill.

**Nightly cron:**
```bash
0 23 * * * cd ~/.openclaw/workspace && \
  openclaw chat "Generate inertia context for today" && \
  cd inertia-engine && ./run-parallel.sh
```

**Manual:**
```bash
cd ~/.openclaw/workspace/inertia-engine
./run-parallel.sh logs/inertia-context-$(date +%Y-%m-%d).json
```

## Future Enhancements

- Add LLM option for complex tasks (fall back to rules if LLM fails)
- Implement reconcile-accomplishments phase (separate parallel run)
- Add web dashboard to visualize decisions over time
- Store decisions in SQLite for analysis

The parallel agent approach is **simpler, more maintainable, and more Unix-y** than custom orchestration.
