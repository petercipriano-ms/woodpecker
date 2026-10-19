// Copyright 2026 Woodpecker Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package pipeline

import (
	"fmt"
	"slices"

	"github.com/rs/zerolog/log"

	"go.woodpecker-ci.org/woodpecker/v3/pipeline/frontend/builder"
	"go.woodpecker-ci.org/woodpecker/v3/server"
	"go.woodpecker-ci.org/woodpecker/v3/server/model"
	"go.woodpecker-ci.org/woodpecker/v3/server/store"
)

// RerunFailed is the RestartOptions.Rerun value that reruns every workflow that did not succeed.
const RerunFailed = "failed"

// RestartOptions selects which workflows of the restarted pipeline run again.
// Empty Rerun restarts everything; otherwise the selected workflows and all their dependents run,
// and every other workflow keeps its previous result and logs.
type RestartOptions struct {
	// Rerun holds RerunFailed, or the names of the workflows to run again.
	Rerun []string
}

func (o RestartOptions) partial() bool { return len(o.Rerun) > 0 }

type workflowKey struct {
	name string
	axis int
}

func keyOf(w *model.Workflow) workflowKey { return workflowKey{w.Name, w.AxisID} }

// selectRerun returns the keys of the new workflows that must run, or an error if the selection is empty or unknown.
func selectRerun(oldWorkflows, newWorkflows []*model.Workflow, opts RestartOptions) (map[workflowKey]bool, error) {
	old := make(map[workflowKey]*model.Workflow, len(oldWorkflows))
	for _, w := range oldWorkflows {
		old[keyOf(w)] = w
	}

	run := map[workflowKey]bool{}
	failedOnly := slices.Contains(opts.Rerun, RerunFailed)
	for _, w := range newWorkflows {
		prev, ok := old[keyOf(w)]
		switch {
		case !ok:
			// New in this config, so there is no result to keep.
			run[keyOf(w)] = true
		case failedOnly && prev.State != model.StatusSuccess && prev.State != model.StatusSkipped:
			run[keyOf(w)] = true
		case slices.Contains(opts.Rerun, w.Name):
			run[keyOf(w)] = true
		}
	}

	if !failedOnly {
		for _, name := range opts.Rerun {
			if !slices.ContainsFunc(newWorkflows, func(w *model.Workflow) bool { return w.Name == name }) {
				return nil, &ErrBadRequest{Msg: fmt.Sprintf("workflow %q is not part of this pipeline", name)}
			}
		}
	}
	if len(run) == 0 {
		return nil, &ErrBadRequest{Msg: "no failed workflows to rerun"}
	}

	// A dependent consumed the output of a workflow that runs again, so it runs again too.
	for changed := true; changed; {
		changed = false
		for _, w := range newWorkflows {
			if run[keyOf(w)] {
				continue
			}
			if slices.ContainsFunc(newWorkflows, func(dep *model.Workflow) bool {
				return run[keyOf(dep)] && slices.Contains(w.DependsOn, dep.Name)
			}) {
				run[keyOf(w)] = true
				changed = true
			}
		}
	}

	return run, nil
}

// reusePreviousResults copies the previous result and logs into every workflow that does not rerun,
// and returns only the pipeline items that still have to be queued.
func reusePreviousResults(_store store.Store, lastPipeline, newPipeline *model.Pipeline, items []*builder.Item, opts RestartOptions) ([]*builder.Item, error) {
	oldWorkflows, err := _store.WorkflowGetTree(lastPipeline)
	if err != nil {
		return nil, fmt.Errorf("failure to load workflows of pipeline %d: %w", lastPipeline.Number, err)
	}
	run, err := selectRerun(oldWorkflows, newPipeline.Workflows, opts)
	if err != nil {
		return nil, err
	}

	old := make(map[workflowKey]*model.Workflow, len(oldWorkflows))
	for _, w := range oldWorkflows {
		old[keyOf(w)] = w
	}
	for _, w := range newPipeline.Workflows {
		if run[keyOf(w)] {
			continue
		}
		if err := copyWorkflowResult(_store, old[keyOf(w)], w); err != nil {
			return nil, err
		}
	}

	return slices.DeleteFunc(items, func(item *builder.Item) bool {
		return !run[workflowKey{item.Workflow.Name, item.Workflow.AxisID}]
	}), nil
}

func copyWorkflowResult(_store store.Store, from, to *model.Workflow) error {
	to.State = from.State
	to.Error = from.Error
	to.Started = from.Started
	to.Finished = from.Finished
	to.AgentID = from.AgentID
	to.Platform = from.Platform
	if err := _store.WorkflowUpdate(to); err != nil {
		return fmt.Errorf("failure to update workflow %s: %w", to.Name, err)
	}

	for _, step := range to.Children {
		idx := slices.IndexFunc(from.Children, func(s *model.Step) bool { return s.Name == step.Name })
		if idx < 0 {
			// The step did not exist last time; it cannot have a result to keep.
			step.State = model.StatusSkipped
		} else {
			prev := from.Children[idx]
			step.State, step.Error, step.ExitCode = prev.State, prev.Error, prev.ExitCode
			step.Started, step.Finished = prev.Started, prev.Finished
			copyStepLogs(prev, step)
		}
		if err := _store.StepUpdate(step); err != nil {
			return fmt.Errorf("failure to update step %s: %w", step.Name, err)
		}
	}
	return nil
}

// copyStepLogs is best effort: a missing log never blocks the rerun.
func copyStepLogs(from, to *model.Step) {
	logStore := server.Config.Services.LogStore
	entries, err := logStore.LogFind(from)
	if err != nil || len(entries) == 0 {
		return
	}
	for _, e := range entries {
		e.ID = 0
		e.StepID = to.ID
	}
	if err := logStore.LogAppend(to, entries); err != nil {
		log.Warn().Err(err).Msgf("could not copy logs of step %s", to.Name)
	}
}
