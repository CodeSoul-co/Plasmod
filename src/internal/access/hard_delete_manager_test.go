package access

import "testing"

func TestHardDeleteManager_EnqueueRejectsEmptyMemoryIDs(t *testing.T) {
	mgr := &hardDeleteManager{
		tasks: map[string]*hardDeleteTask{},
	}
	task := &hardDeleteTask{
		TaskID:      "task_empty",
		WorkspaceID: "w_test",
		MemoryIDs:   nil,
		State:       hardDeleteStateQueued,
	}
	if ok := mgr.enqueue(task); ok {
		t.Fatal("expected enqueue to reject empty memory ids")
	}
}

func TestHardDeleteManager_GetActiveByIdempotencyKey(t *testing.T) {
	mgr := &hardDeleteManager{
		tasks: map[string]*hardDeleteTask{
			"t1": {
				TaskID:         "t1",
				WorkspaceID:    "w_test",
				MemoryIDs:      []string{"mem1"},
				State:          hardDeleteStateQueued,
				IdempotencyKey: "idem_key",
			},
			"t2": {
				TaskID:         "t2",
				WorkspaceID:    "w_test",
				MemoryIDs:      []string{"mem2"},
				State:          hardDeleteStateCompleted,
				IdempotencyKey: "idem_done",
			},
		},
	}

	task, ok := mgr.getActiveByIdempotencyKey("idem_key")
	if !ok {
		t.Fatal("expected active task for idempotency key")
	}
	if task.TaskID != "t1" {
		t.Fatalf("expected task t1, got %s", task.TaskID)
	}

	if _, ok := mgr.getActiveByIdempotencyKey("idem_done"); ok {
		t.Fatal("completed task should not be treated as active")
	}
}
