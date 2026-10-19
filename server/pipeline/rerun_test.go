// Copyright 2026 Woodpecker Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package pipeline

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"go.woodpecker-ci.org/woodpecker/v3/pipeline/frontend/builder"
	"go.woodpecker-ci.org/woodpecker/v3/server"
	"go.woodpecker-ci.org/woodpecker/v3/server/model"
	log_mocks "go.woodpecker-ci.org/woodpecker/v3/server/services/log/mocks"
	store_mocks "go.woodpecker-ci.org/woodpecker/v3/server/store/mocks"
)

func wf(name string, state model.StatusValue, dependsOn ...string) *model.Workflow {
	return &model.Workflow{Name: name, State: state, DependsOn: dependsOn}
}

func names(run map[workflowKey]bool) []string {
	var out []string
	for k, ok := range run {
		if ok {
			out = append(out, k.name)
		}
	}
	return out
}

func TestSelectRerun(t *testing.T) {
	t.Parallel()

	// lint -> build -> deploy, and e2e (failed) -> report
	old := []*model.Workflow{
		wf("lint", model.StatusSuccess),
		wf("build", model.StatusSuccess, "lint"),
		wf("deploy", model.StatusSuccess, "build"),
		wf("e2e", model.StatusFailure),
		wf("report", model.StatusSkipped, "e2e"),
	}
	next := []*model.Workflow{
		wf("lint", model.StatusPending),
		wf("build", model.StatusPending, "lint"),
		wf("deploy", model.StatusPending, "build"),
		wf("e2e", model.StatusPending),
		wf("report", model.StatusPending, "e2e"),
	}

	t.Run("failed reruns failures and their dependents", func(t *testing.T) {
		t.Parallel()
		run, err := selectRerun(old, next, RestartOptions{Rerun: []string{RerunFailed}})
		require.NoError(t, err)
		assert.ElementsMatch(t, []string{"e2e", "report"}, names(run))
	})

	t.Run("named workflow reruns with its dependents only", func(t *testing.T) {
		t.Parallel()
		run, err := selectRerun(old, next, RestartOptions{Rerun: []string{"build"}})
		require.NoError(t, err)
		assert.ElementsMatch(t, []string{"build", "deploy"}, names(run))
	})

	t.Run("unknown workflow is rejected", func(t *testing.T) {
		t.Parallel()
		_, err := selectRerun(old, next, RestartOptions{Rerun: []string{"nope"}})
		var badRequest *ErrBadRequest
		assert.ErrorAs(t, err, &badRequest)
	})

	t.Run("failed on a green pipeline is rejected", func(t *testing.T) {
		t.Parallel()
		green := []*model.Workflow{wf("lint", model.StatusSuccess), wf("build", model.StatusSuccess)}
		_, err := selectRerun(green, green, RestartOptions{Rerun: []string{RerunFailed}})
		var badRequest *ErrBadRequest
		assert.ErrorAs(t, err, &badRequest)
	})

	t.Run("workflow added since the last run always runs", func(t *testing.T) {
		t.Parallel()
		withNew := append(append([]*model.Workflow{}, next...), wf("new", model.StatusPending))
		run, err := selectRerun(old, withNew, RestartOptions{Rerun: []string{RerunFailed}})
		require.NoError(t, err)
		assert.ElementsMatch(t, []string{"e2e", "report", "new"}, names(run))
	})

	t.Run("matrix axes are told apart", func(t *testing.T) {
		t.Parallel()
		oldAxes := []*model.Workflow{
			{Name: "test", AxisID: 1, State: model.StatusSuccess},
			{Name: "test", AxisID: 2, State: model.StatusFailure},
		}
		nextAxes := []*model.Workflow{{Name: "test", AxisID: 1}, {Name: "test", AxisID: 2}}
		run, err := selectRerun(oldAxes, nextAxes, RestartOptions{Rerun: []string{RerunFailed}})
		require.NoError(t, err)
		assert.Equal(t, map[workflowKey]bool{{"test", 2}: true}, run)
	})
}

func TestReusePreviousResults(t *testing.T) {
	oldBuild := &model.Workflow{
		Name: "build", State: model.StatusSuccess, Started: 10, Finished: 20, AgentID: 7,
		Children: []*model.Step{{ID: 101, Name: "compile", State: model.StatusSuccess, ExitCode: 0, Started: 11, Finished: 19}},
	}
	oldE2E := &model.Workflow{
		Name: "e2e", State: model.StatusFailure,
		Children: []*model.Step{{ID: 102, Name: "playwright", State: model.StatusFailure, ExitCode: 1}},
	}
	newBuild := &model.Workflow{
		ID: 2, Name: "build", State: model.StatusPending,
		Children: []*model.Step{{ID: 201, Name: "compile", State: model.StatusPending}},
	}
	newE2E := &model.Workflow{
		ID: 3, Name: "e2e", State: model.StatusPending,
		Children: []*model.Step{{ID: 202, Name: "playwright", State: model.StatusPending}},
	}
	lastPipeline := &model.Pipeline{ID: 1, Number: 1}
	newPipeline := &model.Pipeline{ID: 2, Number: 2, Workflows: []*model.Workflow{newBuild, newE2E}}
	items := []*builder.Item{
		{Workflow: &builder.Workflow{ID: 2, Name: "build"}},
		{Workflow: &builder.Workflow{ID: 3, Name: "e2e"}},
	}

	s := store_mocks.NewMockStore(t)
	s.On("WorkflowGetTree", lastPipeline).Return([]*model.Workflow{oldBuild, oldE2E}, nil)
	s.On("WorkflowUpdate", newBuild).Return(nil)
	s.On("StepUpdate", newBuild.Children[0]).Return(nil)

	logs := log_mocks.NewMockService(t)
	logs.On("LogFind", oldBuild.Children[0]).Return([]*model.LogEntry{{ID: 9, StepID: 101, Line: 0, Data: []byte("ok")}}, nil)
	logs.On("LogAppend", newBuild.Children[0], mock.MatchedBy(func(entries []*model.LogEntry) bool {
		return len(entries) == 1 && entries[0].StepID == 201 && entries[0].ID == 0
	})).Return(nil)
	server.Config.Services.LogStore = logs

	queued, err := reusePreviousResults(s, lastPipeline, newPipeline, items, RestartOptions{Rerun: []string{RerunFailed}})
	require.NoError(t, err)

	require.Len(t, queued, 1)
	assert.Equal(t, "e2e", queued[0].Workflow.Name)
	assert.Equal(t, model.StatusSuccess, newBuild.State)
	assert.Equal(t, int64(7), newBuild.AgentID)
	assert.Equal(t, model.StatusSuccess, newBuild.Children[0].State)
	assert.Equal(t, int64(19), newBuild.Children[0].Finished)
	assert.Equal(t, model.StatusPending, newE2E.State)
}
