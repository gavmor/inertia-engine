#!/bin/bash
# Inertia Engine: Unix-style parallel agent orchestration
set -e

# Configuration
CONTEXT_FILE="${1:-logs/inertia-context-$(date +%Y-%m-%d).json}"
MAX_PARALLEL="${2:-10}"
DRY_RUN="${3:-false}"

echo "=== Inertia Engine (Parallel Agent Mode) ==="
echo "Context: $CONTEXT_FILE"
echo "Max parallel: $MAX_PARALLEL"
echo "Dry run: $DRY_RUN"
echo

# Verify context exists
if [ ! -f "$CONTEXT_FILE" ]; then
  echo "Error: Context file not found: $CONTEXT_FILE"
  exit 1
fi

# Fetch all tasks from Todoist
echo "Fetching tasks from Todoist..."
td task list --json --full > /tmp/all_tasks.json
TOTAL_TASKS=$(jq '.results | length' /tmp/all_tasks.json)
echo "Total tasks: $TOTAL_TASKS"

# Filter for leaf nodes (tasks with no active children)
echo "Filtering leaf node tasks..."
jq -r '.results[] | select(.parentId == null or .parentId == "") | .id' /tmp/all_tasks.json > /tmp/leaf_task_ids.txt
LEAF_COUNT=$(wc -l < /tmp/leaf_task_ids.txt)
echo "Leaf node tasks: $LEAF_COUNT"
echo

# Spawn agent for each task (with parallelism limit)
echo "Spawning agents (max $MAX_PARALLEL in parallel)..."
SPAWNED=0
PIDS=()

while read -r task_id; do
  # Get task details
  TASK_JSON=$(jq -c ".results[] | select(.id == \"$task_id\")" /tmp/all_tasks.json)
  TASK_CONTENT=$(echo "$TASK_JSON" | jq -r '.content')
  
  echo "[$((SPAWNED+1))/$LEAF_COUNT] Spawning agent for: $TASK_CONTENT"
  
  # Write task-specific file
  echo "$TASK_JSON" > "/tmp/inertia_task_${task_id}.json"
  
  # Spawn agent in background
  openclaw sessions spawn \
    --agent inertial \
    --mode run \
    --task "Decide inertia action for task ${task_id}. Read context: ${CONTEXT_FILE}. Read task: /tmp/inertia_task_${task_id}.json. Output decision and execute td command unless dry_run=${DRY_RUN}." \
    > "/tmp/inertia_result_${task_id}.txt" 2>&1 &
  
  PIDS+=($!)
  SPAWNED=$((SPAWNED+1))
  
  # Throttle parallelism
  if [ ${#PIDS[@]} -ge $MAX_PARALLEL ]; then
    wait -n  # Wait for any one to complete
    # Remove completed PIDs
    PIDS=($(jobs -pr))
  fi
  
done < /tmp/leaf_task_ids.txt

echo
echo "Waiting for all agents to complete..."
wait

echo
echo "=== Results ==="
SUCCESS=0
FAILED=0

for task_id in $(cat /tmp/leaf_task_ids.txt); do
  RESULT_FILE="/tmp/inertia_result_${task_id}.txt"
  if grep -q "DECISION:" "$RESULT_FILE" 2>/dev/null; then
    SUCCESS=$((SUCCESS+1))
    echo "✓ Task $task_id: $(grep 'DECISION:' "$RESULT_FILE")"
  else
    FAILED=$((FAILED+1))
    echo "✗ Task $task_id: Failed (see $RESULT_FILE)"
  fi
done

echo
echo "=== Summary ==="
echo "Total leaf tasks: $LEAF_COUNT"
echo "Successful: $SUCCESS"
echo "Failed: $FAILED"
echo
echo "All agent outputs saved to: /tmp/inertia_result_*.txt"
echo "All task JSONs saved to: /tmp/inertia_task_*.json"
