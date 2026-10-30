package chain

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"plasmod/src/internal/schemas"
	"plasmod/src/internal/worker/nodes"
)

func TestMainChainProjectionOrderAndFailure(t *testing.T) {
	c := CreateMainChain(nodes.CreateManager())
	var stages []string
	err := c.Project(context.Background(), func() error { stages = append(stages, "canonical"); return nil }, func() error { stages = append(stages, "memory"); return errors.New("governance unavailable") }, func() error { stages = append(stages, "index"); return nil })
	if err == nil || !reflect.DeepEqual(stages, []string{"canonical", "memory"}) {
		t.Fatalf("visibility advanced past failure: %v %v", stages, err)
	}
}
func TestMemoryChainMissingWorkerIsError(t *testing.T) {
	_, result := CreateMemoryPipelineChain(nodes.CreateManager()).RunAlgorithm(schemas.AlgorithmDispatchInput{Operation: "decay"}, nil)
	if result.OK || result.Error == "" {
		t.Fatal("unavailable algorithm reported success")
	}
}
func TestCollaborationChainPropagatesShareFailure(t *testing.T) {
	c := CreateCollaborationChain(nodes.CreateManager())
	_, err := c.Share("a", "b", "mem", "contract", func(a, b, id, contract string) (string, error) {
		if contract != "contract" {
			t.Fatal("lost policy contract")
		}
		return "", errors.New("denied")
	})
	if err == nil {
		t.Fatal("share error swallowed")
	}
}
