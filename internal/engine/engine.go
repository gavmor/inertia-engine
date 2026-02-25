package engine

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/gavmor/inertia-engine/internal/runner"
)

// Global variables for mocking in tests
var (
	CommandRunner runner.CommandRunner = &runner.RealRunner{}
	NowFunc                            = time.Now
)

type InertiaContext struct {
	Date       string     `json:"date"`
	Gazetteer  Gazetteer  `json:"gazetteer"`
	State      State      `json:"state"`
	Intentions Intentions `json:"intentions"`
}

type Gazetteer struct {
	People   []Entity `json:"people"`
	Projects []Entity `json:"projects"`
	Places   []Entity `json:"places"`
	Concepts []Entity `json:"concepts"`
}

type Entity struct {
	Name             string          `json:"name"`
	Context          string          `json:"context"`
	Sources          []string        `json:"sources"`
	SpanYears        json.RawMessage `json:"span_years,omitempty"`
	Status           string          `json:"status,omitempty"`
	Note             string          `json:"note,omitempty"`
	EmotionalValence string          `json:"emotional_valence,omitempty"`
}

func (e *Entity) GetSpanYears() float64 {
	if len(e.SpanYears) == 0 {
		return 0
	}
	var num float64
	if err := json.Unmarshal(e.SpanYears, &num); err == nil {
		return num
	}
	return 0
}

type State struct {
	Energy         string `json:"energy"`
	Mood           string `json:"mood"`
	Environment    string `json:"environment"`
	WorkVolatility string `json:"work_volatility"`
}

type Intentions struct {
	Explicit []string `json:"explicit"`
	Implicit []string `json:"implicit"`
}

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

type Decision struct {
	TaskID       string
	Action       string
	Priority     *int
	NewContent   *string
	Subtasks     []string
	Reasoning    string
	InertiaScore float64
}

type TaskContext struct {
	Task             Task
	RelatedPeople    []Entity
	RelatedProjects  []Entity
	RelatedConcepts  []Entity
	State            State
	AgeDays          int
	HistoricalWeight float64
}

func LoadContext(path string) (*InertiaContext, error) {
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

func FetchAllTasks() ([]Task, error) {
	output, err := CommandRunner.Output("td", "task", "list", "--json", "--full")
	if err != nil {
		return nil, fmt.Errorf("td command: %w", err)
	}
	var resp TasksResponse
	if err := json.Unmarshal(output, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal tasks: %w", err)
	}
	return resp.Results, nil
}

func FilterLeafNodes(tasks []Task) []Task {
	parentIDs := make(map[string]bool)
	for _, task := range tasks {
		if task.ParentID != nil {
			parentIDs[*task.ParentID] = true
		}
	}
	var leafTasks []Task
	for _, task := range tasks {
		if !parentIDs[task.ID] {
			leafTasks = append(leafTasks, task)
		}
	}
	return leafTasks
}

func ProcessTasksParallel(tasks []Task, context *InertiaContext, maxConcurrency int) []Decision {
	results := make(chan Decision, len(tasks))
	sem := make(chan struct{}, maxConcurrency)
	var wg sync.WaitGroup

	for _, task := range tasks {
		wg.Add(1)
		sem <- struct{}{}
		go func(t Task) {
			defer wg.Done()
			defer func() { <-sem }()
			decision := ProcessTask(t, context)
			results <- decision
		}(task)
	}

	go func() {
		wg.Wait()
		close(results)
	}()

	var decisions []Decision
	for decision := range results {
		decisions = append(decisions, decision)
	}
	return decisions
}

func ProcessTask(task Task, context *InertiaContext) Decision {
	taskCtx := ContextualizeTask(task, context)
	return CallAgentForDecision(taskCtx)
}

func ContextualizeTask(task Task, context *InertiaContext) TaskContext {
	taskText := strings.ToLower(task.Content + " " + task.Description)
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

	ageDays := int(NowFunc().Sub(task.AddedAt).Hours() / 24)
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

func CallAgentForDecision(taskCtx TaskContext) Decision {
	prompt := BuildDecisionPrompt(taskCtx)
	output, err := CommandRunner.RunWithStdin(prompt, "openclaw", "chat")
	if err != nil {
		log.Printf("LLM call failed for task %s: %v", taskCtx.Task.ID, err)
		return Decision{
			TaskID:    taskCtx.Task.ID,
			Action:    "skip",
			Reasoning: fmt.Sprintf("LLM call failed: %v", err),
		}
	}
	return ParseDecisionResponse(string(output), taskCtx.Task.ID)
}

func BuildDecisionPrompt(taskCtx TaskContext) string {
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

	sb.WriteString("Based on this context, decide ONE action for this task:\n")
	sb.WriteString("1. \"skip\" - no action needed\n")
	sb.WriteString("2. \"decompose\" - break into subtasks (if >14 days old and stale)\n")
	sb.WriteString("3. \"ice-box\" - move to ice-box (if >30 days old and low historical alignment)\n")
	sb.WriteString("4. \"reprioritize\" - change priority based on inertia score\n")
	sb.WriteString("5. \"recontextualize\" - rewrite task to be more atomic/specific\n\n")
	sb.WriteString("Respond with JSON only:\n")
	sb.WriteString("{\n")
	sb.WriteString("  \"action\": \"skip|decompose|ice-box|reprioritize|recontextualize\",\n")
	sb.WriteString("  \"priority\": 1-4 (if reprioritizing),\n")
	sb.WriteString("  \"new_content\": \"...\" (if recontextualizing),\n")
	sb.WriteString("  \"subtasks\": [\"...\", \"...\"], (if decomposing),\n")
	sb.WriteString("  \"reasoning\": \"brief explanation\",\n")
	sb.WriteString("  \"inertia_score\": 0-10 (historical_weight * 0.4 + state_alignment * 0.3 + environment * 0.3)\n")
	sb.WriteString("}")
	return sb.String()
}

func ParseDecisionResponse(response string, taskID string) Decision {
	start := strings.Index(response, "{")
	end := strings.LastIndex(response, "}")
	if start == -1 || end == -1 {
		return Decision{TaskID: taskID, Action: "skip", Reasoning: "Failed to parse LLM response"}
	}
	jsonStr := response[start : end+1]
	var result struct {
		Action       string   `json:"action"`
		Priority     *int     `json:"priority"`
		NewContent   *string  `json:"new_content"`
		Subtasks     []string `json:"subtasks"`
		Reasoning    string   `json:"reasoning"`
		InertiaScore float64  `json:"inertia_score"`
	}
	if err := json.Unmarshal([]byte(jsonStr), &result); err != nil {
		return Decision{TaskID: taskID, Action: "skip", Reasoning: fmt.Sprintf("JSON parse error: %v", err)}
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

func ExecuteDecisionsParallel(decisions []Decision) {
	var wg sync.WaitGroup
	for _, decision := range decisions {
		wg.Add(1)
		go func(d Decision) {
			defer wg.Done()
			ExecuteDecision(d)
		}(decision)
	}
	wg.Wait()
}

func ExecuteDecision(decision Decision) {
	switch decision.Action {
	case "skip":
		return
	case "reprioritize":
		if decision.Priority != nil {
			if err := CommandRunner.Run("td", "task", "update", decision.TaskID, "--priority", fmt.Sprintf("p%d", *decision.Priority)); err != nil {
				log.Printf("Failed to reprioritize task %s: %v", decision.TaskID, err)
			}
		}
	case "recontextualize":
		if decision.NewContent != nil {
			if err := CommandRunner.Run("td", "task", "update", decision.TaskID, "--content", *decision.NewContent); err != nil {
				log.Printf("Failed to recontextualize task %s: %v", decision.TaskID, err)
			}
		}
	case "decompose":
		for _, subtask := range decision.Subtasks {
			if err := CommandRunner.Run("td", "task", "add", subtask, "--parent", decision.TaskID); err != nil {
				log.Printf("Failed to add subtask to %s: %v", decision.TaskID, err)
			}
		}
	case "ice-box":
		log.Printf("Ice-boxing task %s (implement project move)", decision.TaskID)
	}
}
