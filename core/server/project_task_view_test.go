package server

import (
	"context"
	"testing"

	"github.com/suakitsu/picpilot-platform/core/store"
)

func TestProjectTaskStatusContract(t *testing.T) {
	s := &Server{}
	for _, kind := range []string{"creative", "main", "aplus"} {
		for _, state := range []struct{ internal, public string }{
			{"succeeded", "success"},
			{"cancelled", "failed"},
			{"queued", "queued"},
			{"running", "running"},
			{"failed", "failed"},
			{"polling", "running"},
			{"waiting_worker", "running"},
		} {
			t.Run(kind+"/"+state.internal, func(t *testing.T) {
				task := store.Task{Kind: kind, Capability: "image", Status: state.internal}
				if got := s.projectTaskView(context.Background(), task)["status"]; got != state.public {
					t.Fatalf("project status = %v, want %s", got, state.public)
				}
				if got := s.taskView(context.Background(), task)["status"]; got != state.internal {
					t.Fatalf("generic status = %v, want %s", got, state.internal)
				}
			})
		}
	}
}
