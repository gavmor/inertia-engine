package engine

import (
	"encoding/json"
	"os"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

type MockRunner struct {
	CalledCommands [][]string
	Outputs        map[string][]byte
	Errors         map[string]error
	StdinSent      string
}

func (m *MockRunner) Run(name string, args ...string) error {
	m.CalledCommands = append(m.CalledCommands, append([]string{name}, args...))
	return m.Errors[name]
}

func (m *MockRunner) Output(name string, args ...string) ([]byte, error) {
	m.CalledCommands = append(m.CalledCommands, append([]string{name}, args...))
	return m.Outputs[name], m.Errors[name]
}

func (m *MockRunner) RunWithStdin(stdin string, name string, args ...string) ([]byte, error) {
	m.StdinSent = stdin
	m.CalledCommands = append(m.CalledCommands, append([]string{name}, args...))
	return m.Outputs[name], m.Errors[name]
}

var _ = Describe("Inertia Engine Orchestrator", func() {
	var mock *MockRunner

	BeforeEach(func() {
		mock = &MockRunner{
			Outputs: make(map[string][]byte),
			Errors:  make(map[string]error),
		}
		CommandRunner = mock
		NowFunc = func() time.Time {
			t, _ := time.Parse(time.RFC3339, "2026-02-24T12:00:00Z")
			return t
		}
	})

	Describe("Phase 2: Task Processing Lifecycle", func() {
		Context("when starting the orchestration", func() {
			It("should load the context JSON artifact generated in Phase 1", func() {
				tmpFile, err := os.CreateTemp("", "context-*.json")
				Expect(err).NotTo(HaveOccurred())
				defer os.Remove(tmpFile.Name())

				ctx := InertiaContext{
					Date: "2026-02-22",
					Gazetteer: Gazetteer{
						Concepts: []Entity{{Name: "Journaling", Context: "Daily habit"}},
					},
				}
				data, _ := json.Marshal(ctx)
				os.WriteFile(tmpFile.Name(), data, 0644)

				loaded, err := LoadContext(tmpFile.Name())
				Expect(err).NotTo(HaveOccurred())
				Expect(loaded.Date).To(Equal("2026-02-22"))
				Expect(loaded.Gazetteer.Concepts).To(HaveLen(1))
				Expect(loaded.Gazetteer.Concepts[0].Name).To(Equal("Journaling"))
			})

			It("should fetch all active tasks using the 'td' CLI", func() {
				resp := TasksResponse{
					Results: []Task{{ID: "1", Content: "Test Task"}},
				}
				data, _ := json.Marshal(resp)
				mock.Outputs["td"] = data

				tasks, err := FetchAllTasks()
				Expect(err).NotTo(HaveOccurred())
				Expect(tasks).To(HaveLen(1))
				Expect(mock.CalledCommands).To(ContainElement([]string{"td", "task", "list", "--json", "--full"}))
			})
		})

		Context("when preparing tasks for processing", func() {
			It("should filter for leaf nodes to prevent redundant updates to parent tasks", func() {
				parentID := "p1"
				tasks := []Task{
					{ID: "p1", Content: "Parent"},
					{ID: "c1", Content: "Child", ParentID: &parentID},
					{ID: "l1", Content: "Lone Task"},
				}

				leafTasks := FilterLeafNodes(tasks)
				Expect(leafTasks).To(HaveLen(2))
				
				ids := []string{leafTasks[0].ID, leafTasks[1].ID}
				Expect(ids).To(ContainElements("c1", "l1"))
				Expect(ids).ToNot(ContainElement("p1"))
			})

			It("should identify related concepts in the gazetteer via keyword matching", func() {
				ctx := &InertiaContext{
					Gazetteer: Gazetteer{
						Concepts: []Entity{
							{Name: "Journaling", Context: "10 years"},
							{Name: "Coding", Context: "5 years"},
						},
					},
				}
				task := Task{Content: "Finish journaling entry", Description: "Use the new app"}

				taskCtx := ContextualizeTask(task, ctx)
				Expect(taskCtx.RelatedConcepts).To(HaveLen(1))
				Expect(taskCtx.RelatedConcepts[0].Name).To(Equal("Journaling"))
			})
		})
	})

	Describe("Inertia Scoring Algorithm", func() {
		Context("Historical Weight (40%)", func() {
			var ctx *InertiaContext
			BeforeEach(func() {
				ctx = &InertiaContext{
					Gazetteer: Gazetteer{
						Concepts: []Entity{
							{Name: "LongTerm", SpanYears: json.RawMessage(`10`)},
							{Name: "MidTerm", SpanYears: json.RawMessage(`5`)},
							{Name: "ShortTerm", SpanYears: json.RawMessage(`0.4`)},
						},
					},
				}
			})

			It("should award 10 points for commitments spanning 10+ years", func() {
				task := Task{Content: "A LongTerm task"}
				taskCtx := ContextualizeTask(task, ctx)
				Expect(taskCtx.HistoricalWeight).To(BeNumerically("==", 10))
			})
			It("should award 5 points for commitments spanning 5 years", func() {
				task := Task{Content: "A MidTerm task"}
				taskCtx := ContextualizeTask(task, ctx)
				Expect(taskCtx.HistoricalWeight).To(BeNumerically("==", 5))
			})
			It("should award less than 1 point for commitments spanning less than 6 months (0.4 years)", func() {
				task := Task{Content: "A ShortTerm task"}
				taskCtx := ContextualizeTask(task, ctx)
				Expect(taskCtx.HistoricalWeight).To(BeNumerically("==", 0.4))
			})
		})

		Context("State Alignment (30%)", func() {
			It("should include state markers in the prompt for LLM alignment", func() {
				taskCtx := TaskContext{
					Task:  Task{Content: "Creative Task"},
					State: State{Energy: "high", Mood: "inspired", Environment: "home"},
				}
				prompt := BuildDecisionPrompt(taskCtx)
				Expect(prompt).To(ContainSubstring("Energy: high"))
				Expect(prompt).To(ContainSubstring("Mood: inspired"))
				Expect(prompt).To(ContainSubstring("Environment: home"))
			})
		})
	})

	Describe("Decision Logic & Actions", func() {
		Context("when the LLM determines an action", func() {
			It("should parse 'skip' if no changes are required", func() {
				resp := `{"action": "skip", "reasoning": "all good"}`
				decision := ParseDecisionResponse(resp, "123")
				Expect(decision.Action).To(Equal("skip"))
			})

			It("should parse 'decompose' with subtasks", func() {
				resp := `Some chatter {"action": "decompose", "subtasks": ["step 1", "step 2"], "reasoning": "too big"}`
				decision := ParseDecisionResponse(resp, "123")
				Expect(decision.Action).To(Equal("decompose"))
				Expect(decision.Subtasks).To(ConsistOf("step 1", "step 2"))
			})

			It("should parse 'reprioritize' with new priority", func() {
				resp := `{"action": "reprioritize", "priority": 1, "reasoning": "urgent"}`
				decision := ParseDecisionResponse(resp, "123")
				Expect(decision.Action).To(Equal("reprioritize"))
				Expect(*decision.Priority).To(Equal(1))
			})
		})
	})

	Describe("Concurrency & Execution Safety", func() {
		It("should execute 'td' update commands correctly", func() {
			priority := 2
			decision := Decision{
				TaskID:   "123",
				Action:   "reprioritize",
				Priority: &priority,
			}
			ExecuteDecision(decision)
			Expect(mock.CalledCommands).To(ContainElement([]string{"td", "task", "update", "123", "--priority", "p2"}))
		})

		It("should handle decomposition by adding subtasks", func() {
			decision := Decision{
				TaskID:   "123",
				Action:   "decompose",
				Subtasks: []string{"sub 1"},
			}
			ExecuteDecision(decision)
			Expect(mock.CalledCommands).To(ContainElement([]string{"td", "task", "add", "sub 1", "--parent", "123"}))
		})
	})
})
