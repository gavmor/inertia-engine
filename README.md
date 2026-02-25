# Inertia Engine

**Context-aware task orchestration that bridges daily diary context with long-term project inertia.**

## Architecture

### Phase 1: Contextualization (Main Agent)
Main OpenClaw agent reads today's diary entry and generates `logs/inertia-context-YYYY-MM-DD.json` containing:
- **Gazetteer**: Historical context for people, projects, places, concepts mentioned
- **State markers**: Current energy, mood, environment, work volatility
- **Intentions**: Explicit and implicit goals from diary

### Phase 2: Task Processing (Go Orchestrator)
This Go program:
1. Loads context JSON from phase 1
2. Fetches all tasks from Todoist via `td` CLI
3. Filters for leaf nodes (tasks with no active children)
4. **For each task in parallel:**
   - Contextualizes against gazetteer (finds related concepts with historical span)
   - Calls LLM agent to calculate inertia score and decide action
   - Inertia score = (historical_weight × 0.4) + (state_alignment × 0.3) + (environment × 0.3)
5. Executes all `td` commands in parallel

## Installation

```bash
# Go should already be installed via mise
go version  # Should be 1.26.0+

# Build
cd ~/.openclaw/workspace/inertia-engine
go build -o inertia-engine main.go
```

## Usage

```bash
# Run with defaults
./inertia-engine --context logs/inertia-context-2026-02-22.json

# Dry run (no actual td commands)
./inertia-engine --context logs/inertia-context-2026-02-22.json --dry-run

# Adjust concurrency
./inertia-engine --concurrency 20
```

## Full Workflow

```bash
# Phase 1: Generate context (main agent)
openclaw chat "Read prepare-context skill and generate today's inertia context file"

# Phase 2: Process tasks
cd ~/.openclaw/workspace/inertia-engine
./inertia-engine --context ../logs/inertia-context-$(date +%Y-%m-%d).json
```

## Decision Actions

The LLM agent can decide one of:
- **skip**: No action needed
- **decompose**: Break into subtasks (for stale tasks >14 days)
- **ice-box**: Move to ice-box project (for low-inertia tasks >30 days)
- **reprioritize**: Change priority based on inertia score
- **recontextualize**: Rewrite task to be more atomic/specific

## Inertia Scoring

Each task gets an inertia score (0-10) based on:

**Historical Weight (40%)**: How long has the related concept been mentioned in diaries?
- 10+ years = 10 points
- 5 years = 5 points
- <6 months = 1 point

**State Alignment (30%)**: Does the task match current energy/mood?
- High energy + creative task = 10 points
- Low energy + admin task = 8 points
- Low energy + creative task = 3 points

**Environment Feasibility (30%)**: Can the task be done in current environment?
- At coffee shop + needs quiet focus = 3 points
- At home + home maintenance = 10 points

## Concurrency

- **LLM calls**: Bounded by `--concurrency` flag (default 10)
- **td commands**: All executed in parallel (independent operations)

## Logging

All decisions logged to stdout with:
- Task ID
- Action taken
- Inertia score
- Reasoning

## Integration

### Manual
```bash
./inertia-engine --context logs/inertia-context-2026-02-22.json
```

### Automated (nightly cron)
```bash
# Add to crontab
0 23 * * * cd /home/user/.openclaw/workspace && openclaw chat "Generate inertia context for today" && cd inertia-engine && ./inertia-engine --context ../logs/inertia-context-$(date +%Y-%m-%d).json
```

### Via diary hook
```bash
# In diary.git/hooks/post-receive
openclaw system event --mode now --text "Diary updated, running Inertia Engine"
# Then main agent triggers context generation + Go orchestrator
```

## Dependencies

- Go 1.26+
- `td` CLI (Todoist API client)
- OpenClaw gateway (for LLM agent calls)
- Context JSON from phase 1

## Files

- `main.go`: Full orchestrator implementation
- `go.mod`: Go module definition
- `logs/inertia-context-*.json`: Input from phase 1
- `logs/inertia-transforms-*.log`: Output log (future)
