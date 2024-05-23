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
	messageIDsToQuery := []string{message.ID}
	var allUniqueThreadMessages []Message
	messageFetcher(messageIDsToQuery, &allUniqueThreadMessages, testCluster)

	threadedProcessedMessages, err := processMessageThreading(allUniqueThreadMessages)
	if err != nil {
		t.Fatalf("Failed to process message threading: %v", err)
	}

	if len(threadedProcessedMessages) != 1 || threadedProcessedMessages[0].ID != message.ID {
		t.Fatalf("Thread content does not match expected: %+v", threadedProcessedMessages)
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
		Tags: []interface{}{
			[]interface{}{"e", parentMessage.ID},
		},
	}

	// Test
	messageIDsToQuery := []string{childMessage.ID}
	var allUniqueThreadMessages []Message
	messageFetcher(messageIDsToQuery, &allUniqueThreadMessages, testCluster)

	threadedProcessedMessages, err := processMessageThreading(allUniqueThreadMessages)
	if err != nil {
		t.Fatalf("Failed to process message threading: %v", err)
	}

	if len(threadedProcessedMessages) != 2 || threadedProcessedMessages[1].ID != childMessage.ID {
		t.Fatalf("Thread content does not match expected: %+v", threadedProcessedMessages)
	}

	// Cleanup
	cleanupTestData(bucket, []string{parentMessage.ID, childMessage.ID})
}
