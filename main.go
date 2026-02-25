package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// InertiaContext from phase 1
type InertiaContext struct {
	Date       string              `json:"date"`
	Gazetteer  Gazetteer           `json:"gazetteer"`
	State      State               `json:"state"`
	Intentions Intentions          `json:"intentions"`
}

type Gazetteer struct {
	People   []Entity `json:"people"`
	Projects []Entity `json:"projects"`
	Places   []Entity `json:"places"`
	Concepts []Entity `json:"concepts"`
}

type Entity struct {
	Name      string          `json:"name"`
	Context   string          `json:"context"`
	Sources   []string        `json:"sources"`
	SpanYears json.RawMessage `json:"span_years,omitempty"`
	// Additional optional fields that may appear in context JSON
	Status           string `json:"status,omitempty"`
	Note             string `json:"note,omitempty"`
	EmotionalValence string `json:"emotional_valence,omitempty"`
}

// GetSpanYears returns the span_years as a float64, handling both numeric and string values
func (e *Entity) GetSpanYears() float64 {
	if len(e.SpanYears) == 0 {
		return 0
	}
	
	// Try to parse as number
	var num float64
	if err := json.Unmarshal(e.SpanYears, &num); err == nil {
		return num
	}
	
	// If it's a string like "unknown", return 0
	return 0
}

type State struct {
	Energy          string `json:"energy"`
	Mood            string `json:"mood"`
	Environment     string `json:"environment"`
	WorkVolatility  string `json:"work_volatility"`
}

type Intentions struct {
	Explicit []string `json:"explicit"`
	Implicit []string `json:"implicit"`
}

// Task from Todoist
type Task struct {
	ID          string    `json:"id"`
	Content     string    `json:"content"`
	Description string    `json:"description"`
	Priority    int       `json:"priority"`
	ParentID    *string   `json:"parentId"`
	AddedAt     time.Time `json:"addedAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
	Labels      []string  `json:"labels"`
	ProjectID   string    `json:"projectId"`
}

type TasksResponse struct {
	Results []Task `json:"results"`
}

// Decision from LLM reasoning
type Decision struct {
	TaskID      string
	Action      string // "decompose", "ice-box", "reprioritize", "recontextualize", "skip"
	Priority    *int
	NewContent  *string
	Subtasks    []string
	Reasoning   string
	InertiaScore float64
}

// TaskContext combines task with relevant gazetteer entries
type TaskContext struct {
	Task             Task
	RelatedPeople    []Entity
	RelatedProjects  []Entity
	RelatedConcepts  []Entity
	State            State
	AgeDays          int
	HistoricalWeight float64
}

var (
	// commandRunner is used for all external CLI calls, allowing mocking in tests
	commandRunner CommandRunner = &RealRunner{}
	// now allows deterministic testing of time-based logic
	nowFunc = time.Now
)

type CommandRunner interface {
	Run(name string, args ...string) error
	Output(name string, args ...string) ([]byte, error)
	RunWithStdin(stdin string, name string, args ...string) ([]byte, error)
}

type RealRunner struct{}

func (r *RealRunner) Run(name string, args ...string) error {
	return exec.Command(name, args...).Run()
}

func (r *RealRunner) Output(name string, args ...string) ([]byte, error) {
	return exec.Command(name, args...).Output()
}

func (r *RealRunner) RunWithStdin(stdin string, name string, args ...string) ([]byte, error) {
	cmd := exec.Command(name, args...)
	cmd.Stdin = strings.NewReader(stdin)
	return cmd.Output()
}

func main() {
	contextFile := flag.String("context", "logs/inertia-context-2026-02-22.json", "Path to context JSON")
	dryRun := flag.Bool("dry-run", false, "Don't execute td commands, just show decisions")
	maxConcurrency := flag.Int("concurrency", 10, "Max concurrent LLM calls")
	flag.Parse()

	log.Printf("Inertia Engine starting...")
	log.Printf("Context file: %s", *contextFile)
	log.Printf("Dry run: %v", *dryRun)
	log.Printf("Max concurrency: %d", *maxConcurrency)

	// Load context from phase 1
	context, err := loadContext(*contextFile)
	if err != nil {
		log.Fatalf("Failed to load context: %v", err)
	}
	log.Printf("Loaded context for date: %s", context.Date)

	// Fetch all tasks from Todoist
	tasks, err := fetchAllTasks()
	if err != nil {
		log.Fatalf("Failed to fetch tasks: %v", err)
	}
	log.Printf("Fetched %d total tasks", len(tasks))

	// Filter for leaf nodes
	leafTasks := filterLeafNodes(tasks)
	log.Printf("Filtered to %d leaf node tasks", len(leafTasks))

	// Process tasks in parallel with bounded concurrency
	decisions := processTasksParallel(leafTasks, context, *maxConcurrency)
	log.Printf("Generated %d decisions", len(decisions))

	// Log decisions
	for _, d := range decisions {
		log.Printf("Task %s: %s (score: %.2f) - %s", d.TaskID, d.Action, d.InertiaScore, d.Reasoning)
	}

	if *dryRun {
		log.Printf("Dry run mode - not executing commands")
		return
	}

	// Execute td commands in parallel
	executeDecisionsParallel(decisions)
	log.Printf("Inertia Engine complete!")
}

func loadContext(path string) (*InertiaContext, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read file: %w", err)
	}

	var ctx InertiaContext
	if err := json.Unmarshal(data, &ctx); err != nil {
		return nil, fmt.Errorf("unmarshal: %w", err)
	}

	return &ctx, nil
}

func fetchAllTasks() ([]Task, error) {
	output, err := commandRunner.Output("td", "task", "list", "--json", "--full")
	if err != nil {
		return nil, fmt.Errorf("td command: %w", err)
	}

	var resp TasksResponse
	if err := json.Unmarshal(output, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal tasks: %w", err)
	}

	return resp.Results, nil
}

func filterLeafNodes(tasks []Task) []Task {
	// Build set of parent IDs
	parentIDs := make(map[string]bool)
	for _, task := range tasks {
		if task.ParentID != nil {
			parentIDs[*task.ParentID] = true
		}
	}

	// Keep tasks that aren't parents of other tasks
	var leafTasks []Task
	for _, task := range tasks {
		if !parentIDs[task.ID] {
			leafTasks = append(leafTasks, task)
		}
	}

	return leafTasks
}

func processTasksParallel(tasks []Task, context *InertiaContext, maxConcurrency int) []Decision {
	results := make(chan Decision, len(tasks))
	sem := make(chan struct{}, maxConcurrency)
	var wg sync.WaitGroup

	for _, task := range tasks {
		wg.Add(1)
		sem <- struct{}{} // Acquire semaphore

		go func(t Task) {
			defer wg.Done()
			defer func() { <-sem }() // Release semaphore

			decision := processTask(t, context)
			results <- decision
		}(task)
	}

	// Close results channel when all goroutines complete
	go func() {
		wg.Wait()
		close(results)
	}()

	// Collect all decisions
	var decisions []Decision
	for decision := range results {
		decisions = append(decisions, decision)
	}

	return decisions
}

func processTask(task Task, context *InertiaContext) Decision {
	// Build task context by matching to gazetteer
	taskCtx := contextualizeTask(task, context)

	// Call LLM agent for reasoning
	decision := callAgentForDecision(taskCtx)

	return decision
}

func contextualizeTask(task Task, context *InertiaContext) TaskContext {
	taskText := strings.ToLower(task.Content + " " + task.Description)

	// Find related entities by keyword matching
	var relatedPeople []Entity
	for _, person := range context.Gazetteer.People {
		if strings.Contains(taskText, strings.ToLower(person.Name)) {
			relatedPeople = append(relatedPeople, person)
		}
	}

	var relatedProjects []Entity
	for _, project := range context.Gazetteer.Projects {
		if strings.Contains(taskText, strings.ToLower(project.Name)) {
			relatedProjects = append(relatedProjects, project)
		}
	}

	var relatedConcepts []Entity
	for _, concept := range context.Gazetteer.Concepts {
		keywords := strings.Split(strings.ToLower(concept.Name), " ")
		for _, kw := range keywords {
			if strings.Contains(taskText, kw) {
				relatedConcepts = append(relatedConcepts, concept)
				break
			}
		}
	}

	// Calculate age in days
	ageDays := int(nowFunc().Sub(task.AddedAt).Hours() / 24)

	// Calculate historical weight (max span_years from related concepts)
	var maxSpan float64
	for _, concept := range relatedConcepts {
		years := concept.GetSpanYears()
		if years > maxSpan {
			maxSpan = years
		}
	}

	return TaskContext{
		Task:             task,
		RelatedPeople:    relatedPeople,
		RelatedProjects:  relatedProjects,
		RelatedConcepts:  relatedConcepts,
		State:            context.State,
		AgeDays:          ageDays,
		HistoricalWeight: maxSpan,
	}
}

func callAgentForDecision(taskCtx TaskContext) Decision {
	// Build prompt for LLM
	prompt := buildDecisionPrompt(taskCtx)

	// Call openclaw chat with prompt via stdin
	output, err := commandRunner.RunWithStdin(prompt, "openclaw", "chat")
	if err != nil {
		log.Printf("LLM call failed for task %s: %v", taskCtx.Task.ID, err)
		return Decision{
			TaskID: taskCtx.Task.ID,
			Action: "skip",
			Reasoning: fmt.Sprintf("LLM call failed: %v", err),
		}
	}

	// Parse LLM response into Decision
	decision := parseDecisionResponse(string(output), taskCtx.Task.ID)
	return decision
}

func buildDecisionPrompt(taskCtx TaskContext) string {
	var sb strings.Builder
	
	sb.WriteString(fmt.Sprintf("Task: %s\n", taskCtx.Task.Content))
	sb.WriteString(fmt.Sprintf("Created: %d days ago\n", taskCtx.AgeDays))
	sb.WriteString(fmt.Sprintf("Current priority: p%d\n\n", taskCtx.Task.Priority))

	sb.WriteString("Current state:\n")
	sb.WriteString(fmt.Sprintf("- Energy: %s\n", taskCtx.State.Energy))
	sb.WriteString(fmt.Sprintf("- Mood: %s\n", taskCtx.State.Mood))
	sb.WriteString(fmt.Sprintf("- Environment: %s\n\n", taskCtx.State.Environment))

	if len(taskCtx.RelatedConcepts) > 0 {
		sb.WriteString("Related concepts from diary history:\n")
		for _, c := range taskCtx.RelatedConcepts {
			sb.WriteString(fmt.Sprintf("- %s (%.0f years): %s\n", c.Name, c.GetSpanYears(), c.Context))
		}
		sb.WriteString("\n")
	}

	if len(taskCtx.RelatedProjects) > 0 {
		sb.WriteString("Related projects:\n")
		for _, p := range taskCtx.RelatedProjects {
			sb.WriteString(fmt.Sprintf("- %s: %s\n", p.Name, p.Context))
		}
		sb.WriteString("\n")
	}

	sb.WriteString(`Based on this context, decide ONE action for this task:
1. "skip" - no action needed
2. "decompose" - break into subtasks (if >14 days old and stale)
3. "ice-box" - move to ice-box (if >30 days old and low historical alignment)
4. "reprioritize" - change priority based on inertia score
5. "recontextualize" - rewrite task to be more atomic/specific

Respond with JSON only:
{
  "action": "skip|decompose|ice-box|reprioritize|recontextualize",
  "priority": 1-4 (if reprioritizing),
  "new_content": "..." (if recontextualizing),
  "subtasks": ["...", "..."] (if decomposing),
  "reasoning": "brief explanation",
  "inertia_score": 0-10 (historical_weight * 0.4 + state_alignment * 0.3 + environment * 0.3)
}`)

	return sb.String()
}

func parseDecisionResponse(response string, taskID string) Decision {
	// Extract JSON from response (LLM might add text before/after)
	start := strings.Index(response, "{")
	end := strings.LastIndex(response, "}")
	
	if start == -1 || end == -1 {
		return Decision{
			TaskID: taskID,
			Action: "skip",
			Reasoning: "Failed to parse LLM response",
		}
	}

	jsonStr := response[start:end+1]
	
	var result struct {
		Action       string   `json:"action"`
		Priority     *int     `json:"priority"`
		NewContent   *string  `json:"new_content"`
		Subtasks     []string `json:"subtasks"`
		Reasoning    string   `json:"reasoning"`
		InertiaScore float64  `json:"inertia_score"`
	}

	if err := json.Unmarshal([]byte(jsonStr), &result); err != nil {
		return Decision{
			TaskID: taskID,
			Action: "skip",
			Reasoning: fmt.Sprintf("JSON parse error: %v", err),
		}
	}

	return Decision{
		TaskID:       taskID,
		Action:       result.Action,
		Priority:     result.Priority,
		NewContent:   result.NewContent,
		Subtasks:     result.Subtasks,
		Reasoning:    result.Reasoning,
		InertiaScore: result.InertiaScore,
	}
}

func executeDecisionsParallel(decisions []Decision) {
	var wg sync.WaitGroup

	for _, decision := range decisions {
		wg.Add(1)
		go func(d Decision) {
			defer wg.Done()
			executeDecision(d)
		}(decision)
	}

	wg.Wait()
}

func executeDecision(decision Decision) {
	switch decision.Action {
	case "skip":
		// No action needed
		return

	case "reprioritize":
		if decision.Priority != nil {
			if err := commandRunner.Run("td", "task", "update", decision.TaskID, "--priority", fmt.Sprintf("p%d", *decision.Priority)); err != nil {
				log.Printf("Failed to reprioritize task %s: %v", decision.TaskID, err)
			}
		}

	case "recontextualize":
		if decision.NewContent != nil {
			if err := commandRunner.Run("td", "task", "update", decision.TaskID, "--content", *decision.NewContent); err != nil {
				log.Printf("Failed to recontextualize task %s: %v", decision.TaskID, err)
			}
		}

	case "decompose":
		for _, subtask := range decision.Subtasks {
			if err := commandRunner.Run("td", "task", "add", subtask, "--parent", decision.TaskID); err != nil {
				log.Printf("Failed to add subtask to %s: %v", decision.TaskID, err)
			}
		}

	case "ice-box":
		// Move to ice-box project (would need project ID lookup)
		log.Printf("Ice-boxing task %s (implement project move)", decision.TaskID)
	}
}

