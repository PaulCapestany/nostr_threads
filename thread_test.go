package main

import (
	"testing"
)

func TestPlaceMessageInThread_NewThread(t *testing.T) {
	// Setup
	bucket, err := setupTestBucket(testCluster)
	if err != nil {
		t.Fatalf("Failed to setup test bucket: %v", err)
	}

	message := Message{
		Content:   "This is a test message",
		CreatedAt: 1622547800,
		ID:        "test_message_1",
		Kind:      1,
		Pubkey:    "pubkey_1",
		Sig:       "signature_1",
		Tags:      []interface{}{},
	}

	// Test
	err = placeMessageInThread(message.ID, message, testCluster)
	if err != nil {
		t.Fatalf("Failed to place message in thread: %v", err)
	}

	// Verify
	collection := bucket.DefaultCollection()
	getResult, err := collection.Get(message.ID, nil)
	if err != nil {
		t.Fatalf("Failed to get thread from bucket: %v", err)
	}

	var thread Thread
	err = getResult.Content(&thread)
	if err != nil {
		t.Fatalf("Failed to decode thread content: %v", err)
	}

	if len(thread.Messages) != 1 || thread.Messages[0].ID != message.ID {
		t.Fatalf("Thread content does not match expected: %+v", thread)
	}

	// Cleanup
	cleanupTestData(bucket, []string{message.ID})
}

func TestPlaceMessageInThread_ExistingThread(t *testing.T) {
	// Setup
	bucket, err := setupTestBucket(testCluster)
	if err != nil {
		t.Fatalf("Failed to setup test bucket: %v", err)
	}

	parentMessage := Message{
		Content:   "This is the parent message",
		CreatedAt: 1622547800,
		ID:        "parent_message_1",
		Kind:      1,
		Pubkey:    "pubkey_1",
		Sig:       "signature_1",
		Tags:      []interface{}{},
	}

	thread := Thread{
		CreatedAt: parentMessage.CreatedAt,
		ID:        parentMessage.ID,
		Kind:      parentMessage.Kind,
		Pubkey:    parentMessage.Pubkey,
		Messages:  []Message{parentMessage},
	}

	collection := bucket.DefaultCollection()
	_, err = collection.Upsert(parentMessage.ID, thread, nil)
	if err != nil {
		t.Fatalf("Failed to insert parent thread into bucket: %v", err)
	}

	childMessage := Message{
		Content:   "This is a child message",
		CreatedAt: 1622547801,
		ID:        "child_message_1",
		Kind:      1,
		Pubkey:    "pubkey_2",
		Sig:       "signature_2",
		Tags:      []interface{}{},
		ParentID:  parentMessage.ID,
	}

	// Test
	err = placeMessageInThread(childMessage.ID, childMessage, testCluster)
	if err != nil {
		t.Fatalf("Failed to place child message in thread: %v", err)
	}

	// Verify
	getResult, err := collection.Get(parentMessage.ID, nil)
	if err != nil {
		t.Fatalf("Failed to get parent thread from bucket: %v", err)
	}

	var updatedThread Thread
	err = getResult.Content(&updatedThread)
	if err != nil {
		t.Fatalf("Failed to decode updated thread content: %v", err)
	}

	if len(updatedThread.Messages) != 2 || updatedThread.Messages[1].ID != childMessage.ID {
		t.Fatalf("Updated thread content does not match expected: %+v", updatedThread)
	}

	// Cleanup
	cleanupTestData(bucket, []string{parentMessage.ID, childMessage.ID})
}
